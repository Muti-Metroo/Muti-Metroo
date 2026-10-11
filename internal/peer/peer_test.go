package peer

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// ============================================================================
// Connection State Tests
// ============================================================================

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state ConnectionState
		want  string
	}{
		{StateDisconnected, "DISCONNECTED"},
		{StateConnecting, "CONNECTING"},
		{StateHandshaking, "HANDSHAKING"},
		{StateConnected, "CONNECTED"},
		{StateReconnecting, "RECONNECTING"},
		{ConnectionState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ConnectionState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestDefaultConnectionConfig(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	if cfg.LocalID != localID {
		t.Error("LocalID not set")
	}
	if cfg.HandshakeTimeout != 10*time.Second {
		t.Errorf("HandshakeTimeout = %v, want 10s", cfg.HandshakeTimeout)
	}
	if cfg.Capabilities == nil {
		t.Error("Capabilities should not be nil")
	}
}

func TestConnection_StateTransitions(t *testing.T) {
	// Create a mock connection
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	// We need a mock PeerConn for testing
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Initial state should be Handshaking
	if conn.State() != StateHandshaking {
		t.Errorf("Initial state = %v, want StateHandshaking", conn.State())
	}

	// Test state transitions
	conn.SetState(StateConnected)
	if conn.State() != StateConnected {
		t.Errorf("State = %v, want StateConnected", conn.State())
	}

	conn.SetState(StateReconnecting)
	if conn.State() != StateReconnecting {
		t.Errorf("State = %v, want StateReconnecting", conn.State())
	}

	// Close and check state
	conn.Close()
	if conn.State() != StateDisconnected {
		t.Errorf("State after close = %v, want StateDisconnected", conn.State())
	}
}

func TestConnection_Activity(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Check that activity is tracked
	activity := conn.LastActivity()
	if time.Since(activity) > 100*time.Millisecond {
		t.Error("LastActivity should be recent after creation")
	}

	// Wait a bit and check again
	time.Sleep(10 * time.Millisecond)
	conn.updateActivity()
	newActivity := conn.LastActivity()

	if !newActivity.After(activity) {
		t.Error("Activity should be updated")
	}
}

func TestConnection_RTT(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Initial RTT should be 0
	if conn.RTT() != 0 {
		t.Errorf("Initial RTT = %v, want 0", conn.RTT())
	}

	// Update RTT
	past := uint64(time.Now().Add(-50 * time.Millisecond).UnixNano())
	conn.UpdateRTT(past)

	// RTT should now be approximately 50ms
	rtt := conn.RTT()
	if rtt < 40*time.Millisecond || rtt > 100*time.Millisecond {
		t.Errorf("RTT = %v, expected ~50ms", rtt)
	}
}

func TestConnection_Done(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Done channel should not be closed initially
	select {
	case <-conn.Done():
		t.Error("Done channel should not be closed before Close()")
	default:
	}

	// Close connection
	conn.Close()

	// Done channel should be closed now
	select {
	case <-conn.Done():
	default:
		t.Error("Done channel should be closed after Close()")
	}
}

func TestConnection_MultipleClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Multiple closes should not panic
	for i := 0; i < 5; i++ {
		if err := conn.Close(); err != nil {
			t.Errorf("Close() error on attempt %d: %v", i, err)
		}
	}
}

func TestConnection_HasCapability(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	cfg.Capabilities = []string{"cap1", "cap2", "cap3"}
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Set capabilities on the connection
	conn.capabilities = cfg.Capabilities

	if !conn.HasCapability("cap1") {
		t.Error("Should have cap1")
	}
	if !conn.HasCapability("cap2") {
		t.Error("Should have cap2")
	}
	if conn.HasCapability("cap4") {
		t.Error("Should not have cap4")
	}
}

// ============================================================================
// Reconnection Tests
// ============================================================================

func TestReconnectConfig_Default(t *testing.T) {
	cfg := DefaultReconnectConfig()

	if cfg.InitialDelay != 1*time.Second {
		t.Errorf("InitialDelay = %v, want 1s", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 60*time.Second {
		t.Errorf("MaxDelay = %v, want 60s", cfg.MaxDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier = %v, want 2.0", cfg.Multiplier)
	}
	if cfg.MaxAttempts != 0 {
		t.Errorf("MaxAttempts = %v, want 0", cfg.MaxAttempts)
	}
}

func TestBackoffCalculator_CalculateDelay(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
	calc := NewBackoffCalculator(cfg)

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second}, // Capped at MaxDelay
		{10, 30 * time.Second},
	}

	for _, tt := range tests {
		got := calc.CalculateDelay(tt.attempt)
		if got != tt.want {
			t.Errorf("CalculateDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestReconnector_Schedule(t *testing.T) {
	attempts := make(map[string]int)
	var mu sync.Mutex

	cfg := ReconnectConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		MaxAttempts:  3,
	}

	// Callback that always fails
	callback := func(addr string) error {
		mu.Lock()
		attempts[addr]++
		mu.Unlock()
		return context.DeadlineExceeded // Simulate failure
	}

	r := NewReconnector(cfg, callback)
	defer r.Stop()

	// Schedule reconnection
	r.Schedule("127.0.0.1:8080")

	// Wait for attempts
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := attempts["127.0.0.1:8080"]
	mu.Unlock()

	if count < 1 || count > 4 {
		t.Errorf("Expected 1-4 reconnect attempts, got %d", count)
	}
}

func TestReconnector_Cancel(t *testing.T) {
	attemptCount := 0
	var mu sync.Mutex

	cfg := ReconnectConfig{
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  0,
	}

	callback := func(addr string) error {
		mu.Lock()
		attemptCount++
		mu.Unlock()
		return context.DeadlineExceeded
	}

	r := NewReconnector(cfg, callback)
	defer r.Stop()

	// Schedule and immediately cancel
	r.Schedule("127.0.0.1:8080")
	time.Sleep(10 * time.Millisecond) // Let it start
	r.Cancel("127.0.0.1:8080")

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := attemptCount
	mu.Unlock()

	// Should have 0 or 1 attempt (depending on timing)
	if count > 1 {
		t.Errorf("Expected 0-1 attempts after cancel, got %d", count)
	}
}

func TestReconnector_SuccessfulReconnect(t *testing.T) {
	attemptCount := 0
	var mu sync.Mutex

	cfg := ReconnectConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		MaxAttempts:  5,
	}

	// Callback that succeeds on 3rd attempt
	callback := func(addr string) error {
		mu.Lock()
		attemptCount++
		count := attemptCount
		mu.Unlock()

		if count >= 3 {
			return nil // Success
		}
		return context.DeadlineExceeded
	}

	r := NewReconnector(cfg, callback)
	defer r.Stop()

	r.Schedule("127.0.0.1:8080")

	// Wait for attempts
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := attemptCount
	mu.Unlock()

	if count != 3 {
		t.Errorf("Expected exactly 3 attempts (success on 3rd), got %d", count)
	}

	// Should not be pending anymore after success
	if r.IsPending("127.0.0.1:8080") {
		t.Error("Should not be pending after successful reconnect")
	}
}

// TestReconnector_ScheduleDuringCallbackSurvives is a regression test for a
// race where the reconnect scheduler would go silent after a brief successful
// reconnect. If a peer disconnects mid-callback (e.g. the remote side sends
// EOF immediately after the handshake, which handleDisconnect reacts to by
// calling Schedule), the success branch of attemptReconnect used to delete
// the state entry and thereby orphan the fresh timer Schedule had just
// installed. No further reconnect attempts would ever fire.
func TestReconnector_ScheduleDuringCallbackSurvives(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
		Jitter:       0, // deterministic delays
	}

	var (
		r          *Reconnector
		callCount  atomic.Int32
		secondCall = make(chan struct{}, 1)
	)

	r = NewReconnector(cfg, func(addr string) error {
		n := callCount.Add(1)
		if n == 1 {
			// Simulate handleDisconnect on a peer that briefly came up during
			// this callback and died right away: Schedule re-invoked while
			// the reconnect callback is still running.
			r.Schedule(addr)
			return nil // dial "succeeded" - this is where the bug struck
		}
		secondCall <- struct{}{}
		return nil
	})
	defer r.Stop()

	r.Schedule("peer:1234")

	select {
	case <-secondCall:
		// Good: the replacement timer fired and the reconnector kept trying.
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("reconnector went silent after Schedule-during-successful-callback race; got %d calls", callCount.Load())
	}
}

func TestReconnector_Stop(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	callback := func(addr string) error {
		return context.DeadlineExceeded
	}

	r := NewReconnector(cfg, callback)

	// Schedule multiple
	r.Schedule("addr1")
	r.Schedule("addr2")
	r.Schedule("addr3")

	// Stop should cancel all
	r.Stop()

	// Nothing should be pending
	if r.IsPending("addr1") || r.IsPending("addr2") || r.IsPending("addr3") {
		t.Error("Nothing should be pending after Stop()")
	}
}

// ============================================================================
// Handshaker Tests
// ============================================================================

func TestNewHandshaker(t *testing.T) {
	localID, _ := identity.NewAgentID()
	caps := []string{"cap1", "cap2"}

	h := NewHandshaker(localID, "test-agent", caps, 5*time.Second)

	if h.localID != localID {
		t.Error("localID not set correctly")
	}
	if len(h.capabilities) != 2 {
		t.Error("capabilities not set correctly")
	}
	if h.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", h.timeout)
	}
}

func TestNewHandshaker_DefaultTimeout(t *testing.T) {
	localID, _ := identity.NewAgentID()

	h := NewHandshaker(localID, "", nil, 0)

	if h.timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", h.timeout)
	}
}

