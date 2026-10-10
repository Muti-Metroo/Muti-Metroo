// Package peer manages peer connections and handshakes for Muti Metroo.
package peer

import (
	"context"
	"fmt"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// HandshakeResult contains the outcome of a successful handshake.
type HandshakeResult struct {
	RemoteID          identity.AgentID
	RemoteDisplayName string
	Capabilities      []string
	RTT               time.Duration
}

// Handshaker handles the handshake protocol between peers.
type Handshaker struct {
	localID      identity.AgentID
	displayName  string
	capabilities []string
	timeout      time.Duration
}

// NewHandshaker creates a new handshaker.
func NewHandshaker(localID identity.AgentID, displayName string, capabilities []string, timeout time.Duration) *Handshaker {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Handshaker{
		localID:      localID,
		displayName:  displayName,
		capabilities: capabilities,
		timeout:      timeout,
	}
}

// PerformHandshake performs the handshake on a new connection.
// The dialer sends PEER_HELLO first, the listener waits to receive it first.
func (h *Handshaker) PerformHandshake(ctx context.Context, conn *Connection, expectedPeerID identity.AgentID) (*HandshakeResult, error) {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	// Get stream for handshake - dialer opens, listener accepts
	var stream transport.Stream
	var err error
	if conn.isDialer {
		stream, err = conn.conn.OpenStream(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to open handshake stream: %w", err)
		}
	} else {
		stream, err = conn.conn.AcceptStream(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to accept handshake stream: %w", err)
		}
	}
	// Note: stream is NOT closed here - it becomes the control stream for the connection.
	// The stream will be closed when the connection is closed.

	// Create frame reader/writer for this stream
	reader := protocol.NewFrameReader(stream)
	writer := protocol.NewFrameWriter(stream)

	// Store in connection for later use
	conn.reader = reader
	conn.writer = writer
	conn.controlStream = stream

	var result *HandshakeResult
	if conn.isDialer {
		result, err = h.dialerHandshake(ctx, conn, reader, writer, expectedPeerID)
	} else {
		result, err = h.listenerHandshake(ctx, conn, reader, writer, expectedPeerID)
	}

	if err != nil {
		stream.Close() // Close stream on handshake failure
		return nil, err
	}

	// Update connection state
	conn.RemoteID = result.RemoteID
	conn.RemoteDisplayName = result.RemoteDisplayName
	conn.capabilities = result.Capabilities
	conn.SetState(StateConnected)

	// Signal that reader/writer are ready for use
	conn.markReady()

	return result, nil
}

// dialerHandshake performs the handshake as the connection initiator.
func (h *Handshaker) dialerHandshake(ctx context.Context, conn *Connection, reader *protocol.FrameReader, writer *protocol.FrameWriter, expectedPeerID identity.AgentID) (*HandshakeResult, error) {
	startTime := time.Now()

	// Send PEER_HELLO
	hello := &protocol.PeerHello{
		Version:      protocol.ProtocolVersion,
		AgentID:      h.localID,
		Timestamp:    uint64(time.Now().UnixNano()),
		Capabilities: h.capabilities,
		DisplayName:  h.displayName,
	}

	if err := writer.Write(&protocol.Frame{
		Type:     protocol.FramePeerHello,
		StreamID: protocol.ControlStreamID,
		Payload:  hello.Encode(),
	}); err != nil {
		return nil, fmt.Errorf("failed to send PEER_HELLO: %w", err)
	}

	// Wait for PEER_HELLO_ACK (uses same format as PeerHello)
	frame, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read PEER_HELLO_ACK: %w", err)
	}

	if frame.Type != protocol.FramePeerHelloAck {
		return nil, fmt.Errorf("expected PEER_HELLO_ACK, got frame type 0x%02x", frame.Type)
	}

	// Decode response (PeerHelloAck uses same format as PeerHello)
	ack, err := protocol.DecodePeerHello(frame.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode PEER_HELLO_ACK: %w", err)
	}

	// Verify peer ID if expected
	remoteID := ack.AgentID

	if expectedPeerID != (identity.AgentID{}) && remoteID != expectedPeerID {
		return nil, fmt.Errorf("peer ID mismatch: expected %s, got %s",
			expectedPeerID.String(), remoteID.String())
	}

	// Calculate RTT
	rtt := time.Since(startTime)
	conn.UpdateRTT(uint64(startTime.UnixNano()))

	return &HandshakeResult{
		RemoteID:          remoteID,
		RemoteDisplayName: ack.DisplayName,
		Capabilities:      ack.Capabilities,
		RTT:               rtt,
	}, nil
}

