package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// WebSocket transport constants
const (
	wsDefaultPath        = "/mesh"
	wsDefaultReadLimit   = 16 * 1024 * 1024 // 16 MB max message size
	wsDefaultIdleTimeout = 60 * time.Second
)

// WebSocketTransport implements Transport using WebSocket protocol.
// Unlike QUIC, WebSocket doesn't have native stream multiplexing.
// All virtual streams are multiplexed over a single WebSocket connection
// using our frame protocol (StreamID in frames identifies the stream).
type WebSocketTransport struct {
	mu        sync.Mutex
	listeners []*WebSocketListener
	closed    bool
}

// NewWebSocketTransport creates a new WebSocket transport.
func NewWebSocketTransport() *WebSocketTransport {
	return &WebSocketTransport{}
}

// Type returns the transport type.
func (t *WebSocketTransport) Type() TransportType {
	return TransportWebSocket
}

// Dial connects to a remote peer using WebSocket.
func (t *WebSocketTransport) Dial(ctx context.Context, addr string, opts DialOptions) (PeerConn, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	t.mu.Unlock()

	// Parse address as URL
	wsURL := parseWebSocketURL(addr)

	// Apply timeout
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Build dial options with configurable subprotocol
	dialOpts := &websocket.DialOptions{}
	wsSubprotocol := opts.WSSubprotocol
	if wsSubprotocol == "" {
		wsSubprotocol = DefaultWSSubprotocol
	}
	if wsSubprotocol != "" {
		dialOpts.Subprotocols = []string{wsSubprotocol}
	}

	// Configure HTTP client for TLS and proxy
	httpClient, httpTransport, err := buildHTTPClient(opts)
	if err != nil {
		return nil, err
	}
	dialOpts.HTTPClient = httpClient

	// Dial WebSocket
	conn, _, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		httpTransport.CloseIdleConnections()
		return nil, fmt.Errorf("WebSocket dial failed: %w", err)
	}

	// Configure connection
	conn.SetReadLimit(wsDefaultReadLimit)

	return &WebSocketPeerConn{
		conn:          conn,
		isDialer:      true,
		httpTransport: httpTransport,
	}, nil
}

// Listen creates a WebSocket listener.
func (t *WebSocketTransport) Listen(addr string, opts ListenOptions) (Listener, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, fmt.Errorf("transport closed")
	}

	tlsConfig := opts.TLSConfig
	if tlsConfig == nil && !opts.PlainText {
		return nil, fmt.Errorf("TLS config required for WebSocket listener (use PlainText: true for reverse proxy mode)")
	}

	path := opts.Path
	if path == "" {
		path = wsDefaultPath
	}

	// Determine WebSocket subprotocol
	wsSubprotocol := opts.WSSubprotocol
	if wsSubprotocol == "" {
		wsSubprotocol = DefaultWSSubprotocol
	}

	listener := &WebSocketListener{
		addr:          addr,
		path:          path,
		tlsConfig:     tlsConfig,
		wsSubprotocol: wsSubprotocol,
		tcpKeepAlive:  opts.TCPKeepAlive,
		connCh:        make(chan *WebSocketPeerConn, 16),
		closeCh:       make(chan struct{}),
	}

	// Start HTTP server
	if err := listener.start(); err != nil {
		return nil, err
	}

	t.listeners = append(t.listeners, listener)
	return listener, nil
}