// ============================================================================
// Manager Tests
// ============================================================================

func TestDefaultManagerConfig(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)

	if cfg.LocalID != localID {
		t.Error("LocalID not set")
	}
	if cfg.Transport != tr {
		t.Error("Transport not set")
	}
	if cfg.HandshakeTimeout != 10*time.Second {
		t.Errorf("HandshakeTimeout = %v, want 10s", cfg.HandshakeTimeout)
	}
	if cfg.KeepaliveInterval != 30*time.Second {
		t.Errorf("KeepaliveInterval = %v, want 30s", cfg.KeepaliveInterval)
	}
	if cfg.KeepaliveJitter != 0.2 {
		t.Errorf("KeepaliveJitter = %v, want 0.2", cfg.KeepaliveJitter)
	}
}

func TestManager_JitteredKeepaliveInterval_NoJitter(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 30 * time.Second
	cfg.KeepaliveJitter = 0 // No jitter

	m := NewManager(cfg)
	defer m.Close()

	// With no jitter, interval should always be exactly the base interval
	for i := 0; i < 10; i++ {
		interval := m.jitteredKeepaliveInterval()
		if interval != 30*time.Second {
			t.Errorf("With jitter=0, interval should be exactly 30s, got %v", interval)
		}
	}
}

func TestManager_JitteredKeepaliveInterval_WithJitter(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 30 * time.Second
	cfg.KeepaliveJitter = 0.2 // 20% jitter

	m := NewManager(cfg)
	defer m.Close()

	// Expected range: 30s * (1 +/- 0.2) = 24s to 36s
	minExpected := 24 * time.Second
	maxExpected := 36 * time.Second

	// Sample multiple intervals and verify they fall within range
	var seenDifferent bool
	var firstInterval time.Duration
	for i := 0; i < 100; i++ {
		interval := m.jitteredKeepaliveInterval()

		if i == 0 {
			firstInterval = interval
		} else if interval != firstInterval {
			seenDifferent = true
		}

		if interval < minExpected || interval > maxExpected {
			t.Errorf("Jittered interval %v outside expected range [%v, %v]", interval, minExpected, maxExpected)
		}
	}

	// With jitter, we should see variation (statistically extremely unlikely to get 100 identical values)
	if !seenDifferent {
		t.Error("Expected to see variation in jittered intervals, but all were identical")
	}
}

func TestManager_JitteredKeepaliveInterval_MinimumEnforced(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 500 * time.Millisecond // Very short base interval
	cfg.KeepaliveJitter = 0.9                      // 90% jitter could go below 1s

	m := NewManager(cfg)
	defer m.Close()

	// With 90% jitter on 500ms, theoretical minimum would be 50ms
	// But the function should enforce a minimum of 1 second
	for i := 0; i < 100; i++ {
		interval := m.jitteredKeepaliveInterval()
		if interval < time.Second {
			t.Errorf("Interval %v is below minimum 1s", interval)
		}
	}
}

func TestManager_RegisterConnection_EvictsStaleDuplicate(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 100 * time.Millisecond
	cfg.KeepaliveTimeout = 100 * time.Millisecond
	var disconnectCount atomic.Int32
	cfg.OnPeerDisconnect = func(*Connection, error) { disconnectCount.Add(1) }

	m := NewManager(cfg)
	defer m.Close()

	remoteID, _ := identity.NewAgentID()

	// Build a stale "existing" connection and insert it directly so
	// registerConnection sees it as a duplicate without us having to
	// orchestrate a real handshake.
	oldConn := NewConnection(&mockPeerConn{}, ConnectionConfig{LocalID: localID})
	oldConn.RemoteID = remoteID
	oldConn.lastActivity.Store(time.Now().Add(-1 * time.Hour).UnixNano())
	oldConn.lastInbound.Store(time.Now().Add(-1 * time.Hour).UnixNano())
	m.peers[remoteID] = oldConn

	newConn := NewConnection(&mockPeerConn{}, ConnectionConfig{LocalID: localID})
	newConn.RemoteID = remoteID

	m.registerConnection(newConn)

	if got := m.GetPeer(remoteID); got != newConn {
		t.Fatalf("expected new connection in peers, got %v", got)
	}

	// Close() flips state to StateDisconnected; verify the stale one was
	// actually closed rather than left hanging.
	if oldConn.State() != StateDisconnected {
		t.Errorf("expected stale connection closed, state=%v", oldConn.State())
	}

	// Eviction must NOT fire OnPeerDisconnect for the stale connection;
	// otherwise the agent-level cleanup would wipe out the routes the new
	// connection just installed.
	if got := disconnectCount.Load(); got != 0 {
		t.Errorf("expected no OnPeerDisconnect call on stale eviction, got %d", got)
	}
}

func TestManager_RegisterConnection_KeepsFreshDuplicate(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	remoteID, _ := identity.NewAgentID()

	// Fresh existing connection (NewConnection sets lastActivity to now).
	oldConn := NewConnection(&mockPeerConn{}, ConnectionConfig{LocalID: localID})
	oldConn.RemoteID = remoteID
	m.peers[remoteID] = oldConn

	// New incoming connection with the same RemoteID -- duplicate while the
	// existing one is healthy, so the new one must be the one that loses.
	newConn := NewConnection(&mockPeerConn{}, ConnectionConfig{LocalID: localID})
	newConn.RemoteID = remoteID

	m.registerConnection(newConn)

	if got := m.GetPeer(remoteID); got != oldConn {
		t.Fatalf("expected existing connection retained, got %v", got)
	}
	if newConn.State() != StateDisconnected {
		t.Errorf("expected duplicate connection closed, state=%v", newConn.State())
	}
}

