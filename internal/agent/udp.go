package agent

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/routing"
	"github.com/postalsys/muti-metroo/internal/socks5"
	"github.com/postalsys/muti-metroo/internal/udp"
)

// UDP-related errors
var (
	ErrUDPNoRoute        = errors.New("no UDP route available")
	ErrUDPStreamNotFound = errors.New("UDP stream not found")
)

// udpDestAssociation tracks a single exit-path for one destination route.
// A SOCKS5 UDP association may have multiple of these (one per exit route).
type udpDestAssociation struct {
	StreamID         uint64
	RequestID        uint64
	ExitPeerID       identity.AgentID
	NextHop          identity.AgentID // First hop peer for sending frames
	SessionKey       *crypto.SessionKey
	EphemeralPrivKey [32]byte
	EphemeralPubKey  [32]byte
	PendingOpen      chan struct{}
	OpenErr          error
	LastActivity     time.Time
	OriginKey        string // Route origin agent ID (cache key)
	closeOnce        sync.Once
	mu               sync.RWMutex
}

// udpDestLookup allows reverse lookup from exit stream to parent association.
type udpDestLookup struct {
	Ingress *udpIngressAssociation
	Dest    *udpDestAssociation
}

// udpIngressAssociation tracks a SOCKS5 UDP association that may route to multiple exits.
// This is the ingress-side tracking (SOCKS5 client -> mesh).
type udpIngressAssociation struct {
	BaseStreamID uint64
	SOCKS5Assoc  *socks5.UDPAssociation

	// Per-destination associations keyed by route.OriginAgent.String()
	destAssocs map[string]*udpDestAssociation
	destMu     sync.RWMutex

	idleTimeout time.Duration
	mu          sync.RWMutex
}

// generateUDPRequestID generates a cryptographically random request ID.
// Using crypto/rand prevents session correlation attacks.
func generateUDPRequestID() uint64 {
	var buf [8]byte
	if _, err := cryptorand.Read(buf[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return binary.BigEndian.Uint64(buf[:])
}

// CreateUDPAssociation implements socks5.UDPAssociationHandler.
// Called when a SOCKS5 client requests UDP ASSOCIATE.
// This creates a lazy association container - actual mesh paths are created on first datagram.
func (a *Agent) CreateUDPAssociation(ctx context.Context, clientAddr *net.UDPAddr) (uint64, error) {
	// Verify at least one route exists (sanity check)
	if a.routeMgr.Lookup(net.IPv4zero) == nil {
		return 0, ErrUDPNoRoute
	}

	baseStreamID := a.udpNextBaseID.Add(1)

	assoc := &udpIngressAssociation{
		BaseStreamID: baseStreamID,
		destAssocs:   make(map[string]*udpDestAssociation),
		idleTimeout:  5 * time.Minute,
	}

	a.udpIngressMu.Lock()
	a.udpIngressByBase[baseStreamID] = assoc
	a.udpIngressMu.Unlock()

	a.logger.Debug("UDP association created (lazy)",
		logging.KeyStreamID, baseStreamID)

	return baseStreamID, nil
}

// getOrCreateDestAssociation finds or creates a destination-specific UDP association.
// It performs route lookup based on actual destination IP and creates mesh paths on demand.
func (a *Agent) getOrCreateDestAssociation(
	ctx context.Context,
	ingress *udpIngressAssociation,
	destIP net.IP,
) (*udpDestAssociation, error) {
	// 1. Route lookup
	route := a.routeMgr.Lookup(destIP)
	if route == nil {
		return nil, ErrUDPNoRoute
	}

	originKey := route.OriginAgent.String()

	// 2. Check cache (read lock)
	ingress.destMu.RLock()
	dest := ingress.destAssocs[originKey]
	ingress.destMu.RUnlock()

	if dest != nil {
		// Wait for the association to complete (or fail)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-dest.PendingOpen:
			// Check result
			if dest.OpenErr != nil {
				// Previous attempt failed, need to retry - remove failed entry
				ingress.destMu.Lock()
				// Double-check it's still the same failed dest
				if ingress.destAssocs[originKey] == dest {
					delete(ingress.destAssocs, originKey)
					a.udpIngressMu.Lock()
					delete(a.udpIngressByLocalStream, streamKey{peer: dest.NextHop, streamID: dest.StreamID})
					a.udpIngressMu.Unlock()
				}
				ingress.destMu.Unlock()
				// Fall through to create new
			} else {
				dest.mu.Lock()
				dest.LastActivity = time.Now()
				dest.mu.Unlock()
				return dest, nil
			}
		}
	}

	// 3. Create new (write lock)
	ingress.destMu.Lock()

	// Double-check after acquiring write lock
	if existingDest := ingress.destAssocs[originKey]; existingDest != nil {
		ingress.destMu.Unlock()
		// Another goroutine created it, wait for it
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-existingDest.PendingOpen:
			if existingDest.OpenErr != nil {
				return nil, existingDest.OpenErr
			}
			existingDest.mu.Lock()
			existingDest.LastActivity = time.Now()
			existingDest.mu.Unlock()
			return existingDest, nil
		}
	}

	// 4. Create destination association (releases lock internally while waiting)
	return a.createDestAssociation(ctx, ingress, route, originKey)
}