// Close shuts down the transport and all listeners.
func (t *WebSocketTransport) Close() error {
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

// WebSocketListener implements Listener for WebSocket.
type WebSocketListener struct {
	addr          string
	path          string
	tlsConfig     *tls.Config
	wsSubprotocol string        // WebSocket subprotocol (empty to disable)
	tcpKeepAlive  time.Duration // SO_KEEPALIVE period for accepted conns (0 disables)
	server        *http.Server
	netLn         net.Listener
	connCh        chan *WebSocketPeerConn
	closeCh       chan struct{}
	closed        atomic.Bool
}

// start initializes the HTTP server.
func (l *WebSocketListener) start() error {
	mux := http.NewServeMux()
	mux.HandleFunc(l.path, l.handleWebSocket)

	l.server = &http.Server{
		Addr:      l.addr,
		Handler:   mux,
		TLSConfig: l.tlsConfig,
	}

	// Create TCP listener
	ln, err := net.Listen("tcp", l.addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	ln = wrapKeepAliveListener(ln, l.tcpKeepAlive)
	l.netLn = ln

	// Start serving in background
	go func() {
		if l.tlsConfig != nil {
			l.server.ServeTLS(ln, "", "")
		} else {
			l.server.Serve(ln)
		}
	}()

	return nil
}

// handleWebSocket handles incoming WebSocket upgrade requests.
func (l *WebSocketListener) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check if we're closed
	if l.closed.Load() {
		http.Error(w, "server closed", http.StatusServiceUnavailable)
		return
	}

	// Accept WebSocket connection with configurable subprotocol
	acceptOpts := &websocket.AcceptOptions{}
	if l.wsSubprotocol != "" {
		acceptOpts.Subprotocols = []string{l.wsSubprotocol}
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		return
	}

	conn.SetReadLimit(wsDefaultReadLimit)

	// Create peer connection
	peerConn := &WebSocketPeerConn{
		conn:     conn,
		isDialer: false,
	}

	// Send to Accept channel
	select {
	case l.connCh <- peerConn:
	case <-l.closeCh:
		conn.Close(websocket.StatusGoingAway, "server closed")
	}
}

// Accept waits for and returns the next WebSocket connection.
func (l *WebSocketListener) Accept(ctx context.Context) (PeerConn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closeCh:
		return nil, fmt.Errorf("listener closed")
	}
}

// Addr returns the listener's address.
func (l *WebSocketListener) Addr() net.Addr {
	if l.netLn != nil {
		return l.netLn.Addr()
	}
	return nil
}