func TestManager_AddRemovePeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	peerID, _ := identity.NewAgentID()
	info := PeerInfo{
		Address:    "127.0.0.1:8080",
		ExpectedID: peerID,
		Persistent: true,
	}

	// Add peer
	m.AddPeer(info)

	// Verify peer was added
	m.mu.RLock()
	_, exists := m.peerInfos["127.0.0.1:8080"]
	m.mu.RUnlock()

	if !exists {
		t.Error("Peer should be added")
	}

	// Remove peer
	m.RemovePeer("127.0.0.1:8080")

	m.mu.RLock()
	_, exists = m.peerInfos["127.0.0.1:8080"]
	m.mu.RUnlock()

	if exists {
		t.Error("Peer should be removed")
	}
}

func TestManager_GetPeer_NotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	unknownID, _ := identity.NewAgentID()
	if conn := m.GetPeer(unknownID); conn != nil {
		t.Error("Should return nil for unknown peer")
	}
}

func TestManager_PeerCount(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	if m.PeerCount() != 0 {
		t.Errorf("Initial PeerCount = %d, want 0", m.PeerCount())
	}
}

func TestManager_Close(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)

	// Add some peer configs
	m.AddPeer(PeerInfo{Address: "127.0.0.1:8080"})
	m.AddPeer(PeerInfo{Address: "127.0.0.1:8081"})

	// Close should not panic
	if err := m.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}

	// PeerCount should be 0 after close
	if m.PeerCount() != 0 {
		t.Errorf("PeerCount after close = %d, want 0", m.PeerCount())
	}
}

// ============================================================================
// Protocol Integration Tests
// ============================================================================

func TestPeerHello_Roundtrip(t *testing.T) {
	localID, _ := identity.NewAgentID()

	hello := &protocol.PeerHello{
		Version:      protocol.ProtocolVersion,
		AgentID:      localID,
		Timestamp:    uint64(time.Now().UnixNano()),
		Capabilities: []string{"exit", "relay"},
	}

	// Encode
	data := hello.Encode()

	// Decode
	decoded, err := protocol.DecodePeerHello(data)
	if err != nil {
		t.Fatalf("DecodePeerHello failed: %v", err)
	}

	if decoded.Version != hello.Version {
		t.Errorf("Version = %d, want %d", decoded.Version, hello.Version)
	}
	if decoded.AgentID != hello.AgentID {
		t.Errorf("AgentID mismatch")
	}
	if decoded.Timestamp != hello.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, hello.Timestamp)
	}
	if len(decoded.Capabilities) != len(hello.Capabilities) {
		t.Errorf("Capabilities count = %d, want %d", len(decoded.Capabilities), len(hello.Capabilities))
	}
}

// ============================================================================
// Mock implementations for testing
// ============================================================================

type mockPeerConn struct {
	localAddr  string
	remoteAddr string
	isDialer   bool
	closed     bool
	mu         sync.Mutex
	streams    []*mockStream
}

func (m *mockPeerConn) OpenStream(ctx context.Context) (transport.Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &mockStream{}
	m.streams = append(m.streams, s)
	return s, nil
}

func (m *mockPeerConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &mockStream{}
	m.streams = append(m.streams, s)
	return s, nil
}

func (m *mockPeerConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockPeerConn) LocalAddr() net.Addr {
	return &mockAddr{addr: m.localAddr}
}

func (m *mockPeerConn) RemoteAddr() net.Addr {
	return &mockAddr{addr: m.remoteAddr}
}

func (m *mockPeerConn) IsDialer() bool {
	return m.isDialer
}

func (m *mockPeerConn) TransportType() transport.TransportType {
	return transport.TransportQUIC
}

type mockAddr struct {
	addr string
}

func (a *mockAddr) Network() string { return "mock" }
func (a *mockAddr) String() string  { return a.addr }

type mockStream struct {
	data     []byte
	readPos  int
	closed   bool
	mu       sync.Mutex
	streamID uint64
}

func (s *mockStream) StreamID() uint64 {
	return s.streamID
}

func (s *mockStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readPos >= len(s.data) {
		return 0, context.DeadlineExceeded // Simulate timeout
	}
	n := copy(p, s.data[s.readPos:])
	s.readPos += n
	return n, nil
}

func (s *mockStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, p...)
	return len(p), nil
}

func (s *mockStream) CloseWrite() error {
	return nil
}

func (s *mockStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *mockStream) SetDeadline(t time.Time) error {
	return nil
}

func (s *mockStream) SetReadDeadline(t time.Time) error {
	return nil
}

func (s *mockStream) SetWriteDeadline(t time.Time) error {
	return nil
}

// ============================================================================
// addrToString Tests
// ============================================================================

