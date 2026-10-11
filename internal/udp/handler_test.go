package udp

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// mockDataWriter is a mock implementation of DataWriter for testing.
type mockDataWriter struct {
	mu          sync.Mutex
	datagrams   []*protocol.UDPDatagram
	closes      []uint8
	openAcks    []*protocol.UDPOpenAck
	openErrs    []*protocol.UDPOpenErr
	datagramErr error
	closeErr    error
	openAckErr  error
	openErrErr  error
}

func newMockDataWriter() *mockDataWriter {
	return &mockDataWriter{
		datagrams: make([]*protocol.UDPDatagram, 0),
		closes:    make([]uint8, 0),
		openAcks:  make([]*protocol.UDPOpenAck, 0),
		openErrs:  make([]*protocol.UDPOpenErr, 0),
	}
}

func (m *mockDataWriter) WriteUDPDatagram(peerID identity.AgentID, streamID uint64, datagram *protocol.UDPDatagram) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.datagramErr != nil {
		return m.datagramErr
	}
	m.datagrams = append(m.datagrams, datagram)
	return nil
}

func (m *mockDataWriter) WriteUDPClose(peerID identity.AgentID, streamID uint64, reason uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closeErr != nil {
		return m.closeErr
	}
	m.closes = append(m.closes, reason)
	return nil
}

func (m *mockDataWriter) WriteUDPOpenAck(peerID identity.AgentID, streamID uint64, ack *protocol.UDPOpenAck) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.openAckErr != nil {
		return m.openAckErr
	}
	m.openAcks = append(m.openAcks, ack)
	return nil
}

func (m *mockDataWriter) WriteUDPOpenErr(peerID identity.AgentID, streamID uint64, err *protocol.UDPOpenErr) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.openErrErr != nil {
		return m.openErrErr
	}
	m.openErrs = append(m.openErrs, err)
	return nil
}

func (m *mockDataWriter) getOpenAcks() []*protocol.UDPOpenAck {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openAcks
}

func (m *mockDataWriter) getOpenErrs() []*protocol.UDPOpenErr {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openErrs
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewHandler(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())
	defer h.Close()

	if h == nil {
		t.Fatal("NewHandler returned nil")
	}

	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount = %d, want 0", h.ActiveCount())
	}
}

func TestHandler_HandleUDPOpen_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false // Disabled

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	open := &protocol.UDPOpen{
		RequestID:   12345,
		AddressType: protocol.AddrTypeIPv4,
		Address:     []byte{0, 0, 0, 0},
		Port:        0,
		TTL:         10,
	}

	var ephKey [protocol.EphemeralKeySize]byte
	err := h.HandleUDPOpen(context.Background(), peerID, 1, open, ephKey)
	if err == nil {
		t.Error("HandleUDPOpen should fail when disabled")
	}

	errs := writer.getOpenErrs()
	if len(errs) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(errs))
	}

	if errs[0].ErrorCode != protocol.ErrUDPDisabled {
		t.Errorf("ErrorCode = %d, want %d", errs[0].ErrorCode, protocol.ErrUDPDisabled)
	}
}

func TestHandler_HandleUDPOpen_Success(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IdleTimeout = 0 // Disable cleanup loop for test

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	open := &protocol.UDPOpen{
		RequestID:   12345,
		AddressType: protocol.AddrTypeIPv4,
		Address:     []byte{0, 0, 0, 0},
		Port:        0,
		TTL:         10,
	}

	// Without encryption (zero ephemeral key)
	var ephKey [protocol.EphemeralKeySize]byte
	err := h.HandleUDPOpen(context.Background(), peerID, 1, open, ephKey)
	if err != nil {
		t.Errorf("HandleUDPOpen error = %v", err)
	}

	if h.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", h.ActiveCount())
	}

	acks := writer.getOpenAcks()
	if len(acks) != 1 {
		t.Fatalf("Expected 1 ack, got %d", len(acks))
	}

	if acks[0].RequestID != 12345 {
		t.Errorf("RequestID = %d, want 12345", acks[0].RequestID)
	}

	// Verify association exists
	assoc := h.GetAssociation(peerID, 1)
	if assoc == nil {
		t.Error("Association should exist")
	}

	if assoc.GetState() != StateOpen {
		t.Errorf("State = %v, want %v", assoc.GetState(), StateOpen)
	}

	// A different peer quoting the same stream ID must not reach it.
	otherPeer, _ := identity.NewAgentID()
	if got := h.GetAssociation(otherPeer, 1); got != nil {
		t.Error("GetAssociation returned another peer's association")
	}
}

