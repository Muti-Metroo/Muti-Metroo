package exit

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// ============================================================================
// DNS Tests
// ============================================================================

func TestDefaultDNSConfig(t *testing.T) {
	cfg := DefaultDNSConfig()

	// Default is empty servers (uses system resolver for .local domain support)
	if len(cfg.Servers) != 0 {
		t.Errorf("Servers len = %d, want 0 (system resolver)", len(cfg.Servers))
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
}

func TestNewResolver(t *testing.T) {
	cfg := DefaultDNSConfig()
	r := NewResolver(cfg)

	if r == nil {
		t.Fatal("NewResolver() returned nil")
	}
	if r.CacheSize() != 0 {
		t.Errorf("Initial CacheSize = %d, want 0", r.CacheSize())
	}
}

func TestResolver_Resolve_IPAddress(t *testing.T) {
	r := NewResolver(DefaultDNSConfig())

	// Already an IP should return immediately
	ip, err := r.Resolve(context.Background(), "192.168.1.1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if ip.String() != "192.168.1.1" {
		t.Errorf("Resolve() = %s, want 192.168.1.1", ip.String())
	}

	// IPv6
	ip, err = r.Resolve(context.Background(), "::1")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if ip.String() != "::1" {
		t.Errorf("Resolve() = %s, want ::1", ip.String())
	}
}

func TestResolver_CacheOperations(t *testing.T) {
	r := NewResolver(DefaultDNSConfig())

	// Manually set cache
	r.setCache("example.com", net.ParseIP("1.2.3.4"), time.Hour)

	if r.CacheSize() != 1 {
		t.Errorf("CacheSize = %d, want 1", r.CacheSize())
	}

	// Get from cache
	ip := r.getCached("example.com")
	if ip == nil || ip.String() != "1.2.3.4" {
		t.Errorf("getCached() = %v, want 1.2.3.4", ip)
	}

	// Non-existent entry
	ip = r.getCached("notexist.com")
	if ip != nil {
		t.Errorf("getCached() = %v, want nil", ip)
	}

	// Clear cache
	r.ClearCache()
	if r.CacheSize() != 0 {
		t.Errorf("CacheSize after clear = %d, want 0", r.CacheSize())
	}
}

func TestResolver_CacheExpiry(t *testing.T) {
	r := NewResolver(DefaultDNSConfig())

	// Set cache with very short TTL
	r.setCache("example.com", net.ParseIP("1.2.3.4"), 1*time.Millisecond)

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	// Should be expired
	ip := r.getCached("example.com")
	if ip != nil {
		t.Errorf("getCached() = %v, want nil (expired)", ip)
	}
}

// ============================================================================
// Handler Config Tests
// ============================================================================

func TestDefaultHandlerConfig(t *testing.T) {
	cfg := DefaultHandlerConfig()

	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("ConnectTimeout = %v, want 30s", cfg.ConnectTimeout)
	}
	if cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want 5m", cfg.IdleTimeout)
	}
	if cfg.MaxConnections != 1000 {
		t.Errorf("MaxConnections = %d, want 1000", cfg.MaxConnections)
	}
}

func TestParseAllowedRoutes(t *testing.T) {
	routes, err := ParseAllowedRoutes([]string{"10.0.0.0/8", "192.168.0.0/16"})
	if err != nil {
		t.Fatalf("ParseAllowedRoutes() error = %v", err)
	}

	if len(routes) != 2 {
		t.Errorf("Routes len = %d, want 2", len(routes))
	}
}

func TestParseAllowedRoutes_Invalid(t *testing.T) {
	_, err := ParseAllowedRoutes([]string{"invalid"})
	if err == nil {
		t.Error("ParseAllowedRoutes() should fail for invalid CIDR")
	}
}

// ============================================================================
// Handler Tests
// ============================================================================

type mockStreamWriter struct {
	mu     sync.Mutex
	acks   []streamAck
	errs   []streamErr
	data   []streamData
	closes []uint64
	resets []uint64
}

type streamAck struct {
	streamID  uint64
	requestID uint64
	boundIP   net.IP
	boundPort uint16
}

type streamErr struct {
	streamID  uint64
	requestID uint64
	errorCode uint16
	message   string
}

type streamData struct {
	streamID uint64
	data     []byte
	flags    uint8
}

func (m *mockStreamWriter) WriteStreamOpenAck(peerID identity.AgentID, streamID uint64, requestID uint64, boundIP net.IP, boundPort uint16, ephemeralPubKey [crypto.KeySize]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acks = append(m.acks, streamAck{streamID, requestID, boundIP, boundPort})
	return nil
}

func (m *mockStreamWriter) WriteStreamOpenErr(peerID identity.AgentID, streamID uint64, requestID uint64, errorCode uint16, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs = append(m.errs, streamErr{streamID, requestID, errorCode, message})
	return nil
}