func TestAddrToString(t *testing.T) {
	tests := []struct {
		name string
		addr net.Addr
		want string
	}{
		{
			name: "nil address",
			addr: nil,
			want: "",
		},
		{
			name: "mock address",
			addr: &mockAddr{addr: "127.0.0.1:8080"},
			want: "127.0.0.1:8080",
		},
		{
			name: "empty address",
			addr: &mockAddr{addr: ""},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addrToString(tt.addr)
			if got != tt.want {
				t.Errorf("addrToString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Connection Address Tests
// ============================================================================

func TestConnection_LocalAddr(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	tests := []struct {
		name      string
		localAddr string
		want      string
	}{
		{
			name:      "with address",
			localAddr: "192.168.1.1:5000",
			want:      "192.168.1.1:5000",
		},
		{
			name:      "empty address",
			localAddr: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockPeerConn{localAddr: tt.localAddr}
			conn := NewConnection(mockConn, cfg)
			defer conn.Close()

			if got := conn.LocalAddr(); got != tt.want {
				t.Errorf("LocalAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnection_LocalAddr_NilConn(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Set conn.conn to nil to test nil check
	// Note: we don't defer Close() here because that would panic with nil conn
	conn.conn = nil

	if got := conn.LocalAddr(); got != "" {
		t.Errorf("LocalAddr() with nil conn = %q, want empty string", got)
	}
}

func TestConnection_RemoteAddr(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{
			name:       "with address",
			remoteAddr: "10.0.0.1:4433",
			want:       "10.0.0.1:4433",
		},
		{
			name:       "empty address",
			remoteAddr: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockPeerConn{remoteAddr: tt.remoteAddr}
			conn := NewConnection(mockConn, cfg)
			defer conn.Close()

			if got := conn.RemoteAddr(); got != tt.want {
				t.Errorf("RemoteAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnection_RemoteAddr_NilConn(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Set conn.conn to nil to test nil check
	// Note: we don't defer Close() here because that would panic with nil conn
	conn.conn = nil

	if got := conn.RemoteAddr(); got != "" {
		t.Errorf("RemoteAddr() with nil conn = %q, want empty string", got)
	}
}

func TestConnection_ConfigAddr(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Initially empty
	if got := conn.ConfigAddr(); got != "" {
		t.Errorf("ConfigAddr() initially = %q, want empty string", got)
	}

	// Set and verify
	conn.SetConfigAddr("example.com:4433")
	if got := conn.ConfigAddr(); got != "example.com:4433" {
		t.Errorf("ConfigAddr() after set = %q, want %q", got, "example.com:4433")
	}
}

// ============================================================================
// Connection Transport Tests
// ============================================================================

func TestConnection_TransportType(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	if got := conn.TransportType(); got != transport.TransportQUIC {
		t.Errorf("TransportType() = %q, want %q", got, transport.TransportQUIC)
	}
}

func TestConnection_TransportType_NilConn(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Set conn.conn to nil to test nil check
	// Note: we don't defer Close() here because that would panic with nil conn
	conn.conn = nil

	if got := conn.TransportType(); got != "" {
		t.Errorf("TransportType() with nil conn = %q, want empty string", got)
	}
}

func TestConnection_IsDialer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	tests := []struct {
		name     string
		isDialer bool
	}{
		{"dialer", true},
		{"listener", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockConn := &mockPeerConn{isDialer: tt.isDialer}
			conn := NewConnection(mockConn, cfg)
			defer conn.Close()

			if got := conn.IsDialer(); got != tt.isDialer {
				t.Errorf("IsDialer() = %v, want %v", got, tt.isDialer)
			}
		})
	}
}

// ============================================================================
// Connection String Representation Tests
// ============================================================================

func TestConnection_String(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	mockConn := &mockPeerConn{remoteAddr: "10.0.0.1:4433"}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	conn.RemoteID = remoteID

	str := conn.String()

	// Should contain the short ID
	if !contains(str, remoteID.ShortString()) {
		t.Errorf("String() = %q, should contain remote ID short string", str)
	}

	// Should contain the state
	if !contains(str, "HANDSHAKING") {
		t.Errorf("String() = %q, should contain state", str)
	}

	// Should contain the address
	if !contains(str, "10.0.0.1:4433") {
		t.Errorf("String() = %q, should contain address", str)
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// Connection Ready/Context Tests
// ============================================================================

func TestConnection_Ready(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Ready channel should not be closed initially
	select {
	case <-conn.Ready():
		t.Error("Ready channel should not be closed initially")
	default:
	}

	// Mark as ready
	conn.markReady()

	// Ready channel should now be closed
	select {
	case <-conn.Ready():
	default:
		t.Error("Ready channel should be closed after markReady()")
	}

	// Multiple markReady calls should not panic
	conn.markReady()
	conn.markReady()
}

func TestConnection_Context(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	ctx := conn.Context()
	if ctx == nil {
		t.Error("Context() should not return nil")
	}

	// Context should not be cancelled initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be cancelled initially")
	default:
	}

	// Close connection
	conn.Close()

	// Context should be cancelled after close
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled after close")
	}
}

// ============================================================================
// Connection Stream Tests
// ============================================================================

func TestConnection_NextStreamID(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)

	t.Run("dialer gets odd IDs", func(t *testing.T) {
		mockConn := &mockPeerConn{isDialer: true}
		conn := NewConnection(mockConn, cfg)
		defer conn.Close()

		for i := 0; i < 5; i++ {
			id := conn.NextStreamID()
			if id%2 != 1 {
				t.Errorf("Dialer NextStreamID() = %d, want odd number", id)
			}
		}
	})

	t.Run("listener gets even IDs", func(t *testing.T) {
		mockConn := &mockPeerConn{isDialer: false}
		conn := NewConnection(mockConn, cfg)
		defer conn.Close()

		for i := 0; i < 5; i++ {
			id := conn.NextStreamID()
			if id%2 != 0 {
				t.Errorf("Listener NextStreamID() = %d, want even number", id)
			}
		}
	})
}

func TestConnection_OpenStream_NotConnected(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// State is HANDSHAKING, not CONNECTED
	_, err := conn.OpenStream(context.Background())
	if err == nil {
		t.Error("OpenStream() should fail when not connected")
	}
}

func TestConnection_OpenStream_Connected(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	conn.SetState(StateConnected)

	stream, err := conn.OpenStream(context.Background())
	if err != nil {
		t.Errorf("OpenStream() error = %v, want nil", err)
	}
	if stream == nil {
		t.Error("OpenStream() returned nil stream")
	}
}

func TestConnection_AcceptStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		t.Errorf("AcceptStream() error = %v, want nil", err)
	}
	if stream == nil {
		t.Error("AcceptStream() returned nil stream")
	}
}

// ============================================================================
// Connection Write Tests
// ============================================================================

func TestConnection_WriteFrame_NotInitialized(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Writer is nil
	err := conn.WriteFrame(&protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 1,
		Payload:  []byte("test"),
	})

	if err == nil {
		t.Error("WriteFrame() should fail when writer not initialized")
	}
}

func TestConnection_WriteFrame_Success(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	// Set up a mock stream for the writer
	mockStream := &mockStream{}
	conn.writer = protocol.NewFrameWriter(mockStream)

	err := conn.WriteFrame(&protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 1,
		Payload:  []byte("test"),
	})

	if err != nil {
		t.Errorf("WriteFrame() error = %v, want nil", err)
	}
}

func TestConnection_SendData(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	mockStream := &mockStream{}
	conn.writer = protocol.NewFrameWriter(mockStream)

	err := conn.SendData(1, []byte("hello"))
	if err != nil {
		t.Errorf("SendData() error = %v, want nil", err)
	}

	// Verify data was written to the stream
	if len(mockStream.data) == 0 {
		t.Error("SendData() should write data to stream")
	}
}

func TestConnection_SendKeepalive(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	mockStream := &mockStream{}
	conn.writer = protocol.NewFrameWriter(mockStream)

	err := conn.SendKeepalive()
	if err != nil {
		t.Errorf("SendKeepalive() error = %v, want nil", err)
	}

	if len(mockStream.data) == 0 {
		t.Error("SendKeepalive() should write data to stream")
	}
}

func TestConnection_SendKeepaliveAck(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	mockStream := &mockStream{}
	conn.writer = protocol.NewFrameWriter(mockStream)

	timestamp := uint64(time.Now().UnixNano())
	err := conn.SendKeepaliveAck(timestamp)
	if err != nil {
		t.Errorf("SendKeepaliveAck() error = %v, want nil", err)
	}

	if len(mockStream.data) == 0 {
		t.Error("SendKeepaliveAck() should write data to stream")
	}
}

// ============================================================================
// Connection Capabilities Tests
// ============================================================================

func TestConnection_Capabilities(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	cfg.Capabilities = []string{"exit", "relay", "shell"}

	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	defer conn.Close()

	conn.capabilities = cfg.Capabilities

	caps := conn.Capabilities()
	if len(caps) != 3 {
		t.Errorf("Capabilities() returned %d items, want 3", len(caps))
	}
}

// ============================================================================
// Manager Extended Tests
// ============================================================================

func TestManager_GetAllPeers(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	// Initially empty
	peers := m.GetAllPeers()
	if len(peers) != 0 {
		t.Errorf("GetAllPeers() initially returned %d peers, want 0", len(peers))
	}
}

func TestManager_GetPeerIDs(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	// Initially empty
	ids := m.GetPeerIDs()
	if len(ids) != 0 {
		t.Errorf("GetPeerIDs() initially returned %d IDs, want 0", len(ids))
	}
}

func TestManager_Broadcast_NoPeers(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	// Broadcast with no peers should succeed (no-op)
	err := m.Broadcast(&protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 1,
		Payload:  []byte("test"),
	})

	if err != nil {
		t.Errorf("Broadcast() with no peers error = %v, want nil", err)
	}
}

func TestManager_Disconnect_NotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	unknownID, _ := identity.NewAgentID()
	err := m.Disconnect(unknownID)
	if err == nil {
		t.Error("Disconnect() should fail for unknown peer")
	}
}

func TestManager_SendToPeer_NotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	unknownID, _ := identity.NewAgentID()
	err := m.SendToPeer(unknownID, &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 1,
		Payload:  []byte("test"),
	})

	if err == nil {
		t.Error("SendToPeer() should fail for unknown peer")
	}
}

func TestManager_SetFrameCallback(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	m.SetFrameCallback(func(id identity.AgentID, frame *protocol.Frame) {
		// Callback registered
	})

	// Verify callback was set
	if m.cfg.OnFrame == nil {
		t.Error("SetFrameCallback should set OnFrame callback")
	}
}

func TestManager_SetFrameCallback_Nil(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	m := NewManager(cfg)
	defer m.Close()

	// Set a nil callback - should not panic
	m.SetFrameCallback(nil)
}

func TestManager_buildConnectionConfig_WithInfo(t *testing.T) {
	localID, _ := identity.NewAgentID()
	expectedID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.Capabilities = []string{"exit", "relay"}
	m := NewManager(cfg)
	defer m.Close()

	dialOpts := transport.DialOptions{
		Timeout: 5 * time.Second,
	}
	info := &PeerInfo{
		Address:     "10.0.0.1:4433",
		ExpectedID:  expectedID,
		Persistent:  true,
		DialOptions: &dialOpts,
	}

	connCfg, opts := m.buildConnectionConfig(info)

	if connCfg.LocalID != localID {
		t.Error("buildConnectionConfig should set LocalID")
	}
	if connCfg.ExpectedPeerID != expectedID {
		t.Error("buildConnectionConfig should set ExpectedPeerID from info")
	}
	if len(connCfg.Capabilities) != 2 {
		t.Errorf("buildConnectionConfig Capabilities = %d, want 2", len(connCfg.Capabilities))
	}
	if opts.Timeout != 5*time.Second {
		t.Errorf("buildConnectionConfig DialOptions.Timeout = %v, want 5s", opts.Timeout)
	}
}

func TestManager_buildConnectionConfig_NilInfo(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.DialOptions = transport.DialOptions{
		Timeout: 10 * time.Second,
	}
	m := NewManager(cfg)
	defer m.Close()

	connCfg, opts := m.buildConnectionConfig(nil)

	if connCfg.LocalID != localID {
		t.Error("buildConnectionConfig with nil info should set LocalID")
	}
	if connCfg.ExpectedPeerID != (identity.AgentID{}) {
		t.Error("buildConnectionConfig with nil info should have empty ExpectedPeerID")
	}
	if opts.Timeout != 10*time.Second {
		t.Error("buildConnectionConfig with nil info should use default DialOptions")
	}
}

// ============================================================================
// Reconnector Extended Tests
// ============================================================================

func TestReconnector_GetAttempts(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
		MaxAttempts:  0,
	}

	callback := func(addr string) error {
		return context.DeadlineExceeded
	}

	r := NewReconnector(cfg, callback)
	defer r.Stop()

	// Initially 0 attempts
	if r.GetAttempts("127.0.0.1:8080") != 0 {
		t.Error("GetAttempts should return 0 for unknown address")
	}

	// Schedule and wait for an attempt
	r.Schedule("127.0.0.1:8080")
	time.Sleep(50 * time.Millisecond)

	attempts := r.GetAttempts("127.0.0.1:8080")
	if attempts < 1 {
		t.Errorf("GetAttempts after schedule = %d, want >= 1", attempts)
	}
}

