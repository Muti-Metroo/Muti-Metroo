package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/hosts"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"golang.org/x/crypto/bcrypt"
)

// mockStatsProvider implements StatsProvider for testing.
type mockStatsProvider struct {
	running bool
	stats   Stats
}

func (m *mockStatsProvider) IsRunning() bool {
	return m.running
}

func (m *mockStatsProvider) Stats() Stats {
	return m.stats
}

func TestNewServer(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}

	s := NewServer(cfg, provider)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestServer_handleHealth(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if body != "OK\n" {
		t.Errorf("expected body 'OK\\n', got %q", body)
	}
}

func TestServer_handleHealth_MethodNotAllowed(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestServer_handleHealthz_Running(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{
		running: true,
		stats: Stats{
			PeerCount:               5,
			StreamCount:             10,
			RouteCount:              3,
			SOCKS5Running:           true,
			ExitHandlerRun:          true,
			CanSignManagement:       true,
			AcceptsSignedManagement: true,
		},
	}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", response["status"])
	}
	if response["running"] != true {
		t.Errorf("expected running true, got %v", response["running"])
	}
	if int(response["peer_count"].(float64)) != 5 {
		t.Errorf("expected peer_count 5, got %v", response["peer_count"])
	}
	if int(response["stream_count"].(float64)) != 10 {
		t.Errorf("expected stream_count 10, got %v", response["stream_count"])
	}
	if response["can_sign_management"] != true {
		t.Errorf("expected can_sign_management true, got %v", response["can_sign_management"])
	}
	if response["accepts_signed_management"] != true {
		t.Errorf("expected accepts_signed_management true, got %v", response["accepts_signed_management"])
	}
}

// TestServer_handleHealthz_NoManagementKeys confirms the capability flags
// are false when neither half of the signing keypair is configured, so
// web clients can disable the management UI.
func TestServer_handleHealthz_NoManagementKeys(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{
		running: true,
		stats: Stats{
			PeerCount:               1,
			CanSignManagement:       false,
			AcceptsSignedManagement: false,
		},
	}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	var response map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response["can_sign_management"] != false {
		t.Errorf("expected can_sign_management false, got %v", response["can_sign_management"])
	}
	if response["accepts_signed_management"] != false {
		t.Errorf("expected accepts_signed_management false, got %v", response["accepts_signed_management"])
	}
}

func TestServer_handleHealthz_NotRunning(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: false}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "unavailable" {
		t.Errorf("expected status 'unavailable', got %v", response["status"])
	}
}

func TestServer_handleReady_Ready(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if body != "READY\n" {
		t.Errorf("expected body 'READY\\n', got %q", body)
	}
}

func TestServer_handleReady_NotReady(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: false}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	body := rec.Body.String()
	if body != "NOT READY\n" {
		t.Errorf("expected body 'NOT READY\\n', got %q", body)
	}
}

func TestServer_StartStop(t *testing.T) {
	cfg := ServerConfig{
		Address:      "127.0.0.1:0", // Dynamic port
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if !s.IsRunning() {
		t.Error("expected server to be running")
	}

	addr := s.Address()
	if addr == nil {
		t.Fatal("expected non-nil address")
	}

	// Give the server time to start accepting connections
	// Use retry loop to handle race between Start() and Serve()
	var resp *http.Response
	var err error
	for i := 0; i < 10; i++ {
		time.Sleep(10 * time.Millisecond)
		resp, err = http.Get("http://" + addr.String() + "/health")
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("request failed after retries: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK\n" {
		t.Errorf("expected body 'OK\\n', got %q", body)
	}

	if err := s.Stop(); err != nil {
		t.Errorf("failed to stop: %v", err)
	}

	if s.IsRunning() {
		t.Error("expected server to be stopped")
	}
}

func TestServer_DoubleStop(t *testing.T) {
	cfg := ServerConfig{
		Address: "127.0.0.1:0",
	}
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	if err := s.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Stop twice should not error
	if err := s.Stop(); err != nil {
		t.Errorf("first stop failed: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Errorf("second stop failed: %v", err)
	}
}

func TestServer_NilProvider(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestServer_PprofIndex(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Check that the response contains pprof content
	body := rec.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body for pprof index")
	}
}

func TestServer_PprofCmdline(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestServer_PprofSymbol(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/symbol", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	// Symbol accepts both GET and POST
	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		data           interface{}
		expectedStatus int
		expectedKeys   []string
	}{
		{
			name:           "simple map",
			status:         http.StatusOK,
			data:           map[string]string{"key": "value"},
			expectedStatus: http.StatusOK,
			expectedKeys:   []string{"key"},
		},
		{
			name:           "status with message",
			status:         http.StatusBadRequest,
			data:           map[string]interface{}{"error": "bad request", "code": 400},
			expectedStatus: http.StatusBadRequest,
			expectedKeys:   []string{"error", "code"},
		},
		{
			name:           "stats struct",
			status:         http.StatusOK,
			data:           Stats{PeerCount: 5, StreamCount: 10},
			expectedStatus: http.StatusOK,
			expectedKeys:   []string{"peer_count", "stream_count"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeJSON(rec, tc.status, tc.data)

			if rec.Code != tc.expectedStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.expectedStatus)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
			}

			var result map[string]interface{}
			if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode JSON: %v", err)
			}

			for _, key := range tc.expectedKeys {
				if _, ok := result[key]; !ok {
					t.Errorf("expected key %q in response", key)
				}
			}
		})
	}
}

func TestRequireGET(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
		status   int
	}{
		{http.MethodGet, true, http.StatusOK},
		{http.MethodPost, false, http.StatusMethodNotAllowed},
		{http.MethodPut, false, http.StatusMethodNotAllowed},
		{http.MethodDelete, false, http.StatusMethodNotAllowed},
		{http.MethodPatch, false, http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/test", nil)

			result := requireGET(rec, req)

			if result != tc.expected {
				t.Errorf("requireGET() = %v, want %v", result, tc.expected)
			}

			if !tc.expected && rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestRequirePOST(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
		status   int
	}{
		{http.MethodPost, true, http.StatusOK},
		{http.MethodGet, false, http.StatusMethodNotAllowed},
		{http.MethodPut, false, http.StatusMethodNotAllowed},
		{http.MethodDelete, false, http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/test", nil)

			result := requirePOST(rec, req)

			if result != tc.expected {
				t.Errorf("requirePOST() = %v, want %v", result, tc.expected)
			}

			if !tc.expected && rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestCalculateUptimeHours(t *testing.T) {
	tests := []struct {
		name      string
		startTime int64
		minHours  float64
		maxHours  float64
	}{
		{
			name:      "zero start time",
			startTime: 0,
			minHours:  0,
			maxHours:  0,
		},
		{
			name:      "negative start time",
			startTime: -100,
			minHours:  0,
			maxHours:  0,
		},
		{
			name:      "one hour ago",
			startTime: time.Now().Unix() - 3600,
			minHours:  0.9,
			maxHours:  1.1,
		},
		{
			name:      "24 hours ago",
			startTime: time.Now().Unix() - 86400,
			minHours:  23.9,
			maxHours:  24.1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateUptimeHours(tc.startTime)

			if result < tc.minHours || result > tc.maxHours {
				t.Errorf("calculateUptimeHours(%d) = %f, want between %f and %f",
					tc.startTime, result, tc.minHours, tc.maxHours)
			}
		})
	}
}

func TestPopulateNodeInfo(t *testing.T) {
	t.Run("nil nodeInfo", func(t *testing.T) {
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "abc123",
		}
		populateNodeInfo(agent, nil)

		// Should not modify agent when nodeInfo is nil
		if agent.Hostname != "" {
			t.Errorf("Hostname should be empty, got %q", agent.Hostname)
		}
	})

	t.Run("populates all fields", func(t *testing.T) {
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "abc123",
		}
		nodeInfo := &protocol.NodeInfo{
			DisplayName:         "test-agent",
			Hostname:            "server1.example.com",
			OS:                  "linux",
			Arch:                "amd64",
			Version:             "2.0.0",
			PID:                 4242,
			StartTime:           time.Now().Unix() - 7200, // 2 hours ago
			IPAddresses:         []string{"192.168.1.10", "10.0.0.5"},
			UDPEnabled:          true,
			FileTransferEnabled: true,
		}

		populateNodeInfo(agent, nodeInfo)

		if agent.Hostname != "server1.example.com" {
			t.Errorf("Hostname = %q, want %q", agent.Hostname, "server1.example.com")
		}
		if agent.OS != "linux" {
			t.Errorf("OS = %q, want %q", agent.OS, "linux")
		}
		if agent.Arch != "amd64" {
			t.Errorf("Arch = %q, want %q", agent.Arch, "amd64")
		}
		if agent.Version != "2.0.0" {
			t.Errorf("Version = %q, want %q", agent.Version, "2.0.0")
		}
		if agent.PID != 4242 {
			t.Errorf("PID = %d, want 4242", agent.PID)
		}
		if len(agent.IPAddresses) != 2 {
			t.Errorf("IPAddresses len = %d, want 2", len(agent.IPAddresses))
		}
		if !agent.UDPEnabled {
			t.Error("UDPEnabled should be true")
		}
		if !agent.FileTransferEnabled {
			t.Error("FileTransferEnabled should be true")
		}
		// Uptime should be approximately 2 hours
		if agent.UptimeHours < 1.9 || agent.UptimeHours > 2.1 {
			t.Errorf("UptimeHours = %f, want approximately 2", agent.UptimeHours)
		}
		// DisplayName should be updated from nodeInfo when it matches ShortID
		if agent.DisplayName != "test-agent" {
			t.Errorf("DisplayName = %q, want %q", agent.DisplayName, "test-agent")
		}
	})

	t.Run("overrides existing display name with fresh NodeInfo value", func(t *testing.T) {
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "old-name",
		}
		nodeInfo := &protocol.NodeInfo{
			DisplayName: "new-name",
		}

		populateNodeInfo(agent, nodeInfo)

		if agent.DisplayName != "new-name" {
			t.Errorf("DisplayName = %q, want %q", agent.DisplayName, "new-name")
		}
	})

	t.Run("preserves existing display name when NodeInfo has none", func(t *testing.T) {
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "existing-name",
		}
		nodeInfo := &protocol.NodeInfo{}

		populateNodeInfo(agent, nodeInfo)

		if agent.DisplayName != "existing-name" {
			t.Errorf("DisplayName = %q, want %q", agent.DisplayName, "existing-name")
		}
	})

	t.Run("hides shell, file transfer, and icmp for local agent", func(t *testing.T) {
		// The local agent's HTTP API cannot route /agents/{self}/shell,
		// /agents/{self}/file/*, or /agents/{self}/icmp through the mesh,
		// so these capabilities are reported as disabled even when actually
		// enabled in config.
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "abc123",
			IsLocal:     true,
		}
		nodeInfo := &protocol.NodeInfo{
			ShellEnabled:        true,
			FileTransferEnabled: true,
			IcmpEnabled:         true,
		}

		populateNodeInfo(agent, nodeInfo)

		if agent.ShellEnabled {
			t.Error("ShellEnabled must be false for local agent (cannot loop back through mesh)")
		}
		if agent.FileTransferEnabled {
			t.Error("FileTransferEnabled must be false for local agent (cannot loop back through mesh)")
		}
		if agent.IcmpEnabled {
			t.Error("IcmpEnabled must be false for local agent (cannot loop back through mesh)")
		}
	})

	t.Run("preserves shell, file transfer, and icmp for remote agent", func(t *testing.T) {
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "abc123",
			IsLocal:     false,
		}
		nodeInfo := &protocol.NodeInfo{
			ShellEnabled:        true,
			FileTransferEnabled: true,
			IcmpEnabled:         true,
		}

		populateNodeInfo(agent, nodeInfo)

		if !agent.ShellEnabled {
			t.Error("ShellEnabled should be true for remote agent")
		}
		if !agent.FileTransferEnabled {
			t.Error("FileTransferEnabled should be true for remote agent")
		}
		if !agent.IcmpEnabled {
			t.Error("IcmpEnabled should be true for remote agent")
		}
	})

	t.Run("zero PID stays zero so omitempty drops it from JSON", func(t *testing.T) {
		agent := &TopologyAgentInfo{
			ShortID:     "abc123",
			DisplayName: "abc123",
		}
		nodeInfo := &protocol.NodeInfo{
			Hostname: "no-pid-host",
		}

		populateNodeInfo(agent, nodeInfo)

		if agent.PID != 0 {
			t.Errorf("PID = %d, want 0", agent.PID)
		}
	})
}