func (m *mockStreamWriter) WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.data = append(m.data, streamData{streamID, dataCopy, flags})
	return nil
}

func (m *mockStreamWriter) WriteStreamClose(peerID identity.AgentID, streamID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, streamID)
	return nil
}

func (m *mockStreamWriter) WriteStreamReset(peerID identity.AgentID, streamID uint64, errorCode uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resets = append(m.resets, streamID)
	return nil
}

func TestNewHandler(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, nil)

	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
	if h.IsRunning() {
		t.Error("New handler should not be running")
	}
}

func TestHandler_StartStop(t *testing.T) {
	localID, _ := identity.NewAgentID()
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, nil)

	h.Start()
	if !h.IsRunning() {
		t.Error("Handler should be running after Start()")
	}

	h.Stop()
	if h.IsRunning() {
		t.Error("Handler should not be running after Stop()")
	}

	// Double stop should be safe
	h.Stop()
}

func TestHandler_HandleStreamOpen_NotRunning(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, nil)

	var testEphemeralKey [crypto.KeySize]byte
	err := h.HandleStreamOpen(context.Background(), 1, 1, remoteID, "127.0.0.1", 8080, testEphemeralKey)
	if err == nil {
		t.Error("HandleStreamOpen() should fail when not running")
	}
}

func TestHandler_HandleStreamOpen_Success(t *testing.T) {
	// Start an echo server
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Echo server listen error: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().(*net.TCPAddr)
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Create handler with localhost allowed
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}
	cfg := DefaultHandlerConfig()
	cfg.AllowedRoutes, _ = ParseAllowedRoutes([]string{"127.0.0.0/8"}) // Allow localhost
	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	// Open stream to echo server - generate valid ephemeral key for E2E encryption
	ctx := context.Background()
	_, ingressPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
	}
	err = h.HandleStreamOpen(ctx, 1, 100, remoteID, "127.0.0.1", uint16(echoAddr.Port), ingressPub)
	if err != nil {
		t.Fatalf("HandleStreamOpen() error = %v", err)
	}

	// Wait for ACK
	time.Sleep(50 * time.Millisecond)

	writer.mu.Lock()
	if len(writer.acks) != 1 {
		t.Errorf("Should have 1 ACK, got %d", len(writer.acks))
	}
	if len(writer.acks) > 0 && writer.acks[0].requestID != 100 {
		t.Errorf("ACK requestID = %d, want 100", writer.acks[0].requestID)
	}
	writer.mu.Unlock()

	// Connection should be tracked
	if h.ConnectionCount() != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", h.ConnectionCount())
	}

	// Get exit's ephemeral public key from ACK to derive session key
	h.mu.RLock()
	ac := h.connections[streamKey{peer: remoteID, streamID: 1}]
	h.mu.RUnlock()
	if ac == nil || ac.sessionKey == nil {
		t.Fatal("connection or session key not found")
	}

	// Derive the ingress-side session key (we are the initiator)
	// We need to get the exit's ephemeral public key, but since this is a test
	// and the mock writer doesn't capture it, we'll create a matching key
	// by getting it from the connection's internal state
	// For simplicity, just verify connection works by closing
	t.Log("Skipping encrypted data test (would require capturing exit ephemeral key)")

	// Clean up
	h.HandleStreamClose(remoteID, 1)
	time.Sleep(50 * time.Millisecond)
}

func TestHandler_HandleStreamOpen_NotAllowed(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}

	// Only allow 10.0.0.0/8
	routes, _ := ParseAllowedRoutes([]string{"10.0.0.0/8"})
	cfg := DefaultHandlerConfig()
	cfg.AllowedRoutes = routes

	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	// Try to connect to 192.168.1.1 (not allowed)
	var testEphemeralKey [crypto.KeySize]byte
	err := h.HandleStreamOpen(context.Background(), 1, 100, remoteID, "192.168.1.1", 80, testEphemeralKey)
	if err != nil {
		t.Errorf("HandleStreamOpen() should return nil (async): %v", err)
	}

	// Wait for async operation to complete
	time.Sleep(50 * time.Millisecond)

	// Should have sent error
	writer.mu.Lock()
	if len(writer.errs) != 1 {
		t.Errorf("Should have 1 error, got %d", len(writer.errs))
	}
	if len(writer.errs) > 0 && writer.errs[0].errorCode != protocol.ErrNotAllowed {
		t.Errorf("ErrorCode = %d, want %d", writer.errs[0].errorCode, protocol.ErrNotAllowed)
	}
	writer.mu.Unlock()
}