func TestReconnector_Reset(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  0,
	}

	callback := func(addr string) error {
		return context.DeadlineExceeded
	}

	r := NewReconnector(cfg, callback)
	defer r.Stop()

	// Schedule reconnection
	r.Schedule("127.0.0.1:8080")

	// Verify pending
	if !r.IsPending("127.0.0.1:8080") {
		t.Error("Should be pending after Schedule")
	}

	// Reset (same as Cancel)
	r.Reset("127.0.0.1:8080")

	// Should not be pending
	if r.IsPending("127.0.0.1:8080") {
		t.Error("Should not be pending after Reset")
	}
}

func TestReconnector_clearState(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  0,
	}

	callback := func(addr string) error {
		return context.DeadlineExceeded
	}

	r := NewReconnector(cfg, callback)
	defer r.Stop()

	// Schedule reconnection
	r.Schedule("127.0.0.1:8080")
	r.Schedule("127.0.0.1:8081")

	// Clear one
	r.clearState("127.0.0.1:8080")

	if r.IsPending("127.0.0.1:8080") {
		t.Error("Should not be pending after clearState")
	}
	if !r.IsPending("127.0.0.1:8081") {
		t.Error("Other address should still be pending")
	}

	// Clear non-existent address (should not panic)
	r.clearState("192.168.1.1:9999")
}

func TestReconnector_addJitter(t *testing.T) {
	tests := []struct {
		name      string
		jitter    float64
		duration  time.Duration
		wantExact bool // If true, result should equal duration exactly
	}{
		{
			name:      "no jitter",
			jitter:    0,
			duration:  1 * time.Second,
			wantExact: true,
		},
		{
			name:      "negative jitter treated as zero",
			jitter:    -0.5,
			duration:  1 * time.Second,
			wantExact: true,
		},
		{
			name:      "with jitter",
			jitter:    0.2,
			duration:  1 * time.Second,
			wantExact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ReconnectConfig{
				InitialDelay: 1 * time.Second,
				MaxDelay:     60 * time.Second,
				Multiplier:   2.0,
				Jitter:       tt.jitter,
			}
			r := NewReconnector(cfg, nil)
			defer r.Stop()

			result := r.addJitter(tt.duration)

			if tt.wantExact {
				if result != tt.duration {
					t.Errorf("addJitter() = %v, want %v", result, tt.duration)
				}
			} else {
				// With jitter, result should be different from exact value (usually)
				// Just verify it's positive
				if result <= 0 {
					t.Errorf("addJitter() = %v, want positive duration", result)
				}
			}
		})
	}
}

func TestReconnector_Schedule_AlreadyClosed(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	callback := func(addr string) error {
		return nil
	}

	r := NewReconnector(cfg, callback)
	r.Stop()

	// Schedule after stop should not panic
	r.Schedule("127.0.0.1:8080")

	// Should not be pending since reconnector is closed
	if r.IsPending("127.0.0.1:8080") {
		t.Error("Should not be pending after Stop")
	}
}

func TestBackoffCalculator_EdgeCases(t *testing.T) {
	cfg := ReconnectConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}
	calc := NewBackoffCalculator(cfg)

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{-5, 1 * time.Second},  // Negative attempt should return initial
		{-1, 1 * time.Second},  // Negative attempt
		{0, 1 * time.Second},   // Zero attempt
		{1, 2 * time.Second},   // First retry
		{10, 10 * time.Second}, // Should be capped at max
		{100, 10 * time.Second},
	}

	for _, tt := range tests {
		got := calc.CalculateDelay(tt.attempt)
		if got != tt.want {
			t.Errorf("CalculateDelay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

// ============================================================================
// Concurrency Race Condition Tests
// ============================================================================

// TestConnection_ConcurrentCloseAndFrameSend verifies that closing a connection
// while frames are being sent to frameCh does not panic (send on closed channel).
func TestConnection_ConcurrentCloseAndFrameSend(t *testing.T) {
	localID, _ := identity.NewAgentID()

	cfg := ConnectionConfig{
		LocalID:          localID,
		HandshakeTimeout: 10 * time.Second,
		OnFrame:          func(c *Connection, f *protocol.Frame) {},
	}

	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)

	// Mark ready so frames can be processed
	conn.markReady()

	var wg sync.WaitGroup

	// Spawn senders that send frames until the connection is closed
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				frame := &protocol.Frame{
					Type:     protocol.FrameStreamData,
					StreamID: 1,
					Payload:  []byte("test"),
				}
				select {
				case conn.frameCh <- frame:
				case <-conn.Done():
					return
				}
			}
		}()
	}

	// Close after a brief delay to create the race window
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(1 * time.Millisecond)
		conn.Close()
	}()

	wg.Wait()

	// Verify connection is closed
	select {
	case <-conn.Done():
		// expected
	default:
		t.Error("connection Done() channel should be closed")
	}
}

