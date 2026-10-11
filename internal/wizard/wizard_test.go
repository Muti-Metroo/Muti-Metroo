package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
)

func TestNew(t *testing.T) {
	w := New()
	if w == nil {
		t.Fatal("New() returned nil")
	}
	// A fresh wizard has not loaded any existing configuration yet.
	if w.existingCfg != nil {
		t.Error("New() returned wizard with non-nil existingCfg")
	}
	if w.existingRaw != nil {
		t.Error("New() returned wizard with non-nil existingRaw")
	}
}

func TestBuildConfig(t *testing.T) {
	w := New()

	tests := []struct {
		name          string
		dataDir       string
		transport     string
		listenAddr    string
		listenPath    string
		tlsConfig     config.GlobalTLSConfig
		peers         []config.PeerConfig
		socks5Config  config.SOCKS5Config
		exitConfig    config.ExitConfig
		healthEnabled bool
		logLevel      string
		validate      func(*testing.T, *config.Config)
	}{
		{
			name:       "basic QUIC config",
			dataDir:    "/data",
			transport:  "quic",
			listenAddr: "0.0.0.0:4433",
			listenPath: "",
			tlsConfig: config.GlobalTLSConfig{
				Cert: "/certs/server.crt",
				Key:  "/certs/server.key",
			},
			peers:         nil,
			socks5Config:  config.SOCKS5Config{Enabled: false},
			exitConfig:    config.ExitConfig{Enabled: false},
			healthEnabled: true,
			logLevel:      "info",
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.Agent.DataDir != "/data" {
					t.Errorf("DataDir = %q, want %q", cfg.Agent.DataDir, "/data")
				}
				// LogLevel is omitted when "info" (default) - runtime applies default
				if cfg.Agent.LogLevel != "" {
					t.Errorf("LogLevel = %q, want empty (default applied at runtime)", cfg.Agent.LogLevel)
				}
				if len(cfg.Listeners) != 1 {
					t.Fatalf("Listeners count = %d, want 1", len(cfg.Listeners))
				}
				if cfg.Listeners[0].Transport != "quic" {
					t.Errorf("Transport = %q, want %q", cfg.Listeners[0].Transport, "quic")
				}
				if cfg.Listeners[0].Address != "0.0.0.0:4433" {
					t.Errorf("Address = %q, want %q", cfg.Listeners[0].Address, "0.0.0.0:4433")
				}
				if cfg.Listeners[0].Path != "" {
					t.Errorf("Path = %q, want empty", cfg.Listeners[0].Path)
				}
				if !cfg.HTTP.Enabled {
					t.Error("HTTP.Enabled = false, want true")
				}
				if cfg.HTTP.Address != ":8080" {
					t.Errorf("HTTP.Address = %q, want %q", cfg.HTTP.Address, ":8080")
				}
			},
		},
		{
			name:       "HTTP/2 with path",
			dataDir:    "./mydata",
			transport:  "h2",
			listenAddr: "0.0.0.0:443",
			listenPath: "/mesh",
			tlsConfig: config.GlobalTLSConfig{
				Cert: "cert.pem",
				Key:  "key.pem",
			},
			peers:         nil,
			socks5Config:  config.SOCKS5Config{Enabled: false},
			exitConfig:    config.ExitConfig{Enabled: false},
			healthEnabled: false,
			logLevel:      "debug",
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.Listeners[0].Transport != "h2" {
					t.Errorf("Transport = %q, want %q", cfg.Listeners[0].Transport, "h2")
				}
				if cfg.Listeners[0].Path != "/mesh" {
					t.Errorf("Path = %q, want %q", cfg.Listeners[0].Path, "/mesh")
				}
				if cfg.Agent.LogLevel != "debug" {
					t.Errorf("LogLevel = %q, want %q", cfg.Agent.LogLevel, "debug")
				}
				if cfg.HTTP.Enabled {
					t.Error("HTTP.Enabled = true, want false")
				}
			},
		},
		{
			name:       "WebSocket with path",
			dataDir:    "/opt/muti-metroo",
			transport:  "ws",
			listenAddr: "0.0.0.0:8080",
			listenPath: "/ws",
			tlsConfig: config.GlobalTLSConfig{
				Cert: "ws.crt",
				Key:  "ws.key",
			},
			peers:         nil,
			socks5Config:  config.SOCKS5Config{Enabled: false},
			exitConfig:    config.ExitConfig{Enabled: false},
			healthEnabled: true,
			logLevel:      "warn",
			validate: func(t *testing.T, cfg *config.Config) {
				if cfg.Listeners[0].Transport != "ws" {
					t.Errorf("Transport = %q, want %q", cfg.Listeners[0].Transport, "ws")
				}
				if cfg.Listeners[0].Path != "/ws" {
					t.Errorf("Path = %q, want %q", cfg.Listeners[0].Path, "/ws")
				}
			},
		},
		{
			name:       "with SOCKS5 enabled",
			dataDir:    "/data",
			transport:  "quic",
			listenAddr: "0.0.0.0:4433",
			listenPath: "",
			tlsConfig:  config.GlobalTLSConfig{Cert: "c", Key: "k"},
			peers:      nil,
			socks5Config: config.SOCKS5Config{
				Enabled:        true,
				Address:        "127.0.0.1:1080",
				MaxConnections: 500,
				Auth: config.SOCKS5AuthConfig{
					Enabled: true,
					Users: []config.SOCKS5UserConfig{
						{Username: "user1", Password: "pass1"},
					},
				},
			},
			exitConfig:    config.ExitConfig{Enabled: false},
			healthEnabled: false,
			logLevel:      "info",
			validate: func(t *testing.T, cfg *config.Config) {
				if !cfg.SOCKS5.Enabled {
					t.Error("SOCKS5.Enabled = false, want true")
				}
				if cfg.SOCKS5.Address != "127.0.0.1:1080" {
					t.Errorf("SOCKS5.Address = %q, want %q", cfg.SOCKS5.Address, "127.0.0.1:1080")
				}
				if cfg.SOCKS5.MaxConnections != 500 {
					t.Errorf("SOCKS5.MaxConnections = %d, want 500", cfg.SOCKS5.MaxConnections)
				}
				if !cfg.SOCKS5.Auth.Enabled {
					t.Error("SOCKS5.Auth.Enabled = false, want true")
				}
				if len(cfg.SOCKS5.Auth.Users) != 1 {
					t.Fatalf("SOCKS5.Auth.Users count = %d, want 1", len(cfg.SOCKS5.Auth.Users))
				}
				if cfg.SOCKS5.Auth.Users[0].Username != "user1" {
					t.Errorf("Username = %q, want %q", cfg.SOCKS5.Auth.Users[0].Username, "user1")
				}
			},
		},
		{
			name:         "with exit enabled",
			dataDir:      "/data",
			transport:    "quic",
			listenAddr:   "0.0.0.0:4433",
			listenPath:   "",
			tlsConfig:    config.GlobalTLSConfig{Cert: "c", Key: "k"},
			peers:        nil,
			socks5Config: config.SOCKS5Config{Enabled: false},
			exitConfig: config.ExitConfig{
				Enabled: true,
				Routes:  []config.RouteConfig{{CIDR: "0.0.0.0/0"}, {CIDR: "10.0.0.0/8"}},
				DNS: config.DNSConfig{
					Servers: []string{"8.8.8.8:53"},
					Timeout: 10 * time.Second,
				},
			},
			healthEnabled: false,
			logLevel:      "info",
			validate: func(t *testing.T, cfg *config.Config) {
				if !cfg.Exit.Enabled {
					t.Error("Exit.Enabled = false, want true")
				}
				if len(cfg.Exit.Routes) != 2 {
					t.Fatalf("Exit.Routes count = %d, want 2", len(cfg.Exit.Routes))
				}
				if cfg.Exit.Routes[0].CIDR != "0.0.0.0/0" {
					t.Errorf("Exit.Routes[0].CIDR = %q, want %q", cfg.Exit.Routes[0].CIDR, "0.0.0.0/0")
				}
				if len(cfg.Exit.DNS.Servers) != 1 {
					t.Fatalf("Exit.DNS.Servers count = %d, want 1", len(cfg.Exit.DNS.Servers))
				}
			},
		},
		{
			name:       "with peers",
			dataDir:    "/data",
			transport:  "quic",
			listenAddr: "0.0.0.0:4433",
			listenPath: "",
			tlsConfig:  config.GlobalTLSConfig{Cert: "c", Key: "k"},
			peers: []config.PeerConfig{
				{
					ID:        "peer1",
					Transport: "quic",
					Address:   "peer1.example.com:4433",
					TLS:       config.TLSConfig{},
				},
				{
					ID:        "peer2",
					Transport: "h2",
					Address:   "peer2.example.com:443",
					Path:      "/mesh",
				},
			},
			socks5Config:  config.SOCKS5Config{Enabled: false},
			exitConfig:    config.ExitConfig{Enabled: false},
			healthEnabled: false,
			logLevel:      "info",
			validate: func(t *testing.T, cfg *config.Config) {
				if len(cfg.Peers) != 2 {
					t.Fatalf("Peers count = %d, want 2", len(cfg.Peers))
				}
				if cfg.Peers[0].ID != "peer1" {
					t.Errorf("Peers[0].ID = %q, want %q", cfg.Peers[0].ID, "peer1")
				}
				if cfg.Peers[0].Address != "peer1.example.com:4433" {
					t.Errorf("Peers[0].Address = %q, want %q", cfg.Peers[0].Address, "peer1.example.com:4433")
				}
				if cfg.Peers[1].Path != "/mesh" {
					t.Errorf("Peers[1].Path = %q, want %q", cfg.Peers[1].Path, "/mesh")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := w.buildConfig(
				tc.dataDir, "", tc.transport, tc.listenAddr, tc.listenPath, false, nil,
				tc.tlsConfig, tc.peers, tc.socks5Config, tc.exitConfig,
				tc.healthEnabled, tc.logLevel, "",
				config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
			)

			if cfg == nil {
				t.Fatal("buildConfig returned nil")
			}

			tc.validate(t, cfg)
		})
	}
}

