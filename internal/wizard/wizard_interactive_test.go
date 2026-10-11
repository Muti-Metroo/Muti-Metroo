package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/wizard/prompt"
)

// feed scripts the prompt package's input for the interactive ask* functions.
// Each argument is one line the operator would type; an empty string is a bare
// Enter (accept the default). A trailing newline is appended so the final line
// is delivered like the others. The prompts' stdout is silenced for the rest of
// the test so scripted runs do not bury the test markers.
func feed(t *testing.T, lines ...string) {
	t.Helper()
	prompt.SetReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))

	orig := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	os.Stdout = devnull
	t.Cleanup(func() {
		os.Stdout = orig
		devnull.Close()
	})
}

func TestAskExitConfig_PreservesRouteMetadataWhenKept(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{Exit: config.ExitConfig{
		Enabled: true,
		Routes: []config.RouteConfig{
			{CIDR: "10.0.0.0/8", Metric: 5, Comment: "corp", SourceIP: "192.168.1.50", Interface: "ens192"},
		},
		DomainRoutes: []string{"*.internal.corp"},
		DNS:          config.DNSConfig{Servers: []string{"9.9.9.9:53"}, Timeout: 3 * time.Second},
	}}

	// Empty route line (keep existing), then empty domain line (keep existing).
	feed(t, "", "")
	cfg, err := w.askExitConfig()
	if err != nil {
		t.Fatalf("askExitConfig error: %v", err)
	}

	if len(cfg.Routes) != 1 {
		t.Fatalf("Routes = %d, want 1 preserved", len(cfg.Routes))
	}
	r := cfg.Routes[0]
	if r.CIDR != "10.0.0.0/8" || r.Metric != 5 || r.Comment != "corp" || r.SourceIP != "192.168.1.50" || r.Interface != "ens192" {
		t.Errorf("route metadata not preserved: %+v", r)
	}
	if len(cfg.DomainRoutes) != 1 || cfg.DomainRoutes[0] != "*.internal.corp" {
		t.Errorf("DomainRoutes not preserved: %+v", cfg.DomainRoutes)
	}
	if len(cfg.DNS.Servers) != 1 || cfg.DNS.Servers[0] != "9.9.9.9:53" || cfg.DNS.Timeout != 3*time.Second {
		t.Errorf("DNS not preserved: %+v", cfg.DNS)
	}
}

func TestAskExitConfig_CarriesMetadataForRetypedCIDR(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{Exit: config.ExitConfig{
		Enabled: true,
		Routes: []config.RouteConfig{
			{CIDR: "10.0.0.0/8", Metric: 5, Comment: "corp"},
		},
	}}

	// Re-type the existing CIDR plus a brand new one, then finish; no domains.
	feed(t, "10.0.0.0/8", "192.168.0.0/16", "", "")
	cfg, err := w.askExitConfig()
	if err != nil {
		t.Fatalf("askExitConfig error: %v", err)
	}

	if len(cfg.Routes) != 2 {
		t.Fatalf("Routes = %d, want 2", len(cfg.Routes))
	}
	if cfg.Routes[0].Metric != 5 || cfg.Routes[0].Comment != "corp" {
		t.Errorf("metadata not carried for re-typed CIDR: %+v", cfg.Routes[0])
	}
	if cfg.Routes[1].CIDR != "192.168.0.0/16" || cfg.Routes[1].Metric != 0 {
		t.Errorf("new CIDR should be bare: %+v", cfg.Routes[1])
	}
}

func TestAskShellConfig_PreservesEnabledAndWhitelist(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{Shell: config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"htop", "vim"},
		Timeout:     30 * time.Second,
		MaxSessions: 5,
	}}

	// Enable? (default yes) -> Enter; Whitelist mode (default custom) -> Enter;
	// command line empty -> keep existing whitelist.
	feed(t, "", "", "")
	cfg, err := w.askShellConfig()
	if err != nil {
		t.Fatalf("askShellConfig error: %v", err)
	}

	if !cfg.Enabled {
		t.Error("Enabled should default to existing true")
	}
	if strings.Join(cfg.Whitelist, ",") != "htop,vim" {
		t.Errorf("Whitelist = %v, want [htop vim]", cfg.Whitelist)
	}
	if cfg.Timeout != 30*time.Second || cfg.MaxSessions != 5 {
		t.Errorf("Timeout/MaxSessions not preserved: %v / %d", cfg.Timeout, cfg.MaxSessions)
	}
}

