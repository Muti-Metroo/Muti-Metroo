package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// loopbackInterfaceName returns the host's loopback interface name (lo, lo0),
// which is always present and enumerable, for tests that exercise route
// interface binding. It returns "" if none is found.
func loopbackInterfaceName() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			return ifc.Name
		}
	}
	return ""
}

func TestDefault(t *testing.T) {
	cfg := Default()

	// Check essential defaults
	if cfg.Agent.ID != "auto" {
		t.Errorf("Agent.ID = %s, want auto", cfg.Agent.ID)
	}
	if cfg.Agent.DataDir != "./data" {
		t.Errorf("Agent.DataDir = %s, want ./data", cfg.Agent.DataDir)
	}
	if cfg.Agent.LogLevel != "info" {
		t.Errorf("Agent.LogLevel = %s, want info", cfg.Agent.LogLevel)
	}
	if cfg.SOCKS5.Address != "127.0.0.1:1080" {
		t.Errorf("SOCKS5.Address = %s, want 127.0.0.1:1080", cfg.SOCKS5.Address)
	}
	if cfg.Routing.MaxHops != 16 {
		t.Errorf("Routing.MaxHops = %d, want 16", cfg.Routing.MaxHops)
	}
	if cfg.Limits.BufferSize != 262144 {
		t.Errorf("Limits.BufferSize = %d, want 262144", cfg.Limits.BufferSize)
	}
}

func TestParse_ValidConfig(t *testing.T) {
	yamlConfig := `
agent:
  id: "auto"
  data_dir: "./data"
  log_level: "debug"
  log_format: "json"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"

peers:
  - id: "abc123def456789012345678901234ab"
    transport: quic
    address: "192.168.1.50:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
  max_connections: 500

exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"
  dns:
    servers:
      - "8.8.8.8:53"
    timeout: 10s

routing:
  advertise_interval: 3m
  route_ttl: 6m
  max_hops: 20

limits:
  max_streams_per_peer: 500
  max_streams_total: 5000
  buffer_size: 131072
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Verify parsed values
	if cfg.Agent.LogLevel != "debug" {
		t.Errorf("Agent.LogLevel = %s, want debug", cfg.Agent.LogLevel)
	}
	if cfg.Agent.LogFormat != "json" {
		t.Errorf("Agent.LogFormat = %s, want json", cfg.Agent.LogFormat)
	}
	if len(cfg.Listeners) != 1 {
		t.Errorf("len(Listeners) = %d, want 1", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Transport != "quic" {
		t.Errorf("Listeners[0].Transport = %s, want quic", cfg.Listeners[0].Transport)
	}
	if len(cfg.Peers) != 1 {
		t.Errorf("len(Peers) = %d, want 1", len(cfg.Peers))
	}
	if cfg.SOCKS5.Enabled != true {
		t.Error("SOCKS5.Enabled = false, want true")
	}
	if cfg.SOCKS5.MaxConnections != 500 {
		t.Errorf("SOCKS5.MaxConnections = %d, want 500", cfg.SOCKS5.MaxConnections)
	}
	if len(cfg.Exit.Routes) != 2 {
		t.Errorf("len(Exit.Routes) = %d, want 2", len(cfg.Exit.Routes))
	}
	if cfg.Routing.MaxHops != 20 {
		t.Errorf("Routing.MaxHops = %d, want 20", cfg.Routing.MaxHops)
	}
	if cfg.Routing.AdvertiseInterval != 3*time.Minute {
		t.Errorf("Routing.AdvertiseInterval = %v, want 3m", cfg.Routing.AdvertiseInterval)
	}
	if cfg.Limits.MaxStreamsPerPeer != 500 {
		t.Errorf("Limits.MaxStreamsPerPeer = %d, want 500", cfg.Limits.MaxStreamsPerPeer)
	}
}

func TestParse_MinimalConfig(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should use defaults for unspecified fields
	if cfg.Agent.LogLevel != "info" {
		t.Errorf("Agent.LogLevel = %s, want info (default)", cfg.Agent.LogLevel)
	}
	if cfg.Routing.MaxHops != 16 {
		t.Errorf("Routing.MaxHops = %d, want 16 (default)", cfg.Routing.MaxHops)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
  invalid yaml here [
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail for invalid YAML")
	}
}

func TestParse_ValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantError string
	}{
		{
			name: "invalid log level",
			yaml: `
agent:
  data_dir: "./data"
  log_level: "invalid"
`,
			wantError: "invalid log_level",
		},
		{
			name: "invalid log format",
			yaml: `
agent:
  data_dir: "./data"
  log_format: "invalid"
`,
			wantError: "invalid log_format",
		},
		{
			name: "listener missing address",
			yaml: `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    tls:
      cert: "cert.pem"
      key: "key.pem"
`,
			wantError: "address is required",
		},
		{
			name: "listener invalid transport",
			yaml: `
agent:
  data_dir: "./data"
listeners:
  - transport: invalid
    address: "0.0.0.0:4433"
    tls:
      cert: "cert.pem"
      key: "key.pem"
`,
			wantError: "invalid transport",
		},
		{
			name: "listener partial TLS config",
			yaml: `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert: "cert.pem"
`,
			wantError: "tls certificate and key must both be specified",
		},
		{
			name: "h2 listener missing path",
			yaml: `
agent:
  data_dir: "./data"
listeners:
  - transport: h2
    address: "0.0.0.0:8443"
    tls:
      cert: "cert.pem"
      key: "key.pem"
`,
			wantError: "path is required",
		},
		{
			name: "peer missing id",
			yaml: `
agent:
  data_dir: "./data"
peers:
  - transport: quic
    address: "192.168.1.50:4433"
`,
			wantError: "id is required",
		},
		{
			name: "invalid CIDR route",
			yaml: `
agent:
  data_dir: "./data"
exit:
  enabled: true
  routes:
    - "invalid"
`,
			wantError: "invalid CIDR",
		},
		{
			name: "negative startup_delay",
			yaml: `
agent:
  data_dir: "./data"
  startup_delay: -5s
`,
			wantError: "startup_delay must not be negative",
		},
		{
			name: "max_hops too low",
			yaml: `
agent:
  data_dir: "./data"
routing:
  max_hops: 0
`,
			wantError: "max_hops must be between 1 and 255",
		},
		{
			name: "max_hops too high",
			yaml: `
agent:
  data_dir: "./data"
routing:
  max_hops: 256
`,
			wantError: "max_hops must be between 1 and 255",
		},
		{
			name: "max_streams_total not positive",
			yaml: `
agent:
  data_dir: "./data"
limits:
  max_streams_total: 0
`,
			wantError: "max_streams_total must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Error("Parse() should fail")
				return
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("Error = %v, want to contain %q", err, tt.wantError)
			}
		})
	}
}

func TestParse_EnvVarSubstitution(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_DATA_DIR", "/custom/data")
	os.Setenv("TEST_PEER_ID", "abc123def456789012345678901234ab")
	os.Setenv("TEST_PEER_ADDR", "10.0.0.1:4433")
	defer func() {
		os.Unsetenv("TEST_DATA_DIR")
		os.Unsetenv("TEST_PEER_ID")
		os.Unsetenv("TEST_PEER_ADDR")
	}()

	yamlConfig := `
agent:
  data_dir: "${TEST_DATA_DIR}"

peers:
  - id: "${TEST_PEER_ID}"
    transport: quic
    address: "$TEST_PEER_ADDR"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Agent.DataDir != "/custom/data" {
		t.Errorf("Agent.DataDir = %s, want /custom/data", cfg.Agent.DataDir)
	}
	if cfg.Peers[0].ID != "abc123def456789012345678901234ab" {
		t.Errorf("Peers[0].ID = %s, want abc123def456789012345678901234ab", cfg.Peers[0].ID)
	}
	if cfg.Peers[0].Address != "10.0.0.1:4433" {
		t.Errorf("Peers[0].Address = %s, want 10.0.0.1:4433", cfg.Peers[0].Address)
	}
}