func TestWriteConfig(t *testing.T) {
	w := New()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "wizard_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = "/data"
	cfg.Agent.LogLevel = "debug"
	cfg.SOCKS5.Enabled = true
	cfg.SOCKS5.Address = "127.0.0.1:1080"

	configPath := filepath.Join(tmpDir, "config.yaml")

	if err := w.writeConfig(cfg, configPath); err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Read and verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	content := string(data)

	// Check header comment
	if !strings.HasPrefix(content, "# Muti Metroo Configuration") {
		t.Error("Config file missing header comment")
	}

	// Check key values are present
	if !strings.Contains(content, "data_dir: /data") {
		t.Error("Config file missing data_dir value")
	}
	if !strings.Contains(content, "log_level: debug") {
		t.Error("Config file missing log_level value")
	}
	if !strings.Contains(content, "enabled: true") {
		t.Error("Config file missing enabled value")
	}
	if !strings.Contains(content, "address: 127.0.0.1:1080") {
		t.Error("Config file missing SOCKS5 address")
	}
}

func TestWriteConfigPermissions(t *testing.T) {
	w := New()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// A pre-existing world-readable file at the path must be tightened, since
	// os.WriteFile only applies the mode when it creates the file.
	if err := os.WriteFile(configPath, []byte("stale: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	if err := w.writeConfig(cfg, configPath); err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != config.DefaultFileMode {
		t.Errorf("config file mode = %o, want %o (secrets must not be world-readable)", got, config.DefaultFileMode)
	}
}

func TestWriteConfigCreatesDirectory(t *testing.T) {
	w := New()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "wizard_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Path with non-existent subdirectory
	configPath := filepath.Join(tmpDir, "subdir", "nested", "config.yaml")

	cfg := config.Default()

	if err := w.writeConfig(cfg, configPath); err != nil {
		t.Fatalf("writeConfig failed: %v", err)
	}

	// Verify directory was created
	dir := filepath.Dir(configPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("writeConfig did not create parent directories")
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}
}

func TestResultStruct(t *testing.T) {
	// Test that Result struct fields are properly initialized
	result := &Result{
		Config:           config.Default(),
		ConfigPath:       "/path/to/config.yaml",
		DataDir:          "/data",
		CertsDir:         "/data/certs",
		ServiceInstalled: true,
	}

	if result.Config == nil {
		t.Error("Result.Config is nil")
	}
	if result.ConfigPath != "/path/to/config.yaml" {
		t.Errorf("Result.ConfigPath = %q, want %q", result.ConfigPath, "/path/to/config.yaml")
	}
	if result.DataDir != "/data" {
		t.Errorf("Result.DataDir = %q, want %q", result.DataDir, "/data")
	}
	if result.CertsDir != "/data/certs" {
		t.Errorf("Result.CertsDir = %q, want %q", result.CertsDir, "/data/certs")
	}
	if !result.ServiceInstalled {
		t.Error("Result.ServiceInstalled = false, want true")
	}
}

func TestBuildConfigLogFormat(t *testing.T) {
	w := New()

	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{Cert: "c", Key: "k"},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)

	// LogFormat is omitted from generated config - runtime applies default
	if cfg.Agent.LogFormat != "" {
		t.Errorf("Agent.LogFormat = %q, want empty (default applied at runtime)", cfg.Agent.LogFormat)
	}
}

