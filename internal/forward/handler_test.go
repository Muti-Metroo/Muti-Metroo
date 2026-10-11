package forward

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// mustNewAgentID creates a new agent ID or panics.
func mustNewAgentID() identity.AgentID {
	id, err := identity.NewAgentID()
	if err != nil {
		panic(err)
	}
	return id
}

// mockStreamWriter is a mock implementation of StreamWriter for testing.
type mockStreamWriter struct {
	mu           sync.Mutex
	dataWrites   []dataWrite
	acks         []ackWrite
	errors       []errWrite
	closes       []closeWrite
	writeFail    bool
	writeDataErr error
}

type dataWrite struct {
	PeerID   identity.AgentID
	StreamID uint64
	Data     []byte
	Flags    uint8
}

type ackWrite struct {
	PeerID       identity.AgentID
	StreamID     uint64
	RequestID    uint64
	BoundIP      net.IP
	BoundPort    uint16
	EphemeralPub [crypto.KeySize]byte
}

type errWrite struct {
	PeerID    identity.AgentID
	StreamID  uint64
	RequestID uint64
	ErrorCode uint16
	Message   string
}

type closeWrite struct {
	PeerID   identity.AgentID
	StreamID uint64
}

func (m *mockStreamWriter) WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error {
	if m.writeDataErr != nil {
		return m.writeDataErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dataWrites = append(m.dataWrites, dataWrite{
		PeerID:   peerID,
		StreamID: streamID,
		Data:     append([]byte(nil), data...),
		Flags:    flags,
	})
	return nil
}

func (m *mockStreamWriter) WriteStreamOpenAck(peerID identity.AgentID, streamID uint64, requestID uint64, boundIP net.IP, boundPort uint16, ephemeralPubKey [crypto.KeySize]byte) error {
	if m.writeFail {
		return net.ErrClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acks = append(m.acks, ackWrite{
		PeerID:       peerID,
		StreamID:     streamID,
		RequestID:    requestID,
		BoundIP:      boundIP,
		BoundPort:    boundPort,
		EphemeralPub: ephemeralPubKey,
	})
	return nil
}

func (m *mockStreamWriter) WriteStreamOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errorCode uint16, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, errWrite{
		PeerID:    peerID,
		StreamID:  streamID,
		RequestID: requestID,
		ErrorCode: errorCode,
		Message:   message,
	})
	return nil
}

func (m *mockStreamWriter) WriteStreamClose(peerID identity.AgentID, streamID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, closeWrite{
		PeerID:   peerID,
		StreamID: streamID,
	})
	return nil
}

func (m *mockStreamWriter) getAcks() []ackWrite {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ackWrite(nil), m.acks...)
}

func (m *mockStreamWriter) getErrors() []errWrite {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]errWrite(nil), m.errors...)
}

func (m *mockStreamWriter) getCloses() []closeWrite {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]closeWrite(nil), m.closes...)
}

func TestDefaultHandlerConfig(t *testing.T) {
	cfg := DefaultHandlerConfig()

	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("expected ConnectTimeout 30s, got %v", cfg.ConnectTimeout)
	}

	if cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("expected IdleTimeout 5m, got %v", cfg.IdleTimeout)
	}

	if cfg.MaxConnections != 1000 {
		t.Errorf("expected MaxConnections 1000, got %d", cfg.MaxConnections)
	}
}

func TestNewHandler(t *testing.T) {
	localID := mustNewAgentID()
	writer := &mockStreamWriter{}

	cfg := HandlerConfig{
		Endpoints: []Endpoint{
			{Key: "web", Target: "localhost:8080"},
			{Key: "api", Target: "localhost:9090"},
		},
		ConnectTimeout: 10 * time.Second,
		IdleTimeout:    1 * time.Minute,
		MaxConnections: 100,
	}

	handler := NewHandler(cfg, localID, writer)

	if handler == nil {
		t.Fatal("NewHandler returned nil")
	}

	// Check targets are configured
	target, ok := handler.GetTarget("web")
	if !ok || target != "localhost:8080" {
		t.Errorf("expected target 'localhost:8080' for key 'web', got '%s', ok=%v", target, ok)
	}

	target, ok = handler.GetTarget("api")
	if !ok || target != "localhost:9090" {
		t.Errorf("expected target 'localhost:9090' for key 'api', got '%s', ok=%v", target, ok)
	}

	_, ok = handler.GetTarget("nonexistent")
	if ok {
		t.Error("expected false for nonexistent key")
	}

	// Check keys
	keys := handler.GetKeys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}

	// Check initial state
	if handler.IsRunning() {
		t.Error("expected handler not running initially")
	}

	if handler.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections, got %d", handler.ConnectionCount())
	}
}