func TestAskShellConfig_DefaultsToDisabledWhenAbsent(t *testing.T) {
	w := New() // no existing config

	// Enable? defaults to No -> Enter accepts the default.
	feed(t, "")
	cfg, err := w.askShellConfig()
	if err != nil {
		t.Fatalf("askShellConfig error: %v", err)
	}
	if cfg.Enabled {
		t.Error("Enabled should default to false with no existing config")
	}
}

func TestAskFileTransferConfig_PreservesPathsAndPasswordHash(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{FileTransfer: config.FileTransferConfig{
		Enabled:      true,
		MaxFileSize:  100 * 1024 * 1024,
		AllowedPaths: []string{"/tmp", "/data"},
		PasswordHash: "$2a$12$examplehash",
	}}

	// Enable? (default yes) -> Enter; max size (default 100) -> Enter;
	// path access (default restricted) -> Enter; path line empty -> keep.
	feed(t, "", "", "", "")
	cfg, err := w.askFileTransferConfig()
	if err != nil {
		t.Fatalf("askFileTransferConfig error: %v", err)
	}

	if !cfg.Enabled {
		t.Error("Enabled should default to existing true")
	}
	if cfg.MaxFileSize != 100*1024*1024 {
		t.Errorf("MaxFileSize = %d, want 100MB", cfg.MaxFileSize)
	}
	if strings.Join(cfg.AllowedPaths, ",") != "/tmp,/data" {
		t.Errorf("AllowedPaths = %v, want [/tmp /data]", cfg.AllowedPaths)
	}
	if cfg.PasswordHash != "$2a$12$examplehash" {
		t.Errorf("PasswordHash not preserved: %q", cfg.PasswordHash)
	}
}

func TestAskSOCKS5Config_KeepsExistingCredentials(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{SOCKS5: config.SOCKS5Config{
		Enabled:        true,
		Address:        "127.0.0.1:1080",
		MaxConnections: 500,
		Auth: config.SOCKS5AuthConfig{
			Enabled: true,
			Users:   []config.SOCKS5UserConfig{{Username: "op", PasswordHash: "$2a$12$h"}},
		},
	}}

	// Listen address (default) -> Enter; enable auth (default yes) -> Enter;
	// keep existing credentials? (default yes) -> Enter.
	feed(t, "", "", "")
	cfg, err := w.askSOCKS5Config()
	if err != nil {
		t.Fatalf("askSOCKS5Config error: %v", err)
	}

	if cfg.MaxConnections != 500 {
		t.Errorf("MaxConnections = %d, want 500", cfg.MaxConnections)
	}
	if !cfg.Auth.Enabled || len(cfg.Auth.Users) != 1 {
		t.Fatalf("Auth not preserved: %+v", cfg.Auth)
	}
	if cfg.Auth.Users[0].PasswordHash != "$2a$12$h" || cfg.Auth.Users[0].Username != "op" {
		t.Errorf("credentials not preserved verbatim: %+v", cfg.Auth.Users[0])
	}
}

func TestAskMonitoringAndLogging_KeepsExistingToken(t *testing.T) {
	w := New()
	w.existingCfg = &config.Config{}
	w.existingCfg.HTTP.Enabled = true
	w.existingRaw = &config.Config{}
	w.existingRaw.HTTP.Enabled = true
	w.existingRaw.HTTP.TokenHash = "$2a$12$tokenhash"
	w.existingRaw.Agent.LogLevel = "debug"
	w.existingCfg.Agent.LogLevel = "debug"

	// Log level (default debug) -> Enter; enable HTTP API (default yes) -> Enter;
	// enable auth (default yes, token present) -> Enter; keep token? (yes) -> Enter.
	feed(t, "", "", "", "")
	healthEnabled, logLevel, tokenHash, err := w.askMonitoringAndLogging()
	if err != nil {
		t.Fatalf("askMonitoringAndLogging error: %v", err)
	}

	if !healthEnabled {
		t.Error("healthEnabled should default to existing true")
	}
	if logLevel != "debug" {
		t.Errorf("logLevel = %q, want debug", logLevel)
	}
	if tokenHash != "$2a$12$tokenhash" {
		t.Errorf("token hash not preserved: %q", tokenHash)
	}
}

