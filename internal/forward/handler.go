package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/logging"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/recovery"
)

// HandlerConfig contains tunnel exit handler configuration.
type HandlerConfig struct {
	// Endpoints define tunnel exit points - key to target mappings.
	Endpoints []Endpoint

	// ConnectTimeout for outbound connections.
	ConnectTimeout time.Duration

	// IdleTimeout for idle connections.
	IdleTimeout time.Duration

	// MaxConnections limits concurrent connections.
	MaxConnections int

	// Logger for logging.
	Logger *slog.Logger
}

// DefaultHandlerConfig returns sensible defaults.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		ConnectTimeout: 30 * time.Second,
		IdleTimeout:    5 * time.Minute,
		MaxConnections: 1000,
	}
}

// Backend represents the local-side endpoint that consumes and produces
// plaintext bytes for an exit-side forward stream. The stream's E2E crypto is
// performed by the Handler; backends only see plaintext.
type Backend interface {
	// Open establishes the local endpoint for a new stream. The returned RWC
	// is the plaintext side of the tunnel: bytes read from it are encrypted
	// and sent upstream; bytes written to it have been decrypted from
	// upstream. On failure the returned error should implement
	// protocolErrorCoder to surface a protocol error code; otherwise
	// ErrGeneralFailure is used.
	Open(ctx context.Context, streamID uint64, key, remoteAddr string) (io.ReadWriteCloser, error)

	// Target returns a human-readable label for the endpoint (e.g. host:port
	// for TCP backends, "expose:<key>" for expose sessions).
	// Used for logging, dashboards, and route advertisements.
	Target() string
}

// halfCloseWriter is an optional capability some backends implement to
// propagate the upstream FIN_WRITE signal to the local endpoint without
// tearing down the read side. *net.TCPConn satisfies it natively.
type halfCloseWriter interface {
	CloseWrite() error
}

// localAddrer is an optional capability used to populate the BoundIP/BoundPort
// fields of STREAM_OPEN_ACK. *net.TCPConn satisfies it natively; ws-backed
// streams typically do not.
type localAddrer interface {
	LocalAddr() net.Addr
}

// readDeadliner is an optional capability used by the read loop to enforce
// IdleTimeout. *net.TCPConn satisfies it natively.
type readDeadliner interface {
	SetReadDeadline(t time.Time) error
}

// ErrKeyInUse is returned by AddBackend when the key is already registered.
var ErrKeyInUse = errors.New("forward key already in use")

// ActiveConnection represents an active tunnel connection.
type ActiveConnection struct {
	StreamID   uint64
	RemoteID   identity.AgentID
	Key        string
	Target     string
	Conn       io.ReadWriteCloser
	StartedAt  time.Time
	closed     atomic.Bool
	closeOnce  sync.Once
	sessionKey *crypto.SessionKey // E2E encryption session key
}

// Close closes the connection.
func (ac *ActiveConnection) Close() error {
	var err error
	ac.closeOnce.Do(func() {
		ac.closed.Store(true)
		if ac.Conn != nil {
			err = ac.Conn.Close()
		}
	})
	return err
}

// IsClosed returns true if the connection is closed.
func (ac *ActiveConnection) IsClosed() bool {
	return ac.closed.Load()
}

// streamKey identifies a stream by the peer that opened it as well as its ID.
// Stream IDs are allocated per peer connection, so the ID alone is not unique
// across peers: two agents forwarding through this endpoint both open stream 1,
// and keying on the ID alone would let the second displace the first and let
// either close or reset the other's stream by quoting a guessable ID.
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// Handler handles tunnel exit connections.
type Handler struct {
	cfg     HandlerConfig
	localID identity.AgentID
	writer  StreamWriter
	logger  *slog.Logger

	// targetsMu guards backends so runtime mutations (AddBackend/RemoveBackend)
	// can race with stream dispatch (GetTarget) without tearing the map.
	targetsMu sync.RWMutex
	backends  map[string]Backend

	mu          sync.RWMutex
	connections map[streamKey]*ActiveConnection
	connCount   atomic.Int64

	running  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewHandler creates a new tunnel exit handler.
func NewHandler(cfg HandlerConfig, localID identity.AgentID, writer StreamWriter) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = logging.NopLogger()
	}

	backends := make(map[string]Backend)
	for _, ep := range cfg.Endpoints {
		backends[ep.Key] = NewTCPBackend(ep.Target, cfg.ConnectTimeout)
	}

	return &Handler{
		cfg:         cfg,
		localID:     localID,
		writer:      writer,
		logger:      logger,
		backends:    backends,
		connections: make(map[streamKey]*ActiveConnection),
		stopCh:      make(chan struct{}),
	}
}