func TestBuildConfigDefaults(t *testing.T) {
	w := New()

	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{Cert: "c", Key: "k"},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)

	// Verify wizard generates minimal config - defaults are NOT included
	// (defaults are applied at runtime by config.Parse())
	if cfg.Routing.MaxHops != 0 {
		t.Error("Routing.MaxHops should be zero (default applied at runtime)")
	}
	if cfg.Limits.BufferSize != 0 {
		t.Error("Limits.BufferSize should be zero (default applied at runtime)")
	}
	if cfg.Connections.Timeout != 0 {
		t.Error("Connections.Timeout should be zero (default applied at runtime)")
	}
}

func TestBuildConfig_AdvertiseListener(t *testing.T) {
	w := New()

	t.Run("nil advertise leaves listener unadvertised", func(t *testing.T) {
		cfg := w.buildConfig(
			"/data", "", "h2", "0.0.0.0:3000", "/mesh", false, nil,
			config.GlobalTLSConfig{Cert: "c", Key: "k"},
			nil, config.SOCKS5Config{}, config.ExitConfig{},
			false, "info", "",
			config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
		)
		if cfg.Listeners[0].Advertise != nil {
			t.Errorf("Advertise = %+v, want nil", cfg.Listeners[0].Advertise)
		}
	})

	t.Run("advertise propagates to listener", func(t *testing.T) {
		adv := &config.AdvertiseConfig{Address: "kreata.ee:3000"}
		cfg := w.buildConfig(
			"/data", "", "h2", "0.0.0.0:3000", "/mesh", false, adv,
			config.GlobalTLSConfig{Cert: "c", Key: "k"},
			nil, config.SOCKS5Config{}, config.ExitConfig{},
			false, "info", "",
			config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
		)
		got := cfg.Listeners[0].Advertise
		if got == nil {
			t.Fatal("Advertise = nil, want set")
		}
		if got.Address != "kreata.ee:3000" {
			t.Errorf("Advertise.Address = %q, want kreata.ee:3000", got.Address)
		}
	})
}

