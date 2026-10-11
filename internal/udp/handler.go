package udp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/logging"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// formatIPAddress returns the address type and byte representation for the given IP.
func formatIPAddress(ip net.IP) (addrType uint8, addr []byte) {
	if ip4 := ip.To4(); ip4 != nil {
		return protocol.AddrTypeIPv4, ip4
	}
	return protocol.AddrTypeIPv6, ip.To16()
}

// datagramResolveTimeout bounds the DNS lookup for domain-addressed
// datagrams. HandleUDPDatagram runs on a peer connection's fast-lane worker,
// so an unbounded lookup would park one of the few drainers. It is a var so
// tests can shrink it.
var datagramResolveTimeout = 5 * time.Second

// lookupIP is injectable for tests.
var lookupIP = func(ctx context.Context, domain string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", domain)
}

// resolveDatagramAddress resolves the destination address from a UDP datagram.
func resolveDatagramAddress(datagram *protocol.UDPDatagram) (*net.UDPAddr, error) {
	port := int(datagram.Port)

	switch datagram.AddressType {
	case protocol.AddrTypeIPv4, protocol.AddrTypeIPv6:
		return &net.UDPAddr{IP: net.IP(datagram.Address), Port: port}, nil

	case protocol.AddrTypeDomain:
		if len(datagram.Address) < 2 {
			return nil, fmt.Errorf("invalid domain address")
		}
		domain := string(datagram.Address[1:])
		ctx, cancel := context.WithTimeout(context.Background(), datagramResolveTimeout)
		defer cancel()
		ips, err := lookupIP(ctx, domain)
		if err != nil {
			return nil, fmt.Errorf("DNS lookup failed: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses for domain")
		}
		return &net.UDPAddr{IP: ips[0], Port: port}, nil

	default:
		return nil, fmt.Errorf("unknown address type: %d", datagram.AddressType)
	}
}

// DataWriter is the interface for sending frames back to the mesh.
type DataWriter interface {
	// WriteUDPDatagram sends a UDP datagram frame to the specified peer.
	WriteUDPDatagram(peerID identity.AgentID, streamID uint64, datagram *protocol.UDPDatagram) error

	// WriteUDPClose sends a UDP close frame to the specified peer.
	WriteUDPClose(peerID identity.AgentID, streamID uint64, reason uint8) error

	// WriteUDPOpenAck sends a UDP open acknowledgment to the specified peer.
	WriteUDPOpenAck(peerID identity.AgentID, streamID uint64, ack *protocol.UDPOpenAck) error

	// WriteUDPOpenErr sends a UDP open error to the specified peer.
	WriteUDPOpenErr(peerID identity.AgentID, streamID uint64, err *protocol.UDPOpenErr) error
}

