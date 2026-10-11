package agent

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/health"
	"github.com/postalsys/muti-metroo/internal/icmp"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/socks5"
)

// ICMP-related errors
var (
	ErrICMPNoRoute        = errors.New("no ICMP route available")
	ErrICMPStreamNotFound = errors.New("ICMP stream not found")
)

// icmpIngressAssociation tracks a SOCKS5 ICMP session from the ingress side.
type icmpIngressAssociation struct {
	// Handle is the agent-wide opaque ID returned to external callers
	// (SOCKS5). StreamID is per-connection and cannot serve that role.
	Handle           uint64
	StreamID         uint64
	RequestID        uint64
	DestIP           net.IP
	ExitPeerID       identity.AgentID
	NextHop          identity.AgentID
	SessionKey       *crypto.SessionKey
	EphemeralPrivKey [32]byte
	EphemeralPubKey  [32]byte
	PendingOpen      chan struct{}
	OpenErr          error
	SOCKS5Assoc      *socks5.ICMPAssociation
	closeOnce        sync.Once
	mu               sync.RWMutex
}

// closePendingOpen safely closes the PendingOpen channel.
func (a *icmpIngressAssociation) closePendingOpen(err error) {
	a.closeOnce.Do(func() {
		if err != nil {
			a.mu.Lock()
			a.OpenErr = err
			a.mu.Unlock()
		}
		close(a.PendingOpen)
	})
}

// generateICMPRequestID generates a cryptographically random request ID.
// Using crypto/rand prevents session correlation attacks.
func generateICMPRequestID() uint64 {
	var buf [8]byte
	if _, err := cryptorand.Read(buf[:]); err != nil {
		// Fallback should never happen, but use time-based ID if it does
		panic("crypto/rand failed: " + err.Error())
	}
	return binary.BigEndian.Uint64(buf[:])
}

// deriveICMPSessionKey performs ECDH key exchange and derives a session key for ICMP sessions.
// The ephPrivKey is zeroed after use.
// Returns nil if the remote key is zero (encryption disabled), or an error if ECDH fails.
func deriveICMPSessionKey(
	ephPrivKey *[32]byte,
	ephPubKey [32]byte,
	remotePubKey [32]byte,
	requestID uint64,
) (*crypto.SessionKey, error) {
	var zeroKey [protocol.EphemeralKeySize]byte
	if remotePubKey == zeroKey {
		return nil, nil
	}

	sharedSecret, err := crypto.ComputeECDH(*ephPrivKey, remotePubKey)
	if err != nil {
		return nil, err
	}

	// Zero out private key immediately
	crypto.ZeroKey(ephPrivKey)

	// Derive session key (caller is initiator)
	sessionKey := crypto.DeriveSessionKey(sharedSecret, requestID, ephPubKey, remotePubKey, true)
	crypto.ZeroKey(&sharedSecret)

	return sessionKey, nil
}