// createDestAssociation creates a new mesh path to a specific exit for UDP.
// Must be called with ingress.destMu held for writing. Releases lock before returning.
func (a *Agent) createDestAssociation(
	ctx context.Context,
	ingress *udpIngressAssociation,
	route *routing.Route,
	originKey string,
) (*udpDestAssociation, error) {
	// Use route.NextHop for first hop (same as TCP implementation)
	nextHop := route.NextHop
	conn := a.peerMgr.GetPeer(nextHop)
	if conn == nil {
		ingress.destMu.Unlock()
		return nil, fmt.Errorf("no connection to next hop: %s", nextHop.ShortString())
	}

	// Build remaining path: find NextHop in route.Path and take everything after it
	// route.Path is [local, hop1, hop2, ..., origin/target]
	var remainingPath []identity.AgentID
	rPath := route.Path
	for i, id := range rPath {
		if id == nextHop && i+1 < len(rPath) {
			remainingPath = make([]identity.AgentID, len(rPath)-i-1)
			copy(remainingPath, rPath[i+1:])
			break
		}
	}

	streamID := conn.NextStreamID()
	requestID := generateUDPRequestID()

	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		ingress.destMu.Unlock()
		return nil, err
	}

	dest := &udpDestAssociation{
		StreamID:         streamID,
		RequestID:        requestID,
		ExitPeerID:       route.OriginAgent,
		NextHop:          nextHop,
		EphemeralPrivKey: ephPriv,
		EphemeralPubKey:  ephPub,
		PendingOpen:      make(chan struct{}),
		LastActivity:     time.Now(),
		OriginKey:        originKey,
	}

	// Register in maps
	ingress.destAssocs[originKey] = dest

	a.udpIngressMu.Lock()
	a.udpIngressByLocalStream[streamKey{peer: nextHop, streamID: streamID}] = &udpDestLookup{
		Ingress: ingress,
		Dest:    dest,
	}
	a.udpIngressMu.Unlock()

	// Build and send UDP_OPEN
	open := &protocol.UDPOpen{
		RequestID:       requestID,
		AddressType:     protocol.AddrTypeIPv4,
		Address:         net.IPv4zero.To4(),
		Port:            0,
		TTL:             uint8(len(rPath)),
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameUDPOpen,
		StreamID: streamID,
		Payload:  open.Encode(),
	}

	a.logger.Debug("Creating UDP destination association",
		logging.KeyStreamID, streamID,
		"exit", route.OriginAgent.ShortString(),
		"next_hop", nextHop.ShortString(),
		"remaining_path_len", len(remainingPath))

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		// Cleanup
		delete(ingress.destAssocs, originKey)
		a.udpIngressMu.Lock()
		delete(a.udpIngressByLocalStream, streamKey{peer: nextHop, streamID: streamID})
		a.udpIngressMu.Unlock()
		// Close PendingOpen to unblock any waiters
		dest.closePendingOpen(err)
		ingress.destMu.Unlock()
		return nil, err
	}

	// Release lock while waiting for ACK
	ingress.destMu.Unlock()

	select {
	case <-ctx.Done():
		dest.closePendingOpen(ctx.Err())
		return nil, ctx.Err()
	case <-dest.PendingOpen:
		if dest.OpenErr != nil {
			return nil, dest.OpenErr
		}

		// Check if session key was established
		dest.mu.RLock()
		hasSessionKey := dest.SessionKey != nil
		dest.mu.RUnlock()

		if !hasSessionKey {
			a.logger.Debug("UDP destination association opened without E2E encryption",
				logging.KeyStreamID, streamID)
		}

		return dest, nil
	}
}