func TestHandler_HandleStreamOpen_ConnectionLimit(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}

	cfg := DefaultHandlerConfig()
	cfg.MaxConnections = 1

	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	// Manually add a connection to simulate limit
	h.mu.Lock()
	h.connections[streamKey{peer: remoteID, streamID: 999}] = &ActiveConnection{StreamID: 999, RemoteID: remoteID}
	h.connCount.Add(1)
	h.mu.Unlock()

	// Try to open another - should fail
	var testEphemeralKey [crypto.KeySize]byte
	err := h.HandleStreamOpen(context.Background(), 1, 100, remoteID, "127.0.0.1", 80, testEphemeralKey)
	if err == nil {
		t.Error("HandleStreamOpen() should fail when at connection limit")
	}

	writer.mu.Lock()
	if len(writer.errs) != 1 {
		t.Errorf("Should have 1 error, got %d", len(writer.errs))
	}
	if len(writer.errs) > 0 && writer.errs[0].errorCode != protocol.ErrConnectionLimit {
		t.Errorf("ErrorCode = %d, want %d", writer.errs[0].errorCode, protocol.ErrConnectionLimit)
	}
	writer.mu.Unlock()
}

func TestHandler_HandleStreamData_UnknownStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, nil)
	h.Start()
	defer h.Stop()

	err := h.HandleStreamData(remoteID, 999, []byte("test"), 0)
	if err == nil {
		t.Error("HandleStreamData() should fail for unknown stream")
	}
}

func TestHandler_HandleStreamClose(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	// Add a mock connection
	h.mu.Lock()
	h.connections[streamKey{peer: remoteID, streamID: 1}] = &ActiveConnection{StreamID: 1, RemoteID: remoteID}
	h.connCount.Add(1)
	h.mu.Unlock()

	h.HandleStreamClose(remoteID, 1)

	if h.ConnectionCount() != 0 {
		t.Errorf("ConnectionCount() = %d, want 0 after close", h.ConnectionCount())
	}
}

func TestHandler_HandleStreamReset(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	// Add a mock connection
	h.mu.Lock()
	h.connections[streamKey{peer: remoteID, streamID: 1}] = &ActiveConnection{StreamID: 1, RemoteID: remoteID}
	h.connCount.Add(1)
	h.mu.Unlock()

	h.HandleStreamReset(remoteID, 1, 123)

	if h.ConnectionCount() != 0 {
		t.Errorf("ConnectionCount() = %d, want 0 after reset", h.ConnectionCount())
	}
}

func TestHandler_GetConnection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, nil)

	// Add a mock connection
	h.mu.Lock()
	h.connections[streamKey{peer: remoteID, streamID: 1}] = &ActiveConnection{StreamID: 1, RemoteID: remoteID}
	h.mu.Unlock()

	conn := h.GetConnection(remoteID, 1)
	if conn == nil {
		t.Error("GetConnection() should return connection")
	}

	conn = h.GetConnection(remoteID, 999)
	if conn != nil {
		t.Error("GetConnection() should return nil for unknown stream")
	}

	// A different peer quoting the same numeric ID must not reach it.
	otherPeer, _ := identity.NewAgentID()
	if conn := h.GetConnection(otherPeer, 1); conn != nil {
		t.Error("GetConnection() should not return another peer's connection")
	}
}

func TestHandler_isAllowed(t *testing.T) {
	localID, _ := identity.NewAgentID()

	tests := []struct {
		name    string
		routes  []string
		ip      string
		allowed bool
	}{
		{
			name:    "no routes denies all (security: deny by default)",
			routes:  nil,
			ip:      "1.2.3.4",
			allowed: false,
		},
		{
			name:    "ip in allowed range",
			routes:  []string{"10.0.0.0/8"},
			ip:      "10.1.2.3",
			allowed: true,
		},
		{
			name:    "ip not in allowed range",
			routes:  []string{"10.0.0.0/8"},
			ip:      "192.168.1.1",
			allowed: false,
		},
		{
			name:    "multiple ranges - first match",
			routes:  []string{"10.0.0.0/8", "192.168.0.0/16"},
			ip:      "10.1.2.3",
			allowed: true,
		},
		{
			name:    "multiple ranges - second match",
			routes:  []string{"10.0.0.0/8", "192.168.0.0/16"},
			ip:      "192.168.1.1",
			allowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultHandlerConfig()
			if tt.routes != nil {
				cfg.AllowedRoutes, _ = ParseAllowedRoutes(tt.routes)
			}
			h := NewHandler(cfg, localID, nil)

			ip := net.ParseIP(tt.ip)
			got := h.isAllowed(ip)
			if got != tt.allowed {
				t.Errorf("isAllowed(%s) = %v, want %v", tt.ip, got, tt.allowed)
			}
		})
	}
}