// handleICMPOpen processes an ICMP_OPEN frame.
func (a *Agent) handleICMPOpen(peerID identity.AgentID, frame *protocol.Frame) {
	open, err := protocol.DecodeICMPOpen(frame.Payload)
	if err != nil {
		a.logger.Debug("failed to decode ICMP_OPEN frame", "error", err)
		return
	}

	a.logger.Debug("received ICMP_OPEN",
		logging.KeyStreamID, frame.StreamID,
		"from_peer", peerID.ShortString(),
		"remaining_path_len", len(open.RemainingPath))

	// Check if we are the exit node (path is empty)
	if len(open.RemainingPath) == 0 {
		// We are the exit node - handle with ICMP handler
		if a.icmpHandler == nil {
			a.sendICMPOpenErr(peerID, frame.StreamID, open.RequestID, protocol.ErrICMPDisabled, "ICMP echo disabled")
			return
		}

		ctx := context.Background()
		a.icmpHandler.HandleICMPOpen(ctx, peerID, frame.StreamID, open, open.EphemeralPubKey)
		return
	}

	// Relay to next hop
	nextHop := open.RemainingPath[0]

	conn := a.peerMgr.GetPeer(nextHop)
	if conn == nil {
		a.sendICMPOpenErr(peerID, frame.StreamID, open.RequestID, protocol.ErrHostUnreachable, "no route to next hop")
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
	a.icmpRelay.Insert(relay)

	// Update remaining path
	newPath := open.RemainingPath[1:]

	// Forward with new stream ID
	fwdOpen := &protocol.ICMPOpen{
		RequestID:       open.RequestID,
		DestIP:          open.DestIP,
		TTL:             open.TTL,
		RemainingPath:   newPath,
		EphemeralPubKey: open.EphemeralPubKey,
	}

	fwdFrame := &protocol.Frame{
		Type:     protocol.FrameICMPOpen,
		StreamID: downstreamID,
		Payload:  fwdOpen.Encode(),
	}

	a.logger.Debug("relaying ICMP_OPEN to next hop",
		logging.KeyStreamID, downstreamID,
		"next_hop", nextHop.ShortString(),
		"remaining_path_len", len(newPath))

	if err := a.peerMgr.SendToPeer(nextHop, fwdFrame); err != nil {
		a.logger.Debug("failed to relay ICMP_OPEN",
			"error", err,
			"next_hop", nextHop.ShortString())

		// Clean up relay entry
		a.icmpRelay.Delete(relay)

		a.sendICMPOpenErr(peerID, frame.StreamID, open.RequestID, protocol.ErrConnectionRefused, err.Error())
	}
}

// handleICMPOpenAck processes an ICMP_OPEN_ACK frame.
func (a *Agent) handleICMPOpenAck(peerID identity.AgentID, frame *protocol.Frame) {
	a.logger.Debug("received ICMP_OPEN_ACK",
		logging.KeyStreamID, frame.StreamID,
		"from_peer", peerID.ShortString())

	// Check if this is a relay response. Relay entries are immutable once
	// inserted, so reading fields after LookupDownstream returns is safe.
	if relay := a.icmpRelay.LookupDownstream(peerID, frame.StreamID); relay != nil {
		a.logger.Debug("relaying ICMP_OPEN_ACK upstream",
			logging.KeyStreamID, relay.UpstreamID,
			"upstream_peer", relay.UpstreamPeer.ShortString())

		// Forward ACK to upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameICMPOpenAck,
			StreamID: relay.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relay.UpstreamPeer, fwdFrame)
		return
	}

	// Check if this is for ingress (SOCKS5 client initiated)
	a.icmpIngressMu.RLock()
	ingress := a.icmpIngressByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	a.icmpIngressMu.RUnlock()

	if ingress != nil {
		ack, err := protocol.DecodeICMPOpenAck(frame.Payload)
		if err != nil {
			ingress.closePendingOpen(err)
			return
		}

		sessionKey, err := deriveICMPSessionKey(&ingress.EphemeralPrivKey, ingress.EphemeralPubKey, ack.EphemeralPubKey, ack.RequestID)
		if err != nil {
			ingress.closePendingOpen(err)
			return
		}

		if sessionKey != nil {
			ingress.mu.Lock()
			ingress.SessionKey = sessionKey
			ingress.mu.Unlock()
		}

		ingress.closePendingOpen(nil)
		return
	}

	// Check if this is for WebSocket session
	a.icmpWSSessionMu.RLock()
	wsSession := a.icmpWSSessionByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	a.icmpWSSessionMu.RUnlock()

	if wsSession == nil {
		return
	}

	ack, err := protocol.DecodeICMPOpenAck(frame.Payload)
	if err != nil {
		wsSession.closePendingOpenWS(err)
		return
	}

	sessionKey, err := deriveICMPSessionKey(&wsSession.EphemeralPrivKey, wsSession.EphemeralPubKey, ack.EphemeralPubKey, ack.RequestID)
	if err != nil {
		wsSession.closePendingOpenWS(err)
		return
	}

	if sessionKey != nil {
		wsSession.mu.Lock()
		wsSession.SessionKey = sessionKey
		wsSession.mu.Unlock()
	}

	wsSession.closePendingOpenWS(nil)
}

