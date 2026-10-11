package icmp

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// mockDataWriter implements DataWriter for testing
type mockDataWriter struct {
	mu         sync.Mutex
	openAcks   []*protocol.ICMPOpenAck
	openErrs   []*protocol.ICMPOpenErr
	echos      []*protocol.ICMPEcho
	closes     []uint8
	ackPeerIDs []identity.AgentID
	errPeerIDs []identity.AgentID
}

func newMockDataWriter() *mockDataWriter {
	return &mockDataWriter{}
}

func (m *mockDataWriter) WriteICMPOpenAck(peerID identity.AgentID, streamID uint64, ack *protocol.ICMPOpenAck) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openAcks = append(m.openAcks, ack)
	m.ackPeerIDs = append(m.ackPeerIDs, peerID)
	return nil
}

func (m *mockDataWriter) WriteICMPOpenErr(peerID identity.AgentID, streamID uint64, err *protocol.ICMPOpenErr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openErrs = append(m.openErrs, err)
	m.errPeerIDs = append(m.errPeerIDs, peerID)
	return nil
}

func (m *mockDataWriter) WriteICMPEcho(peerID identity.AgentID, streamID uint64, echo *protocol.ICMPEcho) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.echos = append(m.echos, echo)
	return nil
}

func (m *mockDataWriter) WriteICMPClose(peerID identity.AgentID, streamID uint64, reason uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, reason)
	return nil
}

func (m *mockDataWriter) getOpenErrs() []*protocol.ICMPOpenErr {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openErrs
}

func (m *mockDataWriter) getCloses() []uint8 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closes
}

func TestNewHandler(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 100,
		IdleTimeout: time.Minute,
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
	if h.config.Enabled != true {
		t.Error("config.Enabled should be true")
	}
	if h.config.MaxSessions != 100 {
		t.Error("config.MaxSessions should be 100")
	}
	if h.ActiveCount() != 0 {
		t.Error("ActiveCount() should be 0 initially")
	}
}

func TestNewHandler_NoCleanupWithZeroTimeout(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 100,
		IdleTimeout: 0, // No cleanup
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	// Should not panic or have issues when cleanup is disabled
	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
}

func TestHandler_HandleICMPOpen_Disabled(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     false, // ICMP disabled
		MaxSessions: 100,
		IdleTimeout: time.Minute,
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	open := &protocol.ICMPOpen{
		RequestID:     12345,
		DestIP:        destIP.To4(), // Must be []byte
		TTL:           64,
		RemainingPath: nil,
	}

	var zeroKey [protocol.EphemeralKeySize]byte
	err := h.HandleICMPOpen(context.Background(), peerID, 1, open, zeroKey)

	if err == nil {
		t.Error("HandleICMPOpen() should return error when ICMP disabled")
	}

	errs := writer.getOpenErrs()
	if len(errs) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(errs))
	}
	if errs[0].ErrorCode != protocol.ErrICMPDisabled {
		t.Errorf("ErrorCode = %d, want %d", errs[0].ErrorCode, protocol.ErrICMPDisabled)
	}
	if errs[0].RequestID != 12345 {
		t.Errorf("RequestID = %d, want 12345", errs[0].RequestID)
	}
}

func TestHandler_HandleICMPOpen_SessionLimit(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 1, // Limit to 1 session
		IdleTimeout: time.Minute,
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Manually add a session to fill the limit
	session := NewSession(999, 999, peerID, destIP)
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: 999}] = session
	h.mu.Unlock()

	// Now try to open another session
	open := &protocol.ICMPOpen{
		RequestID:     12345,
		DestIP:        destIP.To4(), // Must be []byte
		TTL:           64,
		RemainingPath: nil,
	}

	var zeroKey [protocol.EphemeralKeySize]byte
	err := h.HandleICMPOpen(context.Background(), peerID, 1, open, zeroKey)

	if err == nil {
		t.Error("HandleICMPOpen() should return error when session limit reached")
	}

	errs := writer.getOpenErrs()
	if len(errs) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(errs))
	}
	if errs[0].ErrorCode != protocol.ErrICMPSessionLimit {
		t.Errorf("ErrorCode = %d, want %d", errs[0].ErrorCode, protocol.ErrICMPSessionLimit)
	}
}

func TestHandler_HandleICMPClose(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 100,
		IdleTimeout: time.Minute,
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Add a session
	session := NewSession(123, 456, peerID, destIP)
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: 123}] = session
	h.mu.Unlock()

	if h.ActiveCount() != 1 {
		t.Fatalf("ActiveCount() = %d, want 1", h.ActiveCount())
	}

	// Close the session
	err := h.HandleICMPClose(peerID, 123)
	if err != nil {
		t.Errorf("HandleICMPClose() error = %v", err)
	}

	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount() = %d, want 0 after close", h.ActiveCount())
	}

	// Session should be cleaned up from both maps
	if h.GetSession(peerID, 123) != nil {
		t.Error("Session should be removed from sessions map")
	}
}

