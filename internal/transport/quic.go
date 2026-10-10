package transport

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// Default QUIC configuration values.
//
// Layering note: QUIC has its own transport-level keepalive independent of the
// mesh-level keepalive configured via connections.idle_threshold and
// connections.timeout. The mesh-level keepalive is designed for the TCP-based
// transports (HTTP/2, WebSocket) where there is no underlying liveness signal.
// On QUIC, transport-level keepalive frames reset MaxIdleTimeout every
// KeepAlivePeriod, so the mesh-level keepalive is effectively a safety net.
//
// DefaultMaxIdleTimeout matches the mesh-level connections.timeout default
// (90s) so that a truly dead connection is declared dead at the same time
// across transports.
const (
	DefaultMaxIdleTimeout     = 90 * time.Second
	DefaultKeepAlivePeriod    = 30 * time.Second
	DefaultMaxIncomingStreams = 10000
)

// QUICTransport implements Transport using QUIC protocol.
type QUICTransport struct {
	mu        sync.Mutex
	listeners []*QUICListener
	closed    bool
}

// NewQUICTransport creates a new QUIC transport.
func NewQUICTransport() *QUICTransport {
	return &QUICTransport{}
}

// Type returns the transport type.
func (t *QUICTransport) Type() TransportType {
	return TransportQUIC
}

// Dial connects to a remote peer using QUIC.
// Note: TLS fingerprint customization (FingerprintPreset) is not currently supported
// for QUIC transport. QUIC uses its own TLS 1.3 implementation within quic-go,
// and integrating uTLS would require significant changes to quic-go internals.
// To reduce fingerprint distinctiveness, consider using HTTP/2 or WebSocket transport instead.
func (t *QUICTransport) Dial(ctx context.Context, addr string, opts DialOptions) (PeerConn, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	t.mu.Unlock()

	// Log warning if fingerprinting is requested but not supported for QUIC
	if IsFingerprintEnabled(opts.FingerprintPreset) {
		slog.Warn("TLS fingerprinting is not supported for QUIC transport; using standard TLS",
			slog.String("preset", opts.FingerprintPreset),
			slog.String("addr", addr))
	}

	// Determine ALPN protocol to use
	alpn := opts.ALPNProtocol
	if alpn == "" {
		alpn = DefaultALPNProtocol
	}

	tlsConfig, err := prepareTLSConfigForDial(opts.TLSConfig, opts.StrictVerify, []string{alpn})
	if err != nil {
		return nil, err
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout:        DefaultMaxIdleTimeout,
		KeepAlivePeriod:       DefaultKeepAlivePeriod,
		MaxIncomingStreams:    DefaultMaxIncomingStreams,
		MaxIncomingUniStreams: 0, // We don't use uni streams
	}

	// Apply timeout
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	conn, err := quic.DialAddr(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("QUIC dial failed: %w", err)
	}

	return &QUICPeerConn{
		conn:     conn,
		isDialer: true,
	}, nil
}

// Listen creates a QUIC listener.
func (t *QUICTransport) Listen(addr string, opts ListenOptions) (Listener, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, fmt.Errorf("transport closed")
	}

	tlsConfig := opts.TLSConfig
	if tlsConfig == nil {
		return nil, fmt.Errorf("TLS config required for QUIC listener")
	}

	// Determine ALPN protocol to use
	alpn := opts.ALPNProtocol
	if alpn == "" {
		alpn = DefaultALPNProtocol
	}

	// Clone and set ALPN
	tlsConfig = tlsConfig.Clone()
	tlsConfig.NextProtos = []string{alpn}

	maxStreams := opts.MaxStreams
	if maxStreams <= 0 {
		maxStreams = DefaultMaxIncomingStreams
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout:        DefaultMaxIdleTimeout,
		KeepAlivePeriod:       DefaultKeepAlivePeriod,
		MaxIncomingStreams:    int64(maxStreams),
		MaxIncomingUniStreams: 0,
	}

	listener, err := quic.ListenAddr(addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("QUIC listen failed: %w", err)
	}

	ql := &QUICListener{
		listener: listener,
	}
	t.listeners = append(t.listeners, ql)

	return ql, nil
}

// Close shuts down the transport and all listeners.
func (t *QUICTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	var lastErr error
	for _, l := range t.listeners {
		if err := l.Close(); err != nil {
			lastErr = err
		}
	}
	t.listeners = nil

	return lastErr
}

// QUICListener implements Listener for QUIC.
type QUICListener struct {
	listener *quic.Listener
	closed   bool
	mu       sync.Mutex
}

// Accept waits for and returns the next QUIC connection.
func (l *QUICListener) Accept(ctx context.Context) (PeerConn, error) {
	conn, err := l.listener.Accept(ctx)
	if err != nil {
		return nil, err
	}

	return &QUICPeerConn{
		conn:     conn,
		isDialer: false,
	}, nil
}

// Addr returns the listener's address.
func (l *QUICListener) Addr() net.Addr {
	return l.listener.Addr()
}

// Close stops the listener.
func (l *QUICListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	l.closed = true

	return l.listener.Close()
}

// QUICPeerConn implements PeerConn for QUIC.
type QUICPeerConn struct {
	conn     *quic.Conn
	isDialer bool
}

// OpenStream creates a new outgoing QUIC stream.
func (c *QUICPeerConn) OpenStream(ctx context.Context) (Stream, error) {
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open QUIC stream: %w", err)
	}

	return &QUICStream{stream: stream}, nil
}

// AcceptStream waits for an incoming QUIC stream.
func (c *QUICPeerConn) AcceptStream(ctx context.Context) (Stream, error) {
	stream, err := c.conn.AcceptStream(ctx)
	if err != nil {
		return nil, err
	}

	return &QUICStream{stream: stream}, nil
}

// Close terminates the QUIC connection.
func (c *QUICPeerConn) Close() error {
	return c.conn.CloseWithError(0, "connection closed")
}

// LocalAddr returns the local address.
func (c *QUICPeerConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote address.
func (c *QUICPeerConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// IsDialer returns true if this side initiated the connection.
func (c *QUICPeerConn) IsDialer() bool {
	return c.isDialer
}

// TransportType returns the transport protocol type.
func (c *QUICPeerConn) TransportType() TransportType {
	return TransportQUIC
}

// QUICStream implements Stream for QUIC.
type QUICStream struct {
	stream *quic.Stream
}

// StreamID returns the QUIC stream ID.
func (s *QUICStream) StreamID() uint64 {
	return uint64(s.stream.StreamID())
}

// Read reads data from the stream.
func (s *QUICStream) Read(p []byte) (int, error) {
	return s.stream.Read(p)
}

// Write writes data to the stream.
func (s *QUICStream) Write(p []byte) (int, error) {
	return s.stream.Write(p)
}

// CloseWrite sends a half-close (FIN) on the write side.
func (s *QUICStream) CloseWrite() error {
	return s.stream.Close()
}

// Close fully closes the stream.
func (s *QUICStream) Close() error {
	s.stream.CancelRead(0)
	return s.stream.Close()
}

// SetDeadline sets read and write deadlines.
func (s *QUICStream) SetDeadline(t time.Time) error {
	return s.stream.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (s *QUICStream) SetReadDeadline(t time.Time) error {
	return s.stream.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (s *QUICStream) SetWriteDeadline(t time.Time) error {
	return s.stream.SetWriteDeadline(t)
}