// handleICMPOpenErr processes an ICMP_OPEN_ERR frame.
func (a *Agent) handleICMPOpenErr(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is a relay response - PopDownstream atomically
	// looks the entry up by (peer, stream ID) and removes it.
	if relay := a.icmpRelay.PopDownstream(peerID, frame.StreamID); relay != nil {
		// Forward error to upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameICMPOpenErr,
			StreamID: relay.UpstreamID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(relay.UpstreamPeer, fwdFrame)
		return
	}

	// Check if this is for ingress (SOCKS5 client initiated)
	a.icmpIngressMu.Lock()
	ingress := a.icmpIngressByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	if ingress != nil {
		a.removeICMPIngress(ingress)
	}
	a.icmpIngressMu.Unlock()

	if ingress != nil {
		errMsg, err := protocol.DecodeICMPOpenErr(frame.Payload)
		if err != nil {
			ingress.closePendingOpen(err)
			return
		}
		ingress.closePendingOpen(fmt.Errorf("ICMP open failed: %s (code %d)", errMsg.Message, errMsg.ErrorCode))
		return
	}

	// Check if this is for WebSocket session
	a.icmpWSSessionMu.Lock()
	wsSession := a.icmpWSSessionByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	if wsSession != nil {
		a.removeICMPWSSession(wsSession)
	}
	a.icmpWSSessionMu.Unlock()

	if wsSession == nil {
		return
	}

	errMsg, err := protocol.DecodeICMPOpenErr(frame.Payload)
	if err != nil {
		wsSession.closePendingOpenWS(err)
		return
	}

	wsSession.closePendingOpenWS(fmt.Errorf("ICMP open failed: %s (code %d)", errMsg.Message, errMsg.ErrorCode))
}

// failICMPRelay handles a failed echo relay forward: the relay entry is
// removed and both ends receive an ICMP_CLOSE so neither session keeps
// sending into a dead entry until the idle timeout reaps it.
func (a *Agent) failICMPRelay(relay *relayEntry, cause error) {
	a.icmpRelay.Delete(relay)
	a.logger.Debug("icmp relay forward failed, closing session both ways",
		"upstream_peer", relay.UpstreamPeer.ShortString(),
		logging.KeyStreamID, relay.UpstreamID,
		"downstream_peer", relay.DownstreamPeer.ShortString(),
		"downstream_stream_id", relay.DownstreamID,
		logging.KeyError, cause)
	a.peerMgr.SendToPeer(relay.UpstreamPeer, &protocol.Frame{
		Type:     protocol.FrameICMPClose,
		StreamID: relay.UpstreamID,
	})
	a.peerMgr.SendToPeer(relay.DownstreamPeer, &protocol.Frame{
		Type:     protocol.FrameICMPClose,
		StreamID: relay.DownstreamID,
	})
}

