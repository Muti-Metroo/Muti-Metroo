package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

// HTTP/2 transport constants
const (
	h2DefaultPath        = "/mesh"
	h2DefaultIdleTimeout = 60 * time.Second
)

// H2Transport implements Transport using HTTP/2 streaming.
// Unlike QUIC, HTTP/2 doesn't have native stream multiplexing for our use case.
// We use a single HTTP/2 POST request with streaming body in both directions.
// All virtual streams are multiplexed using our frame protocol.
type H2Transport struct {
	mu        sync.Mutex
	listeners []*H2Listener
	closed    bool
}

// NewH2Transport creates a new HTTP/2 transport.
func NewH2Transport() *H2Transport {
	return &H2Transport{}
}

// Type returns the transport type.
func (t *H2Transport) Type() TransportType {
	return TransportHTTP2
}

// Dial connects to a remote peer using HTTP/2 streaming.
func (t *H2Transport) Dial(ctx context.Context, addr string, opts DialOptions) (PeerConn, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport closed")
	}
	t.mu.Unlock()

	// Parse address
	h2URL, path := parseH2Address(addr)

	// Create a connection context that outlives the dial call
	// The request uses a cancellable background context - we cancel it on Close()
	// The dial timeout is handled separately
	connCtx, connCancel := context.WithCancel(context.Background())

	// Apply dial timeout if specified
	var dialCtx context.Context
	var dialCancel context.CancelFunc
	if opts.Timeout > 0 {
		dialCtx, dialCancel = context.WithTimeout(ctx, opts.Timeout)
	} else {
		dialCtx, dialCancel = context.WithCancel(ctx)
	}
	// dialCancel is called after RoundTrip completes (success or failure)

	// Create HTTP/2 client with TLS
	tlsConfig, err := prepareTLSConfigForDial(opts.TLSConfig, opts.StrictVerify, []string{"h2"})
	if err != nil {
		connCancel()
		dialCancel()
		return nil, err
	}
	tlsConfig = ensureH2InNextProtos(tlsConfig)

	var h2Transport *http2.Transport

	// Dialer carrying the configured SO_KEEPALIVE period for the underlying TCP
	// connection. Dial timeout is enforced separately via dialCtx below.
	netDialer := keepaliveDialer(opts.TCPKeepAlive)

	// Use uTLS for fingerprinting if enabled
	if IsFingerprintEnabled(opts.FingerprintPreset) {
		h2Transport = &http2.Transport{
			AllowHTTP: false,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				conn, err := DialUTLSWithALPN(ctx, netDialer, network, addr, tlsConfig, opts.FingerprintPreset, []string{"h2"})
				if err != nil {
					return nil, err
				}
				if conn == nil {
					// Fallback to standard TLS (shouldn't happen if IsFingerprintEnabled returned true)
					return tls.Dial(network, addr, tlsConfig)
				}
				return conn, nil
			},
		}
	} else {
		// Provide DialTLSContext (rather than just TLSClientConfig) so the
		// underlying TCP socket gets our keepalive settings. tls.Dialer handles
		// the TCP dial + TLS handshake; the closure only adapts the signature
		// (http2 passes a per-host *tls.Config we don't need here).
		tlsDialer := &tls.Dialer{NetDialer: netDialer, Config: tlsConfig}
		h2Transport = &http2.Transport{
			AllowHTTP: false,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return tlsDialer.DialContext(ctx, network, addr)
			},
		}
	}

	// Create pipe for bidirectional streaming
	// Client writes to pipeWriter, server reads from pipeReader
	pipeReader, pipeWriter := io.Pipe()

	// Create the request with connection context (long-lived, not subject to dial timeout)
	req, err := http.NewRequestWithContext(connCtx, "POST", h2URL+path, pipeReader)
	if err != nil {
		dialCancel()
		connCancel()
		pipeWriter.Close()
		pipeReader.Close()
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")

	// Set custom protocol header if configured (empty string disables)
	httpHeader := opts.HTTPHeader
	if httpHeader == "" {
		httpHeader = DefaultHTTPHeader
	}
	alpnValue := opts.ALPNProtocol
	if alpnValue == "" {
		alpnValue = DefaultALPNProtocol
	}
	if httpHeader != "" {
		req.Header.Set(httpHeader, alpnValue)
	}

	// Execute request with dial timeout
	// We use a goroutine to monitor the dial timeout context
	type roundTripResult struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan roundTripResult, 1)

	go func() {
		resp, err := h2Transport.RoundTrip(req)
		resultCh <- roundTripResult{resp, err}
	}()

	var resp *http.Response
	select {
	case result := <-resultCh:
		dialCancel() // Dial completed, cancel dial timeout
		if result.err != nil {
			connCancel()
			pipeWriter.Close()
			pipeReader.Close()
			return nil, fmt.Errorf("HTTP/2 dial failed: %w", result.err)
		}
		resp = result.resp
	case <-dialCtx.Done():
		// Dial timeout - cancel the connection context to abort the request
		connCancel()
		dialCancel()
		pipeWriter.Close()
		pipeReader.Close()
		return nil, fmt.Errorf("HTTP/2 dial timeout: %w", dialCtx.Err())
	}

	if resp.StatusCode != http.StatusOK {
		connCancel()
		resp.Body.Close()
		pipeWriter.Close()
		pipeReader.Close()
		return nil, fmt.Errorf("HTTP/2 dial failed: status %d", resp.StatusCode)
	}

	return &H2PeerConn{
		reader:       resp.Body,
		writer:       pipeWriter,
		isDialer:     true,
		cancelDialFn: connCancel, // Cancel connection context on Close()
		h2Transport:  h2Transport,
	}, nil
}