func TestBuildAgentRoles(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	tests := []struct {
		name                string
		isLocal             bool
		hasSOCKS5           bool
		hasExitRoutes       bool
		hasForwardListeners bool
		hasForwardEndpoints bool
		expected            []string
	}{
		{
			name:                "transit only",
			isLocal:             false,
			hasSOCKS5:           false,
			hasExitRoutes:       false,
			hasForwardListeners: false,
			hasForwardEndpoints: false,
			expected:            []string{"transit"},
		},
		{
			name:                "ingress only",
			isLocal:             true,
			hasSOCKS5:           true,
			hasExitRoutes:       false,
			hasForwardListeners: false,
			hasForwardEndpoints: false,
			expected:            []string{"ingress"},
		},
		{
			name:                "exit only",
			isLocal:             false,
			hasSOCKS5:           false,
			hasExitRoutes:       true,
			hasForwardListeners: false,
			hasForwardEndpoints: false,
			expected:            []string{"exit"},
		},
		{
			name:                "ingress and exit",
			isLocal:             true,
			hasSOCKS5:           true,
			hasExitRoutes:       true,
			hasForwardListeners: false,
			hasForwardEndpoints: false,
			expected:            []string{"ingress", "exit"},
		},
		{
			name:                "forward ingress only",
			isLocal:             true,
			hasSOCKS5:           false,
			hasExitRoutes:       false,
			hasForwardListeners: true,
			hasForwardEndpoints: false,
			expected:            []string{"forward_ingress"},
		},
		{
			name:                "forward exit only",
			isLocal:             false,
			hasSOCKS5:           false,
			hasExitRoutes:       false,
			hasForwardListeners: false,
			hasForwardEndpoints: true,
			expected:            []string{"forward_exit"},
		},
		{
			name:                "all roles",
			isLocal:             true,
			hasSOCKS5:           true,
			hasExitRoutes:       true,
			hasForwardListeners: true,
			hasForwardEndpoints: true,
			expected:            []string{"ingress", "exit", "forward_ingress", "forward_exit"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roles := s.buildAgentRoles(tc.isLocal, tc.hasSOCKS5, tc.hasExitRoutes, tc.hasForwardListeners, tc.hasForwardEndpoints)

			if len(roles) != len(tc.expected) {
				t.Errorf("roles len = %d, want %d", len(roles), len(tc.expected))
				return
			}

			for i, role := range roles {
				if role != tc.expected[i] {
					t.Errorf("roles[%d] = %q, want %q", i, role, tc.expected[i])
				}
			}
		})
	}
}

// ============================================================================
// Splash Page Handler Tests
// ============================================================================

func TestServer_handleSplash(t *testing.T) {
	t.Run("root path with dashboard enabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.EnableDashboard = true
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Muti Metroo") {
			t.Error("expected body to contain 'Muti Metroo'")
		}
	})

	t.Run("root path with dashboard disabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.EnableDashboard = false
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("non-root path returns 404", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		// The CLI and every served page are English-only.
		if !strings.Contains(rec.Body.String(), `<html lang="en">`) {
			t.Error(`expected the 404 page to declare <html lang="en">`)
		}
	})

	t.Run("POST to root not allowed", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodPost, "/", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})
}

// ============================================================================
// Disabled Endpoint Tests
// ============================================================================

func TestServer_DisabledEndpoints(t *testing.T) {
	t.Run("pprof disabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.EnablePprof = false
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("dashboard disabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.EnableDashboard = false
		s := NewServer(cfg, nil)

		endpoints := []string{"/api/topology", "/api/dashboard"}
		for _, endpoint := range endpoints {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			rec := httptest.NewRecorder()

			s.server.Handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want %d", endpoint, rec.Code, http.StatusNotFound)
			}
		}
	})

	t.Run("remote API disabled", func(t *testing.T) {
		cfg := DefaultServerConfig()
		cfg.EnableRemoteAPI = false
		s := NewServer(cfg, nil)

		endpoints := []string{"/agents", "/agents/", "/routes/advertise"}
		for _, endpoint := range endpoints {
			method := http.MethodGet
			if endpoint == "/routes/advertise" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, endpoint, nil)
			rec := httptest.NewRecorder()

			s.server.Handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want %d", endpoint, rec.Code, http.StatusNotFound)
			}
		}
	})
}

// ============================================================================
// Route Trigger Tests
// ============================================================================

// mockRouteAdvertiseTrigger implements RouteAdvertiseTrigger for testing.
type mockRouteAdvertiseTrigger struct {
	triggered bool
}

func (m *mockRouteAdvertiseTrigger) TriggerRouteAdvertise() {
	m.triggered = true
}