// handleICMPEcho processes an ICMP_ECHO frame.
func (a *Agent) handleICMPEcho(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is for our ICMP handler (exit node receiving from mesh)
	if a.icmpHandler != nil {
		if session := a.icmpHandler.GetSession(peerID, frame.StreamID); session != nil {
			echo, err := protocol.DecodeICMPEcho(frame.Payload)
			if err != nil {
				return
			}
			a.icmpHandler.HandleICMPEcho(peerID, frame.StreamID, echo)
			return
		}
	}

	// Check if this is for ingress (reply coming back to SOCKS5 client)
	// Hold lock while getting ingress and extracting fields needed for processing
	a.icmpIngressMu.RLock()
	ingress := a.icmpIngressByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	a.icmpIngressMu.RUnlock()

	if ingress != nil {
		echo, err := protocol.DecodeICMPEcho(frame.Payload)
		if err != nil {
			return
		}

		// Only process replies
		if !echo.IsReply {
			return
		}

		// Decrypt if we have a session key - get fields under fine-grained lock
		ingress.mu.RLock()
		sessionKey := ingress.SessionKey
		socks5Assoc := ingress.SOCKS5Assoc
		ingress.mu.RUnlock()

		var plaintext []byte
		if sessionKey != nil {
			plaintext, err = sessionKey.Decrypt(echo.Data)
			if err != nil {
				return
			}
		} else {
			plaintext = echo.Data
		}

		// Forward to SOCKS5 client
		if socks5Assoc != nil {
			socks5Assoc.WriteToClient(echo.Identifier, echo.Sequence, plaintext)
		}
		return
	}

	// Check if this is for WebSocket session (reply coming back to CLI ping)
	a.icmpWSSessionMu.RLock()
	wsSession := a.icmpWSSessionByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	a.icmpWSSessionMu.RUnlock()

	if wsSession != nil {
		echo, err := protocol.DecodeICMPEcho(frame.Payload)
		if err != nil {
			return
		}

		// Only process replies
		if !echo.IsReply {
			return
		}

		// Decrypt if we have a session key - get fields under fine-grained lock
		wsSession.mu.RLock()
		sessionKey := wsSession.SessionKey
		closed := wsSession.closed
		wsSession.mu.RUnlock()

		if closed {
			return
		}

		var plaintext []byte
		var errStr string
		if sessionKey != nil {
			plaintext, err = sessionKey.Decrypt(echo.Data)
			if err != nil {
				errStr = err.Error()
			}
		} else {
			plaintext = echo.Data
		}

		// Send to WebSocket via channel
		select {
		case wsSession.ReceiveEcho <- &health.ICMPEchoResponse{
			Identifier: echo.Identifier,
			Sequence:   echo.Sequence,
			Payload:    plaintext,
			Error:      errStr,
			SrcIP:      net.IP(echo.SrcIP),
		}:
		default:
			// Channel full, drop the response
		}
		return
	}

	// Check if this is a relay. Relay entries are immutable once inserted,
	// so reading entry fields after LookupBoth returns is safe even though
	// the entry could be removed concurrently.
	relayUp, relayDown := a.icmpRelay.LookupBoth(peerID, frame.StreamID)

	if relayUp != nil {
		// Forward downstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameICMPEcho,
			StreamID: relayUp.DownstreamID,
			Payload:  frame.Payload,
		}
		if err := a.peerMgr.SendToPeer(relayUp.DownstreamPeer, fwdFrame); err != nil {
			a.failICMPRelay(relayUp, err)
		}
		return
	}

	if relayDown != nil {
		// Forward upstream
		fwdFrame := &protocol.Frame{
			Type:     protocol.FrameICMPEcho,
			StreamID: relayDown.UpstreamID,
			Payload:  frame.Payload,
		}
		if err := a.peerMgr.SendToPeer(relayDown.UpstreamPeer, fwdFrame); err != nil {
			a.failICMPRelay(relayDown, err)
		}
	}
}

// handleICMPClose processes an ICMP_CLOSE frame.
func (a *Agent) handleICMPClose(peerID identity.AgentID, frame *protocol.Frame) {
	// Check if this is for our ICMP handler (exit node)
	if a.icmpHandler != nil {
		a.icmpHandler.HandleICMPClose(peerID, frame.StreamID)
	}

	// Check if this is for ingress (SOCKS5 client session)
	a.icmpIngressMu.Lock()
	ingress := a.icmpIngressByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	if ingress != nil {
		a.removeICMPIngress(ingress)
	}
	a.icmpIngressMu.Unlock()

	// Note: We don't need to do anything special for ingress close -
	// the SOCKS5 connection will be closed when the TCP connection ends

	// Check if this is for WebSocket session
	a.icmpWSSessionMu.Lock()
	wsSession := a.icmpWSSessionByStream[streamKey{peer: peerID, streamID: frame.StreamID}]
	if wsSession != nil {
		a.removeICMPWSSession(wsSession)
	}
	a.icmpWSSessionMu.Unlock()

	if wsSession != nil {
		wsSession.close()
	}

	// Check if this is a relay - PopMatchingPeer atomically looks up
	// (peer, stream ID), derives the direction, and removes the entry under
	// one Lock.
	if entry, fromUpstream := a.icmpRelay.PopMatchingPeer(peerID, frame.StreamID); entry != nil {
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
			Type:     protocol.FrameICMPClose,
			StreamID: dstID,
			Payload:  frame.Payload,
		}
		a.peerMgr.SendToPeer(dstPeer, fwdFrame)
	}
}