func TestParse_EnvVarDefaultValue(t *testing.T) {
	// Ensure the variable is NOT set
	os.Unsetenv("NONEXISTENT_VAR")

	yamlConfig := `
agent:
  data_dir: "${NONEXISTENT_VAR:-/default/path}"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Agent.DataDir != "/default/path" {
		t.Errorf("Agent.DataDir = %s, want /default/path", cfg.Agent.DataDir)
	}
}

func TestParse_EnvVarNotFound(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR")

	yamlConfig := `
agent:
  data_dir: "${NONEXISTENT_VAR}"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should keep the original placeholder if not found
	if cfg.Agent.DataDir != "${NONEXISTENT_VAR}" {
		t.Errorf("Agent.DataDir = %s, want ${NONEXISTENT_VAR}", cfg.Agent.DataDir)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Load() should fail for nonexistent file")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	// Create temp config file
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
agent:
  data_dir: "./data"
  log_level: "debug"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Agent.LogLevel != "debug" {
		t.Errorf("Agent.LogLevel = %s, want debug", cfg.Agent.LogLevel)
	}
}

func TestConfig_Validate_MissingDataDir(t *testing.T) {
	cfg := Default()
	cfg.Agent.DataDir = ""

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail with empty data_dir")
	}
}

func TestConfig_Validate_SOCKS5EnabledNoAddress(t *testing.T) {
	cfg := Default()
	cfg.SOCKS5.Enabled = true
	cfg.SOCKS5.Address = ""

	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail when SOCKS5 enabled without address")
	}
}

func TestConfig_TCPKeepAlive(t *testing.T) {
	cfg := Default()

	if cfg.Connections.TCPKeepAlive != 30*time.Second {
		t.Errorf("default Connections.TCPKeepAlive = %v, want 30s", cfg.Connections.TCPKeepAlive)
	}

	// nil peer and nil override both inherit the global default.
	if got := cfg.GetEffectiveTCPKeepAlive(nil); got != 30*time.Second {
		t.Errorf("GetEffectiveTCPKeepAlive(nil) = %v, want 30s", got)
	}
	if got := cfg.GetEffectiveTCPKeepAlive(&PeerConfig{}); got != 30*time.Second {
		t.Errorf("GetEffectiveTCPKeepAlive(no override) = %v, want 30s", got)
	}

	// A per-peer override wins over the global default.
	override := 5 * time.Second
	if got := cfg.GetEffectiveTCPKeepAlive(&PeerConfig{TCPKeepAlive: &override}); got != 5*time.Second {
		t.Errorf("GetEffectiveTCPKeepAlive(5s) = %v, want 5s", got)
	}

	// A per-peer 0 disables keepalive (distinct from "inherit").
	zero := time.Duration(0)
	if got := cfg.GetEffectiveTCPKeepAlive(&PeerConfig{TCPKeepAlive: &zero}); got != 0 {
		t.Errorf("GetEffectiveTCPKeepAlive(0) = %v, want 0", got)
	}
}

func TestConfig_Validate_TCPKeepAliveNegative(t *testing.T) {
	// Negative global value is rejected.
	cfg := Default()
	cfg.Connections.TCPKeepAlive = -1 * time.Second
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail when connections.tcp_keepalive is negative")
	}

	// Negative per-peer value is rejected.
	cfg = Default()
	neg := -1 * time.Second
	cfg.Peers = []PeerConfig{{
		ID:           "abc123def456789012345678901234ab",
		Transport:    "h2",
		Address:      "peer.example:3000",
		TCPKeepAlive: &neg,
	}}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should fail when a peer tcp_keepalive is negative")
	}
}

func TestIsValidCIDR(t *testing.T) {
	tests := []struct {
		cidr  string
		valid bool
	}{
		{"10.0.0.0/8", true},
		{"192.168.0.0/16", true},
		{"172.16.0.0/12", true},
		{"0.0.0.0/0", true},
		{"10.5.3.0/24", true},
		{"2001:db8::/32", true},
		{"invalid", false},
		{"10.0.0.0", false},
		{"/8", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			if got := isValidCIDR(tt.cidr); got != tt.valid {
				t.Errorf("isValidCIDR(%q) = %v, want %v", tt.cidr, got, tt.valid)
			}
		})
	}
}

func TestConfig_String(t *testing.T) {
	cfg := Default()
	s := cfg.String()

	// Should contain key fields
	if !strings.Contains(s, "agent") {
		t.Error("String() should contain 'agent'")
	}
	if !strings.Contains(s, "socks5") {
		t.Error("String() should contain 'socks5'")
	}
}

func TestDurationParsing(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
routing:
  advertise_interval: 120s
  route_ttl: 5m
connections:
  idle_threshold: 30s
  timeout: 1m30s
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Routing.AdvertiseInterval != 120*time.Second {
		t.Errorf("AdvertiseInterval = %v, want 2m", cfg.Routing.AdvertiseInterval)
	}
	if cfg.Routing.RouteTTL != 5*time.Minute {
		t.Errorf("RouteTTL = %v, want 5m", cfg.Routing.RouteTTL)
	}
	if cfg.Connections.Timeout != 90*time.Second {
		t.Errorf("Timeout = %v, want 1m30s", cfg.Connections.Timeout)
	}
}

func TestStartupDelayParsing(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
  startup_delay: 90s
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Agent.StartupDelay != 90*time.Second {
		t.Errorf("Agent.StartupDelay = %v, want 90s", cfg.Agent.StartupDelay)
	}
}

func TestStartupDelayDefault(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Agent.StartupDelay != 0 {
		t.Errorf("Agent.StartupDelay = %v, want 0 (no delay)", cfg.Agent.StartupDelay)
	}
}

func TestListenerConfig_WebSocket(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"
    tls:
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(cfg.Listeners) != 1 {
		t.Fatalf("len(Listeners) = %d, want 1", len(cfg.Listeners))
	}
	if cfg.Listeners[0].Transport != "ws" {
		t.Errorf("Transport = %s, want ws", cfg.Listeners[0].Transport)
	}
	if cfg.Listeners[0].Path != "/mesh" {
		t.Errorf("Path = %s, want /mesh", cfg.Listeners[0].Path)
	}
}