// closePendingOpen safely closes the PendingOpen channel, optionally setting an error.
// Pass nil for successful completion.
func (d *udpDestAssociation) closePendingOpen(err error) {
	d.closeOnce.Do(func() {
		if err != nil {
			d.mu.Lock()
			d.OpenErr = err
			d.mu.Unlock()
		}
		close(d.PendingOpen)
	})
}

// RelayUDPDatagram implements socks5.UDPAssociationHandler.
// Called when a SOCKS5 client sends a UDP datagram to relay through the mesh.
// Routes based on actual destination IP, creating mesh paths on demand.
func (a *Agent) RelayUDPDatagram(streamID uint64, destAddr net.Addr, destPort uint16, addrType byte, rawAddr []byte, data []byte) error {
	// Get ingress and verify it exists under lock
	a.udpIngressMu.RLock()
	ingress := a.udpIngressByBase[streamID]
	if ingress == nil {
		a.udpIngressMu.RUnlock()
		return ErrUDPStreamNotFound
	}
	a.udpIngressMu.RUnlock()

	// Extract destination IP for routing
	var destIP net.IP
	switch addrType {
	case protocol.AddrTypeIPv4:
		destIP = net.IP(rawAddr)
	case protocol.AddrTypeIPv6:
		destIP = net.IP(rawAddr)
	case protocol.AddrTypeDomain:
		// Resolve at ingress for routing decision: mesh-host registry
		// first (so registered names work without Mutiauk on this
		// host), then system DNS.
		domain := string(rawAddr[1:]) // Skip length byte
		resolveCtx, resolveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		ips, err := a.resolveHost(resolveCtx, domain)
		resolveCancel()
		if err != nil || len(ips) == 0 {
			return fmt.Errorf("DNS lookup failed: %s", domain)
		}
		destIP = ips[0]
	default:
		return fmt.Errorf("unsupported address type: %d", addrType)
	}

	// Get or create destination association
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dest, err := a.getOrCreateDestAssociation(ctx, ingress, destIP)
	if err != nil {
		return err
	}

	// Encrypt and send
	dest.mu.RLock()
	sessionKey := dest.SessionKey
	exitStreamID := dest.StreamID
	nextHop := dest.NextHop
	dest.mu.RUnlock()

	var ciphertext []byte
	if sessionKey != nil {
		ciphertext, err = sessionKey.Encrypt(data)
		if err != nil {
			return err
		}
	} else {
		ciphertext = data
	}

	datagram := &protocol.UDPDatagram{
		AddressType: addrType,
		Address:     rawAddr,
		Port:        destPort,
		Data:        ciphertext,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameUDPDatagram,
		StreamID: exitStreamID,
		Payload:  datagram.Encode(),
	}

	return a.peerMgr.SendToPeer(nextHop, frame)
}

// CloseUDPAssociation implements socks5.UDPAssociationHandler.
// Called when a SOCKS5 UDP association is closed.
func (a *Agent) CloseUDPAssociation(streamID uint64) {
	a.udpIngressMu.Lock()
	ingress := a.udpIngressByBase[streamID]
	if ingress != nil {
		delete(a.udpIngressByBase, streamID)
	}
	a.udpIngressMu.Unlock()

	if ingress == nil {
		return
	}

	// Close all destination associations
	ingress.destMu.Lock()
	for _, dest := range ingress.destAssocs {
		a.closeDestAssociation(dest)

		a.udpIngressMu.Lock()
		delete(a.udpIngressByLocalStream, streamKey{peer: dest.NextHop, streamID: dest.StreamID})
		a.udpIngressMu.Unlock()
	}
	ingress.destAssocs = nil
	ingress.destMu.Unlock()

	a.logger.Debug("UDP association closed",
		logging.KeyStreamID, streamID)
}

