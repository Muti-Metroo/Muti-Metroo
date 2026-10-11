package socks5

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// WebSocketConfig configures the WebSocket SOCKS5 listener.
type WebSocketConfig struct {
	// Address to listen on (e.g., "0.0.0.0:8443" or "127.0.0.1:8081")
	Address string

	// Path for WebSocket upgrade (default: "/socks5")
	Path string

	// TLSConfig for TLS termination (nil requires PlainText: true)
	TLSConfig *tls.Config

	// PlainText allows running without TLS (for reverse proxy mode)
	PlainText bool

	// Credentials for HTTP Basic Auth validation before WebSocket upgrade.
	// If nil, no authentication is required at the HTTP level.
	// Uses the same credential store as SOCKS5 authentication.
	Credentials CredentialStore

	// OnError is called when the server encounters an error after starting.
	// This is optional - if nil, errors are silently ignored.
	OnError func(err error)
}

// splashPageTemplate is a minimal HTML page served at "/" to make the endpoint
// look like a normal web page. Does not include dashboard link.
const splashPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Muti Metroo</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            background: linear-gradient(135deg, #1a1a2e 0%%, #16213e 100%%);
            color: #e4e4e7;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .container {
            text-align: center;
            padding: 40px 20px;
            max-width: 480px;
        }
        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            margin-bottom: 8px;
            color: #ffffff;
        }
        .tagline {
            font-size: 1.1rem;
            color: #a1a1aa;
            margin-bottom: 16px;
        }
        .description {
            font-size: 0.95rem;
            color: #71717a;
            line-height: 1.6;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Muti Metroo</h1>
        <p class="tagline">Userspace Mesh Networking Agent</p>
        <p class="description">End-to-end encrypted tunnels across heterogeneous transports with multi-hop routing.</p>
    </div>
</body>
</html>
`

// WebSocketListener accepts SOCKS5 connections over WebSocket.
type WebSocketListener struct {
	cfg     WebSocketConfig
	handler *Handler
	server  *http.Server

	// Actual listener address (set after binding)
	addr net.Addr

	tracker *connTracker[*wsConn]

	running atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewWebSocketListener creates a new WebSocket SOCKS5 listener.
func NewWebSocketListener(cfg WebSocketConfig, handler *Handler) (*WebSocketListener, error) {
	if cfg.TLSConfig == nil && !cfg.PlainText {
		return nil, fmt.Errorf("TLS config required (use PlainText: true for reverse proxy mode)")
	}

	if cfg.Path == "" {
		cfg.Path = "/socks5"
	}

	return &WebSocketListener{
		cfg:     cfg,
		handler: handler,
		tracker: newConnTracker[*wsConn](),
		stopCh:  make(chan struct{}),
	}, nil
}

// Start starts the WebSocket listener.
func (l *WebSocketListener) Start() error {
	if l.running.Load() {
		return fmt.Errorf("listener already running")
	}

	mux := http.NewServeMux()

	// Serve splash page at root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, splashPageTemplate)
	})

	// WebSocket upgrade handler for SOCKS5
	mux.HandleFunc(l.cfg.Path, l.handleWebSocket)

	l.server = &http.Server{
		Addr:      l.cfg.Address,
		Handler:   mux,
		TLSConfig: l.cfg.TLSConfig,
	}

	// Start server
	ln, err := net.Listen("tcp", l.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	l.addr = ln.Addr()
	l.running.Store(true)

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()

		var serveErr error
		if l.cfg.TLSConfig != nil {
			serveErr = l.server.ServeTLS(ln, "", "")
		} else {
			serveErr = l.server.Serve(ln)
		}

		if serveErr != nil && serveErr != http.ErrServerClosed {
			// Report error via callback if configured
			if l.cfg.OnError != nil {
				l.cfg.OnError(serveErr)
			}
		}
	}()

	return nil
}

// Stop gracefully stops the listener.
func (l *WebSocketListener) Stop() error {
	if !l.running.Swap(false) {
		return nil
	}

	close(l.stopCh)

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l.server.Shutdown(ctx)

	// Close all active connections
	l.tracker.closeAll()

	l.wg.Wait()
	return nil
}

// Address returns the actual listening address.
func (l *WebSocketListener) Address() string {
	if l.addr != nil {
		return l.addr.String()
	}
	return l.cfg.Address
}

// ConnectionCount returns the number of active WebSocket SOCKS5 connections.
func (l *WebSocketListener) ConnectionCount() int64 {
	return l.tracker.count()
}

// IsRunning returns true if the listener is running.
func (l *WebSocketListener) IsRunning() bool {
	return l.running.Load()
}

// handleWebSocket handles WebSocket upgrade and SOCKS5 protocol.
// Important: This function blocks until the WebSocket connection closes.
// The nhooyr.io/websocket library expects the HTTP handler to remain active
// for the lifetime of the WebSocket connection.
func (l *WebSocketListener) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Validate HTTP Basic Auth if credentials are configured
	if l.cfg.Credentials != nil {
		username, password, ok := r.BasicAuth()
		if !ok || !l.cfg.Credentials.Valid(username, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="SOCKS5 Proxy"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Accept WebSocket connection with socks5 subprotocol
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"socks5"},
	})
	if err != nil {
		return
	}

	// Verify client negotiated the socks5 subprotocol for protocol strictness.
	// Reject connections that don't speak the expected protocol.
	if conn.Subprotocol() != "socks5" {
		conn.Close(websocket.StatusProtocolError, "socks5 subprotocol required")
		return
	}

	// Wrap as net.Conn
	wc := newWsConn(conn)

	l.tracker.add(wc)
	l.wg.Add(1)

	// Handle connection directly in this goroutine - DO NOT spawn a new goroutine.
	// Each HTTP request already has its own goroutine from net/http, and
	// returning from this handler before the WebSocket is done can cause
	// the connection to be prematurely closed.
	defer l.wg.Done()
	defer l.tracker.remove(wc)
	defer wc.Close()

	// Handle SOCKS5 protocol
	l.handler.Handle(wc)
}

// wsConn wraps websocket.Conn to implement net.Conn.
type wsConn struct {
	conn       *websocket.Conn
	baseCtx    context.Context
	baseCancel context.CancelFunc

	mu             sync.RWMutex
	deadline       time.Time
	deadlineCtx    context.Context
	deadlineCancel context.CancelFunc

	readMu sync.Mutex
	reader io.Reader
}

// newWsConn creates a new wsConn wrapper.
func newWsConn(conn *websocket.Conn) *wsConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &wsConn{
		conn:       conn,
		baseCtx:    ctx,
		baseCancel: cancel,
	}
}

// getContext returns a context for the current operation, respecting any deadline.
func (c *wsConn) getContext() context.Context {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.deadlineCtx != nil {
		return c.deadlineCtx
	}
	return c.baseCtx
}

// Read reads data from the WebSocket connection.
func (c *wsConn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	// If we have a partial message buffered, read from it first
	if c.reader != nil {
		n, err := c.reader.Read(b)
		if err == io.EOF {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			// Fall through to read next message
		} else {
			return n, err
		}
	}

	ctx := c.getContext()

	// Read next WebSocket message
	msgType, reader, err := c.conn.Reader(ctx)
	if err != nil {
		return 0, c.translateError(err)
	}

	if msgType != websocket.MessageBinary {
		return 0, fmt.Errorf("unexpected message type: %v", msgType)
	}

	// Try to read directly into the provided buffer
	n, err := reader.Read(b)
	if err == io.EOF {
		return n, nil
	}
	if err != nil {
		return n, err
	}

	// If the message is larger than the buffer, save the reader for next call
	c.reader = reader
	return n, nil
}

// Write writes data as a binary WebSocket message.
func (c *wsConn) Write(b []byte) (int, error) {
	ctx := c.getContext()
	err := c.conn.Write(ctx, websocket.MessageBinary, b)
	if err != nil {
		return 0, c.translateError(err)
	}
	return len(b), nil
}

// Close closes the WebSocket connection.
func (c *wsConn) Close() error {
	c.mu.Lock()
	if c.deadlineCancel != nil {
		c.deadlineCancel()
	}
	c.mu.Unlock()

	c.baseCancel()
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

// NoDeadlineMonitor returns true to indicate that WebSocket connections don't
// support deadline-based polling for disconnect detection. The underlying
// nhooyr.io/websocket library closes the connection when read context is canceled,
// which breaks the polling pattern used by the SOCKS5 handler.
func (c *wsConn) NoDeadlineMonitor() bool {
	return true
}

// LocalAddr returns nil as the underlying WebSocket library does not expose
// the local TCP address. Code that uses LocalAddr() should handle nil gracefully.
// The SOCKS5 handler uses type assertions that safely handle nil values.
func (c *wsConn) LocalAddr() net.Addr {
	return nil
}

// RemoteAddr returns nil as the underlying WebSocket library does not expose
// the remote TCP address. Code that uses RemoteAddr() should handle nil gracefully.
// For logging purposes, the HTTP request's RemoteAddr can be used at accept time.
func (c *wsConn) RemoteAddr() net.Addr {
	return nil
}

// SetDeadline sets both read and write deadlines.
func (c *wsConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Cancel any existing deadline context
	if c.deadlineCancel != nil {
		c.deadlineCancel()
		c.deadlineCancel = nil
		c.deadlineCtx = nil
	}

	c.deadline = t

	// Create new deadline context if deadline is set
	if !t.IsZero() {
		c.deadlineCtx, c.deadlineCancel = context.WithDeadline(c.baseCtx, t)
	}

	return nil
}

// SetReadDeadline delegates to SetDeadline.
func (c *wsConn) SetReadDeadline(t time.Time) error { return c.SetDeadline(t) }

// SetWriteDeadline delegates to SetDeadline.
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

// wsTimeoutError implements net.Error for WebSocket deadline timeouts.
type wsTimeoutError struct {
	err error
}

func (e *wsTimeoutError) Error() string { return e.err.Error() }
func (e *wsTimeoutError) Timeout() bool { return true }

// Temporary is deprecated but required by net.Error interface.
func (e *wsTimeoutError) Temporary() bool { return false }

// translateError converts WebSocket-specific errors to standard net errors.
func (c *wsConn) translateError(err error) error {
	if websocket.CloseStatus(err) != -1 {
		return io.EOF
	}
	// Convert context deadline/cancel errors to net.Error timeouts.
	// This is needed for compatibility with code that checks net.Error.Timeout().
	// Both DeadlineExceeded and Canceled are treated as timeouts because:
	// - DeadlineExceeded: deadline was reached
	// - Canceled: can happen due to internal context management when deadline expires
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &wsTimeoutError{err: err}
	}
	return err
}