// Listen creates an HTTP/2 listener.
func (t *H2Transport) Listen(addr string, opts ListenOptions) (Listener, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, fmt.Errorf("transport closed")
	}

	tlsConfig := opts.TLSConfig
	if tlsConfig == nil {
		return nil, fmt.Errorf("TLS config required for HTTP/2 listener")
	}

	// Ensure HTTP/2 is in NextProtos
	tlsConfig = ensureH2InNextProtos(tlsConfig)

	path := opts.Path
	if path == "" {
		path = h2DefaultPath
	}

	// Determine protocol identifiers
	httpHeader := opts.HTTPHeader
	if httpHeader == "" {
		httpHeader = DefaultHTTPHeader
	}
	alpnProtocol := opts.ALPNProtocol
	if alpnProtocol == "" {
		alpnProtocol = DefaultALPNProtocol
	}

	listener := &H2Listener{
		addr:         addr,
		path:         path,
		tlsConfig:    tlsConfig,
		httpHeader:   httpHeader,
		alpnProtocol: alpnProtocol,
		tcpKeepAlive: opts.TCPKeepAlive,
		connCh:       make(chan *H2PeerConn, 16),
		closeCh:      make(chan struct{}),
	}

	// Start HTTP/2 server
	if err := listener.start(); err != nil {
		return nil, err
	}

	t.listeners = append(t.listeners, listener)
	return listener, nil
}