func TestActiveConnection_Close(t *testing.T) {
	ac := &ActiveConnection{StreamID: 1}

	if ac.IsClosed() {
		t.Error("New connection should not be closed")
	}

	ac.Close()

	if !ac.IsClosed() {
		t.Error("Connection should be closed after Close()")
	}

	// Multiple closes should be safe
	ac.Close()
}

// ============================================================================
// Dynamic Route Modification Tests
// ============================================================================

func TestHandler_AddAllowedRoute(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	// Initially no routes
	if h.AllowedRouteCount() != 0 {
		t.Fatalf("AllowedRouteCount() = %d, want 0", h.AllowedRouteCount())
	}

	// isAllowed should deny
	if h.isAllowed(net.ParseIP("10.1.2.3")) {
		t.Error("isAllowed should return false with no routes")
	}

	// Add a route
	_, network, _ := net.ParseCIDR("10.0.0.0/8")
	h.AddAllowedRoute(network, nil, "")

	if h.AllowedRouteCount() != 1 {
		t.Fatalf("AllowedRouteCount() = %d, want 1", h.AllowedRouteCount())
	}

	// Now isAllowed should permit 10.x.x.x
	if !h.isAllowed(net.ParseIP("10.1.2.3")) {
		t.Error("isAllowed should return true for 10.1.2.3")
	}
	if h.isAllowed(net.ParseIP("192.168.1.1")) {
		t.Error("isAllowed should return false for 192.168.1.1")
	}
}

func TestHandler_RemoveAllowedRoute(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	_, network, _ := net.ParseCIDR("10.0.0.0/8")
	h.AddAllowedRoute(network, nil, "")

	if !h.isAllowed(net.ParseIP("10.1.2.3")) {
		t.Error("isAllowed should return true after add")
	}

	if !h.RemoveAllowedRoute(network) {
		t.Error("RemoveAllowedRoute should return true")
	}

	if h.isAllowed(net.ParseIP("10.1.2.3")) {
		t.Error("isAllowed should return false after remove")
	}

	// Remove non-existent
	if h.RemoveAllowedRoute(network) {
		t.Error("RemoveAllowedRoute should return false for non-existent")
	}
}

func TestHandler_ConcurrentRouteModification(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	var wg sync.WaitGroup
	const goroutines = 20

	// Run concurrent adds, removes, and checks
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, network, _ := net.ParseCIDR(fmt.Sprintf("10.%d.0.0/16", i%256))

			h.AddAllowedRoute(network, nil, "")
			h.isAllowed(net.ParseIP(fmt.Sprintf("10.%d.1.1", i%256)))
			h.RemoveAllowedRoute(network)
			h.AllowedRouteCount()
		}(i)
	}

	wg.Wait()
}

// ============================================================================
// Per-route source-IP binding tests
// ============================================================================

func TestHandler_LookupBinding_LongestPrefix(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	_, broad, _ := net.ParseCIDR("10.0.0.0/8")
	_, narrow, _ := net.ParseCIDR("10.1.0.0/16")
	srcBroad := net.ParseIP("127.0.0.1")
	srcNarrow := net.ParseIP("127.0.0.2")
	h.AddAllowedRoute(broad, srcBroad, "")
	h.AddAllowedRoute(narrow, srcNarrow, "")

	cases := []struct {
		ip   string
		want string
	}{
		{"10.1.2.3", "127.0.0.2"}, // matches /16 (more specific)
		{"10.2.3.4", "127.0.0.1"}, // matches /8 only
	}
	for _, c := range cases {
		got, _ := h.lookupBinding(net.ParseIP(c.ip))
		if got == nil || got.String() != c.want {
			t.Errorf("lookupBinding(%s) src = %v, want %s", c.ip, got, c.want)
		}
	}
}

func TestHandler_LookupBinding_NoBinding(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	_, n, _ := net.ParseCIDR("10.0.0.0/8")
	h.AddAllowedRoute(n, nil, "")

	if src, iface := h.lookupBinding(net.ParseIP("10.1.2.3")); src != nil || iface != "" {
		t.Errorf("lookupBinding on route with no binding = (%v, %q), want (nil, \"\")", src, iface)
	}
	if src, iface := h.lookupBinding(net.ParseIP("192.168.0.1")); src != nil || iface != "" {
		t.Errorf("lookupBinding for unmatched IP = (%v, %q), want (nil, \"\")", src, iface)
	}
}

func TestHandler_LookupBinding_FamilyMismatch(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	// IPv6 CIDR with IPv6 source. A v4 lookup that somehow falls inside
	// must NOT return the v6 source (lookupBinding defends against this).
	_, n6, _ := net.ParseCIDR("::/0")
	h.AddAllowedRoute(n6, net.ParseIP("fd00::1"), "")

	if src, _ := h.lookupBinding(net.ParseIP("10.1.2.3").To4()); src != nil {
		t.Errorf("lookupBinding for v4 with only v6 source = %v, want nil", src)
	}
	if src, _ := h.lookupBinding(net.ParseIP("fd00::5")); src == nil || src.String() != "fd00::1" {
		t.Errorf("lookupBinding for v6 = %v, want fd00::1", src)
	}
}

