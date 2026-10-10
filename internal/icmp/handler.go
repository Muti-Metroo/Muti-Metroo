package icmp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// DataWriter is the interface for sending frames back to the mesh.
type DataWriter interface {
	// WriteICMPOpenAck sends an ICMP open acknowledgment to the specified peer.
	WriteICMPOpenAck(peerID identity.AgentID, streamID uint64, ack *protocol.ICMPOpenAck) error

	// WriteICMPOpenErr sends an ICMP open error to the specified peer.
	WriteICMPOpenErr(peerID identity.AgentID, streamID uint64, err *protocol.ICMPOpenErr) error

	// WriteICMPEcho sends an ICMP echo frame to the specified peer.
	WriteICMPEcho(peerID identity.AgentID, streamID uint64, echo *protocol.ICMPEcho) error

	// WriteICMPClose sends an ICMP close frame to the specified peer.
	WriteICMPClose(peerID identity.AgentID, streamID uint64, reason uint8) error
}

// streamKey identifies a session by its peer as well as its stream ID. Stream
// IDs are allocated per peer connection, so the ID alone is not unique across
// peers: two agents pinging through this exit both open stream 1.
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// Handler manages ICMP echo sessions for exit nodes.
type Handler struct {
	mu       sync.RWMutex
	sessions map[streamKey]*Session // by (peer, StreamID)

	config Config
	writer DataWriter
	logger *slog.Logger

	// Cleanup
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Semaphore for limiting concurrent reply goroutines
	replySem chan struct{}
}

// NewHandler creates a new ICMP handler.
func NewHandler(cfg Config, writer DataWriter, logger *slog.Logger) *Handler {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Handler{
		sessions: make(map[streamKey]*Session),
		config:   cfg,
		writer:   writer,
		logger:   logger.With(slog.String("component", "icmp")),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Initialize reply semaphore if limit is configured
	if cfg.MaxConcurrentReplies > 0 {
		h.replySem = make(chan struct{}, cfg.MaxConcurrentReplies)
	}

	// Start cleanup goroutine if timeout is configured
	if cfg.IdleTimeout > 0 {
		h.wg.Add(1)
		go h.cleanupLoop()
	}

	return h
}

// HandleICMPOpen processes an ICMP_OPEN frame at the exit node.
// Creates an ICMP socket and establishes the session.
func (h *Handler) HandleICMPOpen(
	ctx context.Context,
	peerID identity.AgentID,
	streamID uint64,
	open *protocol.ICMPOpen,
	remoteEphemeralPub [protocol.EphemeralKeySize]byte,
) error {
	destIP := open.GetDestinationIP()

	h.logger.Debug("exit handler received ICMP_OPEN",
		slog.Uint64("stream_id", streamID),
		slog.String("from_peer", peerID.ShortString()),
		slog.Uint64("request_id", open.RequestID),
		slog.String("dest_ip", destIP.String()))

	// Check if ICMP is enabled
	if !h.config.Enabled {
		h.writer.WriteICMPOpenErr(peerID, streamID, &protocol.ICMPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrICMPDisabled,
			Message:   "ICMP echo is disabled",
		})
		return fmt.Errorf("ICMP echo is disabled")
	}

	// Check session limit
	h.mu.RLock()
	count := len(h.sessions)
	h.mu.RUnlock()

	if h.config.MaxSessions > 0 && count >= h.config.MaxSessions {
		h.writer.WriteICMPOpenErr(peerID, streamID, &protocol.ICMPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrICMPSessionLimit,
			Message:   "ICMP session limit reached",
		})
		return fmt.Errorf("session limit reached")
	}

	// Create session
	session := NewSession(streamID, open.RequestID, peerID, destIP)

	// Create ICMP socket for the appropriate IP version
	sock, err := NewSocket(destIP)
	if err != nil {
		h.writer.WriteICMPOpenErr(peerID, streamID, &protocol.ICMPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrGeneralFailure,
			Message:   "failed to create ICMP socket",
		})
		return fmt.Errorf("create ICMP socket: %w", err)
	}

	session.SetSocket(sock)

	// Perform E2E key exchange if remote provided an ephemeral key
	var ephPub [protocol.EphemeralKeySize]byte
	var zeroKey [protocol.EphemeralKeySize]byte
	hasEncryption := remoteEphemeralPub != zeroKey

	if hasEncryption {
		var keyExchangeErr error
		ephPub, keyExchangeErr = h.performKeyExchange(session, open, remoteEphemeralPub, sock)
		if keyExchangeErr != nil {
			return keyExchangeErr
		}
	} else {
		h.logger.Warn("ICMP_OPEN without ephemeral key, encryption disabled",
			logging.KeyStreamID, streamID)
	}

	// Register session
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: streamID}] = session
	h.mu.Unlock()

	// Build and send ICMP_OPEN_ACK
	ack := &protocol.ICMPOpenAck{
		RequestID: open.RequestID,
	}
	if hasEncryption {
		ack.EphemeralPubKey = ephPub
	}

	if err := h.writer.WriteICMPOpenAck(peerID, streamID, ack); err != nil {
		h.removeSession(streamKey{peer: peerID, streamID: streamID})
		return fmt.Errorf("send ICMP_OPEN_ACK: %w", err)
	}

	session.SetOpen()

	if hasEncryption {
		h.logger.Info("ICMP session established",
			logging.KeyStreamID, streamID,
			logging.KeyRequestID, open.RequestID,
			"dest_ip", destIP.String())
	}

	return nil
}

