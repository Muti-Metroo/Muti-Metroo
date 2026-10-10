// Package peer manages peer connections and handshakes for Muti Metroo.
package peer

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// ConnectionState represents the state of a peer connection.
type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateHandshaking
	StateConnected
	StateReconnecting
)

// String returns the string representation of the state.
func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "DISCONNECTED"
	case StateConnecting:
		return "CONNECTING"
	case StateHandshaking:
		return "HANDSHAKING"
	case StateConnected:
		return "CONNECTED"
	case StateReconnecting:
		return "RECONNECTING"
	default:
		return "UNKNOWN"
	}
}

// Connection represents a connection to a single peer.
type Connection struct {
	// Identity
	LocalID           identity.AgentID
	RemoteID          identity.AgentID
	RemoteDisplayName string // Display name received during handshake

	// Connection
	conn       transport.PeerConn
	isDialer   bool
	configAddr string // Original config address used for dialing (for reconnection)

	// State
	state        atomic.Int32
	capabilities []string

	// Frame I/O
	reader        *protocol.FrameReader
	writer        *protocol.FrameWriter
	controlStream transport.Stream
	writeMu       sync.Mutex

	// Streams
	streamAlloc  *transport.StreamIDAllocator
	nextStreamID atomic.Uint64

	// Activity tracking
	lastActivity atomic.Int64
	rtt          atomic.Int64 // Round-trip time in nanoseconds

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closed    chan struct{}
	ready     chan struct{} // Closed when handshake completes and reader/writer are set

	// Frame processing
	frameCh    chan *protocol.Frame // Sequential frame dispatch channel (stream-ordered frames)
	fastLaneCh chan *protocol.Frame // Parallel dispatch for unordered frames (UDP_DATAGRAM, ICMP_ECHO)

	// Callbacks
	onFrame      func(*Connection, *protocol.Frame)
	onDisconnect func(*Connection, error)
}

// ConnectionConfig contains configuration for a connection.
type ConnectionConfig struct {
	LocalID          identity.AgentID
	ExpectedPeerID   identity.AgentID // Optional: verify peer ID during handshake
	Capabilities     []string
	HandshakeTimeout time.Duration
	OnFrame          func(*Connection, *protocol.Frame)
	OnDisconnect     func(*Connection, error)
}

// DefaultConnectionConfig returns a config with defaults.
func DefaultConnectionConfig(localID identity.AgentID) ConnectionConfig {
	return ConnectionConfig{
		LocalID:          localID,
		Capabilities:     []string{},
		HandshakeTimeout: 10 * time.Second,
	}
}

// fastLaneWorkerCount is the number of goroutines draining fastLaneCh per
// connection. Unordered frames (UDP_DATAGRAM, ICMP_ECHO) take this fast
// lane to avoid head-of-line blocking the stream-oriented frame processor.
// 4 is enough to absorb bursty UDP/ICMP traffic without blowing up
// goroutine counts on agents with many peers.
const fastLaneWorkerCount = 4

// NewConnection creates a new peer connection wrapper.
func NewConnection(conn transport.PeerConn, cfg ConnectionConfig) *Connection {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Connection{
		LocalID:      cfg.LocalID,
		conn:         conn,
		isDialer:     conn.IsDialer(),
		capabilities: cfg.Capabilities,
		streamAlloc:  transport.NewStreamIDAllocator(conn.IsDialer()),
		ctx:          ctx,
		cancel:       cancel,
		closed:       make(chan struct{}),
		ready:        make(chan struct{}),
		frameCh:      make(chan *protocol.Frame, 256),
		fastLaneCh:   make(chan *protocol.Frame, 256),
		onFrame:      cfg.OnFrame,
		onDisconnect: cfg.OnDisconnect,
	}

	c.state.Store(int32(StateHandshaking))
	c.updateActivity()

	// frameCh drains sequentially to preserve per-stream ordering
	// (STREAM_CLOSE must not pass STREAM_DATA on the same stream).
	// fastLaneCh drains in parallel because its frame types
	// (UDP_DATAGRAM, ICMP_ECHO) are explicitly unordered.
	go c.drainFrames(c.frameCh)
	for i := 0; i < fastLaneWorkerCount; i++ {
		go c.drainFrames(c.fastLaneCh)
	}

	return c
}

// drainFrames pulls frames from ch and dispatches each to onFrame, returning
// when the connection is closed. A panic in any single handler is recovered
// so it does not kill the goroutine.
func (c *Connection) drainFrames(ch <-chan *protocol.Frame) {
	for {
		select {
		case frame := <-ch:
			if c.onFrame == nil {
				continue
			}
			func() {
				defer func() {
					_ = recover()
				}()
				c.onFrame(c, frame)
			}()
		case <-c.closed:
			return
		}
	}
}

// State returns the current connection state.
func (c *Connection) State() ConnectionState {
	return ConnectionState(c.state.Load())
}

// SetState updates the connection state.
func (c *Connection) SetState(state ConnectionState) {
	c.state.Store(int32(state))
}

// IsDialer returns true if this side initiated the connection.
func (c *Connection) IsDialer() bool {
	return c.isDialer
}