func TestBuildConfig_NoListener(t *testing.T) {
	w := New()

	cfg := w.buildConfig(
		"/data", "", "", "", "", false, nil,
		config.GlobalTLSConfig{},
		nil, config.SOCKS5Config{Enabled: true, Address: "127.0.0.1:1080"}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)

	if cfg == nil {
		t.Fatal("buildConfig returned nil")
	}
	if len(cfg.Listeners) != 0 {
		t.Errorf("Listeners = %+v, want empty for outbound-only agent", cfg.Listeners)
	}
	if !cfg.SOCKS5.Enabled {
		t.Error("SOCKS5 should still be enabled for an ingress-only agent without a listener")
	}
}

func TestBuildConfig_ManagementAndSigningKeys(t *testing.T) {
	w := New()

	mgmt := config.ManagementConfig{
		PublicKey:         "aa11bb22cc33dd44ee55ff66778899aabbccddeeff00112233445566778899aa",
		PrivateKey:        "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff",
		SigningPublicKey:  "bb22cc33dd44ee55ff66778899aabbccddeeff00112233445566778899aabbcc",
		SigningPrivateKey: "2233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff0011",
	}
	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{Cert: "c", Key: "k"},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, mgmt,
	)

	if cfg.Management.PublicKey != mgmt.PublicKey {
		t.Errorf("Management.PublicKey = %q, want %q", cfg.Management.PublicKey, mgmt.PublicKey)
	}
	if cfg.Management.SigningPublicKey != mgmt.SigningPublicKey {
		t.Errorf("Management.SigningPublicKey = %q, want %q", cfg.Management.SigningPublicKey, mgmt.SigningPublicKey)
	}
	if cfg.Management.PrivateKey != mgmt.PrivateKey {
		t.Errorf("Management.PrivateKey not propagated")
	}
	if cfg.Management.SigningPrivateKey != mgmt.SigningPrivateKey {
		t.Errorf("Management.SigningPrivateKey not propagated")
	}
}