func TestListenerConfig_Advertise(t *testing.T) {
	t.Run("absent advertise block parses cleanly", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: h2
    address: "0.0.0.0:3000"
    path: "/mesh"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if cfg.Listeners[0].Advertise != nil {
			t.Errorf("Advertise = %+v, want nil", cfg.Listeners[0].Advertise)
		}
	})

	t.Run("valid hostname host:port", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: h2
    address: "0.0.0.0:3000"
    path: "/mesh"
    advertise:
      address: "kreata.ee:3000"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		adv := cfg.Listeners[0].Advertise
		if adv == nil {
			t.Fatal("Advertise = nil, want non-nil")
		}
		if adv.Address != "kreata.ee:3000" {
			t.Errorf("Advertise.Address = %s, want kreata.ee:3000", adv.Address)
		}
	})

	t.Run("valid IPv4 host:port", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    advertise:
      address: "1.2.3.4:443"
`
		if _, err := Parse([]byte(yamlConfig)); err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
	})

	t.Run("valid IPv6 host:port", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "[::]:4433"
    advertise:
      address: "[2001:db8::1]:8080"
`
		if _, err := Parse([]byte(yamlConfig)); err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
	})

	t.Run("explicit transport and path overrides", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ws
    address: "127.0.0.1:3001"
    path: "/internal"
    plaintext: true
    advertise:
      address: "www.tahvel.info:443"
      transport: ws
      path: "/ws-test/"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		adv := cfg.Listeners[0].Advertise
		if adv == nil {
			t.Fatal("Advertise = nil, want non-nil")
		}
		if adv.Transport != "ws" {
			t.Errorf("Advertise.Transport = %s, want ws", adv.Transport)
		}
		if adv.Path != "/ws-test/" {
			t.Errorf("Advertise.Path = %s, want /ws-test/", adv.Path)
		}
	})

	rejectCases := []struct {
		name    string
		address string
		wantSub string
	}{
		{"unspecified IPv4", "0.0.0.0:443", "unspecified"},
		{"loopback IPv4", "127.0.0.1:8080", "loopback"},
		{"unspecified IPv6", "[::]:8080", "unspecified"},
		{"loopback IPv6", "[::1]:8080", "loopback"},
		{"loopback IPv4 inside 127/8", "127.0.0.5:443", "loopback"},
	}
	for _, tc := range rejectCases {
		t.Run("reject "+tc.name, func(t *testing.T) {
			yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    advertise:
      address: "` + tc.address + `"
`
			_, err := Parse([]byte(yamlConfig))
			if err == nil {
				t.Fatalf("Parse() succeeded; want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Parse() error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}

	t.Run("reject empty advertise.address", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    advertise: {}
`
		_, err := Parse([]byte(yamlConfig))
		if err == nil || !strings.Contains(err.Error(), "advertise.address is required") {
			t.Fatalf("Parse() error = %v, want missing-address error", err)
		}
	})

	t.Run("reject malformed host:port", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    advertise:
      address: "kreata.ee"
`
		_, err := Parse([]byte(yamlConfig))
		if err == nil || !strings.Contains(err.Error(), "advertise.address") {
			t.Fatalf("Parse() error = %v, want advertise.address parse error", err)
		}
	})

	t.Run("reject invalid advertise.transport", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    advertise:
      address: "kreata.ee:4433"
      transport: "foobar"
`
		_, err := Parse([]byte(yamlConfig))
		if err == nil || !strings.Contains(err.Error(), "invalid advertise.transport") {
			t.Fatalf("Parse() error = %v, want invalid-transport error", err)
		}
	})
}