// TransportType returns the transport protocol type for this connection.
func (c *Connection) TransportType() transport.TransportType {
	if c.conn == nil {
		return ""
	}
	return c.conn.TransportType()
}

// Capabilities returns the remote peer's capabilities.
func (c *Connection) Capabilities() []string {
	return c.capabilities
}

// HasCapability checks if the peer has a specific capability.
func (c *Connection) HasCapability(cap string) bool {
	for _, c := range c.capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// NextStreamID returns the next available stream ID.
func (c *Connection) NextStreamID() uint64 {
	return c.streamAlloc.Next()
}

// OpenStream opens a new stream to the peer.
func (c *Connection) OpenStream(ctx context.Context) (transport.Stream, error) {
	if c.State() != StateConnected {
		return nil, fmt.Errorf("connection not in connected state: %s", c.State())
	}
	return c.conn.OpenStream(ctx)
}

// AcceptStream accepts an incoming stream from the peer.
func (c *Connection) AcceptStream(ctx context.Context) (transport.Stream, error) {
	return c.conn.AcceptStream(ctx)
}

// WriteFrame writes a frame to the connection.
func (c *Connection) WriteFrame(f *protocol.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.writer == nil {
		return fmt.Errorf("connection not initialized")
	}

	c.updateActivity()
	return c.writer.Write(f)
}

// SendData sends a STREAM_DATA frame.
func (c *Connection) SendData(streamID uint64, data []byte) error {
	return c.WriteFrame(&protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: streamID,
		Payload:  data,
	})
}

// SendKeepalive sends a KEEPALIVE frame.
func (c *Connection) SendKeepalive() error {
	ka := &protocol.Keepalive{
		Timestamp: uint64(time.Now().UnixNano()),
	}
	return c.WriteFrame(&protocol.Frame{
		Type:     protocol.FrameKeepalive,
		StreamID: protocol.ControlStreamID,
		Payload:  ka.Encode(),
	})
}

// SendKeepaliveAck sends a KEEPALIVE_ACK frame.
func (c *Connection) SendKeepaliveAck(timestamp uint64) error {
	ka := &protocol.Keepalive{
		Timestamp: timestamp,
	}
	return c.WriteFrame(&protocol.Frame{
		Type:     protocol.FrameKeepaliveAck,
		StreamID: protocol.ControlStreamID,
		Payload:  ka.Encode(),
	})
}

// LastActivity returns the time of last activity.
func (c *Connection) LastActivity() time.Time {
	ns := c.lastActivity.Load()
	return time.Unix(0, ns)
}

// RTT returns the measured round-trip time.
func (c *Connection) RTT() time.Duration {
	return time.Duration(c.rtt.Load())
}

// updateActivity updates the last activity timestamp.
func (c *Connection) updateActivity() {
	c.lastActivity.Store(time.Now().UnixNano())
}

// UpdateRTT updates the measured RTT from a keepalive response.
func (c *Connection) UpdateRTT(sentTimestamp uint64) {
	now := uint64(time.Now().UnixNano())
	if now > sentTimestamp {
		c.rtt.Store(int64(now - sentTimestamp))
	}
}

// Close closes the connection.
func (c *Connection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.cancel()
		c.SetState(StateDisconnected)
		// Close control stream if set
		if c.controlStream != nil {
			c.controlStream.Close()
		}
		err = c.conn.Close()
		close(c.closed)
		// Do not close frameCh: processFrames exits via c.closed, and
		// readLoop stops sending after conn.Done() fires. Closing frameCh
		// here would race with readLoop's select (send on closed channel
		// panic).
	})
	return err
}

// Done returns a channel that's closed when the connection is closed.
func (c *Connection) Done() <-chan struct{} {
	return c.closed
}

// Ready returns a channel that's closed when the handshake is complete.
func (c *Connection) Ready() <-chan struct{} {
	return c.ready
}

// markReady signals that the handshake is complete and reader/writer are initialized.
// This should only be called once, after setting reader, writer, and controlStream.
func (c *Connection) markReady() {
	select {
	case <-c.ready:
		// Already closed
	default:
		close(c.ready)
	}
}

// Context returns the connection's context.
func (c *Connection) Context() context.Context {
	return c.ctx
}

// LocalAddr returns the local address.
func (c *Connection) LocalAddr() string {
	if c.conn == nil {
		return ""
	}
	return addrToString(c.conn.LocalAddr())
}

// RemoteAddr returns the remote address.
func (c *Connection) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return addrToString(c.conn.RemoteAddr())
}

// addrToString converts a net.Addr to string, returning empty string if nil.
func addrToString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

// ConfigAddr returns the original config address used for dialing.
// This is used for reconnection when the peer disconnects.
// Returns empty string for accepted (incoming) connections.
func (c *Connection) ConfigAddr() string {
	return c.configAddr
}

// SetConfigAddr sets the original config address used for dialing.
// This should be called after establishing an outbound connection.
func (c *Connection) SetConfigAddr(addr string) {
	c.configAddr = addr
}

// String returns a string representation.
func (c *Connection) String() string {
	return fmt.Sprintf("Peer{id=%s, state=%s, addr=%s}",
		c.RemoteID.ShortString(), c.State(), c.RemoteAddr())
}
