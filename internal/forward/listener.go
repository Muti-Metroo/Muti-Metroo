package forward

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/recovery"
)

// ListenerConfig holds listener configuration.
type ListenerConfig struct {
	// Key is the routing key to look up in the mesh.
	Key string

	// Address is the local address to listen on.
	Address string

	// MaxConnections limits concurrent connections (0 = unlimited).
	MaxConnections int

	// Logger for logging.
	Logger *slog.Logger
}

// Listener is a TCP listener that forwards connections to port forward routes.
type Listener struct {
	cfg      ListenerConfig
	dialer   ForwardDialer
	listener net.Listener
	logger   *slog.Logger

	mu          sync.Mutex
	connections map[net.Conn]struct{}
	connCount   atomic.Int64

	running  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewListener creates a new port forward listener.
func NewListener(cfg ListenerConfig, dialer ForwardDialer) *Listener {
	logger := cfg.Logger
	if logger == nil {
		logger = logging.NopLogger()
	}

	return &Listener{
		cfg:         cfg,
		dialer:      dialer,
		logger:      logger,
		connections: make(map[net.Conn]struct{}),
		stopCh:      make(chan struct{}),
	}
}

// Start starts the tunnel listener.
func (l *Listener) Start() error {
	if l.running.Load() {
		return fmt.Errorf("listener already running")
	}

	listener, err := net.Listen("tcp", l.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", l.cfg.Address, err)
	}

	l.listener = listener
	l.running.Store(true)

	l.wg.Add(1)
	go l.acceptLoop()

	l.logger.Info("forward listener started",
		"key", l.cfg.Key,
		"address", l.listener.Addr().String())

	return nil
}

// Stop gracefully stops the listener.
func (l *Listener) Stop() error {
	var err error
	l.stopOnce.Do(func() {
		l.running.Store(false)
		close(l.stopCh)

		// Close listener
		if l.listener != nil {
			err = l.listener.Close()
		}

		// Close all active connections
		l.mu.Lock()
		for conn := range l.connections {
			conn.Close()
		}
		l.mu.Unlock()

		l.logger.Info("forward listener stopped",
			"key", l.cfg.Key)
	})

	// Wait for all goroutines to finish
	l.wg.Wait()
	return err
}

// Address returns the listening address.
func (l *Listener) Address() net.Addr {
	if l.listener == nil {
		return nil
	}
	return l.listener.Addr()
}

// Key returns the routing key for this listener.
func (l *Listener) Key() string {
	return l.cfg.Key
}

// ConnectionCount returns the number of active connections.
func (l *Listener) ConnectionCount() int64 {
	return l.connCount.Load()
}

// MaxConnections returns the configured maximum connections (0 = unlimited).
func (l *Listener) MaxConnections() int {
	return l.cfg.MaxConnections
}

// acceptLoop accepts incoming connections.
func (l *Listener) acceptLoop() {
	defer l.wg.Done()
	defer recovery.RecoverWithLog(l.logger, "forward.Listener.acceptLoop")

	for {
		conn, err := l.listener.Accept()
		if err != nil {
			select {
			case <-l.stopCh:
				return
			default:
				l.logger.Debug("accept error",
					"key", l.cfg.Key,
					logging.KeyError, err)
				continue
			}
		}

		// Check connection limit
		if l.cfg.MaxConnections > 0 && l.connCount.Load() >= int64(l.cfg.MaxConnections) {
			l.logger.Debug("connection limit reached",
				"key", l.cfg.Key,
				"limit", l.cfg.MaxConnections)
			conn.Close()
			continue
		}

		// Track connection
		l.mu.Lock()
		l.connections[conn] = struct{}{}
		l.mu.Unlock()
		l.connCount.Add(1)

		// Handle connection
		l.wg.Add(1)
		go l.handleConnection(conn)
	}
}

// handleConnection handles a single incoming connection.
func (l *Listener) handleConnection(conn net.Conn) {
	defer l.wg.Done()
	defer recovery.RecoverWithLog(l.logger, "forward.Listener.handleConnection")
	defer func() {
		conn.Close()
		l.mu.Lock()
		delete(l.connections, conn)
		l.mu.Unlock()
		l.connCount.Add(-1)
	}()

	remoteAddr := conn.RemoteAddr().String()
	l.logger.Debug("new forward connection",
		"key", l.cfg.Key,
		"remote", remoteAddr)

	// Create context for dialing (cancellable if we stop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel dial if we're stopping
	go func() {
		select {
		case <-l.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Dial through the mesh to the port forward exit
	target, err := l.dialer.DialForward(ctx, l.cfg.Key)
	if err != nil {
		l.logger.Debug("dial forward failed",
			"key", l.cfg.Key,
			"remote", remoteAddr,
			logging.KeyError, err)
		return
	}
	defer target.Close()

	l.logger.Debug("forward connected",
		"key", l.cfg.Key,
		"remote", remoteAddr)

	// Relay data bidirectionally
	relay(conn, target)

	l.logger.Debug("forward connection closed",
		"key", l.cfg.Key,
		"remote", remoteAddr)
}

// halfCloser is implemented by connections that support half-close.
type halfCloser interface {
	CloseWrite() error
}

// relay copies data bidirectionally between two connections.
// Supports half-close for graceful shutdown.
func relay(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Target
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, client)
		// Signal we're done writing
		if hc, ok := target.(halfCloser); ok {
			hc.CloseWrite()
		}
	}()

	// Target -> Client
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, target)
		// Signal we're done writing
		if hc, ok := client.(halfCloser); ok {
			hc.CloseWrite()
		}
	}()

	wg.Wait()
}