// sendICMPOpenErr is a helper to send an ICMP_OPEN_ERR frame.
func (a *Agent) sendICMPOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errCode uint16, msg string) {
	errPayload := &protocol.ICMPOpenErr{
		RequestID: requestID,
		ErrorCode: errCode,
		Message:   msg,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameICMPOpenErr,
		StreamID: streamID,
		Payload:  errPayload.Encode(),
	}

	a.peerMgr.SendToPeer(peerID, frame)
}

// WriteICMPOpenAck implements icmp.DataWriter.
func (a *Agent) WriteICMPOpenAck(peerID identity.AgentID, streamID uint64, ack *protocol.ICMPOpenAck) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameICMPOpenAck,
		StreamID: streamID,
		Payload:  ack.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteICMPOpenErr implements icmp.DataWriter.
func (a *Agent) WriteICMPOpenErr(peerID identity.AgentID, streamID uint64, errMsg *protocol.ICMPOpenErr) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameICMPOpenErr,
		StreamID: streamID,
		Payload:  errMsg.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteICMPEcho implements icmp.DataWriter.
func (a *Agent) WriteICMPEcho(peerID identity.AgentID, streamID uint64, echo *protocol.ICMPEcho) error {
	frame := &protocol.Frame{
		Type:     protocol.FrameICMPEcho,
		StreamID: streamID,
		Payload:  echo.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// WriteICMPClose implements icmp.DataWriter.
func (a *Agent) WriteICMPClose(peerID identity.AgentID, streamID uint64, reason uint8) error {
	closeFrame := &protocol.ICMPClose{
		Reason: reason,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameICMPClose,
		StreamID: streamID,
		Payload:  closeFrame.Encode(),
	}

	return a.peerMgr.SendToPeer(peerID, frame)
}

// CreateICMPSession implements socks5.ICMPHandler.
// Called when a SOCKS5 client requests ICMP ECHO command.
func (a *Agent) CreateICMPSession(ctx context.Context, destIP net.IP) (uint64, error) {
	// Route lookup based on destination IP
	route := a.routeMgr.Lookup(destIP)
	if route == nil {
		return 0, ErrICMPNoRoute
	}

	nextHop := route.NextHop
	conn := a.peerMgr.GetPeer(nextHop)
	if conn == nil {
		return 0, fmt.Errorf("no connection to next hop: %s", nextHop.ShortString())
	}

	// Build remaining path
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
	requestID := generateICMPRequestID()

	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return 0, err
	}

	handle := a.icmpNextHandle.Add(1)
	assoc := &icmpIngressAssociation{
		Handle:           handle,
		StreamID:         streamID,
		RequestID:        requestID,
		DestIP:           destIP,
		ExitPeerID:       route.OriginAgent,
		NextHop:          nextHop,
		EphemeralPrivKey: ephPriv,
		EphemeralPubKey:  ephPub,
		PendingOpen:      make(chan struct{}),
	}

	// Register in map
	a.icmpIngressMu.Lock()
	a.icmpIngressByStream[streamKey{peer: nextHop, streamID: streamID}] = assoc
	a.icmpIngressByHandle[handle] = assoc
	a.icmpIngressMu.Unlock()

	// Build and send ICMP_OPEN
	open := &protocol.ICMPOpen{
		RequestID:       requestID,
		DestIP:          destIP.To4(),
		TTL:             uint8(len(rPath)),
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameICMPOpen,
		StreamID: streamID,
		Payload:  open.Encode(),
	}

	a.logger.Debug("Creating ICMP session",
		logging.KeyStreamID, streamID,
		"dest_ip", destIP.String(),
		"exit", route.OriginAgent.ShortString(),
		"next_hop", nextHop.ShortString())

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		// Cleanup
		a.icmpIngressMu.Lock()
		a.removeICMPIngress(assoc)
		a.icmpIngressMu.Unlock()
		assoc.closePendingOpen(err)
		return 0, err
	}

	// Wait for ACK with timeout
	select {
	case <-ctx.Done():
		assoc.closePendingOpen(ctx.Err())
		return 0, ctx.Err()
	case <-assoc.PendingOpen:
		if assoc.OpenErr != nil {
			return 0, assoc.OpenErr
		}
		// Return the agent-wide handle, not the wire stream ID: the caller
		// hands this back to SetSOCKS5ICMPAssociation / RelayICMPEcho /
		// CloseICMPSession, which resolve through icmpIngressByHandle.
		return handle, nil
	}
}