// closeDestAssociation sends UDP_CLOSE to the exit and cleans up.
func (a *Agent) closeDestAssociation(dest *udpDestAssociation) {
	closeFrame := &protocol.UDPClose{Reason: protocol.UDPCloseNormal}
	frame := &protocol.Frame{
		Type:     protocol.FrameUDPClose,
		StreamID: dest.StreamID,
		Payload:  closeFrame.Encode(),
	}

	// Use stored NextHop for sending close frame
	a.peerMgr.SendToPeer(dest.NextHop, frame)
}

// IsUDPEnabled implements socks5.UDPAssociationHandler.
// Returns true if this agent can relay UDP traffic.
func (a *Agent) IsUDPEnabled() bool {
	// UDP is enabled for ingress if we have any routes
	// (we can route UDP to exit nodes that have UDP enabled)
	return a.cfg.SOCKS5.Enabled
}

// handleUDPOpen processes a UDP_OPEN frame.
func (a *Agent) handleUDPOpen(peerID identity.AgentID, frame *protocol.Frame) {
	open, err := protocol.DecodeUDPOpen(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode UDP_OPEN frame", "error", err)
		return
	}

	a.logger.Debug("received UDP_OPEN",
		logging.KeyStreamID, frame.StreamID,
		"from_peer", peerID.ShortString(),
		"remaining_path_len", len(open.RemainingPath))

	// Check if we are the exit node (path is empty)
	if len(open.RemainingPath) == 0 {
		// We are the exit node - handle with UDP handler
		if a.udpHandler == nil {
			// Send error back
			a.sendUDPOpenErr(peerID, frame.StreamID, open.RequestID, protocol.ErrUDPDisabled, "UDP relay disabled")
			return
		}

		ctx := context.Background()
		a.udpHandler.HandleUDPOpen(ctx, peerID, frame.StreamID, open, open.EphemeralPubKey)
		return
	}

	// Relay to next hop
	nextHop := open.RemainingPath[0]

	conn := a.peerMgr.GetPeer(nextHop)
	if conn == nil {
		a.sendUDPOpenErr(peerID, frame.StreamID, open.RequestID, protocol.ErrHostUnreachable, "no route to next hop")
		return
	}

	// Generate new downstream stream ID
	downstreamID := conn.NextStreamID()

	// Create relay entry
	relay := &relayEntry{
		UpstreamPeer:   peerID,
		UpstreamID:     frame.StreamID,
		DownstreamPeer: nextHop,
		DownstreamID:   downstreamID,
	}
	a.udpRelay.Insert(relay)

	// Update remaining path
	newPath := open.RemainingPath[1:]

	// Forward with new stream ID
	fwdOpen := &protocol.UDPOpen{
		RequestID:       open.RequestID,
		AddressType:     open.AddressType,
		Address:         open.Address,
		Port:            open.Port,
		TTL:             open.TTL,
		RemainingPath:   newPath,
		EphemeralPubKey: open.EphemeralPubKey,
	}

	fwdFrame := &protocol.Frame{
		Type:     protocol.FrameUDPOpen,
		StreamID: downstreamID,
		Payload:  fwdOpen.Encode(),
	}

	a.logger.Debug("relaying UDP_OPEN to next hop",
		logging.KeyStreamID, downstreamID,
		"next_hop", nextHop.ShortString(),
		"remaining_path_len", len(newPath))

	if err := a.peerMgr.SendToPeer(nextHop, fwdFrame); err != nil {
		a.logger.Debug("failed to relay UDP_OPEN",
			"error", err,
			"next_hop", nextHop.ShortString())

		// Clean up relay entry
		a.udpRelay.Delete(relay)

		a.sendUDPOpenErr(peerID, frame.StreamID, open.RequestID, protocol.ErrConnectionRefused, err.Error())
	}
}