// Start starts the tunnel exit handler.
func (h *Handler) Start() {
	h.running.Store(true)
}

// Stop stops the tunnel exit handler.
func (h *Handler) Stop() {
	h.stopOnce.Do(func() {
		h.running.Store(false)
		close(h.stopCh)

		// Close all connections
		h.mu.Lock()
		for _, conn := range h.connections {
			conn.Close()
		}
		h.connections = make(map[streamKey]*ActiveConnection)
		h.connCount.Store(0)
		h.mu.Unlock()
	})
}

// IsRunning returns true if the handler is running.
func (h *Handler) IsRunning() bool {
	return h.running.Load()
}

// GetBackend returns the backend registered for a routing key.
func (h *Handler) GetBackend(key string) (Backend, bool) {
	h.targetsMu.RLock()
	defer h.targetsMu.RUnlock()
	b, ok := h.backends[key]
	return b, ok
}

// GetTarget returns the target label for a routing key.
func (h *Handler) GetTarget(key string) (string, bool) {
	h.targetsMu.RLock()
	defer h.targetsMu.RUnlock()
	b, ok := h.backends[key]
	if !ok {
		return "", false
	}
	return b.Target(), true
}

// GetKeys returns all configured routing keys.
func (h *Handler) GetKeys() []string {
	h.targetsMu.RLock()
	defer h.targetsMu.RUnlock()
	keys := make([]string, 0, len(h.backends))
	for k := range h.backends {
		keys = append(keys, k)
	}
	return keys
}

// AddBackend registers a backend for the given routing key. Returns
// ErrKeyInUse if the key is already registered; callers that want to replace
// silently should use ReplaceBackend.
func (h *Handler) AddBackend(key string, b Backend) error {
	return h.installBackend(key, b, false)
}

// ReplaceBackend registers a backend for the given routing key, silently
// overwriting any existing entry. Used for config-sourced and journaled
// dynamic endpoints, where higher layers already enforce ownership rules.
func (h *Handler) ReplaceBackend(key string, b Backend) error {
	return h.installBackend(key, b, true)
}

func (h *Handler) installBackend(key string, b Backend, replace bool) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if b == nil {
		return fmt.Errorf("backend is required")
	}
	h.targetsMu.Lock()
	defer h.targetsMu.Unlock()
	if _, exists := h.backends[key]; exists && !replace {
		return ErrKeyInUse
	}
	h.backends[key] = b
	return nil
}

// RemoveBackend deletes a routing key. Returns true if the key existed.
func (h *Handler) RemoveBackend(key string) bool {
	h.targetsMu.Lock()
	defer h.targetsMu.Unlock()
	if _, ok := h.backends[key]; !ok {
		return false
	}
	delete(h.backends, key)
	return true
}

// AddTarget registers a TCP target for the given routing key, replacing any
// existing entry. Wrapper for backwards compatibility.
func (h *Handler) AddTarget(key, target string) error {
	if target == "" {
		return fmt.Errorf("target is required")
	}
	return h.ReplaceBackend(key, NewTCPBackend(target, h.cfg.ConnectTimeout))
}

// RemoveTarget deletes a routing key. Wrapper for backwards compatibility.
func (h *Handler) RemoveTarget(key string) bool {
	return h.RemoveBackend(key)
}

// Targets returns a copy of the current key -> target-label map.
func (h *Handler) Targets() map[string]string {
	h.targetsMu.RLock()
	defer h.targetsMu.RUnlock()
	out := make(map[string]string, len(h.backends))
	for k, b := range h.backends {
		out[k] = b.Target()
	}
	return out
}

// HandleStreamOpen processes a tunnel STREAM_OPEN request.
// The backend Open is performed asynchronously to avoid blocking the frame
// processing loop.
func (h *Handler) HandleStreamOpen(ctx context.Context, streamID uint64, requestID uint64, remoteID identity.AgentID, key string, remoteEphemeralPub [crypto.KeySize]byte) error {
	if !h.running.Load() {
		return fmt.Errorf("handler not running")
	}

	// Check connection limit
	if h.cfg.MaxConnections > 0 && h.connCount.Load() >= int64(h.cfg.MaxConnections) {
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrConnectionLimit, "connection limit exceeded")
		return fmt.Errorf("connection limit exceeded")
	}

	// Look up backend for this routing key
	backend, ok := h.GetBackend(key)
	if !ok {
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrForwardNotFound, "forward key not found")
		return fmt.Errorf("forward key not found: %s", key)
	}

	// Perform the rest asynchronously to avoid blocking the frame processing loop.
	go h.handleStreamOpenAsync(ctx, streamID, requestID, remoteID, key, backend, remoteEphemeralPub)

	return nil
}