// removeICMPIngress deletes an association from both indices. Callers hold
// icmpIngressMu. Keeping the two deletes in one place is what stops the
// indices from drifting apart.
func (a *Agent) removeICMPIngress(assoc *icmpIngressAssociation) {
	if assoc == nil {
		return
	}
	delete(a.icmpIngressByStream, streamKey{peer: assoc.NextHop, streamID: assoc.StreamID})
	delete(a.icmpIngressByHandle, assoc.Handle)
}

// removeICMPWSSession deletes a WebSocket session from both indices. Callers
// hold icmpWSSessionMu.
func (a *Agent) removeICMPWSSession(session *icmpWebSocketSession) {
	if session == nil {
		return
	}
	delete(a.icmpWSSessionByStream, streamKey{peer: session.NextHop, streamID: session.StreamID})
	delete(a.icmpWSSessionByHandle, session.Handle)
}

// SetSOCKS5ICMPAssociation links a SOCKS5 ICMP association to an ingress stream.
func (a *Agent) SetSOCKS5ICMPAssociation(handle uint64, socks5Assoc *socks5.ICMPAssociation) {
	a.icmpIngressMu.Lock()
	defer a.icmpIngressMu.Unlock()

	if assoc := a.icmpIngressByHandle[handle]; assoc != nil {
		assoc.mu.Lock()
		assoc.SOCKS5Assoc = socks5Assoc
		assoc.mu.Unlock()
	}
}

// RelayICMPEcho implements socks5.ICMPHandler.
// Encrypts and sends an ICMP echo request through the mesh.
func (a *Agent) RelayICMPEcho(handle uint64, identifier, sequence uint16, payload []byte) error {
	// Get association and extract fields under lock
	a.icmpIngressMu.RLock()
	assoc := a.icmpIngressByHandle[handle]
	if assoc == nil {
		a.icmpIngressMu.RUnlock()
		return ErrICMPStreamNotFound
	}
	a.icmpIngressMu.RUnlock()

	// Encrypt payload if we have a session key - use fine-grained lock
	assoc.mu.RLock()
	sessionKey := assoc.SessionKey
	nextHop := assoc.NextHop
	streamID := assoc.StreamID
	assoc.mu.RUnlock()

	var ciphertext []byte
	var err error
	if sessionKey != nil {
		ciphertext, err = sessionKey.Encrypt(payload)
		if err != nil {
			return err
		}
	} else {
		ciphertext = payload
	}

	echo := &protocol.ICMPEcho{
		Identifier: identifier,
		Sequence:   sequence,
		IsReply:    false,
		Data:       ciphertext,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameICMPEcho,
		StreamID: streamID,
		Payload:  echo.Encode(),
	}

	return a.peerMgr.SendToPeer(nextHop, frame)
}

// CloseICMPSession implements socks5.ICMPHandler.
func (a *Agent) CloseICMPSession(handle uint64) {
	a.icmpIngressMu.Lock()
	assoc := a.icmpIngressByHandle[handle]
	if assoc != nil {
		a.removeICMPIngress(assoc)
	}
	a.icmpIngressMu.Unlock()

	if assoc == nil {
		return
	}

	// Send ICMP_CLOSE to the mesh. The frame carries the wire stream ID, which
	// is what the next hop indexed the session under -- not our local handle.
	closeFrame := &protocol.ICMPClose{Reason: protocol.ICMPCloseNormal}
	frame := &protocol.Frame{
		Type:     protocol.FrameICMPClose,
		StreamID: assoc.StreamID,
		Payload:  closeFrame.Encode(),
	}

	a.peerMgr.SendToPeer(assoc.NextHop, frame)

	a.logger.Debug("ICMP session closed",
		logging.KeyStreamID, assoc.StreamID)
}

// IsICMPEnabled implements socks5.ICMPHandler.
// Returns true if this agent can relay ICMP traffic.
func (a *Agent) IsICMPEnabled() bool {
	// ICMP is enabled for ingress if we have any routes
	return a.cfg.SOCKS5.Enabled
}