// performKeyExchange generates an ephemeral keypair and derives the session key.
// Returns the public key for inclusion in the ack, or an error.
func (h *Handler) performKeyExchange(
	session *Session,
	open *protocol.ICMPOpen,
	remoteEphemeralPub [protocol.EphemeralKeySize]byte,
	conn interface{ Close() error },
) ([protocol.EphemeralKeySize]byte, error) {
	var zeroKey [protocol.EphemeralKeySize]byte

	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		conn.Close()
		h.writer.WriteICMPOpenErr(session.PeerID, session.StreamID, &protocol.ICMPOpenErr{
			RequestID: open.RequestID,
			ErrorCode: protocol.ErrGeneralFailure,
			Message:   "failed to generate ephemeral key",
		})
		return zeroKey, fmt.Errorf("generate ephemeral key: %w", err)
	}

	sharedSecret, err := crypto.ComputeECDH(ephPriv, remoteEphemeralPub)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		conn.Close()
		h.writer.WriteICMPOpenErr(session.PeerID, session.StreamID, &protocol.ICMPOpenErr{
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

	session.SetSessionKey(sessionKey)

	return ephPub, nil
}

// HandleICMPEcho processes an ICMP_ECHO frame.
// Decrypts the payload, sends the echo request, waits for reply, and returns.
func (h *Handler) HandleICMPEcho(peerID identity.AgentID, streamID uint64, echo *protocol.ICMPEcho) error {
	h.mu.RLock()
	session := h.sessions[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if session == nil {
		return fmt.Errorf("unknown stream ID: %d", streamID)
	}

	if session.IsClosed() {
		return fmt.Errorf("session closed")
	}

	session.UpdateActivity()

	// If this is a reply (shouldn't happen from mesh), ignore
	if echo.IsReply {
		return nil
	}

	// Decrypt payload
	plaintext, err := session.Decrypt(echo.Data)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	sock := session.GetSocket()
	if sock == nil {
		return fmt.Errorf("ICMP socket closed")
	}

	// Send echo request
	if err := sock.SendEchoRequest(session.DestIP, echo.Identifier, echo.Sequence, plaintext); err != nil {
		return fmt.Errorf("send echo: %w", err)
	}

	// Wait for reply
	timeout := h.config.EchoTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Start goroutine to read reply and send back
	go h.waitForReply(session, echo.Identifier, echo.Sequence, timeout)

	return nil
}

// waitForReply waits for an ICMP echo reply and sends it back through the mesh.
func (h *Handler) waitForReply(session *Session, identifier, sequence uint16, timeout time.Duration) {
	// Acquire semaphore if configured
	if h.replySem != nil {
		select {
		case h.replySem <- struct{}{}:
			// Acquired, release when done
			defer func() { <-h.replySem }()
		case <-h.ctx.Done():
			return
		}
	}

	sock := session.GetSocket()
	if sock == nil {
		return
	}

	reply, err := sock.ReadEchoReplyFiltered(identifier, timeout)
	if err != nil {
		// Timeout or error, don't send anything
		h.logger.Debug("ICMP reply timeout or error",
			logging.KeyStreamID, session.StreamID,
			"identifier", identifier,
			"sequence", sequence,
			"error", err)
		return
	}

	// Encrypt reply payload
	ciphertext, err := session.Encrypt(reply.Payload)
	if err != nil {
		h.logger.Error("failed to encrypt ICMP reply",
			logging.KeyStreamID, session.StreamID,
			"error", err)
		return
	}

	// Resolve the local outbound IP (the address this agent uses to reach the destination).
	// reply.SrcIP is the responder address which equals the destination -- not useful.
	localIP := resolveLocalIP(session.DestIP)

	// Send reply back through mesh
	echoReply := &protocol.ICMPEcho{
		Identifier: reply.ID,
		Sequence:   reply.Seq,
		IsReply:    true,
		SrcIP:      localIP,
		Data:       ciphertext,
	}

	if err := h.writer.WriteICMPEcho(session.PeerID, session.StreamID, echoReply); err != nil {
		h.logger.Error("failed to send ICMP reply",
			logging.KeyStreamID, session.StreamID,
			"error", err)
	}
}

// HandleICMPClose processes an ICMP_CLOSE frame.
func (h *Handler) HandleICMPClose(peerID identity.AgentID, streamID uint64) error {
	h.mu.RLock()
	session := h.sessions[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if session == nil {
		return nil // Already closed
	}

	h.removeSession(streamKey{peer: peerID, streamID: streamID})
	return nil
}

// GetSession returns a session by peer and stream ID.
func (h *Handler) GetSession(peerID identity.AgentID, streamID uint64) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.sessions[streamKey{peer: peerID, streamID: streamID}]
}

// CloseSessionsForPeer closes every session opened by peerID and returns how
// many were closed. Called when the peer disconnects: echo replies have nowhere
// to go now, and leaving sessions to age out holds sockets open while the
// peer's stream IDs restart at 1 on reconnect.
func (h *Handler) CloseSessionsForPeer(peerID identity.AgentID) int {
	h.mu.Lock()
	var closing []*Session
	for key, session := range h.sessions {
		if key.peer != peerID {
			continue
		}
		delete(h.sessions, key)
		closing = append(closing, session)
	}
	h.mu.Unlock()

	// Close outside the lock: Close touches the socket and any waiter
	// re-enters removeSession, which takes h.mu.
	for _, session := range closing {
		session.Close()
	}
	return len(closing)
}

// ActiveCount returns the number of active sessions.
func (h *Handler) ActiveCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.sessions)
}