// Close stops the listener.
func (l *WebSocketListener) Close() error {
	if l.closed.Swap(true) {
		return nil
	}

	close(l.closeCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if l.server != nil {
		return l.server.Shutdown(ctx)
	}
	return nil
}

// WebSocketPeerConn implements PeerConn for WebSocket.
// Since WebSocket doesn't support native stream multiplexing,
// this connection provides a single "stream" that the peer manager
// uses as its control stream. All virtual streams are multiplexed
// over this single connection using our frame protocol.
type WebSocketPeerConn struct {
	conn       *websocket.Conn
	isDialer   bool
	streamOnce sync.Once
	stream     *WebSocketStream
	closed     atomic.Bool
	// httpTransport is the per-dial *http.Transport (client only). nhooyr's
	// websocket.Dial hijacks the upgraded conn so the pool is usually empty,
	// but anything that failed pre-hijack (proxy CONNECT, redirect) would
	// otherwise linger here until GC.
	httpTransport *http.Transport
}

// OpenStream returns the single WebSocket stream.
// WebSocket connections only have one bidirectional stream.
func (c *WebSocketPeerConn) OpenStream(ctx context.Context) (Stream, error) {
	c.streamOnce.Do(func() {
		c.stream = &WebSocketStream{
			conn: c.conn,
			ctx:  context.Background(), // Use background context for long-lived stream
			id:   1,                    // Single stream
		}
	})
	return c.stream, nil
}

// AcceptStream returns the single WebSocket stream.
// WebSocket connections only have one bidirectional stream.
func (c *WebSocketPeerConn) AcceptStream(ctx context.Context) (Stream, error) {
	c.streamOnce.Do(func() {
		c.stream = &WebSocketStream{
			conn: c.conn,
			ctx:  context.Background(), // Use background context for long-lived stream
			id:   1,                    // Single stream
		}
	})
	return c.stream, nil
}

// Close terminates the WebSocket connection.
func (c *WebSocketPeerConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	err := c.conn.Close(websocket.StatusNormalClosure, "connection closed")
	if c.httpTransport != nil {
		c.httpTransport.CloseIdleConnections()
	}
	return err
}

// LocalAddr returns the local address (not available for WebSocket).
func (c *WebSocketPeerConn) LocalAddr() net.Addr {
	return nil // WebSocket doesn't expose local address
}

// RemoteAddr returns the remote address (not available for WebSocket).
func (c *WebSocketPeerConn) RemoteAddr() net.Addr {
	return nil // WebSocket doesn't expose remote address directly
}

// IsDialer returns true if this side initiated the connection.
func (c *WebSocketPeerConn) IsDialer() bool {
	return c.isDialer
}

// TransportType returns the transport protocol type.
func (c *WebSocketPeerConn) TransportType() TransportType {
	return TransportWebSocket
}

// WebSocketStream implements Stream for WebSocket.
// It wraps the WebSocket connection as a stream using binary messages.
type WebSocketStream struct {
	conn   *websocket.Conn
	ctx    context.Context
	id     uint64
	reader io.Reader
	// readerCancel belongs to the deadline context that produced reader. The
	// library's message reader keeps using that context on every Read, so the
	// cancel must not run until the message is fully drained.
	readerCancel context.CancelFunc
	readMu       sync.Mutex // Only protects reader buffer, not blocking read
	closed       atomic.Bool

	// The websocket library takes contexts rather than deadlines, so the
	// deadlines are stored and converted into a per-operation context on each
	// Read/Write. A zero deadline means no timeout, matching net.Conn.
	deadlineMu    sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

// StreamID returns the stream ID.
func (s *WebSocketStream) StreamID() uint64 {
	return s.id
}

// Read reads data from the WebSocket stream.
func (s *WebSocketStream) Read(p []byte) (int, error) {
	// Check for buffered data first (with mutex)
	s.readMu.Lock()
	if s.reader != nil {
		n, err := s.reader.Read(p)
		if err == io.EOF {
			s.reader = nil
			if s.readerCancel != nil {
				s.readerCancel()
				s.readerCancel = nil
			}
			s.readMu.Unlock()
			// If we read some data, return it
			if n > 0 {
				return n, nil
			}
			// Otherwise, fall through to get next message
		} else {
			s.readMu.Unlock()
			return n, err
		}
	} else {
		s.readMu.Unlock()
	}

	// Read next WebSocket message (without holding mutex - this blocks).
	// A set read deadline becomes the context deadline for this read. The
	// derived context outlives this call: the library's message reader keeps
	// using it, so cancel runs only when the message is drained (or fails).
	ctx := s.ctx
	var cancel context.CancelFunc
	s.deadlineMu.Lock()
	if !s.readDeadline.IsZero() {
		ctx, cancel = context.WithDeadline(s.ctx, s.readDeadline)
	}
	s.deadlineMu.Unlock()

	msgType, reader, err := s.conn.Reader(ctx)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return 0, err
	}

	if msgType != websocket.MessageBinary {
		if cancel != nil {
			cancel()
		}
		return 0, fmt.Errorf("unexpected message type: %v", msgType)
	}

	// Store reader and read from it (with mutex)
	s.readMu.Lock()
	s.reader = reader
	s.readerCancel = cancel
	n, err := s.reader.Read(p)
	if err == io.EOF {
		s.reader = nil
		if s.readerCancel != nil {
			s.readerCancel()
			s.readerCancel = nil
		}
		err = nil
	}
	s.readMu.Unlock()
	return n, err
}

// Write writes data to the WebSocket stream.
func (s *WebSocketStream) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, fmt.Errorf("stream closed")
	}

	// A set write deadline becomes the context deadline for this write; the
	// library maps it onto the underlying net.Conn write deadline, so a peer
	// that stops reading makes the write fail instead of blocking forever.
	ctx := s.ctx
	s.deadlineMu.Lock()
	if !s.writeDeadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(s.ctx, s.writeDeadline)
		defer cancel()
	}
	s.deadlineMu.Unlock()

	err := s.conn.Write(ctx, websocket.MessageBinary, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// CloseWrite signals end of writes (not really supported in WebSocket).
func (s *WebSocketStream) CloseWrite() error {
	// WebSocket doesn't have half-close; we just mark it
	return nil
}

// Close fully closes the stream.
func (s *WebSocketStream) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "stream closed")
}