func TestPeerConfig_WithProxy(t *testing.T) {
	os.Setenv("PROXY_USER", "testuser")
	os.Setenv("PROXY_PASS", "testpass")
	defer func() {
		os.Unsetenv("PROXY_USER")
		os.Unsetenv("PROXY_PASS")
	}()

	yamlConfig := `
agent:
  data_dir: "./data"
peers:
  - id: "abc123def456789012345678901234ab"
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    proxy: "http://proxy.corp.local:8080"
    proxy_auth:
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	peer := cfg.Peers[0]
	if peer.Proxy != "http://proxy.corp.local:8080" {
		t.Errorf("Proxy = %s, want http://proxy.corp.local:8080", peer.Proxy)
	}
	if peer.ProxyAuth.Username != "testuser" {
		t.Errorf("ProxyAuth.Username = %s, want testuser", peer.ProxyAuth.Username)
	}
	if peer.ProxyAuth.Password != "testpass" {
		t.Errorf("ProxyAuth.Password = %s, want testpass", peer.ProxyAuth.Password)
	}
}

func TestSOCKS5AuthConfig(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: true
    users:
      - username: "user1"
        password: "pass1"
      - username: "user2"
        password: "pass2"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !cfg.SOCKS5.Auth.Enabled {
		t.Error("SOCKS5.Auth.Enabled = false, want true")
	}
	if len(cfg.SOCKS5.Auth.Users) != 2 {
		t.Fatalf("len(SOCKS5.Auth.Users) = %d, want 2", len(cfg.SOCKS5.Auth.Users))
	}
	if cfg.SOCKS5.Auth.Users[0].Username != "user1" {
		t.Errorf("Users[0].Username = %s, want user1", cfg.SOCKS5.Auth.Users[0].Username)
	}
}

func TestTLSConfig_InlinePEM(t *testing.T) {
	// Create a temp directory with cert files for file path testing
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	certContent := "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n"
	keyContent := "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----\n"

	os.WriteFile(certFile, []byte(certContent), 0644)
	os.WriteFile(keyFile, []byte(keyContent), 0600)

	tests := []struct {
		name     string
		tls      TLSConfig
		wantCert string
		wantKey  string
	}{
		{
			name: "inline PEM takes precedence",
			tls: TLSConfig{
				Cert:    certFile,
				Key:     keyFile,
				CertPEM: "inline-cert-pem",
				KeyPEM:  "inline-key-pem",
			},
			wantCert: "inline-cert-pem",
			wantKey:  "inline-key-pem",
		},
		{
			name: "file path fallback",
			tls: TLSConfig{
				Cert: certFile,
				Key:  keyFile,
			},
			wantCert: certContent,
			wantKey:  keyContent,
		},
		{
			name: "inline PEM only",
			tls: TLSConfig{
				CertPEM: "cert-only-inline",
				KeyPEM:  "key-only-inline",
			},
			wantCert: "cert-only-inline",
			wantKey:  "key-only-inline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certPEM, err := tt.tls.GetCertPEM()
			if err != nil {
				t.Fatalf("GetCertPEM() error = %v", err)
			}
			if string(certPEM) != tt.wantCert {
				t.Errorf("GetCertPEM() = %q, want %q", string(certPEM), tt.wantCert)
			}

			keyPEM, err := tt.tls.GetKeyPEM()
			if err != nil {
				t.Fatalf("GetKeyPEM() error = %v", err)
			}
			if string(keyPEM) != tt.wantKey {
				t.Errorf("GetKeyPEM() = %q, want %q", string(keyPEM), tt.wantKey)
			}
		})
	}
}

func TestTLSConfig_HasCertAndKey(t *testing.T) {
	tests := []struct {
		name    string
		tls     TLSConfig
		hasCert bool
		hasKey  bool
	}{
		{
			name:    "empty",
			tls:     TLSConfig{},
			hasCert: false,
			hasKey:  false,
		},
		{
			name:    "file paths only",
			tls:     TLSConfig{Cert: "cert.pem", Key: "key.pem"},
			hasCert: true,
			hasKey:  true,
		},
		{
			name:    "inline PEM only",
			tls:     TLSConfig{CertPEM: "cert", KeyPEM: "key"},
			hasCert: true,
			hasKey:  true,
		},
		{
			name:    "mixed",
			tls:     TLSConfig{Cert: "cert.pem", KeyPEM: "key"},
			hasCert: true,
			hasKey:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tls.HasCert(); got != tt.hasCert {
				t.Errorf("HasCert() = %v, want %v", got, tt.hasCert)
			}
			if got := tt.tls.HasKey(); got != tt.hasKey {
				t.Errorf("HasKey() = %v, want %v", got, tt.hasKey)
			}
		})
	}
}

func TestParse_InlinePEM(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      cert_pem: |
        -----BEGIN CERTIFICATE-----
        MIIBtest
        -----END CERTIFICATE-----
      key_pem: |
        -----BEGIN PRIVATE KEY-----
        MIIEtest
        -----END PRIVATE KEY-----
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(cfg.Listeners) != 1 {
		t.Fatalf("len(Listeners) = %d, want 1", len(cfg.Listeners))
	}

	tls := cfg.Listeners[0].TLS
	if !tls.HasCert() {
		t.Error("HasCert() = false, want true")
	}
	if !tls.HasKey() {
		t.Error("HasKey() = false, want true")
	}
	if !strings.Contains(tls.CertPEM, "BEGIN CERTIFICATE") {
		t.Errorf("CertPEM should contain BEGIN CERTIFICATE, got %q", tls.CertPEM)
	}
	if !strings.Contains(tls.KeyPEM, "BEGIN PRIVATE KEY") {
		t.Errorf("KeyPEM should contain BEGIN PRIVATE KEY, got %q", tls.KeyPEM)
	}
}

func TestListenerConfig_PlainTextWebSocket(t *testing.T) {
	// Test valid plaintext WebSocket configuration (for reverse proxy mode)
	yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ws
    address: "127.0.0.1:8080"
    path: "/mesh"
    plaintext: true
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(cfg.Listeners) != 1 {
		t.Fatalf("len(Listeners) = %d, want 1", len(cfg.Listeners))
	}
	listener := cfg.Listeners[0]
	if listener.Transport != "ws" {
		t.Errorf("Transport = %s, want ws", listener.Transport)
	}
	if listener.Address != "127.0.0.1:8080" {
		t.Errorf("Address = %s, want 127.0.0.1:8080", listener.Address)
	}
	if listener.Path != "/mesh" {
		t.Errorf("Path = %s, want /mesh", listener.Path)
	}
	if !listener.PlainText {
		t.Error("PlainText = false, want true")
	}
}

func TestListenerConfig_PlainTextOnlyForWS(t *testing.T) {
	// Test that plaintext is only allowed for ws transport
	tests := []struct {
		name      string
		transport string
		wantError bool
	}{
		{"ws plaintext allowed", "ws", false},
		{"quic plaintext not allowed", "quic", true},
		{"h2 plaintext not allowed", "h2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ` + tt.transport + `
    address: "127.0.0.1:8080"
    path: "/mesh"
    plaintext: true
`

			_, err := Parse([]byte(yamlConfig))
			if tt.wantError {
				if err == nil {
					t.Errorf("Parse() should fail for %s with plaintext", tt.transport)
				} else if !strings.Contains(err.Error(), "plaintext mode is only supported for ws") {
					t.Errorf("Error = %v, want to contain 'plaintext mode is only supported for ws'", err)
				}
			} else {
				if err != nil {
					t.Errorf("Parse() error = %v, want nil", err)
				}
			}
		})
	}
}

func TestListenerConfig_PlainTextNoTLSRequired(t *testing.T) {
	// Test that plaintext WS does not require TLS config
	yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ws
    address: "127.0.0.1:8080"
    path: "/mesh"
    plaintext: true
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v, plaintext ws should not require TLS", err)
	}

	// Verify TLS is not set
	if cfg.Listeners[0].TLS.HasCert() || cfg.Listeners[0].TLS.HasKey() {
		t.Error("Plaintext WS should work without TLS config")
	}
}

func TestListenerConfig_NonPlainTextAutoGeneratesTLS(t *testing.T) {
	// Test that non-plaintext WS auto-generates TLS cert/key
	yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ws
    address: "127.0.0.1:8080"
    path: "/mesh"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Errorf("Parse() should succeed for ws without explicit TLS (auto-generated), got: %v", err)
		return
	}
	// Verify TLS is not explicitly set (will be auto-generated at runtime)
	if cfg.Listeners[0].TLS.HasCert() {
		t.Error("TLS.HasCert() should be false - cert not specified, will be auto-generated")
	}
}

func TestListenerConfig_PartialTLSConfig(t *testing.T) {
	// Test that partial TLS config (cert without key) fails
	yamlConfig := `
agent:
  data_dir: "./data"
listeners:
  - transport: ws
    address: "127.0.0.1:8080"
    path: "/mesh"
    tls:
      cert: "cert.pem"
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail for partial TLS config (cert without key)")
	}
	if !strings.Contains(err.Error(), "tls certificate and key must both be specified") {
		t.Errorf("Error = %v, want to contain 'tls certificate and key must both be specified'", err)
	}
}

func TestManagementConfig_ValidKeys(t *testing.T) {
	// Valid 64-character hex strings (32 bytes)
	validPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	validPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + validPublicKey + `"
  private_key: "` + validPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Test HasManagementKey
	if !cfg.HasManagementKey() {
		t.Error("HasManagementKey() = false, want true")
	}

	// Test CanDecryptManagement
	if !cfg.CanDecryptManagement() {
		t.Error("CanDecryptManagement() = false, want true")
	}

	// Test GetManagementPublicKey
	pubKey, err := cfg.GetManagementPublicKey()
	if err != nil {
		t.Fatalf("GetManagementPublicKey() failed: %v", err)
	}
	if pubKey[0] != 0xa1 || pubKey[1] != 0xb2 {
		t.Errorf("GetManagementPublicKey() first bytes = %x %x, want a1 b2", pubKey[0], pubKey[1])
	}

	// Test GetManagementPrivateKey
	privKey, err := cfg.GetManagementPrivateKey()
	if err != nil {
		t.Fatalf("GetManagementPrivateKey() failed: %v", err)
	}
	if privKey[0] != 0x12 || privKey[1] != 0x34 {
		t.Errorf("GetManagementPrivateKey() first bytes = %x %x, want 12 34", privKey[0], privKey[1])
	}
}