// handleUDPOpenAck processes a UDP_OPEN_ACK frame.
func (a *Agent) handleUDPOpenAck(peerID identity.AgentID, frame *protocol.Frame) {
	a.logger.Debug("received UDP_OPEN_ACK",
		logging.KeyStreamID, frame.StreamID,
		"from_peer", peerID.ShortString())

	// Check if this is a relay response. Relay entries are immutable once
	// inserted, so reading fields after LookupDownstream returns is safe.
	if relay := a.udpRelay.LookupDownstream(peerID, frame.StreamID); relay != nil {
		a.logger.Debug("relaying UDP_OPEN_ACK upstream",
			logging.KeyStreamID, relay.UpstreamID,
			"upstream_peer", relay.UpstreamPeer.ShortString())

		// Forward ACK to upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameUDPOpenAck,
			StreamID: relay.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relay.UpstreamPeer, fwdFrame)
		return
	}

	// Look up via exit stream (new structure)
	a.udpIngressMu.RLock()
	lookup := a.udpIngressByLocalStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	if lookup == nil {
		a.udpIngressMu.RUnlock()
		return
	}
	dest := lookup.Dest
	a.udpIngressMu.RUnlock()

	ack, err := protocol.DecodeUDPOpenAck(frame.Payload)
	if err != nil {
		return
	}

	// Compute session key from the ephemeral keys
	var zeroKey [protocol.EphemeralKeySize]byte
	if ack.EphemeralPubKey != zeroKey {
		// Compute shared secret using our private key and remote public key
		sharedSecret, err := crypto.ComputeECDH(dest.EphemeralPrivKey, ack.EphemeralPubKey)
		if err != nil {
			dest.closePendingOpen(err)
			return
		}

		// Zero out private key immediately
		crypto.ZeroKey(&dest.EphemeralPrivKey)

		// Derive session key (we are initiator, so isResponder=false)
		sessionKey := crypto.DeriveSessionKey(sharedSecret, ack.RequestID, dest.EphemeralPubKey, ack.EphemeralPubKey, true)
		crypto.ZeroKey(&sharedSecret)

		// Store session key
		dest.mu.Lock()
		dest.SessionKey = sessionKey
		dest.mu.Unlock()
	}

	// Mark as successful (nil error)
	dest.closePendingOpen(nil)
}

// handleUDPOpenErr processes a UDP_OPEN_ERR frame.
func (a *Agent) handleUDPOpenErr(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is a relay response - PopDownstream atomically
	// looks the entry up by (peer, stream ID) and removes it.
	if relay := a.udpRelay.PopDownstream(peerID, frame.StreamID); relay != nil {
		// Forward error to upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameUDPOpenErr,
			StreamID: relay.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relay.UpstreamPeer, fwdFrame)
		return
	}

	// Look up via exit stream (new structure)
	a.udpIngressMu.RLock()
	lookup := a.udpIngressByLocalStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	if lookup == nil {
		a.udpIngressMu.RUnlock()
		return
	}
	dest := lookup.Dest
	ingress := lookup.Ingress
	a.udpIngressMu.RUnlock()

	errMsg, err := protocol.DecodeUDPOpenErr(frame.Payload)
	if err != nil {
		return
	}

	dest.closePendingOpen(fmt.Errorf("UDP open failed: %s (code %d)", errMsg.Message, errMsg.ErrorCode))

	// Clean up
	ingress.destMu.Lock()
	delete(ingress.destAssocs, dest.OriginKey)
	ingress.destMu.Unlock()

	a.udpIngressMu.Lock()
	delete(a.udpIngressByLocalStream, streamKey{peer: dest.NextHop, streamID: dest.StreamID})
	a.udpIngressMu.Unlock()
}