// TestConnection_CloseWithPendingFrames verifies that closing a connection
// with buffered frames in frameCh does not panic or leak.
func TestConnection_CloseWithPendingFrames(t *testing.T) {
	localID, _ := identity.NewAgentID()

	cfg := ConnectionConfig{
		LocalID:          localID,
		HandshakeTimeout: 10 * time.Second,
		OnFrame: func(c *Connection, f *protocol.Frame) {
			// Slow handler to allow buffer to fill
			time.Sleep(10 * time.Millisecond)
		},
	}

	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	conn.markReady()

	// Fill the frame channel buffer (capacity 256)
	filled := 0
	for i := 0; i < 256; i++ {
		frame := &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: 1,
			Payload:  []byte("test"),
		}
		select {
		case conn.frameCh <- frame:
			filled++
		default:
			// Buffer full
		}
	}

	if filled == 0 {
		t.Fatal("failed to fill any frames into buffer")
	}

	// Close with buffered frames -- must not panic
	conn.Close()

	select {
	case <-conn.Done():
		// expected
	default:
		t.Error("connection Done() channel should be closed")
	}
}

// TestConnection_ProcessFramesExitsOnClose verifies that processFrames
// stops processing after the connection is closed.
func TestConnection_ProcessFramesExitsOnClose(t *testing.T) {
	localID, _ := identity.NewAgentID()

	var processed atomic.Int64
	cfg := ConnectionConfig{
		LocalID:          localID,
		HandshakeTimeout: 10 * time.Second,
		OnFrame: func(c *Connection, f *protocol.Frame) {
			processed.Add(1)
		},
	}

	mockConn := &mockPeerConn{}
	conn := NewConnection(mockConn, cfg)
	conn.markReady()

	// Send some frames
	for i := 0; i < 5; i++ {
		conn.frameCh <- &protocol.Frame{
			Type:     protocol.FrameStreamData,
			StreamID: 1,
			Payload:  []byte("test"),
		}
	}

	// Wait for frames to be processed
	time.Sleep(50 * time.Millisecond)

	beforeClose := processed.Load()
	if beforeClose == 0 {
		t.Fatal("no frames were processed before close")
	}

	// Close the connection
	conn.Close()

	// Wait for processFrames to exit
	time.Sleep(50 * time.Millisecond)

	// Try to send more frames -- should not be processed
	select {
	case conn.frameCh <- &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 1,
		Payload:  []byte("after-close"),
	}:
		// Sent to buffer, but won't be processed
	default:
		// Channel buffer might be full, that's ok
	}

	time.Sleep(50 * time.Millisecond)
	afterClose := processed.Load()

	// No new frames should have been processed after close
	if afterClose > beforeClose {
		t.Errorf("frames processed after close: before=%d, after=%d", beforeClose, afterClose)
	}
}

// ============================================================================
// Dynamic Peer Management Tests (RemovePeerAndDisconnect, ConnectedByAddr)
// ============================================================================

func TestManager_RemovePeerAndDisconnect_NoInfoNoConn(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	// Should be a no-op when nothing matches. Must not panic.
	id, closed := m.RemovePeerAndDisconnect("unknown:1234")
	if closed {
		t.Errorf("closed = true, want false for unknown addr")
	}
	if !id.IsZero() {
		t.Errorf("returned id = %v, want zero", id)
	}
}

func TestManager_RemovePeerAndDisconnect_InfoOnly(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	addr := "10.0.0.1:4433"
	m.AddPeer(PeerInfo{Address: addr, Persistent: true})

	id, closed := m.RemovePeerAndDisconnect(addr)
	if closed {
		t.Errorf("closed = true, want false when no live connection exists")
	}
	if !id.IsZero() {
		t.Errorf("returned id = %v, want zero", id)
	}

	m.mu.RLock()
	_, exists := m.peerInfos[addr]
	m.mu.RUnlock()
	if exists {
		t.Error("PeerInfo should be removed from peerInfos")
	}
}

func TestManager_RemovePeerAndDisconnect_WithConnection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	addr := "10.0.0.1:4433"
	remoteID, _ := identity.NewAgentID()

	m.AddPeer(PeerInfo{Address: addr, Persistent: true})

	// Inject a live Connection with matching ConfigAddr directly into the
	// manager's peers map -- avoids the handshake machinery.
	cfg := DefaultConnectionConfig(localID)
	mock := &mockPeerConn{}
	conn := NewConnection(mock, cfg)
	conn.RemoteID = remoteID
	conn.SetConfigAddr(addr)

	m.mu.Lock()
	m.peers[remoteID] = conn
	m.mu.Unlock()

	gotID, closed := m.RemovePeerAndDisconnect(addr)
	if !closed {
		t.Fatal("closed = false, want true when live connection exists")
	}
	if gotID != remoteID {
		t.Errorf("returned id = %v, want %v", gotID, remoteID)
	}

	// PeerInfo cleared, connection cleared, mock closed.
	m.mu.RLock()
	_, hasInfo := m.peerInfos[addr]
	_, hasPeer := m.peers[remoteID]
	m.mu.RUnlock()
	if hasInfo {
		t.Error("PeerInfo should be removed")
	}
	if hasPeer {
		t.Error("live connection entry should be removed")
	}
	mock.mu.Lock()
	mockClosed := mock.closed
	mock.mu.Unlock()
	if !mockClosed {
		t.Error("underlying peer conn should be closed")
	}
}

func TestManager_RemovePeerAndDisconnect_CancelsReconnect(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	addr := "10.0.0.1:4433"
	m.AddPeer(PeerInfo{Address: addr, Persistent: true})

	// Prime the reconnector with pending backoff state for this address.
	m.reconnector.Schedule(addr)
	if !m.reconnector.IsPending(addr) {
		t.Fatal("expected reconnector to be pending after Schedule")
	}

	m.RemovePeerAndDisconnect(addr)

	if m.reconnector.IsPending(addr) {
		t.Error("reconnector still pending after RemovePeerAndDisconnect")
	}
}

func TestManager_ConnectedByAddr_Empty(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	got := m.ConnectedByAddr()
	if len(got) != 0 {
		t.Errorf("ConnectedByAddr on empty manager = %v, want empty map", got)
	}
}

func TestManager_ConnectedByAddr_SkipsAcceptedConns(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	dialerID, _ := identity.NewAgentID()
	acceptedID, _ := identity.NewAgentID()

	cfg := DefaultConnectionConfig(localID)

	// Dialer-side connection has a ConfigAddr.
	dialerMock := &mockPeerConn{}
	dialerConn := NewConnection(dialerMock, cfg)
	dialerConn.RemoteID = dialerID
	dialerConn.SetConfigAddr("10.0.0.1:4433")

	// Accepted-side connection has no ConfigAddr (empty string).
	acceptedMock := &mockPeerConn{}
	acceptedConn := NewConnection(acceptedMock, cfg)
	acceptedConn.RemoteID = acceptedID
	// Intentionally no SetConfigAddr -- this is an inbound connection.

	m.mu.Lock()
	m.peers[dialerID] = dialerConn
	m.peers[acceptedID] = acceptedConn
	m.mu.Unlock()

	got := m.ConnectedByAddr()

	if len(got) != 1 {
		t.Fatalf("ConnectedByAddr = %v, want 1 entry (accepted conn should be skipped)", got)
	}
	if gotID, ok := got["10.0.0.1:4433"]; !ok || gotID != dialerID {
		t.Errorf("ConnectedByAddr[%q] = %v, want %v", "10.0.0.1:4433", gotID, dialerID)
	}
}

func TestManager_AcceptedConnections_Empty(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	got := m.AcceptedConnections()
	if len(got) != 0 {
		t.Errorf("AcceptedConnections on empty manager = %v, want empty slice", got)
	}
}