func TestValidateAdvertiseAddress(t *testing.T) {
	valid := []string{
		"kreata.ee:3000",
		"www.tahvel.info:443",
		"1.2.3.4:443",
		"[2001:db8::1]:8080",
	}
	for _, addr := range valid {
		if err := validateAdvertiseAddress(addr); err != nil {
			t.Errorf("validateAdvertiseAddress(%q) error = %v, want nil", addr, err)
		}
	}

	rejects := []struct {
		addr    string
		wantSub string
	}{
		{"", "required"},
		{"kreata.ee", "host:port"},
		{"0.0.0.0:443", "unspecified"},
		{"127.0.0.1:8080", "loopback"},
		{"127.0.0.5:443", "loopback"},
		{"[::]:8080", "unspecified"},
		{"[::1]:8080", "loopback"},
	}
	for _, tc := range rejects {
		err := validateAdvertiseAddress(tc.addr)
		if err == nil {
			t.Errorf("validateAdvertiseAddress(%q) = nil, want error containing %q", tc.addr, tc.wantSub)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("validateAdvertiseAddress(%q) error = %v, want substring %q", tc.addr, err, tc.wantSub)
		}
	}
}

func TestTestPeerConnectivity(t *testing.T) {
	w := New()

	tests := []struct {
		name          string
		peer          config.PeerConfig
		expectError   bool
		errorContains string
	}{
		{
			name: "connection refused",
			peer: config.PeerConfig{
				Transport: "quic",
				Address:   "127.0.0.1:59999", // Non-existent port
				TLS:       config.TLSConfig{},
			},
			expectError:   true,
			errorContains: "", // Can be timeout or refused
		},
		{
			name: "invalid address",
			peer: config.PeerConfig{
				Transport: "quic",
				Address:   "invalid-host-that-does-not-exist.local:4433",
				TLS:       config.TLSConfig{},
			},
			expectError:   true,
			errorContains: "", // DNS or connection error
		},
		{
			name: "h2 without path uses default",
			peer: config.PeerConfig{
				Transport: "h2",
				Address:   "127.0.0.1:59999",
				Path:      "", // Should default to /mesh
				TLS:       config.TLSConfig{},
			},
			expectError:   true,
			errorContains: "", // Connection error expected
		},
		{
			name: "ws without path uses default",
			peer: config.PeerConfig{
				Transport: "ws",
				Address:   "127.0.0.1:59999",
				Path:      "", // Should default to /mesh
				TLS:       config.TLSConfig{},
			},
			expectError:   true,
			errorContains: "", // Connection error expected
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := w.testPeerConnectivity(tc.peer)
			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
					t.Errorf("Error %q does not contain %q", err.Error(), tc.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestTestPeerConnectivityLive tests against real listeners.
// Skip if testbed is not running.
func TestTestPeerConnectivityLive(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping live connectivity test in short mode")
	}

	// Check if Agent B (Docker) is reachable
	w := New()
	peer := config.PeerConfig{
		Transport: "quic",
		Address:   "127.0.0.1:4433",
		TLS: config.TLSConfig{
			CA:   "../../testbed/certs/ca.crt",
			Cert: "../../testbed/certs/agent-a.crt",
			Key:  "../../testbed/certs/agent-a.key",
		},
	}

	err := w.testPeerConnectivity(peer)
	if err != nil {
		t.Skipf("Agent B not available (testbed not running?): %v", err)
	}

	// If we get here, Agent B is running and the test passed
	t.Log("Successfully connected to Agent B")
}

func TestTransportIndex(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		expected  int
	}{
		{"quic", "quic", 0},
		{"h2", "h2", 1},
		{"ws", "ws", 2},
		{"unknown defaults to 0", "unknown", 0},
		{"empty defaults to 0", "", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := transportIndex(tc.transport)
			if result != tc.expected {
				t.Errorf("transportIndex(%q) = %d, want %d", tc.transport, result, tc.expected)
			}
		})
	}
}

func TestNormalizeHexKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no prefix", "abcd1234", "abcd1234"},
		{"0x prefix", "0xabcd1234", "abcd1234"},
		{"0X prefix", "0Xabcd1234", "abcd1234"},
		{"whitespace", "  abcd1234  ", "abcd1234"},
		{"prefix and whitespace", "  0xabcd1234  ", "abcd1234"},
		{"empty string", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeHexKey(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeHexKey(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// boolPtr is a test helper for the *bool HTTP endpoint toggles.
func boolPtr(b bool) *bool { return &b }

// minimalBuild calls buildConfig with the common "nothing special" arguments so
// individual tests only have to vary the wizard state they care about.
func minimalBuild(w *Wizard) *config.Config {
	return w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)
}

func TestBuildConfig_PreservesUntouchedSections(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{
		DefaultAction: "help",
		Protocol:      config.ProtocolConfig{ALPN: "custom/9"},
		Routing:       config.RoutingConfig{MaxHops: 7},
		Limits:        config.LimitsConfig{BufferSize: 123456},
		UDP:           config.UDPConfig{Enabled: true, MaxAssociations: 42},
		ICMP:          config.ICMPConfig{Enabled: true, MaxSessions: 9},
		Sleep:         config.SleepConfig{Enabled: true},
		Forward: config.ForwardConfig{
			Endpoints: []config.ForwardEndpoint{{Key: "web", Target: "localhost:3000"}},
			Listeners: []config.ForwardListener{{Key: "web", Address: ":9090"}},
		},
		MeshHostsSuffixes: []string{"corp"},
	}

	cfg := minimalBuild(w)

	if cfg.DefaultAction != "help" {
		t.Errorf("DefaultAction = %q, want help", cfg.DefaultAction)
	}
	if cfg.Protocol.ALPN != "custom/9" {
		t.Errorf("Protocol.ALPN = %q, want custom/9", cfg.Protocol.ALPN)
	}
	if cfg.Routing.MaxHops != 7 {
		t.Errorf("Routing.MaxHops = %d, want 7", cfg.Routing.MaxHops)
	}
	if cfg.Limits.BufferSize != 123456 {
		t.Errorf("Limits.BufferSize = %d, want 123456", cfg.Limits.BufferSize)
	}
	if !cfg.UDP.Enabled || cfg.UDP.MaxAssociations != 42 {
		t.Errorf("UDP not preserved: %+v", cfg.UDP)
	}
	if !cfg.ICMP.Enabled || cfg.ICMP.MaxSessions != 9 {
		t.Errorf("ICMP not preserved: %+v", cfg.ICMP)
	}
	if !cfg.Sleep.Enabled {
		t.Error("Sleep.Enabled not preserved")
	}
	if len(cfg.Forward.Endpoints) != 1 || cfg.Forward.Endpoints[0].Key != "web" {
		t.Errorf("Forward.Endpoints not preserved: %+v", cfg.Forward.Endpoints)
	}
	if len(cfg.Forward.Listeners) != 1 || cfg.Forward.Listeners[0].Address != ":9090" {
		t.Errorf("Forward.Listeners not preserved: %+v", cfg.Forward.Listeners)
	}
	if len(cfg.MeshHostsSuffixes) != 1 || cfg.MeshHostsSuffixes[0] != "corp" {
		t.Errorf("MeshHostsSuffixes not preserved: %+v", cfg.MeshHostsSuffixes)
	}
}

func TestBuildConfig_NoExistingRawStaysMinimal(t *testing.T) {
	// With no existing config, untouched sections must remain zero so the
	// runtime applies current defaults (the long-standing minimal-config policy).
	w := New()
	cfg := minimalBuild(w)

	if cfg.UDP.Enabled || cfg.ICMP.Enabled || cfg.Sleep.Enabled {
		t.Error("untouched sections should be zero when no existing config")
	}
	if len(cfg.Forward.Endpoints) != 0 || len(cfg.Forward.Listeners) != 0 {
		t.Error("Forward should be empty when no existing config")
	}
	if cfg.Routing.MaxHops != 0 || cfg.Limits.BufferSize != 0 {
		t.Error("Routing/Limits should be zero when no existing config")
	}
}

func TestBuildConfig_PreservesHTTPSubFieldsAndAddress(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{
		HTTP: config.HTTPConfig{
			Enabled:   true,
			Address:   "127.0.0.1:9999",
			Minimal:   true,
			Pprof:     boolPtr(false),
			Dashboard: boolPtr(false),
		},
	}

	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		true, "info", "", // healthEnabled = true
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)

	if cfg.HTTP.Address != "127.0.0.1:9999" {
		t.Errorf("HTTP.Address = %q, want preserved 127.0.0.1:9999", cfg.HTTP.Address)
	}
	if !cfg.HTTP.Minimal {
		t.Error("HTTP.Minimal not preserved")
	}
	if cfg.HTTP.Pprof == nil || *cfg.HTTP.Pprof {
		t.Error("HTTP.Pprof=false not preserved")
	}
	if cfg.HTTP.Dashboard == nil || *cfg.HTTP.Dashboard {
		t.Error("HTTP.Dashboard=false not preserved")
	}
}

func TestBuildConfig_HTTPFallsBackToDefaultAddress(t *testing.T) {
	w := New() // no existing config
	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		true, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)
	if cfg.HTTP.Address != ":8080" {
		t.Errorf("HTTP.Address = %q, want :8080 fallback", cfg.HTTP.Address)
	}
}

func TestBuildConfig_PreservesExtraListeners(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{
		Listeners: []config.ListenerConfig{
			{Transport: "quic", Address: "0.0.0.0:4433"},
			{Transport: "ws", Address: "0.0.0.0:8443", Path: "/mesh"},
		},
	}
	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)
	if len(cfg.Listeners) != 2 {
		t.Fatalf("Listeners = %d, want 2 (edited first + preserved second)", len(cfg.Listeners))
	}
	if cfg.Listeners[1].Transport != "ws" || cfg.Listeners[1].Address != "0.0.0.0:8443" {
		t.Errorf("second listener not preserved: %+v", cfg.Listeners[1])
	}
}