func TestManagementConfig_PublicKeyOnly(t *testing.T) {
	validPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + validPublicKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	if !cfg.HasManagementKey() {
		t.Error("HasManagementKey() = false, want true")
	}

	if cfg.CanDecryptManagement() {
		t.Error("CanDecryptManagement() = true, want false (no private key)")
	}

	_, err = cfg.GetManagementPrivateKey()
	if err == nil {
		t.Error("GetManagementPrivateKey() should fail without private key")
	}
}

func TestManagementConfig_NoKeys(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	if cfg.HasManagementKey() {
		t.Error("HasManagementKey() = true, want false")
	}

	if cfg.CanDecryptManagement() {
		t.Error("CanDecryptManagement() = true, want false")
	}
}

func TestManagementConfig_InvalidPublicKey(t *testing.T) {
	tests := []struct {
		name      string
		publicKey string
		wantErr   string
	}{
		{
			name:      "too_short",
			publicKey: "a1b2c3d4",
			wantErr:   "must be 32 bytes",
		},
		{
			name:      "invalid_hex",
			publicKey: "not_valid_hex_string_here_needs_to_be_64_chars_long_for_testing!",
			wantErr:   "invalid management public key hex",
		},
		{
			name:      "odd_length",
			publicKey: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b",
			wantErr:   "invalid management public key hex",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + tc.publicKey + `"
`
			_, err := Parse([]byte(yamlConfig))
			if err == nil {
				t.Error("Parse() should fail for invalid public key")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Error = %v, want to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestManagementConfig_PrivateKeyWithoutPublicKey(t *testing.T) {
	validPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  private_key: "` + validPrivateKey + `"
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail when private key is set without public key")
	}
	if !strings.Contains(err.Error(), "requires management.public_key") {
		t.Errorf("Error = %v, want to contain 'requires management.public_key'", err)
	}
}

func TestManagementConfig_Redacted(t *testing.T) {
	validPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	validPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + validPublicKey + `"
  private_key: "` + validPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Get redacted config
	redacted := cfg.Redacted()

	// Public key should NOT be redacted
	if redacted.Management.PublicKey != validPublicKey {
		t.Errorf("Redacted public key = %s, want %s", redacted.Management.PublicKey, validPublicKey)
	}

	// Private key SHOULD be redacted
	if redacted.Management.PrivateKey != "[REDACTED]" {
		t.Errorf("Redacted private key = %s, want [REDACTED]", redacted.Management.PrivateKey)
	}

	// Original should still have the real private key
	if cfg.Management.PrivateKey != validPrivateKey {
		t.Errorf("Original private key was modified")
	}
}

func TestManagementConfig_HasSensitiveData(t *testing.T) {
	validPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	validPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	t.Run("with_private_key", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + validPublicKey + `"
  private_key: "` + validPrivateKey + `"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() failed: %v", err)
		}

		if !cfg.HasSensitiveData() {
			t.Error("HasSensitiveData() = false, want true (has private key)")
		}
	})

	t.Run("public_key_only", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + validPublicKey + `"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() failed: %v", err)
		}

		// Public key alone is not sensitive
		if cfg.HasSensitiveData() {
			t.Error("HasSensitiveData() = true, want false (only public key)")
		}
	})
}

// Tests for agent identity keypair configuration

func TestAgentConfig_HasIdentityKeypair(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
		want       bool
	}{
		{"empty", "", false},
		{"with_private_key", "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AgentConfig{PrivateKey: tt.privateKey}
			if got := cfg.HasIdentityKeypair(); got != tt.want {
				t.Errorf("HasIdentityKeypair() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentityKeypair_ValidPrivateKey(t *testing.T) {
	// Valid 64-character hex string (32 bytes)
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  private_key: "` + validPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Should have identity keypair
	if !cfg.Agent.HasIdentityKeypair() {
		t.Error("HasIdentityKeypair() = false, want true")
	}

	// Should parse private key successfully
	privKey, err := cfg.Agent.GetIdentityPrivateKey()
	if err != nil {
		t.Fatalf("GetIdentityPrivateKey() failed: %v", err)
	}
	if privKey[0] != 0x48 || privKey[1] != 0xbb {
		t.Errorf("GetIdentityPrivateKey() first bytes = %x %x, want 48 bb", privKey[0], privKey[1])
	}
}

func TestIdentityKeypair_WithMatchingPublicKey(t *testing.T) {
	// These are a valid keypair - public key derived from private key
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
	// Derived public key (computed via curve25519.ScalarBaseMult)
	validPublicKey := "de47b21c5f00c58e1f6c11deaaa6b65bf3f38f43cb8f8a8c28987614c2c14a74"

	yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  private_key: "` + validPrivateKey + `"
  public_key: "` + validPublicKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Should parse both keys
	pubKey, hasPublic, err := cfg.Agent.GetIdentityPublicKey()
	if err != nil {
		t.Fatalf("GetIdentityPublicKey() failed: %v", err)
	}
	if !hasPublic {
		t.Error("GetIdentityPublicKey() hasPublic = false, want true")
	}
	if pubKey[0] != 0xde || pubKey[1] != 0x47 {
		t.Errorf("GetIdentityPublicKey() first bytes = %x %x, want de 47", pubKey[0], pubKey[1])
	}
}

func TestIdentityKeypair_DataDirOptionalWithPrivateKey(t *testing.T) {
	// When private_key is set, data_dir should be optional (validation passes)
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	// Explicitly set data_dir to empty to test that validation passes
	yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  data_dir: ""
  private_key: "` + validPrivateKey + `"
`

	_, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() should succeed with private_key and empty data_dir: %v", err)
	}
}

func TestIdentityKeypair_DataDirRequiredWithAutoID(t *testing.T) {
	// When id is "auto" and no private_key, data_dir is required
	// Explicitly set data_dir to empty to override the default
	yamlConfig := `
agent:
  id: "auto"
  data_dir: ""
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail when id=auto and empty data_dir")
	}
	if !strings.Contains(err.Error(), "data_dir is required") {
		t.Errorf("Error = %v, want to contain 'data_dir is required'", err)
	}
}

func TestIdentityKeypair_DataDirRequiredWithoutPrivateKey(t *testing.T) {
	// When no private_key, data_dir is required
	// Explicitly set data_dir to empty to override the default
	yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  data_dir: ""
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail when no private_key and empty data_dir")
	}
	if !strings.Contains(err.Error(), "data_dir is required") {
		t.Errorf("Error = %v, want to contain 'data_dir is required'", err)
	}
}

func TestIdentityKeypair_PublicKeyWithoutPrivateKey(t *testing.T) {
	// public_key without private_key should fail
	validPublicKey := "de47b21c5f00c58e1f6c11deaaa6b65bf3f38f43cb8f8a8c28987614c2c14a74"

	yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  data_dir: "./data"
  public_key: "` + validPublicKey + `"
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail when public_key is set without private_key")
	}
	if !strings.Contains(err.Error(), "requires agent.private_key") {
		t.Errorf("Error = %v, want to contain 'requires agent.private_key'", err)
	}
}