func TestManager_AcceptedConnections_ReturnsOnlyInbound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	dialerID, _ := identity.NewAgentID()
	acceptedID, _ := identity.NewAgentID()

	cfg := DefaultConnectionConfig(localID)

	// Outbound (dialer) connection has a ConfigAddr.
	dialerMock := &mockPeerConn{remoteAddr: "10.0.0.1:4433"}
	dialerConn := NewConnection(dialerMock, cfg)
	dialerConn.RemoteID = dialerID
	dialerConn.SetConfigAddr("10.0.0.1:4433")

	// Accepted connection has no ConfigAddr; the remote addr is the ephemeral
	// source socket the peer dialed from.
	acceptedMock := &mockPeerConn{remoteAddr: "198.51.100.7:54321"}
	acceptedConn := NewConnection(acceptedMock, cfg)
	acceptedConn.RemoteID = acceptedID

	m.mu.Lock()
	m.peers[dialerID] = dialerConn
	m.peers[acceptedID] = acceptedConn
	m.mu.Unlock()

	got := m.AcceptedConnections()

	if len(got) != 1 {
		t.Fatalf("AcceptedConnections = %v, want 1 entry (outbound should be skipped)", got)
	}
	entry := got[0]
	if entry.RemoteID != acceptedID {
		t.Errorf("RemoteID = %v, want %v", entry.RemoteID, acceptedID)
	}
	if entry.RemoteAddr != "198.51.100.7:54321" {
		t.Errorf("RemoteAddr = %q, want %q", entry.RemoteAddr, "198.51.100.7:54321")
	}
	if entry.Transport != transport.TransportQUIC {
		t.Errorf("Transport = %q, want %q", entry.Transport, transport.TransportQUIC)
	}
}

func TestManager_AcceptedConnections_DisjointFromGetPeerInfos(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	m := NewManager(DefaultManagerConfig(localID, tr))
	defer m.Close()

	// Configured outbound peer.
	outboundAddr := "10.0.0.1:4433"
	m.AddPeer(PeerInfo{Address: outboundAddr, Persistent: true})

	dialerID, _ := identity.NewAgentID()
	acceptedID, _ := identity.NewAgentID()

	cfg := DefaultConnectionConfig(localID)

	dialerMock := &mockPeerConn{remoteAddr: outboundAddr}
	dialerConn := NewConnection(dialerMock, cfg)
	dialerConn.RemoteID = dialerID
	dialerConn.SetConfigAddr(outboundAddr)

	acceptedMock := &mockPeerConn{remoteAddr: "198.51.100.7:54321"}
	acceptedConn := NewConnection(acceptedMock, cfg)
	acceptedConn.RemoteID = acceptedID

	m.mu.Lock()
	m.peers[dialerID] = dialerConn
	m.peers[acceptedID] = acceptedConn
	m.mu.Unlock()

	infos := m.GetPeerInfos()
	accepted := m.AcceptedConnections()

	if _, ok := infos[outboundAddr]; !ok {
		t.Fatalf("GetPeerInfos missing outbound entry for %q", outboundAddr)
	}
	if len(accepted) != 1 {
		t.Fatalf("AcceptedConnections = %v, want exactly one inbound entry", accepted)
	}
	if accepted[0].RemoteID == dialerID {
		t.Error("AcceptedConnections leaked outbound peer into inbound result")
	}
	if _, ok := infos[accepted[0].RemoteAddr]; ok {
		t.Errorf("GetPeerInfos unexpectedly keyed on inbound RemoteAddr %q", accepted[0].RemoteAddr)
	}
}

// ============================================================================
// Write Deadline Tests
// ============================================================================

// deadlineTrackingStream records SetWriteDeadline calls so tests can verify
// WriteFrame arms and disarms a per-write deadline on the control stream.
type deadlineTrackingStream struct {
	mockStream
	writeDeadline time.Time
	deadlineCalls int
}

func (s *deadlineTrackingStream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline = t
	s.deadlineCalls++
	return nil
}

func TestConnection_WriteFrame_SetsWriteDeadline(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	if cfg.WriteTimeout != 30*time.Second {
		t.Fatalf("default WriteTimeout = %v, want 30s", cfg.WriteTimeout)
	}
	conn := NewConnection(&mockPeerConn{}, cfg)
	defer conn.Close()

	ms := &deadlineTrackingStream{}
	conn.controlStream = ms
	conn.writer = protocol.NewFrameWriter(ms)

	err := conn.WriteFrame(&protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 1,
		Payload:  []byte("test"),
	})
	if err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}

	// Deadline armed before the write and disarmed after it.
	if ms.deadlineCalls != 2 {
		t.Errorf("SetWriteDeadline calls = %d, want 2 (arm + clear)", ms.deadlineCalls)
	}
	if !ms.writeDeadline.IsZero() {
		t.Errorf("write deadline left set after WriteFrame: %v", ms.writeDeadline)
	}
}

func TestConnection_WriteFrame_WriteTimeoutDisabled(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	cfg.WriteTimeout = 0
	conn := NewConnection(&mockPeerConn{}, cfg)
	defer conn.Close()

	ms := &deadlineTrackingStream{}
	conn.controlStream = ms
	conn.writer = protocol.NewFrameWriter(ms)

	if err := conn.WriteFrame(&protocol.Frame{Type: protocol.FrameStreamData, StreamID: 1, Payload: []byte("x")}); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
	if ms.deadlineCalls != 0 {
		t.Errorf("SetWriteDeadline calls = %d, want 0 with WriteTimeout disabled", ms.deadlineCalls)
	}
}

func TestConnection_WriteFrame_NoControlStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	conn := NewConnection(&mockPeerConn{}, cfg)
	defer conn.Close()

	// Writer without a control stream (unit-test wiring): no deadline, no panic.
	conn.writer = protocol.NewFrameWriter(&mockStream{})
	if err := conn.WriteFrame(&protocol.Frame{Type: protocol.FrameStreamData, StreamID: 1, Payload: []byte("x")}); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
}

// ============================================================================
// Inbound Activity / Keepalive ACK Tests
// ============================================================================

func TestConnection_InboundActivityTrackedSeparately(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	conn := NewConnection(&mockPeerConn{}, cfg)
	defer conn.Close()

	conn.writer = protocol.NewFrameWriter(&mockStream{})

	// Freeze the inbound clock, then write: outbound frames must not refresh it.
	frozen := time.Now().Add(-time.Minute)
	conn.lastInbound.Store(frozen.UnixNano())

	if err := conn.WriteFrame(&protocol.Frame{Type: protocol.FrameStreamData, StreamID: 1, Payload: []byte("x")}); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
	if got := conn.LastInboundActivity(); !got.Equal(frozen) {
		t.Errorf("LastInboundActivity moved after outbound write: %v, want %v", got, frozen)
	}

	// The combined clock does move on writes (status display).
	if time.Since(conn.LastActivity()) > time.Second {
		t.Error("LastActivity should be refreshed by outbound writes")
	}

	// Inbound frames move the inbound clock.
	conn.updateInboundActivity()
	if got := conn.LastInboundActivity(); time.Since(got) > time.Second {
		t.Errorf("LastInboundActivity = %v, want recent", got)
	}
}

func TestConnection_PendingPingLifecycle(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultConnectionConfig(localID)
	conn := NewConnection(&mockPeerConn{}, cfg)
	defer conn.Close()

	conn.writer = protocol.NewFrameWriter(&mockStream{})

	if conn.PendingPing() != 0 {
		t.Fatal("PendingPing should be 0 before any keepalive")
	}

	if err := conn.SendKeepalive(); err != nil {
		t.Fatalf("SendKeepalive() error = %v", err)
	}
	pending := conn.PendingPing()
	if pending == 0 {
		t.Fatal("PendingPing should record the keepalive timestamp")
	}

	// A stale ACK (wrong timestamp) must not clear the outstanding ping.
	conn.clearPendingPing(uint64(pending) - 1)
	if conn.PendingPing() != pending {
		t.Error("clearPendingPing with a stale timestamp cleared the pending ping")
	}

	// The matching ACK clears it.
	conn.clearPendingPing(uint64(pending))
	if conn.PendingPing() != 0 {
		t.Error("clearPendingPing with the matching timestamp did not clear the ping")
	}
}