func TestHandler_StartStop(t *testing.T) {
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})

	if handler.IsRunning() {
		t.Error("expected not running before start")
	}

	handler.Start()

	if !handler.IsRunning() {
		t.Error("expected running after start")
	}

	handler.Stop()

	if handler.IsRunning() {
		t.Error("expected not running after stop")
	}

	// Multiple stops should be safe
	handler.Stop()
	handler.Stop()
}

func TestHandler_HandleStreamOpen_NotRunning(t *testing.T) {
	handler := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{{Key: "test", Target: "localhost:8080"}},
	}, mustNewAgentID(), &mockStreamWriter{})

	// Don't start handler

	var ephPub [crypto.KeySize]byte
	err := handler.HandleStreamOpen(context.Background(), 1, 1, mustNewAgentID(), "test", ephPub)
	if err == nil {
		t.Error("expected error when handler not running")
	}
}

func TestHandler_HandleStreamOpen_UnknownKey(t *testing.T) {
	writer := &mockStreamWriter{}
	handler := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{{Key: "known", Target: "localhost:8080"}},
	}, mustNewAgentID(), writer)

	handler.Start()
	defer handler.Stop()

	var ephPub [crypto.KeySize]byte
	remoteID := mustNewAgentID()
	err := handler.HandleStreamOpen(context.Background(), 1, 100, remoteID, "unknown", ephPub)
	if err == nil {
		t.Error("expected error for unknown key")
	}

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	// Check error was sent
	errors := writer.getErrors()
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}

	if errors[0].ErrorCode != protocol.ErrForwardNotFound {
		t.Errorf("expected error code %d, got %d", protocol.ErrForwardNotFound, errors[0].ErrorCode)
	}

	if errors[0].StreamID != 1 {
		t.Errorf("expected stream ID 1, got %d", errors[0].StreamID)
	}
}

func TestHandler_HandleStreamOpen_ConnectionLimit(t *testing.T) {
	writer := &mockStreamWriter{}
	handler := NewHandler(HandlerConfig{
		Endpoints:      []Endpoint{{Key: "test", Target: "localhost:8080"}},
		MaxConnections: 0, // No limit initially
	}, mustNewAgentID(), writer)

	// Manually set connection count to simulate limit
	handler.connCount.Store(100)
	handler.cfg.MaxConnections = 100

	handler.Start()
	defer handler.Stop()

	var ephPub [crypto.KeySize]byte
	err := handler.HandleStreamOpen(context.Background(), 1, 100, mustNewAgentID(), "test", ephPub)
	if err == nil {
		t.Error("expected error for connection limit")
	}

	// Check error was sent
	errors := writer.getErrors()
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}

	if errors[0].ErrorCode != protocol.ErrConnectionLimit {
		t.Errorf("expected error code %d, got %d", protocol.ErrConnectionLimit, errors[0].ErrorCode)
	}
}