// handleStreamOpenAsync performs the actual stream open work asynchronously.
func (h *Handler) handleStreamOpenAsync(ctx context.Context, streamID uint64, requestID uint64, remoteID identity.AgentID, key string, backend Backend, remoteEphemeralPub [crypto.KeySize]byte) {
	defer recovery.RecoverWithLog(h.logger, "forward.Handler.handleStreamOpenAsync")

	target := backend.Target()

	// Open the local endpoint first. If this fails, we never derive a
	// session key or send STREAM_OPEN_ACK.
	conn, err := backend.Open(ctx, streamID, key, remoteID.String())
	if err != nil {
		h.sendOpenErr(remoteID, streamID, requestID, h.mapDialError(err), err.Error())
		return
	}

	// Generate ephemeral keypair for E2E encryption key exchange.
	ephPriv, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		conn.Close()
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrGeneralFailure, "key generation failed")
		return
	}

	// Compute shared secret from ECDH.
	sharedSecret, err := crypto.ComputeECDH(ephPriv, remoteEphemeralPub)
	if err != nil {
		crypto.ZeroKey(&ephPriv)
		conn.Close()
		h.sendOpenErr(remoteID, streamID, requestID, protocol.ErrGeneralFailure, "key exchange failed")
		return
	}

	crypto.ZeroKey(&ephPriv)

	// Derive session key - we are the responder (exit node).
	// Use requestID (not streamID) because streamID changes at each relay hop.
	sessionKey := crypto.DeriveSessionKey(sharedSecret, requestID, remoteEphemeralPub, ephPub, false)
	crypto.ZeroKey(&sharedSecret)

	// Get local address for ACK if the backend exposes one.
	var boundIP net.IP
	var boundPort uint16
	if la, ok := conn.(localAddrer); ok {
		if tcpAddr, ok := la.LocalAddr().(*net.TCPAddr); ok && tcpAddr != nil {
			boundIP = tcpAddr.IP
			boundPort = uint16(tcpAddr.Port)
		}
	}

	// Track connection with session key.
	ac := &ActiveConnection{
		StreamID:   streamID,
		RemoteID:   remoteID,
		Key:        key,
		Target:     target,
		Conn:       conn,
		StartedAt:  time.Now(),
		sessionKey: sessionKey,
	}

	h.mu.Lock()
	h.connections[streamKey{peer: remoteID, streamID: streamID}] = ac
	h.connCount.Add(1)
	h.mu.Unlock()

	h.logger.Debug("forward stream opened",
		"key", key,
		"target", target,
		logging.KeyStreamID, streamID)

	// Send ACK with our ephemeral public key.
	if err := h.writer.WriteStreamOpenAck(remoteID, streamID, requestID, boundIP, boundPort, ephPub); err != nil {
		ac.Close()
		h.removeConnection(streamKey{peer: remoteID, streamID: streamID})
		return
	}

	// Start reading from the local endpoint.
	go h.readLoop(ac)
}

// HandleStreamData processes incoming stream data.
func (h *Handler) HandleStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error {
	h.mu.RLock()
	ac := h.connections[streamKey{peer: peerID, streamID: streamID}]
	h.mu.RUnlock()

	if ac == nil {
		return fmt.Errorf("unknown stream %d", streamID)
	}

	if ac.IsClosed() {
		return fmt.Errorf("stream %d closed", streamID)
	}

	// Decrypt data before writing to target.
	if len(data) > 0 {
		if ac.sessionKey == nil {
			h.closeConnection(peerID, streamID, fmt.Errorf("no session key"))
			return fmt.Errorf("no session key for stream %d", streamID)
		}

		plaintext, err := ac.sessionKey.DecryptStrict(data)
		if err != nil {
			h.closeConnection(peerID, streamID, err)
			return fmt.Errorf("decrypt: %w", err)
		}

		if _, err := ac.Conn.Write(plaintext); err != nil {
			h.closeConnection(peerID, streamID, err)
			return err
		}
	}

	// Handle FIN flag: half-close the local endpoint when supported.
	if flags&protocol.FlagFinWrite != 0 {
		if hcw, ok := ac.Conn.(halfCloseWriter); ok {
			hcw.CloseWrite()
		}
	}

	return nil
}

// HandleStreamClose processes a stream close request.
func (h *Handler) HandleStreamClose(peerID identity.AgentID, streamID uint64) {
	h.closeConnection(peerID, streamID, nil)
}

// HandleStreamReset processes a stream reset request.
func (h *Handler) HandleStreamReset(peerID identity.AgentID, streamID uint64, errorCode uint16) {
	h.closeConnection(peerID, streamID, fmt.Errorf("reset with code %d", errorCode))
}