func TestBuildConfig_ManagementSigningOnly(t *testing.T) {
	w := New()
	mgmt := config.ManagementConfig{
		SigningPublicKey: "bb22cc33dd44ee55ff66778899aabbccddeeff00112233445566778899aabbcc",
	}
	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, mgmt,
	)
	if cfg.Management.SigningPublicKey != mgmt.SigningPublicKey {
		t.Error("signing-only management config was dropped")
	}
}

func TestBuildConfig_TLSKeepCAOnly(t *testing.T) {
	w := New()
	cfg := w.buildConfig(
		"/data", "", "quic", "0.0.0.0:4433", "", false, nil,
		config.GlobalTLSConfig{CA: "/etc/muti-metroo/ca.pem"},
		nil, config.SOCKS5Config{}, config.ExitConfig{},
		false, "info", "",
		config.ShellConfig{}, config.FileTransferConfig{}, config.ManagementConfig{},
	)
	if cfg.TLS.CA != "/etc/muti-metroo/ca.pem" {
		t.Error("CA-only TLS config was dropped")
	}
}

func TestExistingTLSConfig(t *testing.T) {
	w := New()
	if _, ok := w.existingTLSConfig(); ok {
		t.Error("no existing config should report no TLS")
	}
	w.existingRaw = &config.Config{TLS: config.GlobalTLSConfig{Strict: true}}
	tls, ok := w.existingTLSConfig()
	if !ok || !tls.Strict {
		t.Errorf("expected strict TLS detected, got ok=%v tls=%+v", ok, tls)
	}
}

func TestExistingPeers(t *testing.T) {
	w := New()
	if got := w.existingPeers(); got != nil {
		t.Errorf("existingPeers() = %+v, want nil", got)
	}
	w.existingRaw = &config.Config{Peers: []config.PeerConfig{{Address: "host:4433", Transport: "quic"}}}
	if got := w.existingPeers(); len(got) != 1 || got[0].Address != "host:4433" {
		t.Errorf("existingPeers() = %+v, want one peer", got)
	}
}