// icmpWebSocketSession tracks an ICMP session from WebSocket API.
type icmpWebSocketSession struct {
	// Handle is the agent-wide opaque ID returned to external callers.
	Handle           uint64
	StreamID         uint64
	RequestID        uint64
	DestIP           net.IP
	ExitPeerID       identity.AgentID
	NextHop          identity.AgentID
	SessionKey       *crypto.SessionKey
	EphemeralPrivKey [32]byte
	EphemeralPubKey  [32]byte
	PendingOpen      chan struct{}
	OpenErr          error
	SendEcho         chan *health.ICMPEchoRequest
	ReceiveEcho      chan *health.ICMPEchoResponse
	Done             chan struct{}
	closeOnce        sync.Once
	mu               sync.RWMutex
	closed           bool
}

// closePendingOpen safely closes the PendingOpen channel.
func (s *icmpWebSocketSession) closePendingOpenWS(err error) {
	s.closeOnce.Do(func() {
		if err != nil {
			s.mu.Lock()
			s.OpenErr = err
			s.mu.Unlock()
		}
		close(s.PendingOpen)
	})
}

// close closes the session and all its channels.
func (s *icmpWebSocketSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	close(s.Done)
}

// OpenICMPSession implements health.ICMPProvider.
// Opens an ICMP session to a remote agent for the WebSocket ping API.
func (a *Agent) OpenICMPSession(ctx context.Context, targetID identity.AgentID, destIP net.IP) (*health.ICMPSession, error) {
	// Check if target is a direct peer
	var nextHop identity.AgentID
	var rPath []identity.AgentID
	var remainingPath []identity.AgentID

	if conn := a.peerMgr.GetPeer(targetID); conn != nil {
		nextHop = targetID
		rPath = []identity.AgentID{targetID}
	} else if agentRoute := a.routeMgr.LookupAgent(targetID); agentRoute != nil {
		// Primary: agent presence table
		nextHop = agentRoute.NextHop
		rPath = agentRoute.Path
		for i, id := range rPath {
			if id == nextHop && i+1 < len(rPath) {
				remainingPath = make([]identity.AgentID, len(rPath)-i-1)
				copy(remainingPath, rPath[i+1:])
				break
			}
		}
	} else {
		// Fallback: CIDR routing table (backward compat with old agents)
		routes := a.routeMgr.Table().GetRoutesFromAgent(targetID)
		if len(routes) == 0 {
			return nil, fmt.Errorf("no route to agent %s", targetID.ShortString())
		}

		route := routes[0]
		nextHop = route.NextHop
		rPath = route.Path

		for i, id := range rPath {
			if id == nextHop && i+1 < len(rPath) {
				remainingPath = make([]identity.AgentID, len(rPath)-i-1)
				copy(remainingPath, rPath[i+1:])
				break
			}
		}
	}

	conn := a.peerMgr.GetPeer(nextHop)
	if conn == nil {
		return nil, fmt.Errorf("no connection to next hop: %s", nextHop.ShortString())
	}

	streamID := conn.NextStreamID()
	requestID := generateICMPRequestID()

	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		return nil, err
	}

	session := &icmpWebSocketSession{
		Handle:           a.icmpNextHandle.Add(1),
		StreamID:         streamID,
		RequestID:        requestID,
		DestIP:           destIP,
		ExitPeerID:       targetID,
		NextHop:          nextHop,
		EphemeralPrivKey: ephPriv,
		EphemeralPubKey:  ephPub,
		PendingOpen:      make(chan struct{}),
		SendEcho:         make(chan *health.ICMPEchoRequest, 16),
		ReceiveEcho:      make(chan *health.ICMPEchoResponse, 16),
		Done:             make(chan struct{}),
	}

	// Register in map
	a.icmpWSSessionMu.Lock()
	a.icmpWSSessionByStream[streamKey{peer: nextHop, streamID: streamID}] = session
	a.icmpWSSessionByHandle[session.Handle] = session
	a.icmpWSSessionMu.Unlock()

	// Build and send ICMP_OPEN
	open := &protocol.ICMPOpen{
		RequestID:       requestID,
		DestIP:          destIP.To4(),
		TTL:             uint8(len(rPath)),
		RemainingPath:   remainingPath,
		EphemeralPubKey: ephPub,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameICMPOpen,
		StreamID: streamID,
		Payload:  open.Encode(),
	}

	a.logger.Debug("Opening WebSocket ICMP session",
		logging.KeyStreamID, streamID,
		"dest_ip", destIP.String(),
		"target", targetID.ShortString(),
		"next_hop", nextHop.ShortString())

	if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
		// Cleanup
		a.icmpWSSessionMu.Lock()
		a.removeICMPWSSession(session)
		a.icmpWSSessionMu.Unlock()
		session.closePendingOpenWS(err)
		return nil, err
	}

	// Wait for ACK with timeout
	select {
	case <-ctx.Done():
		session.closePendingOpenWS(ctx.Err())
		a.icmpWSSessionMu.Lock()
		a.removeICMPWSSession(session)
		a.icmpWSSessionMu.Unlock()
		return nil, ctx.Err()
	case <-session.PendingOpen:
		if session.OpenErr != nil {
			a.icmpWSSessionMu.Lock()
			a.removeICMPWSSession(session)
			a.icmpWSSessionMu.Unlock()
			return nil, session.OpenErr
		}
	}

	// Start goroutine to send echo requests from channel
	go a.runWSICMPSender(session)

	// Return the session interface
	return &health.ICMPSession{
		StreamID:    streamID,
		TargetID:    targetID,
		DestIP:      destIP,
		SendEcho:    session.SendEcho,
		ReceiveEcho: session.ReceiveEcho,
		Done:        session.Done,
		Close: func() {
			a.closeWSICMPSession(session.Handle)
		},
	}, nil
}