// TestHandler_HandleStreamOpen_BindToUnboundIP verifies the dial path
// actually applies the configured source IP. We set Source to TEST-NET-1
// (192.0.2.1, RFC 5737, guaranteed not locally bound), which causes the
// kernel to reject the dial with EADDRNOTAVAIL. The exit then sends an
// open-error ACK instead of a successful one.
func TestHandler_HandleStreamOpen_BindToUnboundIP(t *testing.T) {
	// Stand up a local echo server we'd otherwise reach successfully.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	echoAddr := listener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	localID, _ := identity.NewAgentID()
	remoteID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}
	cfg := DefaultHandlerConfig()
	_, lo, _ := net.ParseCIDR("127.0.0.0/8")
	cfg.AllowedRoutes = []AllowedRoute{{Network: lo, Source: net.ParseIP("192.0.2.1")}}

	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	_, ingressPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
	}
	if err := h.HandleStreamOpen(context.Background(), 1, 100, remoteID, "127.0.0.1", uint16(echoAddr.Port), ingressPub); err != nil {
		t.Fatalf("HandleStreamOpen() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.acks) != 0 {
		t.Errorf("expected no successful ACK when source IP is unbound, got %d", len(writer.acks))
	}
	if len(writer.errs) == 0 {
		t.Error("expected an open-error ACK when source IP is unbound")
	}
}