// handleUDPDatagram processes a UDP_DATAGRAM frame.
func (a *Agent) handleUDPDatagram(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is for our UDP handler (exit node receiving from mesh)
	if a.udpHandler != nil {
		if assoc := a.udpHandler.GetAssociation(peerID, frame.StreamID); assoc != nil {
			datagram, err := protocol.DecodeUDPDatagram(frame.Payload)
			if err != nil {
				return
			}
			a.udpHandler.HandleUDPDatagram(peerID, frame.StreamID, datagram)
			return
		}
	}

	// Check if this is for ingress via exit stream lookup (new structure)
	// Hold lock while getting lookup and extracting needed references
	a.udpIngressMu.RLock()
	lookup := a.udpIngressByLocalStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	var dest *udpDestAssociation
	var ingress *udpIngressAssociation
	if lookup != nil {
		dest = lookup.Dest
		ingress = lookup.Ingress
	}
	a.udpIngressMu.RUnlock()

	if lookup != nil {
		datagram, err := protocol.DecodeUDPDatagram(frame.Payload)
		if err != nil {
			return
		}

		// Decrypt if we have a session key
		dest.mu.RLock()
		sessionKey := dest.SessionKey
		dest.mu.RUnlock()

		var plaintext []byte
		if sessionKey != nil {
			plaintext, err = sessionKey.Decrypt(datagram.Data)
			if err != nil {
				return
			}
		} else {
			plaintext = datagram.Data
		}

		// Update activity
		dest.mu.Lock()
		dest.LastActivity = time.Now()
		dest.mu.Unlock()

		// Forward to SOCKS5 client
		ingress.mu.RLock()
		socks5Assoc := ingress.SOCKS5Assoc
		ingress.mu.RUnlock()

		if socks5Assoc != nil {
			socks5Assoc.WriteToClient(datagram.AddressType, datagram.Address, datagram.Port, plaintext)
		}
		return
	}

	// Check if this is a relay. Relay entries are immutable once inserted,
	// so reading entry fields after LookupBoth returns is safe even though
	// the entry could be removed concurrently.
	relayUp, relayDown := a.udpRelay.LookupBoth(peerID, frame.StreamID)

	if relayUp != nil {
		// Forward downstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameUDPDatagram,
			StreamID: relayUp.DownstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relayUp.DownstreamPeer, fwdFrame)
		return
	}

	if relayDown != nil {
		// Forward upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameUDPDatagram,
			StreamID: relayDown.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relayDown.UpstreamPeer, fwdFrame)
	}
}

// handleUDPClose processes a UDP_CLOSE frame.
func (a *Agent) handleUDPClose(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is for our UDP handler (exit node)
	if a.udpHandler != nil {
		a.udpHandler.HandleUDPClose(peerID, frame.StreamID)
	}

	// Check if this is a relay - PopMatchingPeer atomically looks up
	// (peer, stream ID), derives the direction, and removes the entry under
	// one Lock.
	if entry, fromUpstream := a.udpRelay.PopMatchingPeer(peerID, frame.StreamID); entry != nil {
		var dstPeer identity.AgentID
		var dstID uint64
		if fromUpstream {
			dstPeer = entry.DownstreamPeer
			dstID = entry.DownstreamID
		} else {
			dstPeer = entry.UpstreamPeer
			dstID = entry.UpstreamID
		}
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameUDPClose,
			StreamID: dstID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(dstPeer, fwdFrame)
	}

	// Check if this is for our ingress via exit stream lookup
	// Use write lock to atomically check and remove
	a.udpIngressMu.Lock()
	lookup := a.udpIngressByLocalStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	var dest *udpDestAssociation
	var ingress *udpIngressAssociation
	if lookup != nil {
		dest = lookup.Dest
		ingress = lookup.Ingress
		delete(a.udpIngressByLocalStream, streamKey{peer: peerID, streamID: frame.StreamID})
	}
	a.udpIngressMu.Unlock()

	if lookup != nil {
		// Clean up this destination association
		ingress.destMu.Lock()
		delete(ingress.destAssocs, dest.OriginKey)
		ingress.destMu.Unlock()
	}
}

// sendUDPOpenErr is a helper to send a UDP_OPEN_ERR frame.
func (a *Agent) sendUDPOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errCode uint16, msg string) {
	errPayload := &protocol.UDPOpenErr{
		RequestID: requestID,
		ErrorCode: errCode,
		Message:   msg,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameUDPOpenErr,
		StreamID: streamID,
		Payload:  errPayload.Encode(),
	}

	a.peerMgr.SendToPeer(peerID, frame)
}