// listenerHandshake performs the handshake as the connection acceptor.
func (h *Handshaker) listenerHandshake(ctx context.Context, conn *Connection, reader *protocol.FrameReader, writer *protocol.FrameWriter, expectedPeerID identity.AgentID) (*HandshakeResult, error) {
	// Wait for PEER_HELLO
	frame, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read PEER_HELLO: %w", err)
	}

	if frame.Type != protocol.FramePeerHello {
		return nil, fmt.Errorf("expected PEER_HELLO, got frame type 0x%02x", frame.Type)
	}

	// Decode hello
	hello, err := protocol.DecodePeerHello(frame.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode PEER_HELLO: %w", err)
	}

	// Verify protocol version
	if hello.Version != protocol.ProtocolVersion {
		return nil, fmt.Errorf("protocol version mismatch: expected %d, got %d",
			protocol.ProtocolVersion, hello.Version)
	}

	// Verify peer ID if expected
	remoteID := hello.AgentID

	if expectedPeerID != (identity.AgentID{}) && remoteID != expectedPeerID {
		return nil, fmt.Errorf("peer ID mismatch: expected %s, got %s",
			expectedPeerID.String(), remoteID.String())
	}

	// Send PEER_HELLO_ACK (uses same format as PeerHello)
	ack := &protocol.PeerHello{
		Version:      protocol.ProtocolVersion,
		AgentID:      h.localID,
		Timestamp:    hello.Timestamp, // Echo back for RTT calculation
		Capabilities: h.capabilities,
		DisplayName:  h.displayName,
	}

	if err := writer.Write(&protocol.Frame{
		Type:     protocol.FramePeerHelloAck,
		StreamID: protocol.ControlStreamID,
		Payload:  ack.Encode(),
	}); err != nil {
		return nil, fmt.Errorf("failed to send PEER_HELLO_ACK: %w", err)
	}

	return &HandshakeResult{
		RemoteID:          remoteID,
		RemoteDisplayName: hello.DisplayName,
		Capabilities:      hello.Capabilities,
		RTT:               0, // Listener doesn't measure RTT during handshake
	}, nil
}

// AcceptHandshake accepts an incoming connection and performs handshake.
func (h *Handshaker) AcceptHandshake(ctx context.Context, peerConn transport.PeerConn, cfg ConnectionConfig) (*Connection, error) {
	return h.handshakeConnection(ctx, NewConnection(peerConn, cfg), cfg.ExpectedPeerID)
}

// DialAndHandshake dials a peer and performs handshake.
func (h *Handshaker) DialAndHandshake(ctx context.Context, tr transport.Transport, addr string, cfg ConnectionConfig, dialOpts transport.DialOptions) (*Connection, error) {
	peerConn, err := tr.Dial(ctx, addr, dialOpts)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return h.handshakeConnection(ctx, NewConnection(peerConn, cfg), cfg.ExpectedPeerID)
}

// handshakeConnection performs the handshake on a connection, closing it on failure.
func (h *Handshaker) handshakeConnection(ctx context.Context, conn *Connection, expectedPeerID identity.AgentID) (*Connection, error) {
	if _, err := h.PerformHandshake(ctx, conn, expectedPeerID); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