// streamKey identifies an association by its peer as well as its stream ID.
// Stream IDs are allocated per peer connection, so the ID alone is not unique
// across peers: two agents relaying UDP through this exit both open stream 1.
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// Handler manages UDP associations for exit nodes.
type Handler struct {
	mu           sync.RWMutex
	associations map[streamKey]*Association // by (peer, StreamID)

	config Config
	writer DataWriter
	logger *slog.Logger

	// Cleanup
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewHandler creates a new UDP handler.
func NewHandler(cfg Config, writer DataWriter, logger *slog.Logger) *Handler {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Handler{
		associations: make(map[streamKey]*Association),
		config:       cfg,
		writer:       writer,
		logger:       logger.With(slog.String("component", "udp")),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start cleanup goroutine if timeout is configured
	if cfg.IdleTimeout > 0 {
		h.wg.Add(1)
		go h.cleanupLoop()
	}

	return h
}

// HandleUDPOpen processes a UDP_OPEN frame at the exit node.
// Creates a UDP socket and establishes the association.
func (h *Handler) HandleUDPOpen(
	ctx context.Context,
	peerID identity.AgentID,
	streamID uint64,
	open *protocol.UDPOpen,
	remoteEphemeralPub [protocol.EphemeralKeySize]byte,
) error {
	h.logger.Debug("exit handler received UDP_OPEN",
		slog.Uint64("stream_id", streamID),
		slog.String("from_peer", peerID.ShortString()),
		slog.Uint64("request_id", open.RequestID))

	// Check if UDP is enabled
	if !h.config.Enabled {
		h.writer.WriteUDPOpenErr(peerID, streamID, &protocol.UDPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrUDPDisabled,
			Message:   "UDP relay is disabled",
		})
		return fmt.Errorf("UDP relay is disabled")
	}

	// Check association limit
	h.mu.RLock()
	count := len(h.associations)
	h.mu.RUnlock()

	if h.config.MaxAssociations > 0 && count >= h.config.MaxAssociations {
		h.writer.WriteUDPOpenErr(peerID, streamID, &protocol.UDPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrResourceLimit,
			Message:   "UDP association limit reached",
		})
		return fmt.Errorf("association limit reached")
	}

	// Create association
	assoc := NewAssociation(streamID, open.RequestID, peerID)

	// Create UDP socket
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		h.writer.WriteUDPOpenErr(peerID, streamID, &protocol.UDPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrGeneralFailure,
			Message:   "failed to create UDP socket",
		})
		return fmt.Errorf("create UDP socket: %w", err)
	}

	assoc.SetUDPConn(udpConn)

	// Perform E2E key exchange if remote provided an ephemeral key
	var ephPub [protocol.EphemeralKeySize]byte
	var zeroKey [protocol.EphemeralKeySize]byte
	hasEncryption := remoteEphemeralPub != zeroKey

	if hasEncryption {
		var keyExchangeErr error
		ephPub, keyExchangeErr = h.performKeyExchange(assoc, open, remoteEphemeralPub, udpConn)
		if keyExchangeErr != nil {
			return keyExchangeErr
		}
	} else {
		h.logger.Warn("UDP_OPEN without ephemeral key, encryption disabled",
			logging.KeyStreamID, streamID)
	}

	// Register association
	h.mu.Lock()
	h.associations[streamKey{peer: peerID, streamID: streamID}] = assoc
	h.mu.Unlock()

	// Build and send UDP_OPEN_ACK
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	addrType, boundAddr := formatIPAddress(localAddr.IP)
	ack := &protocol.UDPOpenAck{
		RequestID:     open.RequestID,
		BoundAddrType: addrType,
		BoundAddr:     boundAddr,
		BoundPort:     uint16(localAddr.Port),
	}
	if hasEncryption {
		ack.EphemeralPubKey = ephPub
	}

	if err := h.writer.WriteUDPOpenAck(peerID, streamID, ack); err != nil {
		h.removeAssociation(streamKey{peer: peerID, streamID: streamID})
		return fmt.Errorf("send UDP_OPEN_ACK: %w", err)
	}

	assoc.SetOpen()

	// Start read loop
	h.wg.Add(1)
	go h.readLoop(assoc)

	if hasEncryption {
		h.logger.Info("UDP association established",
			logging.KeyStreamID, streamID,
			logging.KeyRequestID, open.RequestID,
			"local_addr", localAddr.String())
	}

	return nil
}

// performKeyExchange generates an ephemeral keypair and derives the session key.
// Returns the public key for inclusion in the ack, or an error.
func (h *Handler) performKeyExchange(
	assoc *Association,
	open *protocol.UDPOpen,
	remoteEphemeralPub [protocol.EphemeralKeySize]byte,
	udpConn *net.UDPConn,
) ([protocol.EphemeralKeySize]byte, error) {
	var zeroKey [protocol.EphemeralKeySize]byte

	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		udpConn.Close()
		h.writer.WriteUDPOpenErr(assoc.PeerID, assoc.StreamID, &protocol.UDPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrGeneralFailure,
			Message:   "failed to generate ephemeral key",
		})
		return zeroKey, fmt.Errorf("generate ephemeral key: %w", err)
	}

	sharedSecret, err := crypto.ComputeECDH(ephPriv, remoteEphemeralPub)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		udpConn.Close()
		h.writer.WriteUDPOpenErr(assoc.PeerID, assoc.StreamID, &protocol.UDPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrGeneralFailure,
			Message:   "key exchange failed",
		})
		return zeroKey, fmt.Errorf("ECDH: %w", err)
	}

	crypto.ZeroKey(&ephPriv)

	// Derive session key (we are responder)
	sessionKey := crypto.DeriveSessionKey(sharedSecret, open.RequestID, remoteEphemeralPub, ephPub, false)
	crypto.ZeroKey(&sharedSecret)

	assoc.SetSessionKey(sessionKey)

	return ephPub, nil
}

// HandleUDPDatagram processes a UDP_DATAGRAM frame.
// Decrypts and forwards the datagram to the destination.
func (h *Handler) HandleUDPDatagram(peerID identity.AgentID, streamID uint64, datagram *protocol.UDPDatagram) error {
	h.mu.RLock()
	assoc := h.associations[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if assoc == nil {
		return fmt.Errorf("unknown stream ID: %d", streamID)
	}

	if assoc.IsClosed() {
		return fmt.Errorf("association closed")
	}

	assoc.UpdateActivity()

	// Decrypt payload
	plaintext, err := assoc.Decrypt(datagram.Data)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// Check size
	if len(plaintext) > h.config.MaxDatagramSize {
		return fmt.Errorf("datagram too large: %d > %d", len(plaintext), h.config.MaxDatagramSize)
	}

	// Resolve destination address
	destAddr, err := resolveDatagramAddress(datagram)
	if err != nil {
		return err
	}

	// Send to destination
	assoc.mu.RLock()
	conn := assoc.UDPConn
	assoc.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("UDP connection closed")
	}

	_, err = conn.WriteToUDP(plaintext, destAddr)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}

	return nil
}