func TestHandler_HandleICMPClose_NonExistent(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := DefaultConfig()
	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()

	// Close a non-existent session should not error
	err := h.HandleICMPClose(peerID, 999)
	if err != nil {
		t.Errorf("HandleICMPClose() for non-existent session should not error, got %v", err)
	}
}

func TestHandler_HandleICMPEcho_NoSession(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := DefaultConfig()
	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()

	echo := &protocol.ICMPEcho{
		Identifier: 1,
		Sequence:   1,
		IsReply:    false,
		Data:       []byte("test"),
	}

	err := h.HandleICMPEcho(peerID, 999, echo)
	if err == nil {
		t.Error("HandleICMPEcho() should error for unknown stream ID")
	}
}

func TestHandler_HandleICMPEcho_ClosedSession(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := DefaultConfig()
	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Add a closed session
	session := NewSession(123, 456, peerID, destIP)
	session.Close()
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: 123}] = session
	h.mu.Unlock()

	echo := &protocol.ICMPEcho{
		Identifier: 1,
		Sequence:   1,
		IsReply:    false,
		Data:       []byte("test"),
	}

	err := h.HandleICMPEcho(peerID, 123, echo)
	if err == nil {
		t.Error("HandleICMPEcho() should error for closed session")
	}
}

func TestHandler_HandleICMPEcho_Reply_Ignored(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := DefaultConfig()
	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Add an open session
	session := NewSession(123, 456, peerID, destIP)
	session.SetOpen()
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: 123}] = session
	h.mu.Unlock()

	// Reply frames from mesh should be ignored
	echo := &protocol.ICMPEcho{
		Identifier: 1,
		Sequence:   1,
		IsReply:    true, // This is a reply
		Data:       []byte("test"),
	}

	err := h.HandleICMPEcho(peerID, 123, echo)
	if err != nil {
		t.Errorf("HandleICMPEcho() for reply should not error, got %v", err)
	}
}

func TestHandler_GetSession(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := DefaultConfig()
	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// No sessions initially
	if h.GetSession(peerID, 123) != nil {
		t.Error("GetSession() should return nil for non-existent session")
	}

	// Add a session
	session := NewSession(123, 456, peerID, destIP)
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: 123}] = session
	h.mu.Unlock()

	got := h.GetSession(peerID, 123)
	if got != session {
		t.Error("GetSession() did not return expected session")
	}
}

func TestHandler_CleanupExpired(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 100,
		IdleTimeout: 50 * time.Millisecond, // Short timeout for test
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Add an old session
	session := NewSession(123, 456, peerID, destIP)
	session.SetOpen()
	h.mu.Lock()
	h.sessions[streamKey{peer: peerID, streamID: 123}] = session
	h.mu.Unlock()

	if h.ActiveCount() != 1 {
		t.Fatalf("ActiveCount() = %d, want 1", h.ActiveCount())
	}

	// Wait for session to expire and cleanup to run
	time.Sleep(100 * time.Millisecond)

	// Manually trigger cleanup since we can't rely on ticker timing
	h.cleanupExpired()

	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount() = %d, want 0 after cleanup", h.ActiveCount())
	}

	// Should have sent a close
	closes := writer.getCloses()
	if len(closes) != 1 {
		t.Errorf("Expected 1 close, got %d", len(closes))
	}
	if closes[0] != protocol.ICMPCloseTimeout {
		t.Errorf("Close reason = %d, want %d (timeout)", closes[0], protocol.ICMPCloseTimeout)
	}
}

func TestHandler_Close(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 100,
		IdleTimeout: time.Minute,
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Add sessions
	for i := uint64(1); i <= 5; i++ {
		session := NewSession(i, i*100, peerID, destIP)
		h.mu.Lock()
		h.sessions[streamKey{peer: peerID, streamID: i}] = session
		h.mu.Unlock()
	}

	if h.ActiveCount() != 5 {
		t.Fatalf("ActiveCount() = %d, want 5", h.ActiveCount())
	}

	// Close handler
	err := h.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// All sessions should be removed
	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount() = %d, want 0 after Close()", h.ActiveCount())
	}
}

func TestHandler_ConcurrentAccess(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.Default()

	cfg := Config{
		Enabled:     true,
		MaxSessions: 1000,
		IdleTimeout: time.Minute,
		EchoTimeout: 5 * time.Second,
	}

	h := NewHandler(cfg, writer, logger)
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	// Concurrent reads and writes
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)

		// Writer goroutine
		go func(id uint64) {
			defer wg.Done()
			session := NewSession(id, id*100, peerID, destIP)
			h.mu.Lock()
			h.sessions[streamKey{peer: peerID, streamID: id}] = session
			h.mu.Unlock()
		}(uint64(i))

		// Reader goroutine 1
		go func(id uint64) {
			defer wg.Done()
			_ = h.GetSession(peerID, id)
		}(uint64(i))

		// Reader goroutine 2
		go func(id uint64) {
			defer wg.Done()
			_ = h.ActiveCount()
		}(uint64(i))
	}

	wg.Wait()
}