func TestServer_handleTriggerAdvertise(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		trigger := &mockRouteAdvertiseTrigger{}
		s.SetRouteAdvertiseTrigger(trigger)

		req := httptest.NewRequest(http.MethodPost, "/routes/advertise", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		if !trigger.triggered {
			t.Error("expected route trigger to be called")
		}

		var response map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response["status"] != "triggered" {
			t.Errorf("status = %v, want %q", response["status"], "triggered")
		}
	})

	t.Run("GET not allowed", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		s.SetRouteAdvertiseTrigger(&mockRouteAdvertiseTrigger{})

		req := httptest.NewRequest(http.MethodGet, "/routes/advertise", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("no trigger configured", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		// Don't set route trigger

		req := httptest.NewRequest(http.MethodPost, "/routes/advertise", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

// ============================================================================
// Remote Provider Tests
// ============================================================================

// mockRemoteStatusProvider implements RemoteStatusProvider for testing.
type mockRemoteStatusProvider struct {
	id                identity.AgentID
	displayName       string
	peerIDs           []identity.AgentID
	knownAgentIDs     []identity.AgentID
	peerDetails       []PeerDetails
	routeDetails      []RouteDetails
	domainRoutesList  []DomainRouteDetails
	forwardRoutesList []PortForwardRouteDetails
	displayNames      map[identity.AgentID]string
	allNodeInfo       map[identity.AgentID]*protocol.NodeInfo
	localNodeInfo     *protocol.NodeInfo
	socks5Info        SOCKS5Info
	udpInfo           UDPInfo
	forwardInfo       PortForwardInfo

	// Optional overrides for tests that need specific behavior. If nil the
	// default (success) is returned.
	uploadFileFn         func(ctx context.Context, targetID identity.AgentID, localPath, remotePath string, opts TransferOptions, progress FileTransferProgress) error
	downloadFileStreamFn func(ctx context.Context, targetID identity.AgentID, remotePath string, opts TransferOptions) (*DownloadStreamResult, error)
}

func (m *mockRemoteStatusProvider) ID() identity.AgentID {
	return m.id
}

func (m *mockRemoteStatusProvider) DisplayName() string {
	return m.displayName
}

func (m *mockRemoteStatusProvider) SendControlRequest(ctx context.Context, targetID identity.AgentID, controlType uint8) (*protocol.ControlResponse, error) {
	return &protocol.ControlResponse{Success: true, Data: []byte("{}")}, nil
}

func (m *mockRemoteStatusProvider) SendControlRequestWithData(ctx context.Context, targetID identity.AgentID, controlType uint8, data []byte) (*protocol.ControlResponse, error) {
	return &protocol.ControlResponse{Success: true, Data: []byte("{}")}, nil
}

func (m *mockRemoteStatusProvider) GetPeerIDs() []identity.AgentID {
	return m.peerIDs
}

func (m *mockRemoteStatusProvider) GetKnownAgentIDs() []identity.AgentID {
	return m.knownAgentIDs
}

func (m *mockRemoteStatusProvider) GetPeerDetails() []PeerDetails {
	return m.peerDetails
}

func (m *mockRemoteStatusProvider) GetRouteDetails() []RouteDetails {
	return m.routeDetails
}

func (m *mockRemoteStatusProvider) GetDomainRouteDetails() []DomainRouteDetails {
	return m.domainRoutesList
}

func (m *mockRemoteStatusProvider) GetAllDisplayNames() map[identity.AgentID]string {
	return m.displayNames
}

func (m *mockRemoteStatusProvider) GetAllNodeInfo() map[identity.AgentID]*protocol.NodeInfo {
	return m.allNodeInfo
}

func (m *mockRemoteStatusProvider) GetLocalNodeInfo() *protocol.NodeInfo {
	return m.localNodeInfo
}

func (m *mockRemoteStatusProvider) GetSOCKS5Info() SOCKS5Info {
	return m.socks5Info
}

func (m *mockRemoteStatusProvider) GetUDPInfo() UDPInfo {
	return m.udpInfo
}

func (m *mockRemoteStatusProvider) GetPortForwardInfo() PortForwardInfo {
	return m.forwardInfo
}

func (m *mockRemoteStatusProvider) GetPortForwardRouteDetails() []PortForwardRouteDetails {
	return m.forwardRoutesList
}

func (m *mockRemoteStatusProvider) UploadFile(ctx context.Context, targetID identity.AgentID, localPath, remotePath string, opts TransferOptions, progress FileTransferProgress) error {
	if m.uploadFileFn != nil {
		return m.uploadFileFn(ctx, targetID, localPath, remotePath, opts, progress)
	}
	return nil
}

func (m *mockRemoteStatusProvider) DownloadFile(ctx context.Context, targetID identity.AgentID, remotePath, localPath string, opts TransferOptions, progress FileTransferProgress) error {
	return nil
}

func (m *mockRemoteStatusProvider) DownloadFileStream(ctx context.Context, targetID identity.AgentID, remotePath string, opts TransferOptions) (*DownloadStreamResult, error) {
	if m.downloadFileStreamFn != nil {
		return m.downloadFileStreamFn(ctx, targetID, remotePath, opts)
	}
	return nil, nil
}

func (m *mockRemoteStatusProvider) ShutdownBoundCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

func TestServer_handleListAgents(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{running: true}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		remoteProvider := &mockRemoteStatusProvider{
			id:            localID,
			displayName:   "local-agent",
			knownAgentIDs: []identity.AgentID{localID, peerID},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/agents", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var agents []map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&agents); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(agents) != 2 {
			t.Errorf("expected 2 agents, got %d", len(agents))
		}

		// First agent should be local
		if agents[0]["local"] != true {
			t.Error("first agent should be local")
		}
	})

	t.Run("no remote provider", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		// Don't set remote provider

		req := httptest.NewRequest(http.MethodGet, "/agents", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

func TestServer_handleTopology(t *testing.T) {
	t.Run("success with peers and routes", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{
			running: true,
			stats: Stats{
				SOCKS5Running:  true,
				ExitHandlerRun: false,
			},
		}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		remoteProvider := &mockRemoteStatusProvider{
			id:          localID,
			displayName: "local-agent",
			peerIDs:     []identity.AgentID{peerID},
			peerDetails: []PeerDetails{
				{
					ID:          peerID,
					DisplayName: "peer-agent",
					State:       "connected",
					RTT:         50 * time.Millisecond,
					Transport:   "quic",
				},
			},
			routeDetails: []RouteDetails{
				{
					Network: "10.0.0.0/8",
					NextHop: peerID,
					Origin:  peerID,
					Metric:  1,
					Path:    []identity.AgentID{peerID},
				},
			},
			displayNames: map[identity.AgentID]string{
				peerID: "peer-agent",
			},
			allNodeInfo:   map[identity.AgentID]*protocol.NodeInfo{},
			localNodeInfo: &protocol.NodeInfo{},
			socks5Info: SOCKS5Info{
				Enabled: true,
				Address: "127.0.0.1:1080",
			},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response TopologyResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !response.LocalAgent.IsLocal {
			t.Error("expected local agent to have IsLocal=true")
		}

		if len(response.Agents) < 1 {
			t.Error("expected at least 1 agent in response")
		}

		if len(response.Connections) < 1 {
			t.Error("expected at least 1 connection in response")
		}
	})

	t.Run("PID surfaced for local and remote", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{running: true, stats: Stats{}}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		const localPID = 1234
		const remotePID = 5678

		remoteProvider := &mockRemoteStatusProvider{
			id:          localID,
			displayName: "local-agent",
			peerIDs:     []identity.AgentID{peerID},
			peerDetails: []PeerDetails{
				{ID: peerID, DisplayName: "peer-agent", State: "connected", Transport: "quic"},
			},
			displayNames: map[identity.AgentID]string{peerID: "peer-agent"},
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
				peerID: {DisplayName: "peer-agent", PID: remotePID},
			},
			localNodeInfo: &protocol.NodeInfo{DisplayName: "local-agent", PID: localPID},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response TopologyResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if response.LocalAgent.PID != localPID {
			t.Errorf("LocalAgent.PID = %d, want %d", response.LocalAgent.PID, localPID)
		}

		var foundLocal, foundRemote bool
		for _, a := range response.Agents {
			switch a.ID {
			case localID.String():
				foundLocal = true
				if a.PID != localPID {
					t.Errorf("Agents[local].PID = %d, want %d", a.PID, localPID)
				}
			case peerID.String():
				foundRemote = true
				if a.PID != remotePID {
					t.Errorf("Agents[remote].PID = %d, want %d", a.PID, remotePID)
				}
			}
		}
		if !foundLocal {
			t.Error("local agent not present in Agents list")
		}
		if !foundRemote {
			t.Error("remote agent not present in Agents list")
		}

		// Zero PID must be omitted from JSON via the omitempty tag.
		// Build a separate fixture where neither side has a PID set.
		remoteProvider2 := &mockRemoteStatusProvider{
			id:            localID,
			displayName:   "local-agent",
			allNodeInfo:   map[identity.AgentID]*protocol.NodeInfo{},
			localNodeInfo: &protocol.NodeInfo{DisplayName: "local-agent"},
		}
		s2 := NewServer(cfg, provider)
		s2.SetRemoteProvider(remoteProvider2)
		req2 := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		rec2 := httptest.NewRecorder()
		s2.server.Handler.ServeHTTP(rec2, req2)

		body := rec2.Body.String()
		if strings.Contains(body, "\"pid\"") {
			t.Errorf("zero PID must be omitted from JSON, got body containing pid: %s", body)
		}
	})

	t.Run("advertised listeners surfaced for local and remote", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{
			running: true,
			stats:   Stats{},
		}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		localAdv := []protocol.AdvertisedListener{
			{Transport: "h2", Address: "local.example:3000", Path: "/mesh"},
		}
		remoteAdv := []protocol.AdvertisedListener{
			{Transport: "quic", Address: "remote.example:4433"},
			{Transport: "ws", Address: "remote.example:443", Path: "/ws"},
		}

		remoteProvider := &mockRemoteStatusProvider{
			id:          localID,
			displayName: "local-agent",
			peerIDs:     []identity.AgentID{peerID},
			peerDetails: []PeerDetails{
				{
					ID:          peerID,
					DisplayName: "peer-agent",
					State:       "connected",
					RTT:         50 * time.Millisecond,
					Transport:   "quic",
				},
			},
			displayNames: map[identity.AgentID]string{
				peerID: "peer-agent",
			},
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
				peerID: {DisplayName: "peer-agent", AdvertisedListeners: remoteAdv},
			},
			localNodeInfo: &protocol.NodeInfo{DisplayName: "local-agent", AdvertisedListeners: localAdv},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response TopologyResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if !equalAdvertised(response.LocalAgent.AdvertisedListeners, localAdv) {
			t.Errorf("LocalAgent.AdvertisedListeners = %+v, want %+v", response.LocalAgent.AdvertisedListeners, localAdv)
		}

		var foundLocal, foundRemote bool
		for _, a := range response.Agents {
			switch a.ID {
			case localID.String():
				foundLocal = true
				if !equalAdvertised(a.AdvertisedListeners, localAdv) {
					t.Errorf("Agents[local].AdvertisedListeners = %+v, want %+v", a.AdvertisedListeners, localAdv)
				}
			case peerID.String():
				foundRemote = true
				if !equalAdvertised(a.AdvertisedListeners, remoteAdv) {
					t.Errorf("Agents[remote].AdvertisedListeners = %+v, want %+v", a.AdvertisedListeners, remoteAdv)
				}
			}
		}
		if !foundLocal {
			t.Error("local agent not present in Agents list")
		}
		if !foundRemote {
			t.Error("remote agent not present in Agents list")
		}
	})

	t.Run("forward listeners surfaced for remote agents", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{running: true, stats: Stats{}}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		remoteProvider := &mockRemoteStatusProvider{
			id:          localID,
			displayName: "local-agent",
			peerIDs:     []identity.AgentID{peerID},
			peerDetails: []PeerDetails{
				{ID: peerID, DisplayName: "peer-agent", State: "connected", Transport: "quic"},
			},
			displayNames: map[identity.AgentID]string{peerID: "peer-agent"},
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
				peerID: {
					DisplayName: "peer-agent",
					ForwardListeners: []protocol.ForwardListenerInfo{
						{Key: "web", Address: ":9090"},
						{Key: "ssh", Address: ":2222"},
					},
				},
			},
			localNodeInfo: &protocol.NodeInfo{DisplayName: "local-agent"},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response TopologyResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		var found bool
		for _, a := range response.Agents {
			if a.ID != peerID.String() {
				continue
			}
			found = true
			wantKeys := map[string]bool{"web": true, "ssh": true}
			if len(a.ForwardListeners) != len(wantKeys) {
				t.Errorf("ForwardListeners = %v, want keys %v", a.ForwardListeners, wantKeys)
			}
			for _, k := range a.ForwardListeners {
				if !wantKeys[k] {
					t.Errorf("unexpected listener key %q", k)
				}
			}
			var hasForwardIngress bool
			for _, r := range a.Roles {
				if r == "forward_ingress" {
					hasForwardIngress = true
					break
				}
			}
			if !hasForwardIngress {
				t.Errorf("Roles = %v, want forward_ingress", a.Roles)
			}
		}
		if !found {
			t.Error("remote agent not present in Agents list")
		}
	})

	t.Run("no remote provider", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

func equalAdvertised(a, b []protocol.AdvertisedListener) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestServer_handleDashboard(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{
			running: true,
			stats: Stats{
				PeerCount:   2,
				StreamCount: 5,
				RouteCount:  3,
			},
		}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		remoteProvider := &mockRemoteStatusProvider{
			id:          localID,
			displayName: "local-agent",
			peerDetails: []PeerDetails{
				{
					ID:          peerID,
					DisplayName: "peer-agent",
					State:       "connected",
					RTT:         30 * time.Millisecond,
				},
			},
			routeDetails: []RouteDetails{
				{
					Network: "0.0.0.0/0",
					NextHop: peerID,
					Origin:  peerID,
					Metric:  1,
					Path:    []identity.AgentID{peerID},
				},
			},
			displayNames: map[identity.AgentID]string{
				peerID: "peer-agent",
			},
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response DashboardResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.Stats.PeerCount != 2 {
			t.Errorf("PeerCount = %d, want 2", response.Stats.PeerCount)
		}

		if len(response.Peers) != 1 {
			t.Errorf("expected 1 peer, got %d", len(response.Peers))
		}

		if len(response.Routes) != 1 {
			t.Errorf("expected 1 route, got %d", len(response.Routes))
		}
	})

	t.Run("no providers configured", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	// Regression: when the local agent owns a listener and a remote agent
	// owns the endpoint, forward_routes must contain exactly one row.
	// AnnounceLocalNodeInfo stores local node info in the routing manager,
	// so GetAllNodeInfo returns the local entry alongside remotes; without
	// the localID skip in handleDashboard, the ingress x exit cross-product
	// emits two identical rows.
	t.Run("forward routes local listener remote endpoint single row", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{running: true}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		remoteID, _ := identity.NewAgentID()

		localNodeInfo := &protocol.NodeInfo{
			DisplayName: "local-agent",
			ForwardListeners: []protocol.ForwardListenerInfo{
				{Key: "web", Address: ":9090"},
			},
			Peers: []protocol.PeerConnectionInfo{
				{PeerID: [16]byte(remoteID)},
			},
		}

		remoteProvider := &mockRemoteStatusProvider{
			id:            localID,
			displayName:   "local-agent",
			localNodeInfo: localNodeInfo,
			// Simulate post-AnnounceLocalNodeInfo state: the routing
			// manager stores local info, so GetAllNodeInfo returns it
			// alongside remote entries.
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
				localID: localNodeInfo,
				remoteID: {
					DisplayName: "remote-agent",
					Peers: []protocol.PeerConnectionInfo{
						{PeerID: [16]byte(localID)},
					},
				},
			},
			displayNames: map[identity.AgentID]string{
				remoteID: "remote-agent",
			},
			forwardRoutesList: []PortForwardRouteDetails{
				{
					Key:             "web",
					ListenerAddress: ":9090",
					Target:          "127.0.0.1:3000",
					NextHop:         remoteID,
					Origin:          remoteID,
					Metric:          1,
					HopCount:        1,
					Path:            []identity.AgentID{remoteID},
					IsLocal:         false,
				},
			},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response DashboardResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(response.ForwardRoutes) != 1 {
			t.Fatalf("ForwardRoutes length = %d, want 1; rows=%+v", len(response.ForwardRoutes), response.ForwardRoutes)
		}

		row := response.ForwardRoutes[0]
		if row.Key != "web" {
			t.Errorf("Key = %q, want %q", row.Key, "web")
		}
		if row.IngressAgentID != localID.ShortString() {
			t.Errorf("IngressAgentID = %q, want %q", row.IngressAgentID, localID.ShortString())
		}
		if row.ExitAgentID != remoteID.ShortString() {
			t.Errorf("ExitAgentID = %q, want %q", row.ExitAgentID, remoteID.ShortString())
		}
		if row.ListenerAddress != ":9090" {
			t.Errorf("ListenerAddress = %q, want %q", row.ListenerAddress, ":9090")
		}
		if row.Target != "127.0.0.1:3000" {
			t.Errorf("Target = %q, want %q", row.Target, "127.0.0.1:3000")
		}
	})

	// Symmetric case: remote listener + local endpoint also emits one row.
	t.Run("forward routes remote listener local endpoint single row", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{running: true}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		remoteID, _ := identity.NewAgentID()

		localNodeInfo := &protocol.NodeInfo{
			DisplayName: "local-agent",
			Peers: []protocol.PeerConnectionInfo{
				{PeerID: [16]byte(remoteID)},
			},
		}

		remoteProvider := &mockRemoteStatusProvider{
			id:            localID,
			displayName:   "local-agent",
			localNodeInfo: localNodeInfo,
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
				localID: localNodeInfo,
				remoteID: {
					DisplayName: "remote-agent",
					ForwardListeners: []protocol.ForwardListenerInfo{
						{Key: "web", Address: ":9090"},
					},
					Peers: []protocol.PeerConnectionInfo{
						{PeerID: [16]byte(localID)},
					},
				},
			},
			displayNames: map[identity.AgentID]string{
				remoteID: "remote-agent",
			},
			forwardRoutesList: []PortForwardRouteDetails{
				{
					Key:      "web",
					Target:   "127.0.0.1:3000",
					NextHop:  localID,
					Origin:   localID,
					Metric:   0,
					HopCount: 0,
					Path:     []identity.AgentID{},
					IsLocal:  true,
				},
			},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response DashboardResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(response.ForwardRoutes) != 1 {
			t.Fatalf("ForwardRoutes length = %d, want 1; rows=%+v", len(response.ForwardRoutes), response.ForwardRoutes)
		}

		row := response.ForwardRoutes[0]
		if row.IngressAgentID != remoteID.ShortString() {
			t.Errorf("IngressAgentID = %q, want %q", row.IngressAgentID, remoteID.ShortString())
		}
		if row.ExitAgentID != localID.ShortString() {
			t.Errorf("ExitAgentID = %q, want %q", row.ExitAgentID, localID.ShortString())
		}
	})
}

func TestServer_handleNodes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cfg := DefaultServerConfig()
		provider := &mockStatsProvider{running: true}
		s := NewServer(cfg, provider)

		localID, _ := identity.NewAgentID()
		peerID, _ := identity.NewAgentID()

		remoteProvider := &mockRemoteStatusProvider{
			id:          localID,
			displayName: "local-agent",
			peerIDs:     []identity.AgentID{peerID},
			localNodeInfo: &protocol.NodeInfo{
				Hostname: "local-host",
				OS:       "linux",
			},
			allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
				peerID: {
					DisplayName: "peer-agent",
					Hostname:    "peer-host",
					OS:          "darwin",
				},
			},
		}
		s.SetRemoteProvider(remoteProvider)

		req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var response NodesResponse
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(response.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(response.Nodes))
		}

		// First node should be local
		if !response.Nodes[0].IsLocal {
			t.Error("first node should be local")
		}
	})

	t.Run("no remote provider", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

// ============================================================================
// Agent Info Handler Tests
// ============================================================================

func TestServer_handleAgentInfo(t *testing.T) {
	t.Run("invalid agent ID", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		s.SetRemoteProvider(&mockRemoteStatusProvider{})

		req := httptest.NewRequest(http.MethodGet, "/agents/invalid-id", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("empty agent ID", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		s.SetRemoteProvider(&mockRemoteStatusProvider{})

		req := httptest.NewRequest(http.MethodGet, "/agents/", nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("no remote provider", func(t *testing.T) {
		cfg := DefaultServerConfig()
		s := NewServer(cfg, nil)
		// Don't set remote provider

		agentID, _ := identity.NewAgentID()
		req := httptest.NewRequest(http.MethodGet, "/agents/"+agentID.String(), nil)
		rec := httptest.NewRecorder()

		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

// ============================================================================
// BuildLocalAgentInfo Tests
// ============================================================================

func TestServer_buildLocalAgentInfo(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	localID, _ := identity.NewAgentID()

	remoteProvider := &mockRemoteStatusProvider{
		id:          localID,
		displayName: "test-agent",
		localNodeInfo: &protocol.NodeInfo{
			Hostname:    "test-host",
			OS:          "linux",
			Arch:        "amd64",
			Version:     "2.0.0",
			StartTime:   time.Now().Unix() - 3600,
			IPAddresses: []string{"192.168.1.10"},
		},
	}
	s.SetRemoteProvider(remoteProvider)

	stats := Stats{
		SOCKS5Running:  true,
		ExitHandlerRun: true,
	}
	socks5Info := SOCKS5Info{
		Enabled: true,
		Address: "127.0.0.1:1080",
	}
	udpInfo := UDPInfo{
		Enabled: true,
	}
	forwardInfo := PortForwardInfo{
		ListenerKeys: []string{"web"},
		EndpointKeys: []string{"api"},
	}

	agent := s.buildLocalAgentInfo(localID, "test-agent", stats, socks5Info, udpInfo, forwardInfo)

	if agent.ID != localID.String() {
		t.Errorf("ID = %q, want %q", agent.ID, localID.String())
	}
	if agent.DisplayName != "test-agent" {
		t.Errorf("DisplayName = %q, want %q", agent.DisplayName, "test-agent")
	}
	if !agent.IsLocal {
		t.Error("expected IsLocal to be true")
	}
	if !agent.IsConnected {
		t.Error("expected IsConnected to be true")
	}
	if agent.SOCKS5Addr != "127.0.0.1:1080" {
		t.Errorf("SOCKS5Addr = %q, want %q", agent.SOCKS5Addr, "127.0.0.1:1080")
	}
	if !agent.UDPEnabled {
		t.Error("expected UDPEnabled to be true")
	}
	// Should have ingress, exit, forward_ingress, and forward_exit roles
	if len(agent.Roles) != 4 {
		t.Errorf("expected 4 roles, got %d: %v", len(agent.Roles), agent.Roles)
	}
	// Check port forward info
	if len(agent.ForwardListeners) != 1 || agent.ForwardListeners[0] != "web" {
		t.Errorf("ForwardListeners = %v, want [web]", agent.ForwardListeners)
	}
	if len(agent.ForwardEndpoints) != 1 || agent.ForwardEndpoints[0] != "api" {
		t.Errorf("ForwardEndpoints = %v, want [api]", agent.ForwardEndpoints)
	}
}

// ============================================================================
// Server Configuration Tests
// ============================================================================

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg.Address != ":8080" {
		t.Errorf("Address = %q, want %q", cfg.Address, ":8080")
	}
	if cfg.ReadTimeout != 10*time.Second {
		t.Errorf("ReadTimeout = %v, want %v", cfg.ReadTimeout, 10*time.Second)
	}
	if cfg.WriteTimeout != 10*time.Second {
		t.Errorf("WriteTimeout = %v, want %v", cfg.WriteTimeout, 10*time.Second)
	}
	if !cfg.EnablePprof {
		t.Error("expected EnablePprof to be true by default")
	}
	if !cfg.EnableDashboard {
		t.Error("expected EnableDashboard to be true by default")
	}
	if !cfg.EnableRemoteAPI {
		t.Error("expected EnableRemoteAPI to be true by default")
	}
}

func TestServer_Handler(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	handler := s.Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}
}

func TestServer_Address_NotStarted(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	addr := s.Address()
	if addr != nil {
		t.Errorf("expected nil address before start, got %v", addr)
	}
}

// ============================================================================
// ShellStreamAdapter Tests
// ============================================================================

func TestNewShellStreamAdapter(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	closeCalled := false
	closeFunc := func() { closeCalled = true }

	adapter := NewShellStreamAdapter(123, targetID, closeFunc)

	if adapter == nil {
		t.Fatal("NewShellStreamAdapter returned nil")
	}
	if adapter.streamID != 123 {
		t.Errorf("streamID = %d, want 123", adapter.streamID)
	}
	if adapter.targetID != targetID {
		t.Error("targetID mismatch")
	}
	if adapter.send == nil {
		t.Error("send channel should not be nil")
	}
	if adapter.receive == nil {
		t.Error("receive channel should not be nil")
	}
	if adapter.done == nil {
		t.Error("done channel should not be nil")
	}

	// Test that close calls closeFunc
	adapter.Close()
	if !closeCalled {
		t.Error("closeFunc should have been called")
	}
}

func TestShellStreamAdapter_ToSession(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(456, targetID, nil)

	session := adapter.ToSession()

	if session == nil {
		t.Fatal("ToSession returned nil")
	}
	if session.StreamID != 456 {
		t.Errorf("StreamID = %d, want 456", session.StreamID)
	}
	if session.TargetID != targetID {
		t.Error("TargetID mismatch")
	}
	if session.Send == nil {
		t.Error("Send channel should not be nil")
	}
	if session.Receive == nil {
		t.Error("Receive channel should not be nil")
	}
	if session.Done == nil {
		t.Error("Done channel should not be nil")
	}
	if session.Close == nil {
		t.Error("Close func should not be nil")
	}
}

func TestShellStreamAdapter_PushReceive(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	// Push data
	testData := []byte("test data")
	adapter.PushReceive(testData)

	// Receive data
	select {
	case received := <-adapter.receive:
		if string(received) != "test data" {
			t.Errorf("received = %q, want %q", received, "test data")
		}
	default:
		t.Error("expected data in receive channel")
	}
}

func TestShellStreamAdapter_PushReceive_Closed(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	adapter.Close()

	// Should not panic when pushing to closed adapter
	adapter.PushReceive([]byte("data"))
}

func TestShellStreamAdapter_PopSend(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	// Send data
	go func() {
		adapter.send <- []byte("sent data")
	}()

	// Pop data
	data, ok := adapter.PopSend()
	if !ok {
		t.Error("expected ok to be true")
	}
	if string(data) != "sent data" {
		t.Errorf("data = %q, want %q", data, "sent data")
	}
}

func TestShellStreamAdapter_PopSend_Closed(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	adapter.Close()

	data, ok := adapter.PopSend()
	if ok {
		t.Error("expected ok to be false after close")
	}
	if data != nil {
		t.Errorf("expected nil data, got %v", data)
	}
}

func TestShellStreamAdapter_SetExitCode(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	adapter.SetExitCode(42)

	if adapter.exitCode != 42 {
		t.Errorf("exitCode = %d, want 42", adapter.exitCode)
	}
}

func TestShellStreamAdapter_SetError(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	testErr := io.EOF
	adapter.SetError(testErr)

	if adapter.err != testErr {
		t.Errorf("err = %v, want %v", adapter.err, testErr)
	}
}

func TestShellStreamAdapter_Close_Idempotent(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	closeCount := 0
	closeFunc := func() { closeCount++ }

	adapter := NewShellStreamAdapter(1, targetID, closeFunc)

	// Close multiple times
	adapter.Close()
	adapter.Close()
	adapter.Close()

	// closeFunc should only be called once
	if closeCount != 1 {
		t.Errorf("closeFunc called %d times, want 1", closeCount)
	}
}

func TestShellStreamAdapter_Read(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	// Push data to receive channel
	go func() {
		adapter.receive <- []byte("hello world")
	}()

	buf := make([]byte, 1024)
	n, err := adapter.Read(buf)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 11 {
		t.Errorf("n = %d, want 11", n)
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("data = %q, want %q", buf[:n], "hello world")
	}
}

func TestShellStreamAdapter_Read_Closed(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	adapter.Close()

	buf := make([]byte, 1024)
	_, err := adapter.Read(buf)

	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestShellStreamAdapter_Read_ChannelClosed(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	close(adapter.receive)

	buf := make([]byte, 1024)
	_, err := adapter.Read(buf)

	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestShellStreamAdapter_Write(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	go func() {
		data := <-adapter.send
		if string(data) != "write test" {
			t.Errorf("data = %q, want %q", data, "write test")
		}
	}()

	n, err := adapter.Write([]byte("write test"))

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 10 {
		t.Errorf("n = %d, want 10", n)
	}
}

func TestShellStreamAdapter_Write_Closed(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	// Fill the buffer first (buffer size is 64)
	for i := 0; i < 64; i++ {
		adapter.send <- []byte("fill")
	}

	adapter.Close()

	// Now write should fail because buffer is full and done is closed
	_, err := adapter.Write([]byte("data"))

	if err != io.ErrClosedPipe {
		t.Errorf("expected io.ErrClosedPipe, got %v", err)
	}
}

func TestShellStreamAdapter_GetStreamID(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(789, targetID, nil)

	if adapter.GetStreamID() != 789 {
		t.Errorf("GetStreamID() = %d, want 789", adapter.GetStreamID())
	}
}

func TestShellStreamAdapter_SetNextHop(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	nextHopID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	// Create a mock peer sender (nil is acceptable for this test)
	adapter.SetNextHop(nextHopID, nil)

	if adapter.nextHop != nextHopID {
		t.Error("nextHop not set correctly")
	}
}

func TestShellStreamAdapter_SessionKey(t *testing.T) {
	targetID, _ := identity.NewAgentID()
	adapter := NewShellStreamAdapter(1, targetID, nil)

	// Initially nil
	if adapter.GetSessionKey() != nil {
		t.Error("expected nil session key initially")
	}

	// Set and get (using nil as test value since we can't easily create a real SessionKey)
	adapter.SetSessionKey(nil)
	if adapter.GetSessionKey() != nil {
		t.Error("expected nil after setting nil")
	}
}

// ============================================================================
// Domain Routes in Dashboard Tests
// ============================================================================

func TestServer_handleDashboard_WithDomainRoutes(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{
		running: true,
		stats: Stats{
			PeerCount:   1,
			StreamCount: 2,
			RouteCount:  2,
		},
	}
	s := NewServer(cfg, provider)

	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()

	remoteProvider := &mockRemoteStatusProvider{
		id:          localID,
		displayName: "local-agent",
		peerDetails: []PeerDetails{
			{
				ID:          peerID,
				DisplayName: "peer-agent",
				State:       "connected",
				RTT:         20 * time.Millisecond,
			},
		},
		routeDetails: []RouteDetails{
			{
				Network: "10.0.0.0/8",
				NextHop: peerID,
				Origin:  peerID,
				Metric:  1,
				Path:    []identity.AgentID{peerID},
			},
		},
		domainRoutesList: []DomainRouteDetails{
			{
				Pattern:    "*.example.com",
				IsWildcard: true,
				NextHop:    peerID,
				Origin:     peerID,
				Metric:     1,
				Path:       []identity.AgentID{peerID},
			},
		},
		displayNames: map[identity.AgentID]string{
			peerID: "peer-agent",
		},
		allNodeInfo: map[identity.AgentID]*protocol.NodeInfo{
			peerID: {
				UDPEnabled: true,
			},
		},
	}
	s.SetRemoteProvider(remoteProvider)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response DashboardResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should have both CIDR and domain routes
	if len(response.Routes) != 2 {
		t.Errorf("expected 2 routes (1 CIDR + 1 domain), got %d", len(response.Routes))
	}

	// Should have domain routes in legacy field
	if len(response.DomainRoutes) != 1 {
		t.Errorf("expected 1 domain route, got %d", len(response.DomainRoutes))
	}

	// Verify domain route details
	if response.DomainRoutes[0].Pattern != "*.example.com" {
		t.Errorf("Pattern = %q, want %q", response.DomainRoutes[0].Pattern, "*.example.com")
	}
	if !response.DomainRoutes[0].IsWildcard {
		t.Error("expected IsWildcard to be true")
	}
	if !response.DomainRoutes[0].UDP {
		t.Error("expected UDP to be true (exit has UDP enabled)")
	}
}

// ============================================================================
// Topology with Unresponsive Peer Tests
// ============================================================================

func TestServer_handleTopology_UnresponsivePeer(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()

	remoteProvider := &mockRemoteStatusProvider{
		id:          localID,
		displayName: "local-agent",
		peerIDs:     []identity.AgentID{peerID},
		peerDetails: []PeerDetails{
			{
				ID:          peerID,
				DisplayName: "slow-peer",
				State:       "connected",
				RTT:         120 * time.Second, // Very slow - should be marked unresponsive
				Transport:   "quic",
			},
		},
		displayNames:  map[identity.AgentID]string{},
		allNodeInfo:   map[identity.AgentID]*protocol.NodeInfo{},
		localNodeInfo: &protocol.NodeInfo{},
	}
	s.SetRemoteProvider(remoteProvider)

	req := httptest.NewRequest(http.MethodGet, "/api/topology", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response TopologyResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Connection should be marked as unresponsive
	if len(response.Connections) < 1 {
		t.Fatal("expected at least 1 connection")
	}

	found := false
	for _, conn := range response.Connections {
		if conn.ToAgent == peerID.ShortString() {
			found = true
			if !conn.Unresponsive {
				t.Error("expected connection to be marked as unresponsive")
			}
		}
	}
	if !found {
		t.Error("connection to peer not found")
	}
}

// ============================================================================
// SetShellProvider Test
// ============================================================================

func TestServer_SetShellProvider(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	// Initially nil
	if s.shellProvider != nil {
		t.Error("expected nil shell provider initially")
	}

	// Set provider (using nil as we don't have a real implementation for unit tests)
	s.SetShellProvider(nil)

	// This test just verifies the method exists and doesn't panic
}

// ============================================================================
// CanDecryptManagement Tests
// ============================================================================

func TestServer_CanDecryptManagement(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	// Without sealed box
	if s.CanDecryptManagement() {
		t.Error("expected CanDecryptManagement to return false without sealed box")
	}
}

// ============================================================================
// ShouldRestrictTopology Tests
// ============================================================================

func TestServer_shouldRestrictTopology(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, nil)

	// Without sealed box - no restriction
	if s.shouldRestrictTopology() {
		t.Error("expected no restriction without sealed box")
	}
}

// ============================================================================
// Route Management Tests
// ============================================================================

// mockMgmtAuthProvider implements MgmtAuthProvider for mutation-handler
// tests. The handlers reject mutations when mgmtAuthProvider is nil or
// CanSignManagement() is false, so mutation tests install this stub to
// focus on the mutation logic rather than signature handling.
type mockMgmtAuthProvider struct {
	canSign bool
}

func (m *mockMgmtAuthProvider) CanSignManagement() bool { return m.canSign }

func (m *mockMgmtAuthProvider) StampMutationBody(_ identity.AgentID, _ string, body []byte) ([]byte, error) {
	return body, nil
}

func (m *mockMgmtAuthProvider) StampBody(_ identity.AgentID, _ string, body []byte) ([]byte, error) {
	return body, nil
}

// installMgmtAuth is a shorthand used by mutation tests.
func installMgmtAuth(s *Server) {
	s.SetMgmtAuthProvider(&mockMgmtAuthProvider{canSign: true})
}

// mockRouteManageProvider implements RouteManageProvider for testing.
type mockRouteManageProvider struct {
	result *RouteManageResult
	err    error

	// got* record the last ManageRoute call so tests can assert the
	// request body was decoded into the expected arguments.
	gotAction    string
	gotNetwork   string
	gotMetric    uint16
	gotComment   string
	gotSourceIP  string
	gotInterface string
	gotScope     string
}

func (m *mockRouteManageProvider) ManageRoute(action, network string, metric uint16, comment, sourceIP, iface, scope string) (*RouteManageResult, error) {
	m.gotAction = action
	m.gotNetwork = network
	m.gotMetric = metric
	m.gotComment = comment
	m.gotSourceIP = sourceIP
	m.gotInterface = iface
	m.gotScope = scope
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestHandleRouteManage_Add(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		result: &RouteManageResult{
			Status:  "ok",
			Message: "route 10.0.0.0/8 added",
		},
	})

	body := strings.NewReader(`{"action":"add","network":"10.0.0.0/8","metric":0}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", result["status"])
	}
}

func TestHandleRouteManage_Remove(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		result: &RouteManageResult{
			Status:  "ok",
			Message: "route 10.0.0.0/8 removed",
		},
	})

	body := strings.NewReader(`{"action":"remove","network":"10.0.0.0/8"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHandleRouteManage_List(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		result: &RouteManageResult{
			Status: "ok",
			Routes: []RouteManageResultEntry{
				{Network: "10.0.0.0/8", Metric: 0},
				{Network: "172.16.0.0/12", Metric: 5},
			},
		},
	})

	body := strings.NewReader(`{"action":"list"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var result struct {
		Status string                   `json:"status"`
		Routes []RouteManageResultEntry `json:"routes"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if len(result.Routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(result.Routes))
	}
}

func TestHandleRouteManage_InvalidCIDR(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		err: fmt.Errorf("invalid CIDR"),
	})

	body := strings.NewReader(`{"action":"add","network":"not-a-cidr"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleRouteManage_MethodNotAllowed(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{})

	req := httptest.NewRequest(http.MethodGet, "/routes/manage", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleRouteManage_NoProvider(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	// No route manage provider set

	body := strings.NewReader(`{"action":"list"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHandleRouteManage_ManagementKeyRestriction(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		result: &RouteManageResult{Status: "ok"},
	})

	// Set up sealed box without private key (public-key-only = can't decrypt = restricted)
	_, pub, _ := crypto.GenerateEphemeralKeypair()
	sb := crypto.NewSealedBox(pub)
	s.SetSealedBox(sb)

	body := strings.NewReader(`{"action":"list"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestHandleRouteManage_NoManagementKey(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		result: &RouteManageResult{Status: "ok"},
	})
	// No sealed box = no restriction

	body := strings.NewReader(`{"action":"list"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

// TestHandleRouteManage_AddForwardsComment ensures the comment field on the
// add request reaches the provider unchanged.
func TestHandleRouteManage_AddForwardsComment(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	provider := &mockRouteManageProvider{
		result: &RouteManageResult{Status: "ok", Message: "added"},
	}
	s.SetRouteManageProvider(provider)

	body := strings.NewReader(`{"action":"add","network":"10.0.0.0/8","metric":0,"comment":"office LAN"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if provider.gotAction != "add" || provider.gotComment != "office LAN" {
		t.Errorf("provider got action=%q comment=%q, want add / office LAN", provider.gotAction, provider.gotComment)
	}
}

// TestHandleRouteManage_SetComment verifies the set_comment action is
// accepted, treated as a mutation, and forwards the comment to the
// provider unchanged.
func TestHandleRouteManage_SetComment(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	provider := &mockRouteManageProvider{
		result: &RouteManageResult{Status: "ok", Message: "comment updated"},
	}
	s.SetRouteManageProvider(provider)

	body := strings.NewReader(`{"action":"set_comment","network":"10.0.0.0/8","comment":"renamed"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if provider.gotAction != "set_comment" || provider.gotNetwork != "10.0.0.0/8" || provider.gotComment != "renamed" {
		t.Errorf("provider got %+v, want action=set_comment network=10.0.0.0/8 comment=renamed", provider)
	}
}

// TestHandleRouteManage_SetComment_RequiresMgmtAuth confirms set_comment is
// gated by the management-signing capability: without a signing private key
// the handler returns 403 and the provider must never see the call.
func TestHandleRouteManage_SetComment_RequiresMgmtAuth(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	s.SetMgmtAuthProvider(&mockMgmtAuthProvider{canSign: false})
	provider := &mockRouteManageProvider{}
	s.SetRouteManageProvider(provider)

	body := strings.NewReader(`{"action":"set_comment","network":"10.0.0.0/8","comment":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d: %s", http.StatusForbidden, rec.Code, rec.Body.String())
	}
	if provider.gotAction != "" {
		t.Errorf("provider was called (action=%q) despite missing auth", provider.gotAction)
	}
}

// TestHandleRouteManage_ListReturnsComment confirms the list response
// carries the comment field through to the JSON wire shape.
func TestHandleRouteManage_ListReturnsComment(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetRouteManageProvider(&mockRouteManageProvider{
		result: &RouteManageResult{
			Status: "ok",
			Routes: []RouteManageResultEntry{
				{Network: "10.0.0.0/8", Metric: 0, Comment: "internal"},
				{Network: "172.16.0.0/12", Metric: 5}, // no comment
			},
		},
	})

	body := strings.NewReader(`{"action":"list"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	var result struct {
		Routes []RouteManageResultEntry `json:"routes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Routes) != 2 || result.Routes[0].Comment != "internal" || result.Routes[1].Comment != "" {
		t.Errorf("routes = %+v, want first comment=internal and second comment empty", result.Routes)
	}
}

// TestHandleRouteManage_ScopeAll confirms the scope field on a list
// request reaches the provider unchanged.
func TestHandleRouteManage_ScopeAll(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	provider := &mockRouteManageProvider{
		result: &RouteManageResult{Status: "ok"},
	}
	s.SetRouteManageProvider(provider)

	body := strings.NewReader(`{"action":"list","scope":"all"}`)
	req := httptest.NewRequest(http.MethodPost, "/routes/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if provider.gotAction != "list" || provider.gotScope != "all" {
		t.Errorf("provider got action=%q scope=%q, want list / all", provider.gotAction, provider.gotScope)
	}
}

// ============================================================================
// Display Name Management Tests
// ============================================================================

// mockDisplayNameManageProvider implements DisplayNameManageProvider for testing.
type mockDisplayNameManageProvider struct {
	result *DisplayNameManageResult
	err    error
}

func (m *mockDisplayNameManageProvider) ManageDisplayName(action, name string) (*DisplayNameManageResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestHandleDisplayNameManage_Set(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{
		result: &DisplayNameManageResult{
			Status:  "ok",
			Message: `display name set to "gateway-us-east"`,
			Name:    "gateway-us-east",
		},
	})

	body := strings.NewReader(`{"action":"set","name":"gateway-us-east"}`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var result DisplayNameManageResult
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", result.Status)
	}
	if result.Name != "gateway-us-east" {
		t.Errorf("expected name 'gateway-us-east', got %q", result.Name)
	}
}

func TestHandleDisplayNameManage_Get(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{
		result: &DisplayNameManageResult{
			Status: "ok",
			Name:   "gateway-us-east",
		},
	})

	body := strings.NewReader(`{"action":"get"}`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var result DisplayNameManageResult
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", result.Status)
	}
	if result.Name != "gateway-us-east" {
		t.Errorf("expected name 'gateway-us-east', got %q", result.Name)
	}
}

func TestHandleDisplayNameManage_InvalidAction(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{
		err: fmt.Errorf("unknown action foo (expected set or get)"),
	})

	body := strings.NewReader(`{"action":"foo"}`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleDisplayNameManage_MethodNotAllowed(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{})

	req := httptest.NewRequest(http.MethodGet, "/display-name/manage", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleDisplayNameManage_NoProvider(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	// No display name manage provider set

	body := strings.NewReader(`{"action":"get"}`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHandleDisplayNameManage_ManagementKeyRestriction(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{
		result: &DisplayNameManageResult{Status: "ok"},
	})

	// Set up sealed box without private key (public-key-only = can't decrypt = restricted)
	_, pub, _ := crypto.GenerateEphemeralKeypair()
	sb := crypto.NewSealedBox(pub)
	s.SetSealedBox(sb)

	body := strings.NewReader(`{"action":"get"}`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestHandleDisplayNameManage_NoManagementKey(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{
		result: &DisplayNameManageResult{Status: "ok"},
	})
	// No sealed box = no restriction

	body := strings.NewReader(`{"action":"get"}`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
}

func TestHandleDisplayNameManage_DecodeError(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	installMgmtAuth(s)
	s.SetDisplayNameManageProvider(&mockDisplayNameManageProvider{})

	body := strings.NewReader(`{invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/display-name/manage", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// newAuthServer creates a test server with bearer token authentication enabled.
func newAuthServer(t *testing.T, token string) *Server {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash token: %v", err)
	}
	cfg := DefaultServerConfig()
	cfg.TokenHash = string(hash)
	provider := &mockStatsProvider{running: true}
	return NewServer(cfg, provider)
}

func TestAuth_NoTokenConfigured_PassesThrough(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)

	// Request to a protected endpoint should succeed without any token
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuth_ValidBearerToken(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	// Use /agents which is a protected endpoint
	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	// Should not get 401 (may get other status depending on provider, but not 401)
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("valid token should not return 401, got %d", rec.Code)
	}
}

func TestAuth_InvalidToken_Returns401(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != `Bearer realm="muti-metroo"` {
		t.Errorf("expected WWW-Authenticate header, got %q", rec.Header().Get("WWW-Authenticate"))
	}
}

func TestAuth_MissingToken_Returns401(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_QueryParamToken(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/agents?token=test-secret-token", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	// Should not get 401 (may get other status depending on provider, but not 401)
	if rec.Code == http.StatusUnauthorized {
		t.Errorf("valid query param token should not return 401, got %d", rec.Code)
	}
}

func TestAuth_HealthEndpointsExempt(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	exemptPaths := []string{"/health", "/healthz", "/ready", "/", "/logo.png"}
	for _, path := range exemptPaths {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusUnauthorized {
			t.Errorf("path %s should be exempt from auth, got 401", path)
		}
	}
}

func TestAuth_TokenCaching(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	// First request - slow path (bcrypt)
	req1 := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req1.Header.Set("Authorization", "Bearer test-secret-token")
	rec1 := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec1, req1)

	if rec1.Code == http.StatusUnauthorized {
		t.Fatalf("first request should pass auth, got 401")
	}

	// Verify cache is populated
	s.tokenCacheMu.RLock()
	if !s.tokenCacheValid {
		t.Error("token cache should be valid after successful auth")
	}
	s.tokenCacheMu.RUnlock()

	// Second request - fast path (SHA-256 cache)
	req2 := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req2.Header.Set("Authorization", "Bearer test-secret-token")
	rec2 := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec2, req2)

	if rec2.Code == http.StatusUnauthorized {
		t.Errorf("second request (cached) should pass auth, got 401")
	}
}

func TestAuth_WrongTokenAfterCacheHit(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	// First: valid token to populate cache
	req1 := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req1.Header.Set("Authorization", "Bearer test-secret-token")
	rec1 := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec1, req1)

	if rec1.Code == http.StatusUnauthorized {
		t.Fatalf("first request should pass auth, got 401")
	}

	// Second: wrong token should still fail
	req2 := httptest.NewRequest(http.MethodGet, "/agents", nil)
	req2.Header.Set("Authorization", "Bearer wrong-token")
	rec2 := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("wrong token after cache hit should return 401, got %d", rec2.Code)
	}
}

func TestAuth_ProtectedEndpoints(t *testing.T) {
	s := newAuthServer(t, "test-secret-token")

	// These endpoints should be protected (return 401 without token)
	protectedPaths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/agents"},
		{http.MethodGet, "/api/topology"},
		{http.MethodGet, "/api/dashboard"},
		{http.MethodPost, "/routes/advertise"},
		{http.MethodPost, "/sleep"},
		{http.MethodGet, "/sleep/status"},
		{http.MethodPost, "/wake"},
	}

	for _, tc := range protectedPaths {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		s.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("path %s %s should require auth, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		query    string
		expected string
	}{
		{"bearer header", "Bearer my-token", "", "my-token"},
		{"query param", "", "my-token", "my-token"},
		{"header takes precedence", "Bearer header-token", "query-token", "header-token"},
		{"no token", "", "", ""},
		{"invalid header prefix", "Basic abc123", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "/test"
			if tc.query != "" {
				url += "?token=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			got := extractBearerToken(req)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

// mockMeshHostProvider implements MeshHostProvider for testing.
type mockMeshHostProvider struct {
	suffixes     []string
	hosts        []hosts.Host
	result       *MeshHostManageResult
	suffixResult *MeshHostSuffixManageResult
	err          error
	suffixErr    error
}

func (m *mockMeshHostProvider) ManageMeshHost(_, _, _, _, _ string) (*MeshHostManageResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func (m *mockMeshHostProvider) ManageMeshHostSuffix(_, _ string) (*MeshHostSuffixManageResult, error) {
	if m.suffixErr != nil {
		return nil, m.suffixErr
	}
	return m.suffixResult, nil
}

func (m *mockMeshHostProvider) MeshHosts() []hosts.Host     { return m.hosts }
func (m *mockMeshHostProvider) MeshHostsSuffixes() []string { return m.suffixes }

func TestHandleMeshHostManage_IncludesSuffixes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"list", `{"action":"list"}`},
		{"add", `{"action":"add","name":"gw.lan","ipv4":"10.0.0.1"}`},
		{"remove", `{"action":"remove","name":"gw.lan"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			s := NewServer(cfg, &mockStatsProvider{running: true})
			installMgmtAuth(s)
			s.SetMeshHostProvider(&mockMeshHostProvider{
				result: &MeshHostManageResult{
					Suffixes: []string{"lan"},
					Hosts:    []hosts.Host{{Name: "gw.lan", IPv4: "10.0.0.1"}},
				},
			})

			req := httptest.NewRequest(http.MethodPost, "/hosts/manage", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var got MeshHostManageResult
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(got.Suffixes) != 1 || got.Suffixes[0] != "lan" {
				t.Errorf("expected suffixes [lan], got %v", got.Suffixes)
			}
		})
	}
}

func TestHandleMeshHostSuffixManage(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		mockResult     *MeshHostSuffixManageResult
		mockErr        error
		wantStatus     int
		wantSuffixesIn []string
	}{
		{
			name:           "list",
			body:           `{"action":"list"}`,
			mockResult:     &MeshHostSuffixManageResult{Suffixes: []string{"alt", "mesh"}},
			wantStatus:     http.StatusOK,
			wantSuffixesIn: []string{"alt", "mesh"},
		},
		{
			name:           "add",
			body:           `{"action":"add","suffix":"alt"}`,
			mockResult:     &MeshHostSuffixManageResult{Suffixes: []string{"alt", "mesh"}},
			wantStatus:     http.StatusOK,
			wantSuffixesIn: []string{"alt", "mesh"},
		},
		{
			name:       "remove in use returns 409",
			body:       `{"action":"remove","suffix":"mesh"}`,
			mockErr:    fmt.Errorf("%w: mesh used by gw.mesh", hosts.ErrSuffixInUse),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "remove not found returns 404",
			body:       `{"action":"remove","suffix":"nope"}`,
			mockErr:    fmt.Errorf("%w: nope", hosts.ErrNotFound),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "add duplicate returns 409",
			body:       `{"action":"add","suffix":"mesh"}`,
			mockErr:    fmt.Errorf("%w: mesh", hosts.ErrExists),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "invalid suffix returns 400",
			body:       `{"action":"add","suffix":"bad..label"}`,
			mockErr:    fmt.Errorf("%w: bad..label", hosts.ErrInvalid),
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			s := NewServer(cfg, &mockStatsProvider{running: true})
			installMgmtAuth(s)
			s.SetMeshHostProvider(&mockMeshHostProvider{
				suffixResult: tc.mockResult,
				suffixErr:    tc.mockErr,
			})

			req := httptest.NewRequest(http.MethodPost, "/hosts/suffixes/manage", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var got MeshHostSuffixManageResult
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(got.Suffixes) != len(tc.wantSuffixesIn) {
				t.Fatalf("Suffixes = %v, want %v", got.Suffixes, tc.wantSuffixesIn)
			}
			for i, s := range tc.wantSuffixesIn {
				if got.Suffixes[i] != s {
					t.Errorf("Suffixes[%d] = %q, want %q", i, got.Suffixes[i], s)
				}
			}
		})
	}
}

func TestServer_DashboardSnapshotMatchesHTTPBody(t *testing.T) {
	cfg := DefaultServerConfig() // EnableDashboard defaults true
	provider := &mockStatsProvider{running: true, stats: Stats{SOCKS5Running: true}}
	s := NewServer(cfg, provider)

	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	remoteProvider := &mockRemoteStatusProvider{
		id:          localID,
		displayName: "local-agent",
		peerIDs:     []identity.AgentID{peerID},
		peerDetails: []PeerDetails{
			{ID: peerID, DisplayName: "peer-agent", State: "connected", RTT: 50 * time.Millisecond, Transport: "quic"},
		},
		routeDetails: []RouteDetails{
			{Network: "10.0.0.0/8", NextHop: peerID, Origin: peerID, Metric: 1},
		},
	}
	s.SetRemoteProvider(remoteProvider)

	// Body served by GET /api/dashboard.
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Bytes pushed over the SOCKS5 route-watch path.
	snap, err := s.BuildDashboardSnapshotJSON()
	if err != nil {
		t.Fatalf("BuildDashboardSnapshotJSON: %v", err)
	}

	// writeJSON appends a trailing newline via json.Encoder; trim it before
	// comparing. The remainder must be byte-identical so the two paths cannot
	// drift.
	httpBody := strings.TrimRight(rec.Body.String(), "\n")
	if httpBody != string(snap) {
		t.Fatalf("snapshot differs from /api/dashboard body\nhttp: %s\nsnap: %s", httpBody, snap)
	}
}