func TestHandler_SetAllowedRouteSource(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	_, n, _ := net.ParseCIDR("10.0.0.0/8")
	h.AddAllowedRoute(n, nil, "")

	if got := h.boundRoutes.Load(); got != 0 {
		t.Fatalf("boundRoutes after AddAllowedRoute(nil source) = %d, want 0", got)
	}

	// Set: nil -> non-nil
	if err := h.SetAllowedRouteSource(n, net.ParseIP("127.0.0.1")); err != nil {
		t.Fatalf("SetAllowedRouteSource(set) error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 1 {
		t.Errorf("boundRoutes after set = %d, want 1", got)
	}
	if src, _ := h.lookupBinding(net.ParseIP("10.1.2.3")); src == nil || src.String() != "127.0.0.1" {
		t.Errorf("lookupBinding src = %v, want 127.0.0.1", src)
	}

	// Update: non-nil -> non-nil (counter must NOT change)
	if err := h.SetAllowedRouteSource(n, net.ParseIP("127.0.0.2")); err != nil {
		t.Fatalf("SetAllowedRouteSource(update) error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 1 {
		t.Errorf("boundRoutes after update = %d, want 1", got)
	}
	if src, _ := h.lookupBinding(net.ParseIP("10.1.2.3")); src == nil || src.String() != "127.0.0.2" {
		t.Errorf("lookupBinding src after update = %v, want 127.0.0.2", src)
	}

	// Clear: non-nil -> nil
	if err := h.SetAllowedRouteSource(n, nil); err != nil {
		t.Fatalf("SetAllowedRouteSource(clear) error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 0 {
		t.Errorf("boundRoutes after clear = %d, want 0", got)
	}
	if src, _ := h.lookupBinding(net.ParseIP("10.1.2.3")); src != nil {
		t.Errorf("lookupBinding src after clear = %v, want nil", src)
	}

	// Not found
	_, other, _ := net.ParseCIDR("192.168.0.0/16")
	if err := h.SetAllowedRouteSource(other, net.ParseIP("127.0.0.1")); err == nil {
		t.Error("SetAllowedRouteSource on missing route should return error")
	}
}

// TestHandler_DialerFor covers the exported binding entry point used by the
// agent's local-exit dial path: no binding -> plain dialer; a source binding ->
// LocalAddr set; an off-interface source on a non-device-capable host -> error.
func TestHandler_DialerFor(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)
	// Force the portable (non-root / non-Linux) tier so the test is
	// deterministic across platforms.
	h.canBindDevice = func() bool { return false }

	// No binding configured: plain dialer, no source/interface, no error.
	dialer, src, iface, err := h.DialerFor(net.ParseIP("10.1.2.3"), time.Second)
	if err != nil {
		t.Fatalf("DialerFor(no binding) error = %v", err)
	}
	if dialer == nil || dialer.LocalAddr != nil || src != "" || iface != "" {
		t.Errorf("DialerFor(no binding) = (%v, %q, %q), want plain dialer with no binding", dialer.LocalAddr, src, iface)
	}

	// Source binding: LocalAddr set to the route source.
	_, n, _ := net.ParseCIDR("10.0.0.0/8")
	h.AddAllowedRoute(n, net.ParseIP("127.0.0.1"), "")
	dialer, src, iface, err = h.DialerFor(net.ParseIP("10.1.2.3"), time.Second)
	if err != nil {
		t.Fatalf("DialerFor(source) error = %v", err)
	}
	if src != "127.0.0.1" || iface != "" {
		t.Errorf("DialerFor(source) = (%q, %q), want (127.0.0.1, \"\")", src, iface)
	}
	if la, ok := dialer.LocalAddr.(*net.TCPAddr); !ok || !la.IP.Equal(net.ParseIP("127.0.0.1")) {
		t.Errorf("DialerFor(source) LocalAddr = %v, want 127.0.0.1", dialer.LocalAddr)
	}

	// Off-interface source with a non-device-capable host: not satisfiable.
	h.ifaceHasAddr = func(string, net.IP) (bool, bool) { return false, true } // resolves, source absent
	_, n2, _ := net.ParseCIDR("10.5.0.0/16")
	h.AddAllowedRoute(n2, net.ParseIP("10.9.9.9"), "eth0")
	if _, _, _, err := h.DialerFor(net.ParseIP("10.5.1.1"), time.Second); err == nil {
		t.Error("DialerFor(off-interface source, non-device host) should return error")
	}
}

func TestHandler_SetAllowedRouteInterface(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	_, n, _ := net.ParseCIDR("10.0.0.0/8")
	h.AddAllowedRoute(n, nil, "")

	if got := h.boundRoutes.Load(); got != 0 {
		t.Fatalf("boundRoutes after AddAllowedRoute(nil source, empty iface) = %d, want 0", got)
	}

	// Set: empty -> non-empty (route gains a binding)
	if err := h.SetAllowedRouteInterface(n, "eth0"); err != nil {
		t.Fatalf("SetAllowedRouteInterface(set) error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 1 {
		t.Errorf("boundRoutes after set = %d, want 1", got)
	}
	_, iface := h.lookupBinding(net.ParseIP("10.1.2.3"))
	if iface != "eth0" {
		t.Errorf("lookupBinding iface = %q, want eth0", iface)
	}

	// Update: non-empty -> non-empty (counter must NOT change)
	if err := h.SetAllowedRouteInterface(n, "eth1"); err != nil {
		t.Fatalf("SetAllowedRouteInterface(update) error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 1 {
		t.Errorf("boundRoutes after update = %d, want 1", got)
	}
	_, iface = h.lookupBinding(net.ParseIP("10.1.2.3"))
	if iface != "eth1" {
		t.Errorf("lookupBinding iface after update = %q, want eth1", iface)
	}

	// Clear: non-empty -> empty
	if err := h.SetAllowedRouteInterface(n, ""); err != nil {
		t.Fatalf("SetAllowedRouteInterface(clear) error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 0 {
		t.Errorf("boundRoutes after clear = %d, want 0", got)
	}

	// Counter must not double-count when both source and interface are set.
	if err := h.SetAllowedRouteInterface(n, "eth2"); err != nil {
		t.Fatalf("SetAllowedRouteInterface(reset) error = %v", err)
	}
	if err := h.SetAllowedRouteSource(n, net.ParseIP("127.0.0.5")); err != nil {
		t.Fatalf("SetAllowedRouteSource error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 1 {
		t.Errorf("boundRoutes with both bindings = %d, want 1 (deduped per route)", got)
	}

	// Both -> only iface (still bound, no change)
	if err := h.SetAllowedRouteSource(n, nil); err != nil {
		t.Fatalf("SetAllowedRouteSource clear error = %v", err)
	}
	if got := h.boundRoutes.Load(); got != 1 {
		t.Errorf("boundRoutes after clearing source while iface still set = %d, want 1", got)
	}

	// Not found
	_, other, _ := net.ParseCIDR("192.168.0.0/16")
	if err := h.SetAllowedRouteInterface(other, "eth0"); err == nil {
		t.Error("SetAllowedRouteInterface on missing route should return error")
	}
}

func TestHandler_LookupBinding_SourceAndInterface(t *testing.T) {
	cfg := DefaultHandlerConfig()
	localID, _ := identity.NewAgentID()
	h := NewHandler(cfg, localID, nil)

	// Three routes: more-specific has iface only, parent has source only.
	_, broad, _ := net.ParseCIDR("10.0.0.0/8")
	_, narrow, _ := net.ParseCIDR("10.1.0.0/16")
	h.AddAllowedRoute(broad, net.ParseIP("127.0.0.1"), "")
	h.AddAllowedRoute(narrow, nil, "eth0")

	src, iface := h.lookupBinding(net.ParseIP("10.1.2.3"))
	if iface != "eth0" {
		t.Errorf("lookupBinding(10.1.2.3) iface = %q, want eth0 (longest prefix wins)", iface)
	}
	if src != nil {
		t.Errorf("lookupBinding(10.1.2.3) src = %v, want nil (narrow route has no source)", src)
	}

	src, iface = h.lookupBinding(net.ParseIP("10.2.3.4"))
	if src == nil || src.String() != "127.0.0.1" {
		t.Errorf("lookupBinding(10.2.3.4) src = %v, want 127.0.0.1", src)
	}
	if iface != "" {
		t.Errorf("lookupBinding(10.2.3.4) iface = %q, want empty", iface)
	}
}

// Two agents peering directly with the same exit both allocate stream ID 1,
// because IDs are per-connection. Their connections must stay independent:
// neither may displace the other's entry, and neither may close or reset the
// other's stream by quoting that guessable ID.
func TestHandler_StreamIDCollisionAcrossPeers(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	h := NewHandler(DefaultHandlerConfig(), localID, nil)
	h.Start()
	defer h.Stop()

	connA := &ActiveConnection{StreamID: 1, RemoteID: peerA, DestAddr: "10.0.0.1"}
	connB := &ActiveConnection{StreamID: 1, RemoteID: peerB, DestAddr: "10.0.0.2"}

	h.mu.Lock()
	h.connections[streamKey{peer: peerA, streamID: 1}] = connA
	h.connCount.Add(1)
	h.connections[streamKey{peer: peerB, streamID: 1}] = connB
	h.connCount.Add(1)
	h.mu.Unlock()

	// Both survive registration: neither displaced the other.
	if got := h.ConnectionCount(); got != 2 {
		t.Fatalf("ConnectionCount() = %d, want 2", got)
	}
	if got := h.GetConnection(peerA, 1); got != connA {
		t.Errorf("peer A's stream 1 resolved to %v, want peer A's connection", got)
	}
	if got := h.GetConnection(peerB, 1); got != connB {
		t.Errorf("peer B's stream 1 resolved to %v, want peer B's connection", got)
	}

	// Peer B closing its own stream 1 must leave peer A's untouched.
	h.HandleStreamClose(peerB, 1)
	if got := h.GetConnection(peerA, 1); got != connA {
		t.Error("peer B's close removed peer A's connection")
	}
	if connA.IsClosed() {
		t.Error("peer B's close tore down peer A's underlying connection")
	}
	if got := h.GetConnection(peerB, 1); got != nil {
		t.Error("peer B's own connection was not removed")
	}
	if got := h.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount() = %d, want 1 after one close", got)
	}

	// A reset from a peer that owns nothing here must be a no-op.
	stranger, _ := identity.NewAgentID()
	h.HandleStreamReset(stranger, 1, 123)
	if got := h.GetConnection(peerA, 1); got != connA {
		t.Error("a stranger's reset removed peer A's connection")
	}

	// Data addressed with the right ID but the wrong peer must not be accepted.
	if err := h.HandleStreamData(stranger, 1, []byte("x"), 0); err == nil {
		t.Error("HandleStreamData accepted data from a peer that owns no such stream")
	}
}

// When a peer disconnects its exit connections must be closed immediately.
// Leaving them to age out holds real sockets open for a peer that can no
// longer receive their data, and the peer's stream IDs restart at 1 on
// reconnect and would collide with the stale entries.
func TestHandler_CloseConnectionsForPeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	h := NewHandler(DefaultHandlerConfig(), localID, nil)
	h.Start()
	defer h.Stop()

	a1 := &ActiveConnection{StreamID: 1, RemoteID: peerA}
	a2 := &ActiveConnection{StreamID: 3, RemoteID: peerA}
	b1 := &ActiveConnection{StreamID: 1, RemoteID: peerB}

	h.mu.Lock()
	for _, ac := range []*ActiveConnection{a1, a2, b1} {
		h.connections[streamKey{peer: ac.RemoteID, streamID: ac.StreamID}] = ac
		h.connCount.Add(1)
	}
	h.mu.Unlock()

	if n := h.CloseConnectionsForPeer(peerA); n != 2 {
		t.Errorf("CloseConnectionsForPeer(A) = %d, want 2", n)
	}
	if !a1.IsClosed() || !a2.IsClosed() {
		t.Error("peer A's connections were not closed")
	}
	if b1.IsClosed() {
		t.Error("peer B's connection was closed by peer A's disconnect")
	}
	if got := h.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", got)
	}
	if got := h.GetConnection(peerB, 1); got != b1 {
		t.Error("peer B's connection should still be reachable")
	}

	// Idempotent: a second disconnect for the same peer is a no-op.
	if n := h.CloseConnectionsForPeer(peerA); n != 0 {
		t.Errorf("second CloseConnectionsForPeer(A) = %d, want 0", n)
	}
}

// testSessionKeys derives the matching initiator/responder session key pair
// for a stream so HandleStreamData ciphertext decrypts at the responder.
func testSessionKeys(t *testing.T, requestID uint64) (initiator, responder *crypto.SessionKey) {
	t.Helper()
	initPriv, initPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair: %v", err)
	}
	respPriv, respPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair: %v", err)
	}
	sharedInit, err := crypto.ComputeECDH(initPriv, respPub)
	if err != nil {
		t.Fatalf("ComputeECDH: %v", err)
	}
	sharedResp, err := crypto.ComputeECDH(respPriv, initPub)
	if err != nil {
		t.Fatalf("ComputeECDH: %v", err)
	}
	initiator = crypto.DeriveSessionKey(sharedInit, requestID, initPub, respPub, true)
	responder = crypto.DeriveSessionKey(sharedResp, requestID, initPub, respPub, false)
	return initiator, responder
}

// registerTestConn installs a connection with a responder session key and
// starts its writer goroutine, mimicking handleStreamOpenAsync. Returns the
// initiator-side key for encrypting test frames.
func registerTestConn(t *testing.T, h *Handler, peer identity.AgentID, streamID, requestID uint64, conn net.Conn) *crypto.SessionKey {
	t.Helper()
	initiator, responder := testSessionKeys(t, requestID)
	ac := &ActiveConnection{
		StreamID:   streamID,
		RemoteID:   peer,
		Conn:       conn,
		sessionKey: responder,
		writeCh:    make(chan writeRequest, writeQueueSize),
		done:       make(chan struct{}),
	}
	h.mu.Lock()
	h.connections[streamKey{peer: peer, streamID: streamID}] = ac
	h.connCount.Add(1)
	h.mu.Unlock()
	go h.writeLoop(ac)
	t.Cleanup(func() { ac.Close() })
	return initiator
}

// A destination that stops reading must not stall HandleStreamData for other
// streams, and when its write queue fills only that stream is reset.
// net.Pipe writes block until the peer reads, so an unread pipe end is a
// stalled destination.
func TestHandler_HandleStreamData_SlowDestinationResetsOnlyThatStream(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	writer := &mockStreamWriter{}
	cfg := DefaultHandlerConfig()
	h := NewHandler(cfg, localID, writer)
	h.Start()
	defer h.Stop()

	// Stalled destination: nobody reads the other end.
	stalledConn, stalledPeer := net.Pipe()
	t.Cleanup(func() { stalledPeer.Close() })
	stalledKey := registerTestConn(t, h, peerID, 1, 100, stalledConn)

	// Healthy destination with a reader draining it.
	healthyConn, healthyPeer := net.Pipe()
	t.Cleanup(func() { healthyPeer.Close() })
	healthyKey := registerTestConn(t, h, peerID, 2, 200, healthyConn)
	var readMu sync.Mutex
	var readBuf []byte
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := healthyPeer.Read(buf)
			if n > 0 {
				readMu.Lock()
				readBuf = append(readBuf, buf[:n]...)
				readMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// The first frame wedges the writer goroutine in Write; the next
	// writeQueueSize frames fill the queue; one more overflows it.
	var overflowErr error
	for i := 0; i < writeQueueSize+2; i++ {
		ct, err := stalledKey.Encrypt([]byte("payload"))
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		overflowErr = h.HandleStreamData(peerID, 1, ct, 0)
		if overflowErr != nil {
			break
		}
	}
	if overflowErr == nil {
		t.Fatal("HandleStreamData on a stalled destination should fail once the queue is full")
	}

	// Only the stalled stream is reset and torn down.
	writer.mu.Lock()
	resets := append([]uint64(nil), writer.resets...)
	writer.mu.Unlock()
	if len(resets) != 1 || resets[0] != 1 {
		t.Errorf("resets = %v, want exactly stream 1", resets)
	}
	if ac := h.GetConnection(peerID, 1); ac != nil {
		t.Error("stalled stream should be removed from tracking")
	}

	// The healthy stream on the same peer connection never blocked and its
	// data arrives in order.
	var want []byte
	for i := 0; i < 10; i++ {
		msg := []byte(fmt.Sprintf("msg-%d", i))
		want = append(want, msg...)
		ct, err := healthyKey.Encrypt(msg)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		if err := h.HandleStreamData(peerID, 2, ct, 0); err != nil {
			t.Fatalf("HandleStreamData on healthy stream: %v", err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		readMu.Lock()
		got := append([]byte(nil), readBuf...)
		readMu.Unlock()
		if len(got) >= len(want) {
			if string(got) != string(want) {
				t.Errorf("healthy stream wrote %q, want %q (ordering)", got, want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("healthy stream data never arrived: got %q, want %q", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}