func TestHandler_HandleStreamOpen_Success(t *testing.T) {
	// Create a target server
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create target listener: %v", err)
	}
	defer targetListener.Close()

	targetAddr := targetListener.Addr().String()

	// Accept connections on target
	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			// Just hold the connection
			go func(c net.Conn) {
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						c.Close()
						return
					}
				}
			}(conn)
		}
	}()

	writer := &mockStreamWriter{}
	handler := NewHandler(HandlerConfig{
		Endpoints:      []Endpoint{{Key: "test", Target: targetAddr}},
		ConnectTimeout: 5 * time.Second,
		IdleTimeout:    1 * time.Minute,
	}, mustNewAgentID(), writer)

	handler.Start()
	defer handler.Stop()

	// Generate ephemeral keypair for client
	_, ephPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate ephemeral key: %v", err)
	}

	remoteID := mustNewAgentID()
	err = handler.HandleStreamOpen(context.Background(), 1, 100, remoteID, "test", ephPub)
	if err != nil {
		t.Fatalf("HandleStreamOpen failed: %v", err)
	}

	// Wait for async processing
	time.Sleep(200 * time.Millisecond)

	// Check ACK was sent
	acks := writer.getAcks()
	if len(acks) != 1 {
		t.Fatalf("expected 1 ack, got %d", len(acks))
	}

	if acks[0].StreamID != 1 {
		t.Errorf("expected stream ID 1, got %d", acks[0].StreamID)
	}

	if acks[0].RequestID != 100 {
		t.Errorf("expected request ID 100, got %d", acks[0].RequestID)
	}

	// Check connection count
	if handler.ConnectionCount() != 1 {
		t.Errorf("expected 1 connection, got %d", handler.ConnectionCount())
	}
}

func TestHandler_HandleStreamData_UnknownStream(t *testing.T) {
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})
	handler.Start()
	defer handler.Stop()

	err := handler.HandleStreamData(mustNewAgentID(), 999, []byte("data"), 0)
	if err == nil {
		t.Error("expected error for unknown stream")
	}
}

func TestHandler_HandleStreamClose(t *testing.T) {
	writer := &mockStreamWriter{}
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), writer)
	handler.Start()
	defer handler.Stop()

	// Add a mock connection
	peerID := mustNewAgentID()
	ac := &ActiveConnection{
		StreamID: 1,
		RemoteID: peerID,
		Key:      "test",
		Target:   "localhost:8080",
	}
	handler.mu.Lock()
	handler.connections[streamKey{peer: ac.RemoteID, streamID: 1}] = ac
	handler.connCount.Add(1)
	handler.mu.Unlock()

	// Close the stream
	handler.HandleStreamClose(peerID, 1)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Check connection was removed
	if handler.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections after close, got %d", handler.ConnectionCount())
	}

	// Check close was sent
	closes := writer.getCloses()
	if len(closes) != 1 {
		t.Fatalf("expected 1 close, got %d", len(closes))
	}
}

func TestHandler_HandleStreamReset(t *testing.T) {
	writer := &mockStreamWriter{}
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), writer)
	handler.Start()
	defer handler.Stop()

	// Add a mock connection
	peerID := mustNewAgentID()
	ac := &ActiveConnection{
		StreamID: 1,
		RemoteID: peerID,
		Key:      "test",
		Target:   "localhost:8080",
	}
	handler.mu.Lock()
	handler.connections[streamKey{peer: ac.RemoteID, streamID: 1}] = ac
	handler.connCount.Add(1)
	handler.mu.Unlock()

	// Reset the stream
	handler.HandleStreamReset(peerID, 1, protocol.ErrGeneralFailure)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Check connection was removed
	if handler.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections after reset, got %d", handler.ConnectionCount())
	}
}

func TestActiveConnection_Close(t *testing.T) {
	conn := newMockConn()
	ac := &ActiveConnection{
		StreamID: 1,
		Conn:     conn,
	}

	if ac.IsClosed() {
		t.Error("expected connection not closed initially")
	}

	err := ac.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	if !ac.IsClosed() {
		t.Error("expected connection closed after Close()")
	}

	// Multiple closes should be safe
	err = ac.Close()
	if err != nil {
		t.Errorf("Second Close returned error: %v", err)
	}
}

func TestActiveConnection_CloseNilConn(t *testing.T) {
	ac := &ActiveConnection{
		StreamID: 1,
		Conn:     nil,
	}

	err := ac.Close()
	if err != nil {
		t.Errorf("Close with nil conn returned error: %v", err)
	}

	if !ac.IsClosed() {
		t.Error("expected connection marked as closed")
	}
}