// Close shuts down the handler and all sessions.
func (h *Handler) Close() error {
	h.cancel()

	// Close all sessions
	h.mu.Lock()
	for _, session := range h.sessions {
		session.Close()
	}
	h.sessions = make(map[streamKey]*Session)
	h.mu.Unlock()

	// Wait for goroutines
	h.wg.Wait()

	return nil
}

// removeSession removes a session and cleans up resources.
func (h *Handler) removeSession(key streamKey) {
	h.mu.Lock()
	session := h.sessions[key]
	if session != nil {
		delete(h.sessions, key)
	}
	h.mu.Unlock()

	if session != nil {
		session.Close()
	}
}

// cleanupLoop periodically removes expired sessions.
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

// cleanupExpired removes sessions that have exceeded the idle timeout.
func (h *Handler) cleanupExpired() {
	h.mu.RLock()
	var expired []streamKey
	for key, session := range h.sessions {
		if session.IsExpired(h.config.IdleTimeout) {
			expired = append(expired, key)
		}
	}
	h.mu.RUnlock()

	for _, key := range expired {
		h.mu.RLock()
		session := h.sessions[key]
		h.mu.RUnlock()

		if session != nil {
			// Send close to peer
			h.writer.WriteICMPClose(session.PeerID, key.streamID, protocol.ICMPCloseTimeout)
			h.removeSession(key)
		}
	}
}

// resolveLocalIP returns the local outbound IP used to reach destIP.
// Uses a UDP dial (no traffic sent) to let the OS select the source address.
func resolveLocalIP(destIP net.IP) net.IP {
	network := "udp4"
	if destIP.To4() == nil {
		network = "udp6"
	}
	conn, err := net.Dial(network, net.JoinHostPort(destIP.String(), "80"))
	if err != nil {
		return nil
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP
	}
	return nil
}