func TestHandler_HandleUDPOpen_MaxAssociations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxAssociations = 1
	cfg.IdleTimeout = 0

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	var ephKey [protocol.EphemeralKeySize]byte

	// First association should succeed
	open1 := &protocol.UDPOpen{RequestID: 1, AddressType: protocol.AddrTypeIPv4, Address: []byte{0, 0, 0, 0}}
	err := h.HandleUDPOpen(context.Background(), peerID, 1, open1, ephKey)
	if err != nil {
		t.Errorf("First HandleUDPOpen error = %v", err)
	}

	// Second association should fail
	open2 := &protocol.UDPOpen{RequestID: 2, AddressType: protocol.AddrTypeIPv4, Address: []byte{0, 0, 0, 0}}
	err = h.HandleUDPOpen(context.Background(), peerID, 2, open2, ephKey)
	if err == nil {
		t.Error("Second HandleUDPOpen should fail due to limit")
	}

	errs := writer.getOpenErrs()
	if len(errs) != 1 {
		t.Fatalf("Expected 1 error, got %d", len(errs))
	}

	if errs[0].ErrorCode != protocol.ErrResourceLimit {
		t.Errorf("ErrorCode = %d, want %d", errs[0].ErrorCode, protocol.ErrResourceLimit)
	}
}

func TestHandler_HandleUDPClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IdleTimeout = 0

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	var ephKey [protocol.EphemeralKeySize]byte

	// Create association
	open := &protocol.UDPOpen{RequestID: 1, AddressType: protocol.AddrTypeIPv4, Address: []byte{0, 0, 0, 0}}
	err := h.HandleUDPOpen(context.Background(), peerID, 1, open, ephKey)
	if err != nil {
		t.Fatalf("HandleUDPOpen error = %v", err)
	}

	if h.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", h.ActiveCount())
	}

	// Close association
	err = h.HandleUDPClose(peerID, 1)
	if err != nil {
		t.Errorf("HandleUDPClose error = %v", err)
	}

	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount after close = %d, want 0", h.ActiveCount())
	}

	// Close non-existent should not error
	err = h.HandleUDPClose(peerID, 999)
	if err != nil {
		t.Errorf("HandleUDPClose for non-existent error = %v", err)
	}
}

func TestHandler_Close(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IdleTimeout = 0

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())

	peerID, _ := identity.NewAgentID()
	var ephKey [protocol.EphemeralKeySize]byte

	// Create multiple associations
	for i := uint64(1); i <= 3; i++ {
		open := &protocol.UDPOpen{RequestID: i, AddressType: protocol.AddrTypeIPv4, Address: []byte{0, 0, 0, 0}}
		h.HandleUDPOpen(context.Background(), peerID, i, open, ephKey)
	}

	if h.ActiveCount() != 3 {
		t.Errorf("ActiveCount = %d, want 3", h.ActiveCount())
	}

	// Close handler
	err := h.Close()
	if err != nil {
		t.Errorf("Close error = %v", err)
	}

	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount after Close = %d, want 0", h.ActiveCount())
	}
}

func TestHandler_CleanupExpired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.IdleTimeout = 50 * time.Millisecond

	writer := newMockDataWriter()
	h := NewHandler(cfg, writer, testLogger())
	defer h.Close()

	peerID, _ := identity.NewAgentID()
	var ephKey [protocol.EphemeralKeySize]byte

	// Create association
	open := &protocol.UDPOpen{RequestID: 1, AddressType: protocol.AddrTypeIPv4, Address: []byte{0, 0, 0, 0}}
	err := h.HandleUDPOpen(context.Background(), peerID, 1, open, ephKey)
	if err != nil {
		t.Fatalf("HandleUDPOpen error = %v", err)
	}

	if h.ActiveCount() != 1 {
		t.Errorf("ActiveCount = %d, want 1", h.ActiveCount())
	}

	// Wait for cleanup (IdleTimeout/2 check interval + IdleTimeout)
	time.Sleep(150 * time.Millisecond)

	if h.ActiveCount() != 0 {
		t.Errorf("ActiveCount after cleanup = %d, want 0", h.ActiveCount())
	}
}

// A domain-addressed datagram triggers a DNS lookup on a fast-lane worker;
// the lookup must be bounded by datagramResolveTimeout so a slow resolver
// cannot park the worker indefinitely.
func TestResolveDatagramAddress_DomainLookupBounded(t *testing.T) {
	oldLookup, oldTimeout := lookupIP, datagramResolveTimeout
	lookupIP = func(ctx context.Context, domain string) ([]net.IP, error) {
		<-ctx.Done() // Resolver that never answers on its own
		return nil, ctx.Err()
	}
	datagramResolveTimeout = 50 * time.Millisecond
	t.Cleanup(func() { lookupIP, datagramResolveTimeout = oldLookup, oldTimeout })

	datagram := &protocol.UDPDatagram{
		AddressType: protocol.AddrTypeDomain,
		Address:     append([]byte{byte(len("slow.example"))}, "slow.example"...),
		Port:        53,
	}

	start := time.Now()
	_, err := resolveDatagramAddress(datagram)
	if err == nil {
		t.Fatal("expected error from bounded lookup")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("lookup took %v, want bounded near 50ms", elapsed)
	}
}