func TestHandler_GetTarget(t *testing.T) {
	handler := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{
			{Key: "web", Target: "localhost:80"},
			{Key: "api", Target: "api.local:8080"},
		},
	}, mustNewAgentID(), &mockStreamWriter{})

	tests := []struct {
		key      string
		expected string
		ok       bool
	}{
		{"web", "localhost:80", true},
		{"api", "api.local:8080", true},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			target, ok := handler.GetTarget(tt.key)
			if ok != tt.ok {
				t.Errorf("GetTarget(%q): expected ok=%v, got %v", tt.key, tt.ok, ok)
			}
			if target != tt.expected {
				t.Errorf("GetTarget(%q): expected %q, got %q", tt.key, tt.expected, target)
			}
		})
	}
}

func TestHandler_GetKeys(t *testing.T) {
	handler := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{
			{Key: "a", Target: "localhost:1"},
			{Key: "b", Target: "localhost:2"},
			{Key: "c", Target: "localhost:3"},
		},
	}, mustNewAgentID(), &mockStreamWriter{})

	keys := handler.GetKeys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}

	// Check all keys are present (order may vary)
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}

	for _, expected := range []string{"a", "b", "c"} {
		if !keyMap[expected] {
			t.Errorf("expected key %q in keys", expected)
		}
	}
}

func TestHandler_MapDialError(t *testing.T) {
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), nil)

	tests := []struct {
		name     string
		err      error
		expected uint16
	}{
		{"nil error", nil, 0},
		{"connection refused", &net.OpError{Err: &net.DNSError{Err: "connection refused"}}, protocol.ErrConnectionRefused},
		{"host unreachable", &net.OpError{Err: &net.DNSError{Err: "host unreachable"}}, protocol.ErrHostUnreachable},
		{"timeout in message", &net.OpError{Err: &net.DNSError{Err: "i/o timeout"}}, protocol.ErrConnectionTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := handler.mapDialError(tt.err)
			if code != tt.expected {
				t.Errorf("mapDialError(%v): expected %d, got %d", tt.err, tt.expected, code)
			}
		})
	}
}

func TestHandler_StopClosesAllConnections(t *testing.T) {
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})
	handler.Start()

	// Add multiple mock connections
	closedCount := atomic.Int64{}
	for i := uint64(1); i <= 3; i++ {
		conn := newMockConn()
		ac := &ActiveConnection{
			StreamID: i,
			RemoteID: mustNewAgentID(),
			Conn:     conn,
		}
		handler.mu.Lock()
		handler.connections[streamKey{peer: ac.RemoteID, streamID: i}] = ac
		handler.connCount.Add(1)
		handler.mu.Unlock()

		// Track when connections are closed
		go func(c *mockConn) {
			for !c.closed.Load() {
				time.Sleep(10 * time.Millisecond)
			}
			closedCount.Add(1)
		}(conn)
	}

	// Stop should close all connections
	handler.Stop()

	// Wait for close tracking
	time.Sleep(100 * time.Millisecond)

	if closedCount.Load() != 3 {
		t.Errorf("expected 3 connections closed, got %d", closedCount.Load())
	}

	if handler.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections after stop, got %d", handler.ConnectionCount())
	}
}

func TestHandler_ConnectionCount(t *testing.T) {
	handler := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})

	if handler.ConnectionCount() != 0 {
		t.Errorf("expected 0 initially, got %d", handler.ConnectionCount())
	}

	// Add connections
	handler.connCount.Add(5)
	if handler.ConnectionCount() != 5 {
		t.Errorf("expected 5, got %d", handler.ConnectionCount())
	}

	// Remove some
	handler.connCount.Add(-2)
	if handler.ConnectionCount() != 3 {
		t.Errorf("expected 3, got %d", handler.ConnectionCount())
	}
}

