// Package transport provides network transport implementations for Muti Metroo.
package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// TransportType identifies the transport protocol.
type TransportType string

const (
	TransportQUIC      TransportType = "quic"
	TransportHTTP2     TransportType = "h2"
	TransportWebSocket TransportType = "ws"
)

// Transport creates and accepts peer connections.
type Transport interface {
	// Dial connects to a remote peer.
	Dial(ctx context.Context, addr string, opts DialOptions) (PeerConn, error)

	// Listen creates a listener for incoming connections.
	Listen(addr string, opts ListenOptions) (Listener, error)

	// Type returns the transport type identifier.
	Type() TransportType

	// Close shuts down the transport.
	Close() error
}

// Listener accepts incoming peer connections.
type Listener interface {
	// Accept waits for and returns the next connection.
	Accept(ctx context.Context) (PeerConn, error)

	// Addr returns the listener's network address.
	Addr() net.Addr

	// Close stops the listener.
	Close() error
}

// PeerConn represents a connection to a peer.
type PeerConn interface {
	// OpenStream creates a new outgoing stream.
	OpenStream(ctx context.Context) (Stream, error)

	// AcceptStream waits for an incoming stream.
	AcceptStream(ctx context.Context) (Stream, error)

	// Close terminates the connection.
	Close() error

	// LocalAddr returns the local address.
	LocalAddr() net.Addr

	// RemoteAddr returns the remote address.
	RemoteAddr() net.Addr

	// IsDialer returns true if this side initiated the connection.
	IsDialer() bool

	// TransportType returns the transport protocol type.
	TransportType() TransportType
}

// Stream is a bidirectional byte stream with half-close support.
type Stream interface {
	io.Reader
	io.Writer

	// StreamID returns the stream identifier.
	StreamID() uint64

	// CloseWrite sends a half-close (FIN) - signals done sending.
	CloseWrite() error

	// Close fully closes the stream in both directions.
	Close() error

	// SetDeadline sets read and write deadlines.
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// DialOptions contains options for dialing a peer.
type DialOptions struct {
	// TLSConfig is the TLS configuration for the connection.
	TLSConfig *tls.Config

	// StrictVerify enables TLS certificate verification (default: false).
	// When false (default), certificate verification is skipped, which is safe
	// because Muti Metroo uses an E2E encryption layer (X25519 + ChaCha20-Poly1305)
	// that provides confidentiality and integrity.
	// When true, peer certificates must be signed by a trusted CA.
	StrictVerify bool

	// Timeout is the connection timeout.
	Timeout time.Duration

	// ProxyURL is the HTTP proxy URL (for WebSocket transport).
	ProxyURL string

	// ProxyUsername is the proxy authentication username.
	ProxyUsername string

	// ProxyPassword is the proxy authentication password.
	ProxyPassword string

	// Protocol identifiers for customization (ALPN, HTTP header, WS subprotocol).
	// Empty string disables the identifier.

	// ALPNProtocol is the ALPN protocol identifier for QUIC/TLS.
	// Default: "muti-metroo/1". Empty string uses transport default.
	ALPNProtocol string

	// HTTPHeader is the custom header name for HTTP/2 transport.
	// Default: "X-Muti-Metroo-Protocol". Empty string disables custom header.
	HTTPHeader string

	// WSSubprotocol is the WebSocket subprotocol identifier.
	// Default: "muti-metroo/1". Empty string disables subprotocol.
	WSSubprotocol string

	// FingerprintPreset specifies the TLS ClientHello profile to use for client connections.
	// Valid values: "chrome", "firefox", "safari", "edge", "ios", "android", "random", "go", "disabled", "".
	// Empty string or "disabled" uses standard Go TLS.
	// Browser-compatible profiles improve compatibility with strict proxies and servers.
	FingerprintPreset string

	// TCPKeepAlive sets the SO_KEEPALIVE period on TCP-based transports (h2, ws).
	// A positive value keeps NAT mappings warm and speeds dead-peer detection;
	// 0 disables it. Ignored by QUIC, which manages its own keepalive.
	TCPKeepAlive time.Duration
}

// ListenOptions contains options for creating a listener.
type ListenOptions struct {
	// TLSConfig is the TLS configuration for the listener.
	TLSConfig *tls.Config

	// Path is the HTTP path (for HTTP/2 and WebSocket transports).
	Path string

	// MaxStreams is the maximum number of concurrent streams per connection.
	MaxStreams int

	// PlainText allows WebSocket listeners to accept connections without TLS.
	// Use this when the agent is behind a reverse proxy that handles TLS termination.
	// WARNING: Only use in trusted network environments (e.g., localhost or internal network).
	PlainText bool

	// Protocol identifiers for customization (ALPN, HTTP header, WS subprotocol).
	// Empty string disables the identifier.

	// ALPNProtocol is the ALPN protocol identifier for QUIC/TLS.
	// Default: "muti-metroo/1". Empty string uses transport default.
	ALPNProtocol string

	// HTTPHeader is the custom header name for HTTP/2 transport.
	// Default: "X-Muti-Metroo-Protocol". Empty string disables custom header.
	HTTPHeader string

	// WSSubprotocol is the WebSocket subprotocol identifier.
	// Default: "muti-metroo/1". Empty string disables subprotocol.
	WSSubprotocol string

	// TCPKeepAlive sets the SO_KEEPALIVE period on accepted TCP connections
	// (h2, ws). A positive value keeps idle inbound links alive; 0 disables it.
	TCPKeepAlive time.Duration
}

// DefaultDialOptions returns DialOptions with sensible defaults.
func DefaultDialOptions() DialOptions {
	return DialOptions{
		Timeout: 30 * time.Second,
	}
}

// DefaultListenOptions returns ListenOptions with sensible defaults.
func DefaultListenOptions() ListenOptions {
	return ListenOptions{
		MaxStreams: 10000,
	}
}

// StreamIDAllocator helps allocate stream IDs avoiding collisions.
// - Dialers use odd IDs (1, 3, 5, ...)
// - Listeners use even IDs (2, 4, 6, ...)
// Thread-safe: uses atomic operations for concurrent access.
type StreamIDAllocator struct {
	next     atomic.Uint64
	isDialer bool
}

// NewStreamIDAllocator creates a new allocator.
func NewStreamIDAllocator(isDialer bool) *StreamIDAllocator {
	start := uint64(2) // even for listener
	if isDialer {
		start = 1 // odd for dialer
	}
	a := &StreamIDAllocator{
		isDialer: isDialer,
	}
	a.next.Store(start)
	return a
}

// Next returns the next available stream ID.
// Thread-safe: can be called concurrently from multiple goroutines.
func (a *StreamIDAllocator) Next() uint64 {
	// Add 2 and return the value before the add
	return a.next.Add(2) - 2
}

// IsDialer returns true if this allocator is for a dialer.
func (a *StreamIDAllocator) IsDialer() bool {
	return a.isDialer
}
