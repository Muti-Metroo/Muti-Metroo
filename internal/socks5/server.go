package socks5

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ServerConfig holds server configuration.
type ServerConfig struct {
	// Address to listen on (e.g., "127.0.0.1:1080")
	Address string

	// MaxConnections limits concurrent connections (0 = unlimited)
	MaxConnections int

	// ConnectTimeout for outbound connections
	ConnectTimeout time.Duration

	// IdleTimeout for idle connections
	IdleTimeout time.Duration

	// Authenticators for authentication
	Authenticators []Authenticator

	// Dialer for making outbound connections
	Dialer Dialer
}

// DefaultServerConfig returns sensible defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Address:        "127.0.0.1:1080",
		MaxConnections: 1000,
		ConnectTimeout: 30 * time.Second,
		IdleTimeout:    5 * time.Minute,
		Authenticators: []Authenticator{&NoAuthAuthenticator{}},
		Dialer:         &DirectDialer{},
	}
}

// Server is a SOCKS5 proxy server.
type Server struct {
	cfg      ServerConfig
	handler  *Handler
	listener net.Listener

	// WebSocket listener (optional)
	wsListener *WebSocketListener

	tracker *connTracker[net.Conn]

	running atomic.Bool
	mu      sync.Mutex // Protects listener and stopCh
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewServer creates a new SOCKS5 server.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Dialer == nil {
		cfg.Dialer = &DirectDialer{}
	}
	if len(cfg.Authenticators) == 0 {
		cfg.Authenticators = []Authenticator{&NoAuthAuthenticator{}}
	}

	return &Server{
		cfg:     cfg,
		handler: NewHandler(cfg.Authenticators, cfg.Dialer),
		tracker: newConnTracker[net.Conn](),
		stopCh:  make(chan struct{}),
	}
}

// Start starts the SOCKS5 server.
func (s *Server) Start() error {
	if s.running.Load() {
		return fmt.Errorf("server already running")
	}

	listener, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Create a new stopCh for this run (supports restart after Stop)
	s.mu.Lock()
	s.listener = listener
	s.stopCh = make(chan struct{})
	s.mu.Unlock()
	s.running.Store(true)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop gracefully stops the server.
func (s *Server) Stop() error {
	if !s.running.Load() {
		return nil
	}

	s.running.Store(false)

	// Close stopCh to signal accept loops
	s.mu.Lock()
	if s.stopCh != nil {
		select {
		case <-s.stopCh:
			// Already closed
		default:
			close(s.stopCh)
		}
	}

	// Close listener
	var err error
	if s.listener != nil {
		err = s.listener.Close()
		s.listener = nil
	}
	s.mu.Unlock()

	// Stop WebSocket listener if running
	if s.wsListener != nil {
		s.wsListener.Stop()
	}

	// Close all active connections
	s.tracker.closeAll()

	// Wait for all goroutines to finish
	s.wg.Wait()
	return err
}

// StopWithContext stops with a timeout.
func (s *Server) StopWithContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- s.Stop()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Address returns the listening address.
func (s *Server) Address() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// ConnectionCount returns the number of active connections.
func (s *Server) ConnectionCount() int64 {
	return s.tracker.count()
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	return s.running.Load()
}

// SetUDPHandler sets the UDP association handler.
// This enables SOCKS5 UDP ASSOCIATE support.
func (s *Server) SetUDPHandler(handler UDPAssociationHandler) {
	s.handler.SetUDPHandler(handler)

	// Set the UDP bind IP from the server's configured address
	// This ensures UDP relay sockets bind to the same interface as the TCP listener
	if host, _, err := net.SplitHostPort(s.cfg.Address); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			s.handler.SetUDPBindIP(ip)
		}
	}
}

// SetICMPHandler sets the ICMP echo handler.
// This enables SOCKS5 ICMP ECHO support (custom command 0x04).
func (s *Server) SetICMPHandler(handler ICMPHandler) {
	s.handler.SetICMPHandler(handler)
}

// SetRouteWatchHandler sets the route-watch provider that backs the
// CmdRouteWatch (0x05) and CmdSnapshot (0x06) commands, enabling dashboard
// snapshot push over the SOCKS5 endpoint.
func (s *Server) SetRouteWatchHandler(provider RouteWatchProvider) {
	s.handler.SetRouteWatchHandler(provider)
}

// SetExposeHandler sets the handler that backs the CmdExpose (0x07) command,
// enabling expose sessions over the SOCKS5 endpoint.
func (s *Server) SetExposeHandler(handler ExposeOverSocks) {
	s.handler.SetExposeHandler(handler)
}

// StartWebSocket starts a WebSocket listener for SOCKS5 connections.
// This allows SOCKS5 protocol to be tunneled over WebSocket transport.
func (s *Server) StartWebSocket(cfg WebSocketConfig) error {
	if s.wsListener != nil && s.wsListener.IsRunning() {
		return fmt.Errorf("WebSocket listener already running")
	}

	listener, err := NewWebSocketListener(cfg, s.handler)
	if err != nil {
		return fmt.Errorf("create WebSocket listener: %w", err)
	}

	if err := listener.Start(); err != nil {
		return fmt.Errorf("start WebSocket listener: %w", err)
	}

	s.wsListener = listener
	return nil
}

// StopWebSocket stops the WebSocket listener if running.
func (s *Server) StopWebSocket() error {
	if s.wsListener == nil {
		return nil
	}
	return s.wsListener.Stop()
}

// WebSocketAddress returns the WebSocket listener address, or empty if not running.
func (s *Server) WebSocketAddress() string {
	if s.wsListener == nil || !s.wsListener.IsRunning() {
		return ""
	}
	return s.wsListener.Address()
}

// WebSocketConnectionCount returns the number of active WebSocket SOCKS5 connections.
func (s *Server) WebSocketConnectionCount() int64 {
	if s.wsListener == nil {
		return 0
	}
	return s.wsListener.ConnectionCount()
}

// acceptLoop accepts new connections.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	// Capture listener and stopCh at start to handle server restart properly
	s.mu.Lock()
	listener := s.listener
	stopCh := s.stopCh
	s.mu.Unlock()

	if listener == nil {
		return
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
				// Log error and continue
				continue
			}
		}

		// Check connection limit
		if s.cfg.MaxConnections > 0 && s.tracker.count() >= int64(s.cfg.MaxConnections) {
			conn.Close()
			continue
		}

		s.tracker.add(conn)
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn handles a single connection.
func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer s.tracker.remove(conn)
	defer conn.Close()

	// Set timeouts
	if s.cfg.IdleTimeout > 0 {
		conn.SetDeadline(time.Now().Add(s.cfg.IdleTimeout))
	}

	// Handle the connection
	s.handler.Handle(conn)
}

// WithAuthenticators returns a new server config with authenticators.
func (cfg ServerConfig) WithAuthenticators(auths ...Authenticator) ServerConfig {
	cfg.Authenticators = auths
	return cfg
}

// WithDialer returns a new server config with a custom dialer.
func (cfg ServerConfig) WithDialer(dialer Dialer) ServerConfig {
	cfg.Dialer = dialer
	return cfg
}

// WithMaxConnections returns a new server config with max connections.
func (cfg ServerConfig) WithMaxConnections(max int) ServerConfig {
	cfg.MaxConnections = max
	return cfg
}