// readLoop reads data from the target and forwards to the stream.
func (h *Handler) readLoop(ac *ActiveConnection) {
	defer h.closeConnection(ac.RemoteID, ac.StreamID, nil)
	defer recovery.RecoverWithLog(h.logger, "forward.readLoop")

	// Account for encryption overhead when reading.
	// Each encrypted message must fit in a single frame to be decryptable.
	maxPlaintext := protocol.MaxPayloadSize - crypto.EncryptionOverhead
	buf := make([]byte, maxPlaintext)

	rd, _ := ac.Conn.(readDeadliner)

	for {
		select {
		case <-h.stopCh:
			return
		default:
		}

		// Set read deadline for idle timeout (TCP-style backends only).
		if h.cfg.IdleTimeout > 0 && rd != nil {
			rd.SetReadDeadline(time.Now().Add(h.cfg.IdleTimeout))
		}

		n, err := ac.Conn.Read(buf)
		if n > 0 {
			// Encrypt data before forwarding.
			if ac.sessionKey == nil {
				h.logger.Error("no session key in readLoop",
					logging.KeyStreamID, ac.StreamID)
				return
			}

			ciphertext, encErr := ac.sessionKey.Encrypt(buf[:n])
			if encErr != nil {
				h.logger.Error("encrypt failed in readLoop",
					logging.KeyStreamID, ac.StreamID,
					logging.KeyError, encErr)
				return
			}

			// Forward encrypted data to stream.
			if writeErr := h.writer.WriteStreamData(ac.RemoteID, ac.StreamID, ciphertext, 0); writeErr != nil {
				return
			}
		}

		if err != nil {
			if err == io.EOF {
				// Send FIN_WRITE (no data to encrypt).
				h.writer.WriteStreamData(ac.RemoteID, ac.StreamID, nil, protocol.FlagFinWrite)
			}
			return
		}
	}
}

// closeConnection closes a connection and cleans up.
func (h *Handler) closeConnection(peerID identity.AgentID, streamID uint64, err error) {
	ac := h.removeConnection(streamKey{peer: peerID, streamID: streamID})
	if ac == nil {
		return
	}

	ac.Close()

	h.logger.Debug("forward stream closed",
		"key", ac.Key,
		"target", ac.Target,
		logging.KeyStreamID, streamID)

	// Notify stream is closed.
	if h.writer != nil {
		h.writer.WriteStreamClose(peerID, streamID)
	}
}

// removeConnection removes a connection from tracking.
func (h *Handler) removeConnection(key streamKey) *ActiveConnection {
	h.mu.Lock()
	defer h.mu.Unlock()

	ac, ok := h.connections[key]
	if !ok {
		return nil
	}

	delete(h.connections, key)
	h.connCount.Add(-1)
	return ac
}

// sendOpenErr sends a stream open error.
func (h *Handler) sendOpenErr(peerID identity.AgentID, streamID, requestID uint64, code uint16, msg string) {
	h.logger.Debug("forward stream open error",
		logging.KeyStreamID, streamID,
		"error_code", code,
		"message", msg)

	if h.writer != nil {
		h.writer.WriteStreamOpenErr(peerID, streamID, requestID, code, msg)
	}
}

// mapDialError maps a dial error to a protocol error code.
func (h *Handler) mapDialError(err error) uint16 {
	if err == nil {
		return 0
	}

	// Check for timeout via net.Error interface.
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return protocol.ErrConnectionTimeout
	}

	// Check error message for common patterns (case-insensitive).
	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "refused") {
		return protocol.ErrConnectionRefused
	}
	if strings.Contains(errLower, "unreachable") {
		return protocol.ErrHostUnreachable
	}
	if strings.Contains(errLower, "timeout") {
		return protocol.ErrConnectionTimeout
	}

	return protocol.ErrGeneralFailure
}

// CloseConnectionsForPeer closes every connection opened by peerID and returns
// how many were closed. Called when the peer disconnects: without it these
// connections linger until their idle timeout, holding real TCP sockets open
// for a peer that can no longer receive their data, and the peer's stream IDs
// restart at 1 on reconnect and collide with the stale entries.
func (h *Handler) CloseConnectionsForPeer(peerID identity.AgentID) int {
	h.mu.Lock()
	var closing []*ActiveConnection
	for key, ac := range h.connections {
		if key.peer != peerID {
			continue
		}
		delete(h.connections, key)
		h.connCount.Add(-1)
		closing = append(closing, ac)
	}
	h.mu.Unlock()

	// Close outside the lock: Close touches the network and the read loop
	// wakes up and re-enters removeConnection, which takes h.mu.
	for _, ac := range closing {
		ac.Close()
	}
	return len(closing)
}

// ConnectionCount returns the number of active connections.
func (h *Handler) ConnectionCount() int64 {
	return h.connCount.Load()
}