// runWSICMPSender reads from the SendEcho channel and sends to the mesh.
func (a *Agent) runWSICMPSender(session *icmpWebSocketSession) {
	for {
		select {
		case <-session.Done:
			return
		case req, ok := <-session.SendEcho:
			if !ok {
				return
			}

			// Encrypt payload if we have a session key
			session.mu.RLock()
			sessionKey := session.SessionKey
			nextHop := session.NextHop
			streamID := session.StreamID
			session.mu.RUnlock()

			var ciphertext []byte
			var err error
			if sessionKey != nil {
				ciphertext, err = sessionKey.Encrypt(req.Payload)
				if err != nil {
					a.logger.Debug("failed to encrypt ICMP payload", "error", err)
					continue
				}
			} else {
				ciphertext = req.Payload
			}

			echo := &protocol.ICMPEcho{
				Identifier: req.Identifier,
				Sequence:   req.Sequence,
				IsReply:    false,
				Data:       ciphertext,
			}

			frame := &protocol.Frame{
				Type:     protocol.FrameICMPEcho,
				StreamID: streamID,
				Payload:  echo.Encode(),
			}

			if err := a.peerMgr.SendToPeer(nextHop, frame); err != nil {
				a.logger.Debug("failed to send ICMP echo", "error", err)
			}
		}
	}
}

// closeWSICMPSession closes a WebSocket ICMP session.
func (a *Agent) closeWSICMPSession(handle uint64) {
	a.icmpWSSessionMu.Lock()
	session := a.icmpWSSessionByHandle[handle]
	if session != nil {
		a.removeICMPWSSession(session)
	}
	a.icmpWSSessionMu.Unlock()

	if session == nil {
		return
	}

	session.close()

	// Send ICMP_CLOSE to the mesh
	closeFrame := &protocol.ICMPClose{Reason: protocol.ICMPCloseNormal}
	frame := &protocol.Frame{
		Type:     protocol.FrameICMPClose,
		StreamID: session.StreamID,
		Payload:  closeFrame.Encode(),
	}

	a.peerMgr.SendToPeer(session.NextHop, frame)

	a.logger.Debug("WebSocket ICMP session closed",
		logging.KeyStreamID, session.StreamID)
}

// Compile-time interface verification
var _ icmp.DataWriter = (*Agent)(nil)
var _ socks5.ICMPHandler = (*Agent)(nil)
var _ health.ICMPProvider = (*Agent)(nil)