func TestIdentityKeypair_InvalidPrivateKeyHex(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
		wantErr    string
	}{
		{
			name:       "too_short",
			privateKey: "a1b2c3d4",
			wantErr:    "must be 32 bytes",
		},
		{
			name:       "invalid_hex",
			privateKey: "not_valid_hex_string_here_needs_to_be_64_chars_long_for_testing!",
			wantErr:    "invalid agent private key hex",
		},
		{
			name:       "odd_length",
			privateKey: "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57",
			wantErr:    "invalid agent private key hex",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  private_key: "` + tc.privateKey + `"
`
			_, err := Parse([]byte(yamlConfig))
			if err == nil {
				t.Error("Parse() should fail for invalid private key")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Error = %v, want to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestIdentityKeypair_InvalidPublicKeyHex(t *testing.T) {
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	tests := []struct {
		name      string
		publicKey string
		wantErr   string
	}{
		{
			name:      "too_short",
			publicKey: "a1b2c3d4",
			wantErr:   "must be 32 bytes",
		},
		{
			name:      "invalid_hex",
			publicKey: "not_valid_hex_string_here_needs_to_be_64_chars_long_for_testing!",
			wantErr:   "invalid agent public key hex",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  private_key: "` + validPrivateKey + `"
  public_key: "` + tc.publicKey + `"
`
			_, err := Parse([]byte(yamlConfig))
			if err == nil {
				t.Error("Parse() should fail for invalid public key")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Error = %v, want to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestIdentityKeypair_Redacted(t *testing.T) {
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  private_key: "` + validPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Get redacted config
	redacted := cfg.Redacted()

	// Private key SHOULD be redacted
	if redacted.Agent.PrivateKey != "[REDACTED]" {
		t.Errorf("Redacted agent private key = %s, want [REDACTED]", redacted.Agent.PrivateKey)
	}

	// Original should still have the real private key
	if cfg.Agent.PrivateKey != validPrivateKey {
		t.Errorf("Original agent private key was modified")
	}
}

func TestIdentityKeypair_HasSensitiveData(t *testing.T) {
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	t.Run("with_agent_private_key", func(t *testing.T) {
		yamlConfig := `
agent:
  id: "ea468d30f0e0b80ea37ba9f6a7902407"
  private_key: "` + validPrivateKey + `"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() failed: %v", err)
		}

		if !cfg.HasSensitiveData() {
			t.Error("HasSensitiveData() = false, want true (has agent private key)")
		}
	})

	t.Run("without_agent_private_key", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() failed: %v", err)
		}

		// No private key alone is not sensitive
		if cfg.HasSensitiveData() {
			t.Error("HasSensitiveData() = true, want false (no private key)")
		}
	})
}

// Tests for Ed25519 signing key configuration

func TestManagementConfig_HasSigningKey(t *testing.T) {
	// Valid 32-byte key (64 hex chars)
	validSigningPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	tests := []struct {
		name             string
		signingPublicKey string
		want             bool
	}{
		{"empty", "", false},
		{"with_signing_public_key", validSigningPublicKey, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Management: ManagementConfig{
					SigningPublicKey: tt.signingPublicKey,
				},
			}
			if got := cfg.HasSigningKey(); got != tt.want {
				t.Errorf("HasSigningKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagementConfig_CanSign(t *testing.T) {
	// Valid 64-byte key (128 hex chars)
	validSigningPrivateKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"

	tests := []struct {
		name              string
		signingPrivateKey string
		want              bool
	}{
		{"empty", "", false},
		{"with_signing_private_key", validSigningPrivateKey, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Management: ManagementConfig{
					SigningPrivateKey: tt.signingPrivateKey,
				},
			}
			if got := cfg.CanSign(); got != tt.want {
				t.Errorf("CanSign() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagementConfig_GetSigningPublicKey_Valid(t *testing.T) {
	// Valid 32-byte key (64 hex chars)
	validSigningPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + validSigningPublicKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Test HasSigningKey
	if !cfg.HasSigningKey() {
		t.Error("HasSigningKey() = false, want true")
	}

	// Test GetSigningPublicKey
	pubKey, err := cfg.GetSigningPublicKey()
	if err != nil {
		t.Fatalf("GetSigningPublicKey() failed: %v", err)
	}
	if pubKey[0] != 0xa1 || pubKey[1] != 0xb2 {
		t.Errorf("GetSigningPublicKey() first bytes = %x %x, want a1 b2", pubKey[0], pubKey[1])
	}
	if pubKey[30] != 0xa1 || pubKey[31] != 0xb2 {
		t.Errorf("GetSigningPublicKey() last bytes = %x %x, want a1 b2", pubKey[30], pubKey[31])
	}
}

func TestManagementConfig_GetSigningPublicKey_Invalid(t *testing.T) {
	tests := []struct {
		name             string
		signingPublicKey string
		wantErr          string
	}{
		{
			name:             "too_short",
			signingPublicKey: "a1b2c3d4",
			wantErr:          "must be 32 bytes",
		},
		{
			name:             "invalid_hex",
			signingPublicKey: "not_valid_hex_string_here_needs_to_be_64_chars_long_for_testing!",
			wantErr:          "invalid signing public key hex",
		},
		{
			name:             "odd_length",
			signingPublicKey: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b",
			wantErr:          "invalid signing public key hex",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + tc.signingPublicKey + `"
`
			_, err := Parse([]byte(yamlConfig))
			if err == nil {
				t.Error("Parse() should fail for invalid signing public key")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Error = %v, want to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestManagementConfig_GetSigningPublicKey_Empty(t *testing.T) {
	cfg := &Config{}

	_, err := cfg.GetSigningPublicKey()
	if err == nil {
		t.Error("GetSigningPublicKey() should fail when no signing public key configured")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("Error = %v, want to contain 'not configured'", err)
	}
}

func TestManagementConfig_GetSigningPrivateKey_Valid(t *testing.T) {
	// Valid 32-byte public key (64 hex chars)
	validSigningPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	// Valid 64-byte private key (128 hex chars)
	validSigningPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" +
		"fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + validSigningPublicKey + `"
  signing_private_key: "` + validSigningPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Test CanSign
	if !cfg.CanSign() {
		t.Error("CanSign() = false, want true")
	}

	// Test GetSigningPrivateKey
	privKey, err := cfg.GetSigningPrivateKey()
	if err != nil {
		t.Fatalf("GetSigningPrivateKey() failed: %v", err)
	}
	if privKey[0] != 0x12 || privKey[1] != 0x34 {
		t.Errorf("GetSigningPrivateKey() first bytes = %x %x, want 12 34", privKey[0], privKey[1])
	}
	if privKey[62] != 0x43 || privKey[63] != 0x21 {
		t.Errorf("GetSigningPrivateKey() last bytes = %x %x, want 43 21", privKey[62], privKey[63])
	}
}

func TestManagementConfig_GetSigningPrivateKey_Invalid(t *testing.T) {
	// Valid public key required when private key is set
	validSigningPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	tests := []struct {
		name              string
		signingPrivateKey string
		wantErr           string
	}{
		{
			name:              "too_short",
			signingPrivateKey: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
			wantErr:           "must be 64 bytes",
		},
		{
			name: "invalid_hex",
			signingPrivateKey: "not_valid_hex_string_here_needs_to_be_128_chars_long_for_testing!" +
				"not_valid_hex_string_here_needs_to_be_128_chars_long_for_testing!",
			wantErr: "invalid signing private key hex",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + validSigningPublicKey + `"
  signing_private_key: "` + tc.signingPrivateKey + `"
`
			_, err := Parse([]byte(yamlConfig))
			if err == nil {
				t.Error("Parse() should fail for invalid signing private key")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Error = %v, want to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestManagementConfig_GetSigningPrivateKey_Empty(t *testing.T) {
	cfg := &Config{}

	_, err := cfg.GetSigningPrivateKey()
	if err == nil {
		t.Error("GetSigningPrivateKey() should fail when no signing private key configured")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("Error = %v, want to contain 'not configured'", err)
	}
}

func TestManagementConfig_SigningPrivateKeyWithoutPublicKey(t *testing.T) {
	// Valid 64-byte private key (128 hex chars)
	validSigningPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" +
		"fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_private_key: "` + validSigningPrivateKey + `"
`

	_, err := Parse([]byte(yamlConfig))
	if err == nil {
		t.Error("Parse() should fail when signing private key is set without public key")
	}
	if !strings.Contains(err.Error(), "requires management.signing_public_key") {
		t.Errorf("Error = %v, want to contain 'requires management.signing_public_key'", err)
	}
}

func TestManagementConfig_Redacted_HidesSigningPrivateKey(t *testing.T) {
	validSigningPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	validSigningPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" +
		"fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + validSigningPublicKey + `"
  signing_private_key: "` + validSigningPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Get redacted config
	redacted := cfg.Redacted()

	// Signing public key should NOT be redacted
	if redacted.Management.SigningPublicKey != validSigningPublicKey {
		t.Errorf("Redacted signing public key = %s, want %s", redacted.Management.SigningPublicKey, validSigningPublicKey)
	}

	// Signing private key SHOULD be redacted
	if redacted.Management.SigningPrivateKey != "[REDACTED]" {
		t.Errorf("Redacted signing private key = %s, want [REDACTED]", redacted.Management.SigningPrivateKey)
	}

	// Original should still have the real signing private key
	if cfg.Management.SigningPrivateKey != validSigningPrivateKey {
		t.Errorf("Original signing private key was modified")
	}
}

func TestManagementConfig_SigningKeys_HasSensitiveData(t *testing.T) {
	validSigningPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	validSigningPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" +
		"fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"

	t.Run("with_signing_private_key", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + validSigningPublicKey + `"
  signing_private_key: "` + validSigningPrivateKey + `"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() failed: %v", err)
		}

		if !cfg.HasSensitiveData() {
			t.Error("HasSensitiveData() = false, want true (has signing private key)")
		}
	})

	t.Run("signing_public_key_only", func(t *testing.T) {
		yamlConfig := `
agent:
  data_dir: "./data"

management:
  signing_public_key: "` + validSigningPublicKey + `"
`
		cfg, err := Parse([]byte(yamlConfig))
		if err != nil {
			t.Fatalf("Parse() failed: %v", err)
		}

		// Public key alone is not sensitive
		if cfg.HasSensitiveData() {
			t.Error("HasSensitiveData() = true, want false (only signing public key)")
		}
	})
}

func TestManagementConfig_BothEncryptionAndSigningKeys(t *testing.T) {
	// Test that both encryption and signing keys can coexist
	validPublicKey := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	validPrivateKey := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	validSigningPublicKey := "b1c2d3e4f5a6b1c2d3e4f5a6b1c2d3e4f5a6b1c2d3e4f5a6b1c2d3e4f5a6b1c2"
	validSigningPrivateKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" +
		"0987654321fedcba0987654321fedcba0987654321fedcba0987654321fedcba"

	yamlConfig := `
agent:
  data_dir: "./data"

management:
  public_key: "` + validPublicKey + `"
  private_key: "` + validPrivateKey + `"
  signing_public_key: "` + validSigningPublicKey + `"
  signing_private_key: "` + validSigningPrivateKey + `"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	// Check encryption keys
	if !cfg.HasManagementKey() {
		t.Error("HasManagementKey() = false, want true")
	}
	if !cfg.CanDecryptManagement() {
		t.Error("CanDecryptManagement() = false, want true")
	}

	// Check signing keys
	if !cfg.HasSigningKey() {
		t.Error("HasSigningKey() = false, want true")
	}
	if !cfg.CanSign() {
		t.Error("CanSign() = false, want true")
	}

	// Verify keys are different
	encPubKey, _ := cfg.GetManagementPublicKey()
	signPubKey, _ := cfg.GetSigningPublicKey()
	if encPubKey == signPubKey {
		t.Error("Encryption and signing public keys should be different")
	}
}

// TestParse_ExitRoutes_MixedForms verifies that exit.routes accepts both the
// legacy bare-string CIDR form and the structured {cidr, metric, comment}
// mapping form, and that they coexist in the same list.
func TestParse_ExitRoutes_MixedForms(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - cidr: "192.168.0.0/16"
      comment: "office LAN"
    - cidr: "0.0.0.0/0"
      metric: 5
      comment: "default route"
`

	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(cfg.Exit.Routes) != 3 {
		t.Fatalf("len(Exit.Routes) = %d, want 3", len(cfg.Exit.Routes))
	}

	// Bare scalar parses as CIDR-only.
	if got := cfg.Exit.Routes[0]; got.CIDR != "10.0.0.0/8" || got.Metric != 0 || got.Comment != "" {
		t.Errorf("Routes[0] = %+v, want {CIDR:10.0.0.0/8 Metric:0 Comment:}", got)
	}

	// Mapping form with comment, no metric.
	if got := cfg.Exit.Routes[1]; got.CIDR != "192.168.0.0/16" || got.Metric != 0 || got.Comment != "office LAN" {
		t.Errorf("Routes[1] = %+v, want {CIDR:192.168.0.0/16 Metric:0 Comment:office LAN}", got)
	}

	// Mapping form with metric and comment.
	if got := cfg.Exit.Routes[2]; got.CIDR != "0.0.0.0/0" || got.Metric != 5 || got.Comment != "default route" {
		t.Errorf("Routes[2] = %+v, want {CIDR:0.0.0.0/0 Metric:5 Comment:default route}", got)
	}
}

// TestParse_ExitRoutes_StructuredFormFailsValidation guards two error paths:
// a missing CIDR field is reported as required, and an invalid CIDR carries
// the CIDR string into the message.
func TestParse_ExitRoutes_StructuredFormFailsValidation(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name: "missing cidr",
			yaml: `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls: {cert: "/dev/null", key: "/dev/null"}
exit:
  enabled: true
  routes:
    - comment: "no cidr here"
`,
			wantSub: "exit.routes[0]: cidr is required",
		},
		{
			name: "invalid cidr in structured form",
			yaml: `
agent:
  data_dir: "./data"
listeners:
  - transport: quic
    address: "0.0.0.0:4433"
    tls: {cert: "/dev/null", key: "/dev/null"}
exit:
  enabled: true
  routes:
    - cidr: "not-a-cidr"
      comment: "broken"
`,
			wantSub: "exit.routes[0]: invalid CIDR: not-a-cidr",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse() error = nil, want substring %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Parse() error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestRouteConfig_MarshalYAML_Roundtrip ensures bare CIDR entries marshal back
// as scalars (so existing configs round-trip unchanged through the wizard),
// while entries with metric or comment marshal as the structured mapping.
func TestRouteConfig_MarshalYAML_Roundtrip(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - cidr: "192.168.0.0/16"
      comment: "office LAN"
`
	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	raw, err := yaml.Marshal(cfg.Exit.Routes)
	if err != nil {
		t.Fatalf("yaml.Marshal error = %v", err)
	}
	out := string(raw)

	if !strings.Contains(out, "- 10.0.0.0/8") {
		t.Errorf("expected bare CIDR scalar in output, got:\n%s", out)
	}
	if !strings.Contains(out, "cidr: 192.168.0.0/16") || !strings.Contains(out, "comment: office LAN") {
		t.Errorf("expected structured mapping for commented route in output, got:\n%s", out)
	}
}

// TestRouteConfig_SourceIP_Roundtrip ensures source_ip survives YAML
// marshal/unmarshal and forces structured (mapping) output.
func TestRouteConfig_SourceIP_Roundtrip(t *testing.T) {
	yamlConfig := `
agent:
  data_dir: "./data"
exit:
  enabled: true
  routes:
    - cidr: "10.0.0.0/8"
      source_ip: "192.168.1.50"
`
	cfg, err := Parse([]byte(yamlConfig))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Exit.Routes) != 1 || cfg.Exit.Routes[0].SourceIP != "192.168.1.50" {
		t.Fatalf("source_ip not preserved on parse: %+v", cfg.Exit.Routes)
	}

	raw, err := yaml.Marshal(cfg.Exit.Routes)
	if err != nil {
		t.Fatalf("yaml.Marshal error = %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "cidr: 10.0.0.0/8") || !strings.Contains(out, "source_ip: 192.168.1.50") {
		t.Errorf("expected structured mapping with source_ip in output, got:\n%s", out)
	}
}

// TestRouteConfig_Interface_TwoTier covers the route.interface policy:
//   - an interface-only binding (no source, or a source on the interface) is
//     portable and accepted on every platform without root;
//   - a source IP that is not assigned to the interface needs the privileged
//     SO_BINDTODEVICE path and is rejected unless running as root on Linux.
func TestRouteConfig_Interface_TwoTier(t *testing.T) {
	ifname := loopbackInterfaceName()
	if ifname == "" {
		t.Skip("no loopback interface found to test against")
	}

	// Interface-only: accepted everywhere (portable bind-to-IP).
	cfg := Default()
	cfg.Exit.Enabled = true
	cfg.Exit.Routes = []RouteConfig{{CIDR: "10.0.0.0/8", Interface: ifname}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("interface-only route should be accepted on all platforms, got %v", err)
	}

	// Off-interface source (TEST-NET-1, not assigned to loopback): needs
	// Linux + root. The runner is unlikely to be root-on-Linux, so expect
	// rejection; skip if the environment happens to permit it.
	cfg2 := Default()
	cfg2.Exit.Enabled = true
	cfg2.Exit.Routes = []RouteConfig{{CIDR: "10.0.0.0/8", SourceIP: "192.0.2.1", Interface: ifname}}
	err := cfg2.Validate()
	if err == nil {
		t.Skip("running as root on Linux -- off-interface source is permitted here, can't test rejection")
	}
	if !strings.Contains(err.Error(), "interface") {
		t.Errorf("Validate() error = %v, want substring \"interface\"", err)
	}
}

// TestRouteConfig_SourceIP_Validation covers the family-mismatch and
// invalid-IP rejections in Validate().
func TestRouteConfig_SourceIP_Validation(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		source    string
		wantError string
	}{
		{name: "v4 ok", cidr: "10.0.0.0/8", source: "10.0.0.5"},
		{name: "v6 ok", cidr: "fd00::/8", source: "fd00::1"},
		{name: "empty source ok", cidr: "10.0.0.0/8", source: ""},
		{name: "invalid IP", cidr: "10.0.0.0/8", source: "not-an-ip", wantError: "invalid source_ip"},
		{name: "v4 source on v6 CIDR", cidr: "fd00::/8", source: "10.0.0.5", wantError: "address family"},
		{name: "v6 source on v4 CIDR", cidr: "10.0.0.0/8", source: "fd00::1", wantError: "address family"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Exit.Enabled = true
			cfg.Exit.Routes = []RouteConfig{{CIDR: tc.cidr, SourceIP: tc.source}}

			err := cfg.Validate()
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("Validate() error = %v, want substring %q", err, tc.wantError)
			}
		})
	}
}

func TestParseRaw_NoDefaultsApplied(t *testing.T) {
	// Only agent.id is set; every other section must stay at its zero value so
	// callers can tell "operator set this" apart from "this is a default".
	cfg, err := ParseRaw([]byte("agent:\n  id: abc123\n"))
	if err != nil {
		t.Fatalf("ParseRaw error: %v", err)
	}
	if cfg.Agent.ID != "abc123" {
		t.Errorf("Agent.ID = %q, want abc123", cfg.Agent.ID)
	}
	// Parse() would fill these from Default(); ParseRaw() must not.
	if cfg.Routing.MaxHops != 0 {
		t.Errorf("Routing.MaxHops = %d, want 0 (no defaults)", cfg.Routing.MaxHops)
	}
	if cfg.Limits.BufferSize != 0 {
		t.Errorf("Limits.BufferSize = %d, want 0 (no defaults)", cfg.Limits.BufferSize)
	}
	if cfg.UDP.Enabled {
		t.Error("UDP.Enabled = true, want false (no defaults)")
	}
	if cfg.HTTP.Address != "" {
		t.Errorf("HTTP.Address = %q, want empty (no defaults)", cfg.HTTP.Address)
	}
}

func TestParseRaw_ExpandsEnvVars(t *testing.T) {
	t.Setenv("MUTI_METROO_TEST_DISPLAY", "field-7")
	cfg, err := ParseRaw([]byte("agent:\n  display_name: ${MUTI_METROO_TEST_DISPLAY}\n"))
	if err != nil {
		t.Fatalf("ParseRaw error: %v", err)
	}
	if cfg.Agent.DisplayName != "field-7" {
		t.Errorf("DisplayName = %q, want field-7 (env expanded)", cfg.Agent.DisplayName)
	}
}