// SetDeadline sets read and write deadlines. Deadlines are absolute points in
// time applied to every subsequent Read/Write until changed, matching net.Conn
// semantics; the zero time disables the deadline.
func (s *WebSocketStream) SetDeadline(t time.Time) error {
	s.deadlineMu.Lock()
	s.readDeadline = t
	s.writeDeadline = t
	s.deadlineMu.Unlock()
	return nil
}

// SetReadDeadline sets the read deadline. See SetDeadline.
func (s *WebSocketStream) SetReadDeadline(t time.Time) error {
	s.deadlineMu.Lock()
	s.readDeadline = t
	s.deadlineMu.Unlock()
	return nil
}

// SetWriteDeadline sets the write deadline. See SetDeadline.
func (s *WebSocketStream) SetWriteDeadline(t time.Time) error {
	s.deadlineMu.Lock()
	s.writeDeadline = t
	s.deadlineMu.Unlock()
	return nil
}

// parseWebSocketURL parses the address into a WebSocket URL.
func parseWebSocketURL(addr string) string {
	// If already a URL, use as-is
	if strings.HasPrefix(addr, "ws://") || strings.HasPrefix(addr, "wss://") {
		return addr
	}

	// Always use wss:// for security. If no TLS config is provided,
	// buildHTTPClient will create a default insecure config.
	return "wss://" + addr + wsDefaultPath
}

// buildHTTPClient creates an HTTP client with optional TLS, proxy, and fingerprint settings.
// The returned *http.Transport is the same one wired into the client; callers keep a
// reference so they can CloseIdleConnections() on shutdown -- websocket.Dial hijacks
// the upgraded conn but anything that failed before hijack (proxy CONNECT etc.) can
// otherwise linger in the pool.
func buildHTTPClient(opts DialOptions) (*http.Client, *http.Transport, error) {
	tlsConfig, err := prepareTLSConfigForDial(opts.TLSConfig, opts.StrictVerify, nil)
	if err != nil {
		return nil, nil, err
	}

	transport := &http.Transport{}

	// Dialer carrying the configured SO_KEEPALIVE period for the underlying TCP
	// connection (keeps idle WebSocket links warm across NAT).
	netDialer := keepaliveDialer(opts.TCPKeepAlive)

	// Use uTLS for fingerprinting if enabled
	if IsFingerprintEnabled(opts.FingerprintPreset) {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Pin ALPN to http/1.1: WebSocket upgrade only works over HTTP/1.1,
			// but browser fingerprints (Chrome etc.) advertise [h2, http/1.1] by
			// default. If the server picks h2, the dial fails with
			// "server selected unadvertised ALPN protocol".
			conn, err := DialUTLSWithALPN(ctx, netDialer, network, addr, tlsConfig, opts.FingerprintPreset, []string{"http/1.1"})
			if err != nil {
				return nil, err
			}
			if conn == nil {
				return tls.Dial(network, addr, tlsConfig)
			}
			return conn, nil
		}
	} else {
		transport.TLSClientConfig = tlsConfig
		transport.DialContext = netDialer.DialContext
	}

	// Configure proxy if specified
	if opts.ProxyURL != "" {
		proxyURL, err := url.Parse(opts.ProxyURL)
		if err == nil {
			// Add proxy authentication if provided
			if opts.ProxyUsername != "" {
				proxyURL.User = url.UserPassword(opts.ProxyUsername, opts.ProxyPassword)
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
	}, transport, nil
}
