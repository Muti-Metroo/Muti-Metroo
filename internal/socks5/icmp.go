package socks5

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// ICMPHandler is the interface for handling ICMP echo sessions.
// Implemented by the agent to relay ICMP through the mesh.
type ICMPHandler interface {
	// CreateICMPSession creates a new ICMP session through the mesh.
	// Returns the stream ID for the session, or an error if disabled.
	CreateICMPSession(ctx context.Context, destIP net.IP) (streamID uint64, err error)

	// SetSOCKS5ICMPAssociation links a SOCKS5 ICMP association to an ingress stream.
	// This allows echo replies to be forwarded back to the SOCKS5 client.
	SetSOCKS5ICMPAssociation(streamID uint64, assoc *ICMPAssociation)

	// RelayICMPEcho relays an ICMP echo request through the mesh.
	// The payload is encrypted and forwarded to the exit node.
	RelayICMPEcho(streamID uint64, identifier, sequence uint16, payload []byte) error

	// CloseICMPSession closes an ICMP session.
	CloseICMPSession(streamID uint64)

	// IsICMPEnabled returns whether ICMP echo is enabled.
	IsICMPEnabled() bool
}

// ICMPAssociation represents an active SOCKS5 ICMP echo session.
// Created when a client sends ICMP ECHO command (0x04).
type ICMPAssociation struct {
	// TCP control connection (also used for echo data)
	TCPConn net.Conn

	// Destination IP for echo requests
	DestIP net.IP

	// Stream ID for mesh routing
	StreamID uint64

	// Handler for relaying through mesh
	Handler ICMPHandler

	// Cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// State
	closed atomic.Bool
	mu     sync.RWMutex

	// Reply channel - used to wait for echo replies
	replyMu   sync.Mutex
	replyCond *sync.Cond
	replyData *ICMPReply
}

// ICMPReply holds a pending echo reply.
type ICMPReply struct {
	Identifier uint16
	Sequence   uint16
	Payload    []byte
}

// NewICMPAssociation creates a new ICMP association.
func NewICMPAssociation(tcpConn net.Conn, handler ICMPHandler, destIP net.IP) (*ICMPAssociation, error) {
	ctx, cancel := context.WithCancel(context.Background())

	assoc := &ICMPAssociation{
		TCPConn: tcpConn,
		DestIP:  destIP,
		Handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
	assoc.replyCond = sync.NewCond(&assoc.replyMu)

	return assoc, nil
}

// SetStreamID sets the stream ID for mesh routing.
func (a *ICMPAssociation) SetStreamID(id uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.StreamID = id
}

// GetStreamID returns the stream ID.
func (a *ICMPAssociation) GetStreamID() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.StreamID
}

// Close terminates the association and releases resources.
func (a *ICMPAssociation) Close() error {
	if a.closed.Swap(true) {
		return nil // Already closed
	}

	a.cancel()

	// Wake up any waiting goroutines
	a.replyCond.Broadcast()

	// Notify handler
	a.mu.RLock()
	streamID := a.StreamID
	a.mu.RUnlock()

	if a.Handler != nil && streamID != 0 {
		a.Handler.CloseICMPSession(streamID)
	}

	return nil
}

// IsClosed returns true if the association is closed.
func (a *ICMPAssociation) IsClosed() bool {
	return a.closed.Load()
}

// Context returns the association's context.
func (a *ICMPAssociation) Context() context.Context {
	return a.ctx
}

// RelayLoop reads ICMP echo requests from the client and relays them through the mesh.
// The client sends: [Identifier:2][Sequence:2][PayloadLen:2][Payload:N]
// Returns when the connection is closed or an error occurs.
func (a *ICMPAssociation) RelayLoop() error {
	header := make([]byte, 6) // ID(2) + Seq(2) + PayloadLen(2)

	for {
		select {
		case <-a.ctx.Done():
			return nil
		default:
		}

		// Read header
		if _, err := io.ReadFull(a.TCPConn, header); err != nil {
			if a.IsClosed() || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		identifier := binary.BigEndian.Uint16(header[0:2])
		sequence := binary.BigEndian.Uint16(header[2:4])
		payloadLen := binary.BigEndian.Uint16(header[4:6])

		// Read payload
		var payload []byte
		if payloadLen > 0 {
			// Sanity check payload length (max 65535 - IP/ICMP headers)
			if payloadLen > 65507 {
				return errors.New("ICMP payload too large")
			}
			payload = make([]byte, payloadLen)
			if _, err := io.ReadFull(a.TCPConn, payload); err != nil {
				if a.IsClosed() {
					return nil
				}
				return err
			}
		}

		// Relay through mesh
		a.mu.RLock()
		streamID := a.StreamID
		handler := a.Handler
		a.mu.RUnlock()

		if handler != nil && streamID != 0 {
			if err := handler.RelayICMPEcho(streamID, identifier, sequence, payload); err != nil {
				// Log error but continue - non-fatal
				continue
			}
		}
	}
}

// WriteToClient sends an ICMP echo reply back to the SOCKS5 client.
// The format is: [Identifier:2][Sequence:2][PayloadLen:2][Payload:N]
func (a *ICMPAssociation) WriteToClient(identifier, sequence uint16, payload []byte) error {
	if a.IsClosed() {
		return errors.New("association closed")
	}

	// Build response header
	header := make([]byte, 6)
	binary.BigEndian.PutUint16(header[0:2], identifier)
	binary.BigEndian.PutUint16(header[2:4], sequence)
	binary.BigEndian.PutUint16(header[4:6], uint16(len(payload)))

	// Write header + payload atomically
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, err := a.TCPConn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := a.TCPConn.Write(payload); err != nil {
			return err
		}
	}

	return nil
}