// Close shuts down the transport and all listeners.
func (t *H2Transport) Close() error {
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

// H2Listener implements Listener for HTTP/2.
type H2Listener struct {
	addr         string
	path         string
	tlsConfig    *tls.Config
	httpHeader   string        // Custom protocol header name (empty to disable)
	alpnProtocol string        // Protocol identifier value
	tcpKeepAlive time.Duration // SO_KEEPALIVE period for accepted conns (0 disables)
	server       *http.Server
	netLn        net.Listener
	connCh       chan *H2PeerConn
	closeCh      chan struct{}
	closed       atomic.Bool
}

// start initializes the HTTP/2 server.
func (l *H2Listener) start() error {
	mux := http.NewServeMux()
	mux.HandleFunc(l.path, l.handleH2Stream)

	l.server = &http.Server{
		Addr:      l.addr,
		Handler:   mux,
		TLSConfig: l.tlsConfig,
	}

	// Configure HTTP/2
	http2.ConfigureServer(l.server, &http2.Server{})

	// Create TCP listener
	ln, err := net.Listen("tcp", l.addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	ln = wrapKeepAliveListener(ln, l.tcpKeepAlive)
	l.netLn = ln

	// Start serving in background with TLS
	go func() {
		tlsLn := tls.NewListener(ln, l.tlsConfig)
		l.server.Serve(tlsLn)
	}()

	return nil
}

// handleH2Stream handles incoming HTTP/2 streaming POST requests.
func (l *H2Listener) handleH2Stream(w http.ResponseWriter, r *http.Request) {
	// Check if we're closed
	if l.closed.Load() {
		http.Error(w, "server closed", http.StatusServiceUnavailable)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check protocol header (only if configured)
	if l.httpHeader != "" {
		proto := r.Header.Get(l.httpHeader)
		if proto != "" && proto != l.alpnProtocol {
			http.Error(w, "unsupported protocol", http.StatusBadRequest)
			return
		}
	}

	// Enable streaming response
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	// Set protocol header in response (only if configured)
	if l.httpHeader != "" {
		w.Header().Set(l.httpHeader, l.alpnProtocol)
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Create pipe for server-to-client writes
	// Server writes to pipeWriter, response body reads from pipeReader
	pipeReader, pipeWriter := io.Pipe()

	// Channel to signal data pump has stopped
	pumpDone := make(chan struct{})

	// Channel to signal pump error (write failure to client)
	pumpErr := make(chan error, 1)

	// Create peer connection with doneCh initialized to avoid race condition
	peerConn := &H2PeerConn{
		reader:     r.Body,
		writer:     pipeWriter,
		isDialer:   false,
		flusher:    flusher,
		respWriter: w,
		doneCh:     make(chan struct{}),
	}

	// Start goroutine to pump from pipe to response
	go func() {
		defer close(pumpDone)
		defer pipeReader.Close()
		buf := make([]byte, 32768)
		for {
			n, err := pipeReader.Read(buf)
			if err != nil {
				// Normal close - pipe writer was closed
				return
			}
			if n > 0 {
				_, writeErr := w.Write(buf[:n])
				if writeErr != nil {
					// Write to client failed - signal error and close the pipe writer
					// to ensure subsequent writes fail immediately
					select {
					case pumpErr <- writeErr:
					default:
					}
					pipeWriter.CloseWithError(writeErr)
					return
				}
				flusher.Flush()
			}
		}
	}()

	// Send to Accept channel
	select {
	case l.connCh <- peerConn:
		// Wait for either:
		// 1. Connection closed normally (doneCh)
		// 2. Pump error (write to client failed)
		// 3. Listener closed
		select {
		case <-peerConn.doneCh:
			// Normal close - connection was closed by peer manager
		case err := <-pumpErr:
			// Pump failed to write to client - close the connection
			// This will trigger handleDisconnect in the peer manager when
			// reads fail or the next write fails
			_ = err // Error already logged by pump
			peerConn.Close()
		case <-l.closeCh:
			// Listener closing
			peerConn.Close()
		}
		// Close the pipe writer to stop the pump goroutine
		pipeWriter.Close()
		// Wait for pump to finish before returning (avoids race with http2 handlerDone)
		<-pumpDone
	case <-l.closeCh:
		pipeWriter.Close()
		<-pumpDone
	}
}

// Accept waits for and returns the next HTTP/2 connection.
func (l *H2Listener) Accept(ctx context.Context) (PeerConn, error) {
	select {
	case conn := <-l.connCh:
		// doneCh is already initialized in handleH2Stream to avoid race condition
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closeCh:
		return nil, fmt.Errorf("listener closed")
	}
}

// Addr returns the listener's address.
func (l *H2Listener) Addr() net.Addr {
	if l.netLn != nil {
		return l.netLn.Addr()
	}
	return nil
}

// Close stops the listener.
func (l *H2Listener) Close() error {
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

// H2PeerConn implements PeerConn for HTTP/2.
// Since HTTP/2 streaming uses a single request/response pair,
// this connection provides a single "stream" that the peer manager
// uses as its control stream.
type H2PeerConn struct {
	reader       io.ReadCloser
	writer       io.WriteCloser
	isDialer     bool
	flusher      http.Flusher
	respWriter   http.ResponseWriter
	streamOnce   sync.Once
	stream       *H2Stream
	closed       atomic.Bool
	doneCh       chan struct{}
	cancelDialFn context.CancelFunc // Cancel function for dial context (client only)
	// h2Transport is the per-dial *http2.Transport (client only). Without an
	// explicit CloseIdleConnections in Close, the TCP socket lingers in its
	// pool until GC + finalizer -- a real leak under reconnect churn.
	h2Transport *http2.Transport
}

// OpenStream returns the single HTTP/2 stream.
func (c *H2PeerConn) OpenStream(ctx context.Context) (Stream, error) {
	c.streamOnce.Do(func() {
		c.stream = &H2Stream{
			reader: c.reader,
			writer: c.writer,
			id:     1,
		}
	})
	return c.stream, nil
}

// AcceptStream returns the single HTTP/2 stream.
func (c *H2PeerConn) AcceptStream(ctx context.Context) (Stream, error) {
	c.streamOnce.Do(func() {
		c.stream = &H2Stream{
			reader: c.reader,
			writer: c.writer,
			id:     1,
		}
	})
	return c.stream, nil
}

// Close terminates the HTTP/2 connection.
func (c *H2PeerConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	if c.doneCh != nil {
		close(c.doneCh)
	}

	// Cancel the dial context to clean up HTTP/2 resources
	if c.cancelDialFn != nil {
		c.cancelDialFn()
	}

	var err error
	if c.writer != nil {
		if closeErr := c.writer.Close(); closeErr != nil {
			err = closeErr
		}
	}
	if c.reader != nil {
		if closeErr := c.reader.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if c.h2Transport != nil {
		c.h2Transport.CloseIdleConnections()
	}
	return err
}

// LocalAddr returns the local address (not available for HTTP/2).
func (c *H2PeerConn) LocalAddr() net.Addr {
	return nil
}

// RemoteAddr returns the remote address (not available for HTTP/2).
func (c *H2PeerConn) RemoteAddr() net.Addr {
	return nil
}

// IsDialer returns true if this side initiated the connection.
func (c *H2PeerConn) IsDialer() bool {
	return c.isDialer
}

// TransportType returns the transport protocol type.
func (c *H2PeerConn) TransportType() TransportType {
	return TransportHTTP2
}

// H2Stream implements Stream for HTTP/2.
type H2Stream struct {
	reader  io.ReadCloser
	writer  io.WriteCloser
	id      uint64
	writeMu sync.Mutex // Protects concurrent writes
	closed  atomic.Bool
}

// StreamID returns the stream ID.
func (s *H2Stream) StreamID() uint64 {
	return s.id
}

// Read reads data from the HTTP/2 stream.
func (s *H2Stream) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

// Write writes data to the HTTP/2 stream.
func (s *H2Stream) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, fmt.Errorf("stream closed")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writer.Write(p)
}

// CloseWrite signals end of writes.
func (s *H2Stream) CloseWrite() error {
	return nil // HTTP/2 streaming doesn't support half-close in this model
}

// Close fully closes the stream.
func (s *H2Stream) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	var err error
	if s.writer != nil {
		if closeErr := s.writer.Close(); closeErr != nil {
			err = closeErr
		}
	}
	if s.reader != nil {
		if closeErr := s.reader.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// SetDeadline sets read and write deadlines.
func (s *H2Stream) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline sets the read deadline.
func (s *H2Stream) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline sets the write deadline.
func (s *H2Stream) SetWriteDeadline(t time.Time) error {
	return nil
}

// parseH2Address parses the address into HTTP/2 URL components.
func parseH2Address(addr string) (baseURL, path string) {
	// Check for URL schemes
	var hostStart int
	switch {
	case strings.HasPrefix(addr, "https://"):
		hostStart = 8
	case strings.HasPrefix(addr, "http://"):
		hostStart = 7
	default:
		// Bare host:port - use https
		return "https://" + addr, h2DefaultPath
	}

	// Find path separator after the scheme
	pathIdx := strings.Index(addr[hostStart:], "/")
	if pathIdx == -1 {
		return addr, h2DefaultPath
	}
	pathIdx += hostStart
	return addr[:pathIdx], addr[pathIdx:]
}