// WriteUDPDatagram implements udp.DataWriter.
func (a *Agent) WriteUDPDatagram(peerID identity.AgentID, streamID uint64, datagram *protocol.UDPDatagram) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameUDPDatagram,
		StreamID: streamID,
		Payload:  datagram.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteUDPClose implements udp.DataWriter.
func (a *Agent) WriteUDPClose(peerID identity.AgentID, streamID uint64, reason uint8) error {
	closeFrame := &protocol.UDPClose{
		Reason: reason,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameUDPClose,
		StreamID: streamID,
		Payload:  closeFrame.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteUDPOpenAck implements udp.DataWriter.
func (a *Agent) WriteUDPOpenAck(peerID identity.AgentID, streamID uint64, ack *protocol.UDPOpenAck) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameUDPOpenAck,
		StreamID: streamID,
		Payload:  ack.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteUDPOpenErr implements udp.DataWriter.
func (a *Agent) WriteUDPOpenErr(peerID identity.AgentID, streamID uint64, errMsg *protocol.UDPOpenErr) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameUDPOpenErr,
		StreamID: streamID,
		Payload:  errMsg.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// SetSOCKS5UDPAssociation links a SOCKS5 UDP association to an ingress stream.
func (a *Agent) SetSOCKS5UDPAssociation(streamID uint64, socks5Assoc *socks5.UDPAssociation) {
	a.udpIngressMu.Lock()
	defer a.udpIngressMu.Unlock()

	if assoc := a.udpIngressByBase[streamID]; assoc != nil {
		assoc.mu.Lock()
		assoc.SOCKS5Assoc = socks5Assoc
		assoc.mu.Unlock()
	}
}

// startUDPDestCleanupLoop starts a goroutine that periodically cleans up idle destination associations.
func (a *Agent) startUDPDestCleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer ticker.Stop()
		for {
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
				a.cleanupIdleDestAssociations()
			}
		}
	}()
}

// cleanupIdleDestAssociations removes destination associations that have been
// idle for too long. The scan + per-ingress map removal happens under one
// destMu.Lock so a concurrent getOrCreateDestAssociation cannot hand out a
// dest we are about to close. No I/O happens under the lock; UDP_CLOSE
// frames are sent after the lock is released.
func (a *Agent) cleanupIdleDestAssociations() {
	now := time.Now()

	// Snapshot under RLock so we can acquire each ingress.destMu without
	// holding the global lock (avoids lock-order inversion with the
	// hot-path getOrCreateDestAssociation, which takes destMu first).
	a.udpIngressMu.RLock()
	ingressList := make([]*udpIngressAssociation, 0, len(a.udpIngressByBase))
	for _, ingress := range a.udpIngressByBase {
		ingressList = append(ingressList, ingress)
	}
	a.udpIngressMu.RUnlock()

	type idleCandidate struct {
		dest     *udpDestAssociation
		streamID uint64
	}

	for _, ingress := range ingressList {
		var candidates []idleCandidate
		ingress.destMu.Lock()
		for key, dest := range ingress.destAssocs {
			dest.mu.RLock()
			idle := now.Sub(dest.LastActivity) > ingress.idleTimeout
			streamID := dest.StreamID
			dest.mu.RUnlock()
			if idle {
				delete(ingress.destAssocs, key)
				candidates = append(candidates, idleCandidate{dest: dest, streamID: streamID})
			}
		}
		ingress.destMu.Unlock()

		if len(candidates) == 0 {
			continue
		}

		a.udpIngressMu.Lock()
		for _, c := range candidates {
			delete(a.udpIngressByLocalStream, streamKey{peer: c.dest.NextHop, streamID: c.streamID})
		}
		a.udpIngressMu.Unlock()

		// By this point the dest is unreachable via both indices, so no
		// concurrent goroutine can race a datagram against the close.
		for _, c := range candidates {
			a.closeDestAssociation(c.dest)
			a.logger.Debug("Cleaned up idle UDP destination association",
				logging.KeyStreamID, c.streamID)
		}
	}
}

// Compile-time interface verification
var _ udp.DataWriter = (*Agent)(nil)
var _ socks5.UDPAssociationHandler = (*Agent)(nil)