func TestHandler_AddRemoveTarget(t *testing.T) {
	h := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{{Key: "static", Target: "localhost:1"}},
	}, mustNewAgentID(), &mockStreamWriter{})

	// Static endpoint is reachable via GetTarget.
	if tgt, ok := h.GetTarget("static"); !ok || tgt != "localhost:1" {
		t.Errorf("GetTarget(static) = %q,%v; want localhost:1,true", tgt, ok)
	}

	// Add a new runtime target.
	if err := h.AddTarget("dyn", "127.0.0.1:3000"); err != nil {
		t.Fatalf("AddTarget(dyn): %v", err)
	}
	if tgt, ok := h.GetTarget("dyn"); !ok || tgt != "127.0.0.1:3000" {
		t.Errorf("GetTarget(dyn) = %q,%v; want 127.0.0.1:3000,true", tgt, ok)
	}

	// AddTarget overwrites.
	if err := h.AddTarget("dyn", "127.0.0.1:4000"); err != nil {
		t.Fatalf("AddTarget(dyn, second): %v", err)
	}
	if tgt, _ := h.GetTarget("dyn"); tgt != "127.0.0.1:4000" {
		t.Errorf("GetTarget after overwrite = %q; want 127.0.0.1:4000", tgt)
	}

	// Empty key and empty target are rejected.
	if err := h.AddTarget("", "localhost:5"); err == nil {
		t.Error("AddTarget(empty key): expected error")
	}
	if err := h.AddTarget("x", ""); err == nil {
		t.Error("AddTarget(empty target): expected error")
	}

	// RemoveTarget returns true on first remove and false on second.
	if !h.RemoveTarget("dyn") {
		t.Error("RemoveTarget(dyn): expected true")
	}
	if h.RemoveTarget("dyn") {
		t.Error("RemoveTarget(dyn) twice: expected false the second time")
	}
	if _, ok := h.GetTarget("dyn"); ok {
		t.Error("GetTarget(dyn) after remove: expected not found")
	}

	// Static target is unaffected by dynamic removal.
	if tgt, ok := h.GetTarget("static"); !ok || tgt != "localhost:1" {
		t.Errorf("GetTarget(static) after dyn remove = %q,%v; want localhost:1,true", tgt, ok)
	}
}

func TestHandler_TargetsSnapshot(t *testing.T) {
	h := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{{Key: "a", Target: "localhost:1"}},
	}, mustNewAgentID(), &mockStreamWriter{})
	if err := h.AddTarget("b", "localhost:2"); err != nil {
		t.Fatalf("AddTarget: %v", err)
	}

	snap := h.Targets()
	if len(snap) != 2 || snap["a"] != "localhost:1" || snap["b"] != "localhost:2" {
		t.Errorf("Targets() = %v; want {a:localhost:1, b:localhost:2}", snap)
	}

	// Mutating the snapshot must not affect the handler.
	snap["a"] = "mutated"
	if tgt, _ := h.GetTarget("a"); tgt != "localhost:1" {
		t.Errorf("GetTarget(a) after snapshot mutation = %q; want localhost:1", tgt)
	}
}

func TestHandler_AddTarget_RaceWithGetTarget(t *testing.T) {
	h := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = h.AddTarget("k", "localhost:1")
			h.RemoveTarget("k")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			h.GetTarget("k")
		}
	}()
	close(stop)
	wg.Wait()
}

func TestHandler_NoEndpoints(t *testing.T) {
	handler := NewHandler(HandlerConfig{
		Endpoints: nil,
	}, mustNewAgentID(), &mockStreamWriter{})

	keys := handler.GetKeys()
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}

	_, ok := handler.GetTarget("any")
	if ok {
		t.Error("expected false for any key with no endpoints")
	}
}

func TestHandler_AddBackend_Collision(t *testing.T) {
	h := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})

	if err := h.AddBackend("k", NewTCPBackend("localhost:1", time.Second)); err != nil {
		t.Fatalf("AddBackend first: %v", err)
	}

	// Second AddBackend should fail with ErrKeyInUse.
	err := h.AddBackend("k", NewTCPBackend("localhost:2", time.Second))
	if err == nil {
		t.Fatal("expected ErrKeyInUse, got nil")
	}
	if !errors.Is(err, ErrKeyInUse) {
		t.Errorf("expected ErrKeyInUse, got %v", err)
	}

	// ReplaceBackend overwrites silently.
	if err := h.ReplaceBackend("k", NewTCPBackend("localhost:3", time.Second)); err != nil {
		t.Fatalf("ReplaceBackend: %v", err)
	}
	if tgt, _ := h.GetTarget("k"); tgt != "localhost:3" {
		t.Errorf("after replace = %q; want localhost:3", tgt)
	}
}