func TestAskTLSSetup_KeepsExistingStrictTLS(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{TLS: config.GlobalTLSConfig{
		Strict:  true,
		CAPEM:   "-----BEGIN CERTIFICATE-----ca-----END CERTIFICATE-----",
		CertPEM: "-----BEGIN CERTIFICATE-----cert-----END CERTIFICATE-----",
		KeyPEM:  "-----BEGIN PRIVATE KEY-----key-----END PRIVATE KEY-----",
	}}

	// Certificate setup menu defaults to "Keep existing" -> Enter.
	feed(t, "")
	_, tlsCfg, err := w.askTLSSetup("/data")
	if err != nil {
		t.Fatalf("askTLSSetup error: %v", err)
	}

	if !tlsCfg.Strict {
		t.Error("Strict not preserved")
	}
	if !strings.Contains(tlsCfg.CAPEM, "ca") || !strings.Contains(tlsCfg.CertPEM, "cert") {
		t.Errorf("TLS material not preserved: %+v", tlsCfg)
	}
}

func TestAskTLSSetup_DefaultsToSelfSignedWhenAbsent(t *testing.T) {
	w := New() // no existing TLS

	// Two options (self-signed, strict); default self-signed -> Enter.
	feed(t, "")
	_, tlsCfg, err := w.askTLSSetup("/data")
	if err != nil {
		t.Fatalf("askTLSSetup error: %v", err)
	}
	if tlsCfg.Strict || tlsCfg.CertPEM != "" || tlsCfg.CAPEM != "" {
		t.Errorf("expected empty self-signed TLS config, got %+v", tlsCfg)
	}
}

func TestAskPeerConnections_KeepsExistingPeers(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{Peers: []config.PeerConfig{
		{Address: "kreata.ee:4433", Transport: "quic", ID: "abc123"},
		{Address: "tahvel.info:443", Transport: "ws", Path: "/ws-test/"},
	}}

	// Keep existing peers? (default yes) -> Enter; add more? (default no) -> Enter.
	feed(t, "", "")
	peers, err := w.askPeerConnections("quic")
	if err != nil {
		t.Fatalf("askPeerConnections error: %v", err)
	}

	if len(peers) != 2 {
		t.Fatalf("peers = %d, want 2 preserved", len(peers))
	}
	if peers[0].Address != "kreata.ee:4433" || peers[1].Address != "tahvel.info:443" {
		t.Errorf("peers not preserved verbatim: %+v", peers)
	}
	if peers[1].Transport != "ws" || peers[1].Path != "/ws-test/" {
		t.Errorf("second peer transport/path not preserved: %+v", peers[1])
	}
}

func TestAskPeerConnections_DropsExistingWhenDeclined(t *testing.T) {
	w := New()
	w.existingRaw = &config.Config{Peers: []config.PeerConfig{
		{Address: "kreata.ee:4433", Transport: "quic"},
	}}

	// Keep existing peers? -> "n"; add more? (default no) -> Enter.
	feed(t, "n", "")
	peers, err := w.askPeerConnections("quic")
	if err != nil {
		t.Fatalf("askPeerConnections error: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("peers = %d, want 0 after declining to keep", len(peers))
	}
}

func TestAskBasicSetup_CancelsOnUnparseableExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Valid path, but the file is not a parseable Muti Metroo config.
	if err := os.WriteFile(path, []byte("{not valid yaml"), 0600); err != nil {
		t.Fatal(err)
	}

	w := New()
	// Config path, then decline the overwrite prompt.
	feed(t, path, "n")
	_, _, _, err := w.askBasicSetup()
	if err == nil {
		t.Fatal("expected setup to cancel rather than overwrite an unparseable file")
	}
	if !strings.Contains(err.Error(), "not overwritten") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAskBasicSetup_LoadsValidExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("agent:\n  data_dir: /var/lib/muti-metroo\n  display_name: edge-1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	w := New()
	// Config path, accept data dir default, accept display name default.
	feed(t, path, "", "")
	dataDir, configPath, displayName, err := w.askBasicSetup()
	if err != nil {
		t.Fatalf("askBasicSetup error: %v", err)
	}
	if configPath != path {
		t.Errorf("configPath = %q, want %q", configPath, path)
	}
	if dataDir != "/var/lib/muti-metroo" {
		t.Errorf("dataDir = %q, want /var/lib/muti-metroo (from existing config)", dataDir)
	}
	if displayName != "edge-1" {
		t.Errorf("displayName = %q, want edge-1 (from existing config)", displayName)
	}
	if w.existingCfg == nil || w.existingRaw == nil {
		t.Error("existing config should be loaded for use as defaults")
	}
}