// HandleUDPClose processes a UDP_CLOSE frame.
func (h *Handler) HandleUDPClose(peerID identity.AgentID, streamID uint64) error {
	h.mu.RLock()
	assoc := h.associations[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if assoc == nil {
		return nil // Already closed
	}

	h.removeAssociation(streamKey{peer: peerID, streamID: streamID})
	return nil
}

// GetAssociation returns an association by peer and stream ID.
func (h *Handler) GetAssociation(peerID identity.AgentID, streamID uint64) *Association {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.associations[streamKey{peer: peerID, streamID: streamID}]
}

// CloseAssociationsForPeer closes every association opened by peerID and
// returns how many were closed. Called when the peer disconnects: relayed
// datagrams have nowhere to go now, and leaving associations to age out holds
// UDP sockets open while the peer's stream IDs restart at 1 on reconnect.
func (h *Handler) CloseAssociationsForPeer(peerID identity.AgentID) int {
	h.mu.Lock()
	var closing []*Association
	for key, assoc := range h.associations {
		if key.peer != peerID {
			continue
		}
		delete(h.associations, key)
		closing = append(closing, assoc)
	}
	h.mu.Unlock()

	// Close outside the lock: Close touches the socket and the read loop
	// wakes up and re-enters removeAssociation, which takes h.mu.
	for _, assoc := range closing {
		assoc.Close()
	}
	return len(closing)
}

// ActiveCount returns the number of active associations.
func (h *Handler) ActiveCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.associations)
}

// Close shuts down the handler and all associations.
func (h *Handler) Close() error {
	h.cancel()

	// Close all associations
	h.mu.Lock()
	for _, assoc := range h.associations {
		assoc.Close()
	}
	h.associations = make(map[streamKey]*Association)
	h.mu.Unlock()

	// Wait for goroutines
	h.wg.Wait()

	return nil
}

// removeAssociation removes an association and cleans up resources.
func (h *Handler) removeAssociation(key streamKey) {
	h.mu.Lock()
	assoc := h.associations[key]
	if assoc != nil {
		delete(h.associations, key)
	}
	h.mu.Unlock()

	if assoc != nil {
		assoc.Close()
	}
}

// readLoop reads datagrams from the UDP socket and sends them back through the mesh.
func (h *Handler) readLoop(assoc *Association) {
	defer h.wg.Done()

	buf := make([]byte, h.config.MaxDatagramSize+128) // Extra for encryption overhead

	for {
		select {
		case <-assoc.Context().Done():
			return
		case <-h.ctx.Done():
			return
		default:
		}

		assoc.mu.RLock()
		conn := assoc.UDPConn
		assoc.mu.RUnlock()

		if conn == nil {
			return
		}

		// Set read deadline for responsiveness to cancellation
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // Timeout, check for cancellation
			}
			if assoc.IsClosed() {
				return
			}
			continue
		}

		assoc.UpdateActivity()

		// Encrypt payload
		plaintext := buf[:n]
		ciphertext, err := assoc.Encrypt(plaintext)
		if err != nil {
			continue
		}

		// Build datagram
		addrType, addr := formatIPAddress(remoteAddr.IP)
		datagram := &protocol.UDPDatagram{
			AddressType: addrType,
			Address:     addr,
			Port:        uint16(remoteAddr.Port),
			Data:        ciphertext,
		}

		// Send back through mesh
		h.writer.WriteUDPDatagram(assoc.PeerID, assoc.StreamID, datagram)
	}
}

// cleanupLoop periodically removes expired associations.
func (h *Handler) cleanupLoop() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.config.IdleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.cleanupExpired()
		}
	}
}

// cleanupExpired removes associations that have exceeded the idle timeout.
func (h *Handler) cleanupExpired() {
	h.mu.RLock()
	var expired []streamKey
	for key, assoc := range h.associations {
		if assoc.IsExpired(h.config.IdleTimeout) {
			expired = append(expired, key)
		}
	}
	h.mu.RUnlock()

	for _, key := range expired {
		h.mu.RLock()
		assoc := h.associations[key]
		h.mu.RUnlock()

		if assoc != nil {
			// Send close to peer
			h.writer.WriteUDPClose(assoc.PeerID, key.streamID, protocol.UDPCloseTimeout)
			h.removeAssociation(key)
		}
	}
}