func TestHandler_AddBackend_RejectsEmpty(t *testing.T) {
	h := NewHandler(HandlerConfig{}, mustNewAgentID(), &mockStreamWriter{})

	if err := h.AddBackend("", NewTCPBackend("localhost:1", time.Second)); err == nil {
		t.Error("expected error for empty key")
	}
	if err := h.AddBackend("k", nil); err == nil {
		t.Error("expected error for nil backend")
	}
}

func TestHandler_GetBackend(t *testing.T) {
	h := NewHandler(HandlerConfig{
		Endpoints: []Endpoint{{Key: "k1", Target: "localhost:1"}},
	}, mustNewAgentID(), &mockStreamWriter{})

	b, ok := h.GetBackend("k1")
	if !ok {
		t.Fatal("expected backend for k1")
	}
	if b.Target() != "localhost:1" {
		t.Errorf("Target() = %q; want localhost:1", b.Target())
	}

	if _, ok := h.GetBackend("missing"); ok {
		t.Error("expected no backend for missing key")
	}
}

// errorBackend returns a configurable error from Open.
type errorBackend struct {
	target string
	err    error
}

func (e *errorBackend) Target() string { return e.target }
func (e *errorBackend) Open(ctx context.Context, streamID uint64, key, remoteAddr string) (io.ReadWriteCloser, error) {
	return nil, e.err
}

func TestHandler_OpenError_PropagatesAsStreamOpenErr(t *testing.T) {
	writer := &mockStreamWriter{}
	h := NewHandler(HandlerConfig{}, mustNewAgentID(), writer)
	h.Start()
	defer h.Stop()

	// "connection refused" matches mapDialError's pattern -> ErrConnectionRefused.
	if err := h.AddBackend("k", &errorBackend{target: "x", err: errors.New("dial tcp: connection refused")}); err != nil {
		t.Fatalf("AddBackend: %v", err)
	}

	var ephPub [crypto.KeySize]byte
	if err := h.HandleStreamOpen(context.Background(), 1, 1, mustNewAgentID(), "k", ephPub); err != nil {
		t.Fatalf("HandleStreamOpen: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	errs := writer.getErrors()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs[0].ErrorCode != protocol.ErrConnectionRefused {
		t.Errorf("error code = %d; want ErrConnectionRefused", errs[0].ErrorCode)
	}
}

// halfCloseConn is a mockConn-like ReadWriteCloser that records CloseWrite.
type halfCloseConn struct {
	*mockConn
	writeClosed atomic.Bool
}

func (h *halfCloseConn) CloseWrite() error {
	h.writeClosed.Store(true)
	return nil
}

func TestHandler_HandleStreamData_HalfCloseWriter(t *testing.T) {
	writer := &mockStreamWriter{}
	h := NewHandler(HandlerConfig{}, mustNewAgentID(), writer)
	h.Start()
	defer h.Stop()

	hc := &halfCloseConn{mockConn: newMockConn()}
	ac := &ActiveConnection{
		StreamID: 1,
		RemoteID: mustNewAgentID(),
		Key:      "test",
		Conn:     hc,
	}
	h.mu.Lock()
	h.connections[streamKey{peer: ac.RemoteID, streamID: 1}] = ac
	h.connCount.Add(1)
	h.mu.Unlock()

	if err := h.HandleStreamData(ac.RemoteID, 1, nil, protocol.FlagFinWrite); err != nil {
		t.Fatalf("HandleStreamData: %v", err)
	}
	if !hc.writeClosed.Load() {
		t.Error("expected CloseWrite to be invoked via halfCloseWriter")
	}
}