// pipePeerConn is a mock PeerConn whose control stream is backed by io.Pipe
// pairs, letting a test drive inbound frames and observe outbound ones.
// Close unblocks a pending Read so the manager's readLoop can exit.
type pipePeerConn struct {
	isDialer bool

	inR  *io.PipeReader // connection reads inbound frames from here
	inW  *io.PipeWriter // test writes inbound frames here
	outR *io.PipeReader // test reads outbound frames here
	outW *io.PipeWriter // connection writes outbound frames here

	closeOnce sync.Once
}

func newPipePeerConn(isDialer bool) *pipePeerConn {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	return &pipePeerConn{isDialer: isDialer, inR: inR, inW: inW, outR: outR, outW: outW}
}

func (p *pipePeerConn) OpenStream(ctx context.Context) (transport.Stream, error) {
	return &mockStream{}, nil
}

func (p *pipePeerConn) AcceptStream(ctx context.Context) (transport.Stream, error) {
	return &mockStream{}, nil
}

func (p *pipePeerConn) Close() error {
	p.closeOnce.Do(func() {
		p.inR.Close()
		p.inW.Close()
		p.outR.Close()
		p.outW.Close()
	})
	return nil
}

func (p *pipePeerConn) LocalAddr() net.Addr                    { return &mockAddr{addr: "local"} }
func (p *pipePeerConn) RemoteAddr() net.Addr                   { return &mockAddr{addr: "remote"} }
func (p *pipePeerConn) IsDialer() bool                         { return p.isDialer }
func (p *pipePeerConn) TransportType() transport.TransportType { return transport.TransportQUIC }

// newRegisteredPipeConn builds a connected, ready Connection over a
// pipePeerConn and registers it with the manager, starting readLoop and
// keepaliveLoop.
func newRegisteredPipeConn(t *testing.T, m *Manager, remoteID identity.AgentID) (*Connection, *pipePeerConn) {
	t.Helper()

	pc := newPipePeerConn(false)
	conn := NewConnection(pc, ConnectionConfig{LocalID: m.cfg.LocalID})
	conn.RemoteID = remoteID
	conn.reader = protocol.NewFrameReader(pc.inR)
	conn.writer = protocol.NewFrameWriter(pc.outW)
	conn.SetState(StateConnected)
	conn.markReady()

	m.registerConnection(conn)

	if got := m.GetPeer(remoteID); got != conn {
		t.Fatalf("connection not registered")
	}
	t.Cleanup(func() { conn.Close() })
	return conn, pc
}

// drainKeepalives reads outbound frames written by conn and, for each
// KEEPALIVE, optionally writes the matching KEEPALIVE_ACK back inbound.
// Returns a channel closed when the drainer exits.
func drainKeepalives(pc *pipePeerConn, answerAcks bool) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := protocol.NewFrameReader(pc.outR)
		for {
			frame, err := reader.Read()
			if err != nil {
				return
			}
			if frame.Type != protocol.FrameKeepalive || !answerAcks {
				continue
			}
			ka, err := protocol.DecodeKeepalive(frame.Payload)
			if err != nil {
				continue
			}
			ack := &protocol.Frame{
				Type:     protocol.FrameKeepaliveAck,
				StreamID: protocol.ControlStreamID,
				Payload:  (&protocol.Keepalive{Timestamp: ka.Timestamp}).Encode(),
			}
			if err := protocol.NewFrameWriter(pc.inW).Write(ack); err != nil {
				return
			}
		}
	}()
	return done
}

// TestManager_KeepaliveAckTimeout verifies that a keepalive left unanswered
// for KeepaliveTimeout closes the connection and fires OnPeerDisconnect, even
// though the transport socket itself looks fine.
func TestManager_KeepaliveAckTimeout(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 100 * time.Millisecond
	cfg.KeepaliveTimeout = 50 * time.Millisecond // ACK deadline (150ms) beats the staleness check (threshold 150ms, tick at 200ms)
	cfg.KeepaliveJitter = 0
	cfg.WriteTimeout = 0

	disconnected := make(chan error, 1)
	cfg.OnPeerDisconnect = func(_ *Connection, err error) { disconnected <- err }

	m := NewManager(cfg)
	defer m.Close()

	remoteID, _ := identity.NewAgentID()
	conn, pc := newRegisteredPipeConn(t, m, remoteID)
	drainKeepalives(pc, false) // blackhole: never answer

	select {
	case err := <-disconnected:
		if err == nil || !strings.Contains(err.Error(), "keepalive ack timeout") {
			t.Errorf("disconnect error = %v, want keepalive ack timeout", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection not closed after unanswered keepalive")
	}

	select {
	case <-conn.Done():
	default:
		t.Error("connection should be closed after keepalive ack timeout")
	}
	if m.GetPeer(remoteID) != nil {
		t.Error("peer should be removed from the manager after ack timeout")
	}
}

// TestManager_KeepaliveAckReceived verifies the connection stays up when every
// keepalive is answered within KeepaliveTimeout.
func TestManager_KeepaliveAckReceived(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 40 * time.Millisecond
	cfg.KeepaliveTimeout = 150 * time.Millisecond
	cfg.KeepaliveJitter = 0
	cfg.WriteTimeout = 0

	var disconnectCount atomic.Int32
	cfg.OnPeerDisconnect = func(_ *Connection, _ error) { disconnectCount.Add(1) }

	m := NewManager(cfg)
	defer m.Close()

	remoteID, _ := identity.NewAgentID()
	conn, pc := newRegisteredPipeConn(t, m, remoteID)
	drainKeepalives(pc, true) // answer every ping

	// Run for several keepalive rounds.
	time.Sleep(500 * time.Millisecond)

	if disconnectCount.Load() != 0 {
		t.Error("connection dropped despite timely keepalive acks")
	}
	select {
	case <-conn.Done():
		t.Error("connection closed despite timely keepalive acks")
	default:
	}
	// Survival past several KeepaliveTimeouts proves acks clear the pending
	// ping; RTT>0 proves the ack frame was processed.
	if conn.RTT() <= 0 {
		t.Error("RTT should be measured from the ack")
	}
}

// TestManager_KeepaliveInboundStale verifies the inbound-silence detector in
// isolation (ACK deadline disabled): a connection that receives nothing for
// longer than KeepaliveInterval is closed, and our own outbound keepalives do
// not refresh the clock.
func TestManager_KeepaliveInboundStale(t *testing.T) {
	localID, _ := identity.NewAgentID()
	tr := transport.NewQUICTransport()
	defer tr.Close()

	cfg := DefaultManagerConfig(localID, tr)
	cfg.KeepaliveInterval = 100 * time.Millisecond
	cfg.KeepaliveTimeout = 0 // disables the ACK deadline; staleness threshold = interval
	cfg.KeepaliveJitter = 0
	cfg.WriteTimeout = 0

	disconnected := make(chan error, 1)
	cfg.OnPeerDisconnect = func(_ *Connection, err error) { disconnected <- err }

	m := NewManager(cfg)
	defer m.Close()

	remoteID, _ := identity.NewAgentID()
	conn, pc := newRegisteredPipeConn(t, m, remoteID)
	drainKeepalives(pc, false) // drain outbound keepalives, never send inbound

	select {
	case err := <-disconnected:
		if err == nil || !strings.Contains(err.Error(), "keepalive timeout") {
			t.Errorf("disconnect error = %v, want keepalive timeout", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("silent connection never closed")
	}

	select {
	case <-conn.Done():
	default:
		t.Error("connection should be closed")
	}
}
