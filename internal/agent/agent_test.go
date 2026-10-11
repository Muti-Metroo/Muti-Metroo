package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
	"github.com/Muti-Metroo/Muti-Metroo/internal/health"
	"github.com/Muti-Metroo/Muti-Metroo/internal/hosts"
	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/probe"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

func TestNew(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if agent == nil {
		t.Fatal("New() returned nil")
	}

	if agent.ID().IsZero() {
		t.Error("Agent ID should not be zero")
	}

	if agent.IsRunning() {
		t.Error("New agent should not be running")
	}
}

func TestAgent_StartStop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = agent.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !agent.IsRunning() {
		t.Error("Agent should be running after Start()")
	}

	// Double start should fail
	err = agent.Start()
	if err == nil {
		t.Error("Double Start() should fail")
	}

	err = agent.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	if agent.IsRunning() {
		t.Error("Agent should not be running after Stop()")
	}

	// Double stop should be safe
	err = agent.Stop()
	if err != nil {
		t.Errorf("Double Stop() error = %v", err)
	}
}

func TestAgent_WithSOCKS5(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.SOCKS5.Enabled = true
	cfg.SOCKS5.Address = "127.0.0.1:0"

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = agent.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer agent.Stop()

	stats := agent.Stats()
	if !stats.SOCKS5Running {
		t.Error("SOCKS5 should be running")
	}
}

func TestAgent_WithExit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Exit.Enabled = true
	cfg.Exit.Routes = []config.RouteConfig{{CIDR: "10.0.0.0/8"}}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = agent.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer agent.Stop()

	stats := agent.Stats()
	if !stats.ExitHandlerRun {
		t.Error("Exit handler should be running")
	}
}

func TestAgent_WithSOCKS5Auth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.SOCKS5.Enabled = true
	cfg.SOCKS5.Address = "127.0.0.1:0"
	cfg.SOCKS5.Auth.Enabled = true
	cfg.SOCKS5.Auth.Users = []config.SOCKS5UserConfig{
		{Username: "user1", Password: "pass1"},
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = agent.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer agent.Stop()
}

func TestAgent_Stats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stats := agent.Stats()
	if stats.PeerCount != 0 {
		t.Errorf("PeerCount = %d, want 0", stats.PeerCount)
	}
	if stats.StreamCount != 0 {
		t.Errorf("StreamCount = %d, want 0", stats.StreamCount)
	}
}

func TestAgent_ID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	id := agent.ID()
	if id.IsZero() {
		t.Error("ID() should not return zero ID")
	}
}

func TestAgent_ProcessFrame_Keepalive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a keepalive frame
	ka := &protocol.Keepalive{Timestamp: uint64(time.Now().UnixNano())}
	frame := &protocol.Frame{
		Type:     protocol.FrameKeepalive,
		StreamID: protocol.ControlStreamID,
		Payload:  ka.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

func TestAgent_ProcessFrame_RouteAdvertise(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a route advertise frame
	adv := &protocol.RouteAdvertise{
		OriginAgent: peerID,
		Sequence:    1,
		Routes: []protocol.Route{
			{
				AddressFamily: protocol.AddrFamilyIPv4,
				PrefixLength:  8,
				Prefix:        []byte{10, 0, 0, 0},
				Metric:        10,
			},
		},
		Path:   []identity.AgentID{peerID},
		SeenBy: []identity.AgentID{peerID},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameRouteAdvertise,
		StreamID: protocol.ControlStreamID,
		Payload:  adv.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)

	// Route should be added
	if agent.routeMgr.TotalRoutes() != 1 {
		t.Errorf("TotalRoutes = %d, want 1", agent.routeMgr.TotalRoutes())
	}
}

func TestAgent_ProcessFrame_StreamOpen(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Exit.Enabled = true
	cfg.Exit.Routes = []config.RouteConfig{{CIDR: "0.0.0.0/0"}}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = agent.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer agent.Stop()

	peerID, _ := identity.NewAgentID()

	// Process a stream open frame with empty path (we are exit)
	open := &protocol.StreamOpen{
		RequestID:     1,
		AddressType:   protocol.AddrTypeIPv4,
		Address:       []byte{127, 0, 0, 1},
		Port:          8080,
		TTL:           16,
		RemainingPath: nil, // Empty path means we are exit
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpen,
		StreamID: 1,
		Payload:  open.Encode(),
	}

	// Should not panic (even though connection may fail)
	agent.processFrame(peerID, frame)
}

func TestAgent_buildSOCKS5Auth_NoAuth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.SOCKS5.Auth.Enabled = false

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	auths := agent.buildSOCKS5Auth()
	if len(auths) != 1 {
		t.Errorf("Authenticators len = %d, want 1", len(auths))
	}
}

func TestAgent_buildSOCKS5Auth_WithUsers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.SOCKS5.Auth.Enabled = true
	cfg.SOCKS5.Auth.Users = []config.SOCKS5UserConfig{
		{Username: "user1", Password: "pass1"},
		{Username: "user2", Password: "pass2"},
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	auths := agent.buildSOCKS5Auth()
	if len(auths) != 1 {
		t.Errorf("Authenticators len = %d, want 1", len(auths))
	}
}

// Tests for addressToString helper function
func TestAddressToString(t *testing.T) {
	tests := []struct {
		name     string
		addrType uint8
		addr     []byte
		want     string
	}{
		{
			name:     "IPv4 localhost",
			addrType: protocol.AddrTypeIPv4,
			addr:     []byte{127, 0, 0, 1},
			want:     "127.0.0.1",
		},
		{
			name:     "IPv4 external",
			addrType: protocol.AddrTypeIPv4,
			addr:     []byte{192, 168, 1, 1},
			want:     "192.168.1.1",
		},
		{
			name:     "IPv4 zeros",
			addrType: protocol.AddrTypeIPv4,
			addr:     []byte{0, 0, 0, 0},
			want:     "0.0.0.0",
		},
		{
			name:     "IPv6 localhost",
			addrType: protocol.AddrTypeIPv6,
			addr:     []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			want:     "::1",
		},
		{
			name:     "IPv6 address",
			addrType: protocol.AddrTypeIPv6,
			addr:     []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			want:     "2001:db8::1",
		},
		{
			name:     "Domain simple",
			addrType: protocol.AddrTypeDomain,
			addr:     []byte{11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:     "example.com",
		},
		{
			name:     "Domain with subdomain",
			addrType: protocol.AddrTypeDomain,
			addr:     []byte{15, 'w', 'w', 'w', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:     "www.example.com",
		},
		{
			name:     "IPv4 too short",
			addrType: protocol.AddrTypeIPv4,
			addr:     []byte{127, 0, 0},
			want:     "",
		},
		{
			name:     "IPv6 too short",
			addrType: protocol.AddrTypeIPv6,
			addr:     []byte{0, 0, 0, 0},
			want:     "",
		},
		{
			name:     "Domain empty",
			addrType: protocol.AddrTypeDomain,
			addr:     []byte{},
			want:     "",
		},
		{
			name:     "Unknown type",
			addrType: 0xFF,
			addr:     []byte{1, 2, 3, 4},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addressToString(tt.addrType, tt.addr)
			if got != tt.want {
				t.Errorf("addressToString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Tests for deriveResponderSessionKey helper function
func TestDeriveResponderSessionKey(t *testing.T) {
	t.Run("valid key exchange", func(t *testing.T) {
		// Generate an initiator keypair to simulate the remote side
		_, remotePub, err := crypto.GenerateEphemeralKeypair()
		if err != nil {
			t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
		}

		requestID := uint64(12345)
		sessionKey, localPub, err := deriveResponderSessionKey(requestID, remotePub)
		if err != nil {
			t.Fatalf("deriveResponderSessionKey() error = %v", err)
		}

		if sessionKey == nil {
			t.Fatal("deriveResponderSessionKey() returned nil session key")
		}

		// Local public key should not be zero
		var zeroKey [crypto.KeySize]byte
		if localPub == zeroKey {
			t.Error("deriveResponderSessionKey() returned zero local public key")
		}

		// Session key should be usable for encryption/decryption
		plaintext := []byte("test message for encryption")
		ciphertext, err := sessionKey.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("SessionKey.Encrypt() error = %v", err)
		}

		if len(ciphertext) <= len(plaintext) {
			t.Error("ciphertext should be larger than plaintext due to overhead")
		}
	})

	t.Run("zero key rejected", func(t *testing.T) {
		var zeroKey [crypto.KeySize]byte
		requestID := uint64(12345)

		_, _, err := deriveResponderSessionKey(requestID, zeroKey)
		if err == nil {
			t.Error("deriveResponderSessionKey() should reject zero key")
		}

		if err.Error() != "encryption required" {
			t.Errorf("deriveResponderSessionKey() error = %q, want %q", err.Error(), "encryption required")
		}
	})

	t.Run("different requests produce different keys", func(t *testing.T) {
		_, remotePub, err := crypto.GenerateEphemeralKeypair()
		if err != nil {
			t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
		}

		sk1, pub1, err := deriveResponderSessionKey(1, remotePub)
		if err != nil {
			t.Fatalf("deriveResponderSessionKey(1) error = %v", err)
		}

		sk2, pub2, err := deriveResponderSessionKey(2, remotePub)
		if err != nil {
			t.Fatalf("deriveResponderSessionKey(2) error = %v", err)
		}

		// Different request IDs should produce different local keys
		// (because we generate new ephemeral keys each time)
		if pub1 == pub2 {
			t.Error("different requests should produce different local public keys")
		}

		// Verify both session keys work
		msg := []byte("test")
		_, err = sk1.Encrypt(msg)
		if err != nil {
			t.Errorf("sk1.Encrypt() error = %v", err)
		}
		_, err = sk2.Encrypt(msg)
		if err != nil {
			t.Errorf("sk2.Encrypt() error = %v", err)
		}
	})
}

// Tests for GetSOCKS5Info accessor method
func TestAgent_GetSOCKS5Info(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		address     string
		wantEnabled bool
		wantAddress string
	}{
		{
			name:        "SOCKS5 enabled",
			enabled:     true,
			address:     "127.0.0.1:1080",
			wantEnabled: true,
			wantAddress: "127.0.0.1:1080",
		},
		{
			name:        "SOCKS5 disabled",
			enabled:     false,
			address:     "",
			wantEnabled: false,
			wantAddress: "",
		},
		{
			name:        "custom address",
			enabled:     true,
			address:     "0.0.0.0:9050",
			wantEnabled: true,
			wantAddress: "0.0.0.0:9050",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "agent-test")
			if err != nil {
				t.Fatalf("Create temp dir error: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			cfg := config.Default()
			cfg.Agent.DataDir = tmpDir
			cfg.SOCKS5.Enabled = tt.enabled
			cfg.SOCKS5.Address = tt.address

			agent, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			info := agent.GetSOCKS5Info()
			if info.Enabled != tt.wantEnabled {
				t.Errorf("GetSOCKS5Info().Enabled = %v, want %v", info.Enabled, tt.wantEnabled)
			}
			if info.Address != tt.wantAddress {
				t.Errorf("GetSOCKS5Info().Address = %q, want %q", info.Address, tt.wantAddress)
			}
		})
	}
}

// Tests for GetUDPInfo accessor method
func TestAgent_GetUDPInfo(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		wantEnabled bool
	}{
		{
			name:        "UDP enabled",
			enabled:     true,
			wantEnabled: true,
		},
		{
			name:        "UDP disabled",
			enabled:     false,
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "agent-test")
			if err != nil {
				t.Fatalf("Create temp dir error: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			cfg := config.Default()
			cfg.Agent.DataDir = tmpDir
			cfg.UDP.Enabled = tt.enabled

			agent, err := New(cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			info := agent.GetUDPInfo()
			if info.Enabled != tt.wantEnabled {
				t.Errorf("GetUDPInfo().Enabled = %v, want %v", info.Enabled, tt.wantEnabled)
			}
		})
	}
}

// Tests for SOCKS5Address and HealthServerAddress
func TestAgent_AddressMethods(t *testing.T) {
	t.Run("SOCKS5Address without server", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "agent-test")
		if err != nil {
			t.Fatalf("Create temp dir error: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfg := config.Default()
		cfg.Agent.DataDir = tmpDir
		cfg.SOCKS5.Enabled = false

		agent, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		addr := agent.SOCKS5Address()
		if addr != nil {
			t.Errorf("SOCKS5Address() = %v, want nil", addr)
		}
	})

	t.Run("HealthServerAddress without server", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "agent-test")
		if err != nil {
			t.Fatalf("Create temp dir error: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfg := config.Default()
		cfg.Agent.DataDir = tmpDir
		cfg.HTTP.Enabled = false

		agent, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		addr := agent.HealthServerAddress()
		if addr != nil {
			t.Errorf("HealthServerAddress() = %v, want nil", addr)
		}
	})

	t.Run("SOCKS5Address with running server", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "agent-test")
		if err != nil {
			t.Fatalf("Create temp dir error: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfg := config.Default()
		cfg.Agent.DataDir = tmpDir
		cfg.SOCKS5.Enabled = true
		cfg.SOCKS5.Address = "127.0.0.1:0"

		agent, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		err = agent.Start()
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		defer agent.Stop()

		addr := agent.SOCKS5Address()
		if addr == nil {
			t.Error("SOCKS5Address() returned nil for running server")
		}
	})
}

// Tests for ProcessFrame with NodeInfoAdvertise
func TestAgent_ProcessFrame_NodeInfoAdvertise(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a node info advertise frame
	nodeInfo := &protocol.NodeInfoAdvertise{
		OriginAgent: peerID,
		Sequence:    1,
		Info: protocol.NodeInfo{
			DisplayName: "test-node",
			StartTime:   time.Now().Unix(),
		},
		SeenBy: []identity.AgentID{peerID},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameNodeInfoAdvertise,
		StreamID: protocol.ControlStreamID,
		Payload:  nodeInfo.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with StreamClose
func TestAgent_ProcessFrame_StreamClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a stream close frame for non-existent stream
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamClose,
		StreamID: 12345,
	}

	// Should not panic (stream doesn't exist)
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with StreamData
func TestAgent_ProcessFrame_StreamData(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a stream data frame for non-existent stream
	frame := &protocol.Frame{
		Type:     protocol.FrameStreamData,
		StreamID: 12345,
		Payload:  []byte("test data"),
	}

	// Should not panic (stream doesn't exist, will be logged)
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with StreamReset
func TestAgent_ProcessFrame_StreamReset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a stream reset frame
	reset := &protocol.StreamReset{
		ErrorCode: protocol.ErrConnectionRefused,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamReset,
		StreamID: 12345,
		Payload:  reset.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with KeepaliveAck
func TestAgent_ProcessFrame_KeepaliveAck(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a keepalive ack frame
	ka := &protocol.Keepalive{Timestamp: uint64(time.Now().UnixNano())}
	frame := &protocol.Frame{
		Type:     protocol.FrameKeepaliveAck,
		StreamID: protocol.ControlStreamID,
		Payload:  ka.Encode(),
	}

	// Should not panic (peer not tracked, will be logged)
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with StreamOpenAck for non-existent stream
func TestAgent_ProcessFrame_StreamOpenAck_NoStream(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a stream open ack for non-existent stream
	ack := &protocol.StreamOpenAck{
		RequestID:     1,
		BoundAddrType: protocol.AddrTypeIPv4,
		BoundAddr:     []byte{127, 0, 0, 1},
		BoundPort:     8080,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpenAck,
		StreamID: 99999,
		Payload:  ack.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with StreamOpenErr for non-existent stream
func TestAgent_ProcessFrame_StreamOpenErr_NoStream(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a stream open error for non-existent stream
	openErr := &protocol.StreamOpenErr{
		RequestID: 1,
		ErrorCode: protocol.ErrNoRoute,
		Message:   "no route to host",
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameStreamOpenErr,
		StreamID: 99999,
		Payload:  openErr.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with RouteWithdraw
func TestAgent_ProcessFrame_RouteWithdraw(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a route withdraw frame
	withdraw := &protocol.RouteWithdraw{
		OriginAgent: peerID,
		Sequence:    1,
		Routes: []protocol.Route{
			{
				AddressFamily: protocol.AddrFamilyIPv4,
				PrefixLength:  8,
				Prefix:        []byte{10, 0, 0, 0},
			},
		},
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameRouteWithdraw,
		StreamID: protocol.ControlStreamID,
		Payload:  withdraw.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

// Tests for TriggerRouteAdvertise
func TestAgent_TriggerRouteAdvertise(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// TriggerRouteAdvertise should not block or panic
	agent.TriggerRouteAdvertise()

	// Call it again to test buffered channel behavior
	agent.TriggerRouteAdvertise()
}

// Tests for getLocalStatus method
func TestAgent_getLocalStatus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data, success := agent.getLocalStatus()
	if !success {
		t.Error("getLocalStatus() should succeed")
	}

	if len(data) == 0 {
		t.Error("getLocalStatus() returned empty data")
	}

	// Verify it's valid JSON
	var status map[string]interface{}
	if err := json.Unmarshal(data, &status); err != nil {
		t.Errorf("getLocalStatus() returned invalid JSON: %v", err)
	}

	// Check expected fields
	if _, ok := status["agent_id"]; !ok {
		t.Error("getLocalStatus() missing agent_id field")
	}
	if _, ok := status["running"]; !ok {
		t.Error("getLocalStatus() missing running field")
	}
	if _, ok := status["peer_count"]; !ok {
		t.Error("getLocalStatus() missing peer_count field")
	}
}

// Tests for getLocalPeers method
func TestAgent_getLocalPeers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data, success := agent.getLocalPeers()
	if !success {
		t.Error("getLocalPeers() should succeed")
	}

	// Verify it's valid JSON array
	var peers []string
	if err := json.Unmarshal(data, &peers); err != nil {
		t.Errorf("getLocalPeers() returned invalid JSON: %v", err)
	}

	// No peers connected yet
	if len(peers) != 0 {
		t.Errorf("getLocalPeers() = %d peers, want 0", len(peers))
	}
}

// Tests for getLocalRoutes method
func TestAgent_getLocalRoutes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Exit.Enabled = true
	cfg.Exit.Routes = []config.RouteConfig{{CIDR: "10.0.0.0/8"}}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	data, success := agent.getLocalRoutes()
	if !success {
		t.Error("getLocalRoutes() should succeed")
	}

	// Verify it's valid JSON array
	var routes []map[string]interface{}
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Errorf("getLocalRoutes() returned invalid JSON: %v", err)
	}

	// Should have one local route
	if len(routes) != 1 {
		t.Errorf("getLocalRoutes() = %d routes, want 1", len(routes))
	}

	if len(routes) > 0 {
		route := routes[0]
		if _, ok := route["network"]; !ok {
			t.Error("route missing network field")
		}
		if _, ok := route["metric"]; !ok {
			t.Error("route missing metric field")
		}
	}
}

// Tests for cleanupRelaysForPeer method
func TestAgent_cleanupRelaysForPeer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Create some relay entries
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()

	agent.tcpRelay.Insert(&relayEntry{
		UpstreamPeer:   peerA,
		UpstreamID:     1,
		DownstreamPeer: peerB,
		DownstreamID:   100,
	})
	agent.tcpRelay.Insert(&relayEntry{
		UpstreamPeer:   peerB,
		UpstreamID:     2,
		DownstreamPeer: peerC,
		DownstreamID:   200,
	})
	agent.tcpRelay.Insert(&relayEntry{
		UpstreamPeer:   peerC,
		UpstreamID:     3,
		DownstreamPeer: peerA,
		DownstreamID:   300,
	})

	// Cleanup relays for peerA (should remove entries 1 and 3)
	agent.cleanupRelaysForPeer(peerA)

	// Entry 2 should remain (only involves peerB and peerC)
	if e := agent.tcpRelay.LookupDownstream(peerC, 200); e == nil {
		t.Error("relay entry 2 should not be removed")
	}

	// Entries 1 and 3 should be removed
	if e := agent.tcpRelay.LookupDownstream(peerB, 100); e != nil {
		t.Error("relay entry 1 should be removed (involves peerA)")
	}
	if e := agent.tcpRelay.LookupDownstream(peerA, 300); e != nil {
		t.Error("relay entry 3 should be removed (involves peerA)")
	}
}

// Tests for buildSOCKS5Auth with hashed passwords
func TestAgent_buildSOCKS5Auth_WithHashedUsers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.SOCKS5.Auth.Enabled = true
	cfg.SOCKS5.Auth.Users = []config.SOCKS5UserConfig{
		// bcrypt hash for "password123"
		{Username: "hasheduser", PasswordHash: "$2a$10$N9qo8uLOickgx2ZMRZoMyed0W/OLrKcKqM5Zds6UvDKTVBVVgDg5a"},
		// Mixed: plaintext and hashed
		{Username: "plainuser", Password: "plainpass"},
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	auths := agent.buildSOCKS5Auth()
	if len(auths) != 1 {
		t.Errorf("Authenticators len = %d, want 1", len(auths))
	}
}

// Tests for getFileTransferStream
func TestAgent_getFileTransferStream(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Non-existent stream should return nil
	fts := agent.getFileTransferStream(peerID, 12345)
	if fts != nil {
		t.Error("getFileTransferStream() should return nil for non-existent stream")
	}

	// Add a stream and retrieve it
	agent.fileStreamsMu.Lock()
	agent.fileStreams[streamKey{peer: peerID, streamID: 100}] = &fileTransferStream{
		StreamID:  100,
		PeerID:    peerID,
		RequestID: 1,
		IsUpload:  true,
	}
	agent.fileStreamsMu.Unlock()

	fts = agent.getFileTransferStream(peerID, 100)
	if fts == nil {
		t.Error("getFileTransferStream() should return the stream")
	}
	if fts.StreamID != 100 {
		t.Errorf("getFileTransferStream().StreamID = %d, want 100", fts.StreamID)
	}

	// Another peer quoting the same numeric ID must not reach it.
	otherPeer, _ := identity.NewAgentID()
	if fts := agent.getFileTransferStream(otherPeer, 100); fts != nil {
		t.Error("getFileTransferStream() should not return another peer's stream")
	}
}

// Tests for cleanupFileTransferStream
func TestAgent_cleanupFileTransferStream(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Add a stream
	peerID, _ := identity.NewAgentID()
	agent.fileStreamsMu.Lock()
	agent.fileStreams[streamKey{peer: peerID, streamID: 100}] = &fileTransferStream{
		StreamID:  100,
		PeerID:    peerID,
		RequestID: 1,
		IsUpload:  true,
	}
	agent.fileStreamsMu.Unlock()

	// Cleanup the stream
	agent.cleanupFileTransferStream(peerID, 100)

	// Stream should be removed
	agent.fileStreamsMu.RLock()
	_, exists := agent.fileStreams[streamKey{peer: peerID, streamID: 100}]
	agent.fileStreamsMu.RUnlock()

	if exists {
		t.Error("cleanupFileTransferStream() should remove the stream")
	}

	// Cleanup non-existent stream should not panic
	agent.cleanupFileTransferStream(peerID, 99999)
}

// TestAgent_cleanupIdleDestAssociations_RemovesFromBothIndices verifies that
// an idle UDP destination association is removed from both the per-ingress
// destAssocs map and the agent-global udpIngressByLocalStream map. Regression
// test for the race where a concurrent getOrCreateDestAssociation could
// observe (and hand out) a dest that was on the cleanup chopping block.
func TestAgent_cleanupIdleDestAssociations_RemovesFromBothIndices(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ingress := &udpIngressAssociation{
		BaseStreamID: 1,
		destAssocs:   make(map[string]*udpDestAssociation),
		idleTimeout:  100 * time.Millisecond,
	}
	nextHop, _ := identity.NewAgentID()
	dest := &udpDestAssociation{
		StreamID:     42,
		OriginKey:    "origin1",
		NextHop:      nextHop,
		LastActivity: time.Now().Add(-1 * time.Hour), // far past => idle
		PendingOpen:  make(chan struct{}),
	}
	close(dest.PendingOpen) // mark "open complete" so it looks like a valid in-use dest
	ingress.destAssocs["origin1"] = dest

	agent.udpIngressMu.Lock()
	agent.udpIngressByBase[1] = ingress
	agent.udpIngressByLocalStream[streamKey{peer: dest.NextHop, streamID: 42}] = &udpDestLookup{Ingress: ingress, Dest: dest}
	agent.udpIngressMu.Unlock()

	agent.cleanupIdleDestAssociations()

	// Both indices must have dropped the dest.
	ingress.destMu.RLock()
	_, perIngressExists := ingress.destAssocs["origin1"]
	ingress.destMu.RUnlock()
	if perIngressExists {
		t.Error("dest should have been removed from ingress.destAssocs")
	}

	agent.udpIngressMu.RLock()
	_, globalExists := agent.udpIngressByLocalStream[streamKey{peer: dest.NextHop, streamID: 42}]
	agent.udpIngressMu.RUnlock()
	if globalExists {
		t.Error("dest should have been removed from udpIngressByLocalStream")
	}
}

// TestAgent_cleanupIdleDestAssociations_KeepsActive verifies that a
// destination association whose LastActivity is recent is NOT removed.
func TestAgent_cleanupIdleDestAssociations_KeepsActive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ingress := &udpIngressAssociation{
		BaseStreamID: 2,
		destAssocs:   make(map[string]*udpDestAssociation),
		idleTimeout:  1 * time.Hour,
	}
	nextHop, _ := identity.NewAgentID()
	dest := &udpDestAssociation{
		StreamID:     43,
		OriginKey:    "origin2",
		NextHop:      nextHop,
		LastActivity: time.Now(), // active
		PendingOpen:  make(chan struct{}),
	}
	close(dest.PendingOpen)
	ingress.destAssocs["origin2"] = dest

	agent.udpIngressMu.Lock()
	agent.udpIngressByBase[2] = ingress
	agent.udpIngressByLocalStream[streamKey{peer: dest.NextHop, streamID: 43}] = &udpDestLookup{Ingress: ingress, Dest: dest}
	agent.udpIngressMu.Unlock()

	agent.cleanupIdleDestAssociations()

	ingress.destMu.RLock()
	_, exists := ingress.destAssocs["origin2"]
	ingress.destMu.RUnlock()
	if !exists {
		t.Error("active dest should not have been removed")
	}

	agent.udpIngressMu.RLock()
	_, globalExists := agent.udpIngressByLocalStream[streamKey{peer: dest.NextHop, streamID: 43}]
	agent.udpIngressMu.RUnlock()
	if !globalExists {
		t.Error("active dest should still be in udpIngressByLocalStream")
	}
}

// Tests for ProcessFrame with ControlRequest
func TestAgent_ProcessFrame_ControlRequest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a control request for local agent (status request)
	req := &protocol.ControlRequest{
		RequestID:   1,
		ControlType: protocol.ControlTypeStatus,
		TargetAgent: agent.id, // Request for this agent
		Path:        nil,
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameControlRequest,
		StreamID: protocol.ControlStreamID,
		Payload:  req.Encode(),
	}

	// Should not panic (will try to send response to non-existent peer)
	agent.processFrame(peerID, frame)
}

// Tests for ProcessFrame with ControlResponse
func TestAgent_ProcessFrame_ControlResponse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	peerID, _ := identity.NewAgentID()

	// Process a control response for non-existent request
	resp := &protocol.ControlResponse{
		RequestID:   99999,
		ControlType: protocol.ControlTypeStatus,
		Success:     true,
		Data:        []byte("{}"),
	}

	frame := &protocol.Frame{
		Type:     protocol.FrameControlResponse,
		StreamID: protocol.ControlStreamID,
		Payload:  resp.Encode(),
	}

	// Should not panic
	agent.processFrame(peerID, frame)
}

// TestAgent_ControlResponse_ChunkReassembly verifies that a multi-frame
// control response (HasMore=true on all but the last chunk) is reassembled
// into a single payload delivered to the pending request's ResponseCh.
func TestAgent_ControlResponse_ChunkReassembly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const requestID uint64 = 7777
	peerID, _ := identity.NewAgentID()
	respCh := make(chan *protocol.ControlResponse, 1)
	agent.controlMu.Lock()
	agent.pendingControl[requestID] = &pendingControlRequest{
		RequestID:   requestID,
		NextHop:     peerID,
		ControlType: protocol.ControlTypeStatus,
		ResponseCh:  respCh,
		Timeout:     time.Now().Add(30 * time.Second),
	}
	agent.controlMu.Unlock()

	chunks := [][]byte{[]byte("part-one,"), []byte("part-two,"), []byte("part-three")}
	for i, chunk := range chunks {
		resp := &protocol.ControlResponse{
			RequestID:   requestID,
			ControlType: protocol.ControlTypeStatus,
			Success:     true,
			HasMore:     i < len(chunks)-1,
			Data:        chunk,
		}
		agent.processFrame(peerID, &protocol.Frame{
			Type:     protocol.FrameControlResponse,
			StreamID: protocol.ControlStreamID,
			Payload:  resp.Encode(),
		})
	}

	select {
	case final := <-respCh:
		want := "part-one,part-two,part-three"
		if string(final.Data) != want {
			t.Errorf("reassembled data = %q, want %q", final.Data, want)
		}
		if !final.Success {
			t.Error("Success = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no response delivered within timeout")
	}

	agent.controlMu.Lock()
	_, stillPending := agent.pendingControl[requestID]
	agent.controlMu.Unlock()
	if stillPending {
		t.Error("pending entry should be deleted after final chunk")
	}
}

// Tests for DisplayName accessor
func TestAgent_DisplayName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Agent.DisplayName = "test-agent-name"

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	name := agent.DisplayName()
	if name != "test-agent-name" {
		t.Errorf("DisplayName() = %q, want %q", name, "test-agent-name")
	}
}

// Tests for DisplayName with fallback to ID
func TestAgent_DisplayName_Fallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Agent.DisplayName = "" // No display name set

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	name := agent.DisplayName()
	// Should fall back to short agent ID
	if name == "" {
		t.Error("DisplayName() should not be empty when falling back to ID")
	}
	if name != agent.ID().ShortString() {
		t.Errorf("DisplayName() = %q, want %q", name, agent.ID().ShortString())
	}
}

func TestAgent_StopDuringStartupDelay(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Agent.StartupDelay = 10 * time.Second

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Start in a goroutine (will block on startup delay)
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- a.Start()
	}()

	// Give Start() time to enter the delay
	time.Sleep(100 * time.Millisecond)

	// Stop during startup delay
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Start() should return ErrInterrupted
	startErr := <-startErrCh
	if !errors.Is(startErr, ErrInterrupted) {
		t.Errorf("Start() returned %v, want ErrInterrupted", startErr)
	}

	if a.IsRunning() {
		t.Error("Agent should not be running after interrupted start")
	}
}

func TestAgent_HandleProbeRequest_BadJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	body, success := a.handleProbeRequest([]byte(`{not json`))
	if success {
		t.Error("expected success=false for malformed JSON")
	}
	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected an error message in response body")
	}
}

func TestAgent_HandleProbeRequest_ValidationError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Missing address triggers validation failure.
	body, success := a.handleProbeRequest([]byte(`{"transport":"quic"}`))
	if success {
		t.Error("expected success=false for validation error")
	}
	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected an error message in response body")
	}
}

func TestAgent_HandleProbeRequest_DialFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Dial 127.0.0.1:1 with a short timeout. The control-layer success flag
	// should still be true because the probe ran to completion; the body
	// should report success=false with a non-empty error. We do not pin the
	// exact classification string because refused-vs-timeout-vs-filtered
	// depends on the host's networking stack.
	req := `{"transport":"quic","address":"127.0.0.1:1","timeout_ms":1000}`
	body, success := a.handleProbeRequest([]byte(req))
	if !success {
		t.Fatalf("control success: got false, want true (probe ran to completion); body=%s", body)
	}
	var resp probe.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Success {
		t.Error("expected probe success=false for failing dial")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error detail for failing dial")
	}
}

func TestAgent_HandleProbeRequest_CancelsOnStop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test")
	if err != nil {
		t.Fatalf("Create temp dir error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Stub the probe to block until ctx is cancelled, so the only way
	// handleProbeRequest returns is via shutdownBoundCtx firing when stopCh
	// closes.
	probeStarted := make(chan struct{})
	a.probeFunc = func(ctx context.Context, opts probe.Options) *probe.Result {
		close(probeStarted)
		<-ctx.Done()
		return &probe.Result{
			Transport:   opts.Transport,
			Address:     opts.Address,
			Error:       ctx.Err(),
			ErrorDetail: ctx.Err().Error(),
		}
	}

	req := []byte(`{"transport":"quic","address":"127.0.0.1:1","timeout_ms":60000}`)
	done := make(chan struct{})
	var body []byte
	var controlOK bool
	go func() {
		body, controlOK = a.handleProbeRequest(req)
		close(done)
	}()

	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start in time")
	}

	// Closing stopCh is what Stop() does; invoke it directly because Stop()
	// tears down components the test never started.
	close(a.stopCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleProbeRequest did not return after stopCh closed")
	}

	if !controlOK {
		t.Fatalf("control success: got false, want true; body=%s", body)
	}
	var resp probe.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Success {
		t.Error("expected probe success=false after cancellation")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error detail after cancellation")
	}
}

func TestAgent_TraceRoute_MeshHostRegistry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-trace-test")
	if err != nil {
		t.Fatalf("Create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.MeshHostsSuffixes = []string{"mesh"}
	cfg.MeshHosts = []hosts.Host{
		{Name: "neti.mesh", IPv4: "185.154.221.53", Comment: "test"},
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("registered name resolves via registry", func(t *testing.T) {
		res, err := a.TraceRoute("neti.mesh")
		if err != nil {
			t.Fatalf("TraceRoute: %v", err)
		}
		if res.Resolution != health.ResolutionMesh {
			t.Errorf("Resolution = %q, want %q", res.Resolution, health.ResolutionMesh)
		}
		if !res.ResolverOK {
			t.Error("ResolverOK = false, want true")
		}
		if res.Hostname != "neti.mesh" {
			t.Errorf("Hostname = %q, want %q", res.Hostname, "neti.mesh")
		}
		if len(res.IPs) != 1 || res.IPs[0].IP != "185.154.221.53" {
			t.Errorf("IPs = %+v, want single 185.154.221.53", res.IPs)
		}
		if res.Error != "" {
			t.Errorf("unexpected Error: %q", res.Error)
		}
	})

	t.Run("unregistered name does not use registry", func(t *testing.T) {
		// .invalid is RFC 6761 reserved; every conformant resolver
		// returns NXDOMAIN. Some networks intercept and rewrite, so we
		// don't pin Resolution to "system" -- both branches (system DNS
		// hit and system DNS error) prove the registry was bypassed.
		res, err := a.TraceRoute("definitely-not-registered.example.invalid")
		if err != nil {
			t.Fatalf("TraceRoute: %v", err)
		}
		if res.Resolution == health.ResolutionMesh {
			t.Errorf("registry must not match: Resolution = %q, IPs = %+v", res.Resolution, res.IPs)
		}
		if res.ResolverOK && res.Resolution != health.ResolutionSystem {
			t.Errorf("ResolverOK=true but Resolution = %q, want %q", res.Resolution, health.ResolutionSystem)
		}
		if !res.ResolverOK && res.Error == "" {
			t.Error("ResolverOK=false but Error is empty")
		}
	})

	t.Run("literal IP tags resolution=ip", func(t *testing.T) {
		res, err := a.TraceRoute("127.0.0.1")
		if err != nil {
			t.Fatalf("TraceRoute: %v", err)
		}
		if res.Resolution != health.ResolutionIP {
			t.Errorf("Resolution = %q, want %q", res.Resolution, health.ResolutionIP)
		}
		if res.Hostname != "" {
			t.Errorf("Hostname = %q, want empty", res.Hostname)
		}
		if len(res.IPs) != 1 {
			t.Errorf("IPs = %+v, want single entry", res.IPs)
		}
	})
}

func TestAgent_resolveHost_MeshHostRegistry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-resolve-test")
	if err != nil {
		t.Fatalf("Create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.MeshHostsSuffixes = []string{"mesh"}
	cfg.MeshHosts = []hosts.Host{
		{Name: "single.mesh", IPv4: "10.0.0.1"},
		{Name: "dual.mesh", IPv4: "10.0.0.2", IPv6: "fd00::2"},
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("registered name returns registry IPs", func(t *testing.T) {
		ips, err := a.resolveHost(context.Background(), "single.mesh")
		if err != nil {
			t.Fatalf("resolveHost: %v", err)
		}
		if len(ips) != 1 || ips[0].String() != "10.0.0.1" {
			t.Errorf("ips = %v, want [10.0.0.1]", ips)
		}
	})

	t.Run("dual-stack name returns both families", func(t *testing.T) {
		ips, err := a.resolveHost(context.Background(), "dual.mesh")
		if err != nil {
			t.Fatalf("resolveHost: %v", err)
		}
		if len(ips) != 2 {
			t.Fatalf("ips = %v, want 2 entries", ips)
		}
		if ips[0].String() != "10.0.0.2" || ips[1].String() != "fd00::2" {
			t.Errorf("ips = %v, want [10.0.0.2, fd00::2]", ips)
		}
	})

	t.Run("unregistered .invalid name falls through to system DNS", func(t *testing.T) {
		// .invalid is RFC 6761 reserved; should NXDOMAIN. We only need
		// to confirm the registry didn't match -- err != nil and the
		// returned slice not being a registry shape both prove that.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ips, err := a.resolveHost(ctx, "definitely-not-registered.example.invalid")
		if err == nil {
			// Some networks intercept NXDOMAIN. Tolerate that, but
			// confirm the returned IPs don't match a registry entry.
			for _, ip := range ips {
				if ip.String() == "10.0.0.1" || ip.String() == "10.0.0.2" || ip.String() == "fd00::2" {
					t.Errorf("registry leaked into system-DNS path: %v", ips)
				}
			}
			return
		}
		if len(ips) != 0 {
			t.Errorf("err set but ips = %v", ips)
		}
	})
}

// TestAgent_DialContext_LocalExitBinding verifies that a local exit (ingress and
// exit on the same agent) honors the route's source_ip binding on the direct-dial
// path. The route binds an unbound source (TEST-NET 192.0.2.1), so the dial must
// fail at bind time instead of silently ignoring the binding and connecting from
// the default source. A real listener guarantees that an unbound dial would
// otherwise succeed, so a failure here can only come from the source binding.
func TestAgent_DialContext_LocalExitBinding(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()

	cfg := config.Default()
	cfg.Agent.DataDir = t.TempDir()
	cfg.Exit.Enabled = true
	cfg.Exit.Routes = []config.RouteConfig{{CIDR: "127.0.0.0/8", SourceIP: "192.0.2.1"}}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer a.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := a.DialContext(ctx, "tcp", ln.Addr().String())
	if err == nil {
		conn.Close()
		t.Fatal("DialContext via local exit succeeded; route binds unbound source 192.0.2.1, so binding was not applied on the direct-dial path")
	}
}

// ICMP ingress sessions live in two indices: by (next hop, wire stream ID) for
// frames arriving off the wire, and by an agent-wide handle for the SOCKS5 API.
// The wire stream ID cannot serve as the handle, because it comes from a
// per-connection allocator: two sessions routed via different next hops both
// get stream ID 1. Each handle must resolve to its own session, and removing
// one must clear both of its indices without disturbing the other.
func TestAgent_ICMPIngressHandleAndStreamIndices(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	hopA, _ := identity.NewAgentID()
	hopB, _ := identity.NewAgentID()

	// Both sessions draw stream ID 1: separate connections, separate ID spaces.
	assocA := &icmpIngressAssociation{Handle: 1, StreamID: 1, NextHop: hopA}
	assocB := &icmpIngressAssociation{Handle: 2, StreamID: 1, NextHop: hopB}

	agent.icmpIngressMu.Lock()
	for _, a := range []*icmpIngressAssociation{assocA, assocB} {
		agent.icmpIngressByStream[streamKey{peer: a.NextHop, streamID: a.StreamID}] = a
		agent.icmpIngressByHandle[a.Handle] = a
	}
	agent.icmpIngressMu.Unlock()

	// Handles must not alias each other.
	agent.icmpIngressMu.RLock()
	gotA := agent.icmpIngressByHandle[assocA.Handle]
	gotB := agent.icmpIngressByHandle[assocB.Handle]
	agent.icmpIngressMu.RUnlock()
	if gotA != assocA {
		t.Errorf("handle %d resolved to the wrong session", assocA.Handle)
	}
	if gotB != assocB {
		t.Errorf("handle %d resolved to the wrong session", assocB.Handle)
	}

	// Wire lookups stay separated by next hop despite the shared stream ID.
	agent.icmpIngressMu.RLock()
	wireA := agent.icmpIngressByStream[streamKey{peer: hopA, streamID: 1}]
	wireB := agent.icmpIngressByStream[streamKey{peer: hopB, streamID: 1}]
	agent.icmpIngressMu.RUnlock()
	if wireA != assocA || wireB != assocB {
		t.Error("wire lookups crossed between next hops sharing a stream ID")
	}

	// Removing one clears both of its indices and leaves the other intact.
	agent.icmpIngressMu.Lock()
	agent.removeICMPIngress(assocA)
	_, streamStillThere := agent.icmpIngressByStream[streamKey{peer: hopA, streamID: 1}]
	_, handleStillThere := agent.icmpIngressByHandle[assocA.Handle]
	survivingStream := agent.icmpIngressByStream[streamKey{peer: hopB, streamID: 1}]
	survivingHandle := agent.icmpIngressByHandle[assocB.Handle]
	agent.icmpIngressMu.Unlock()

	if streamStillThere {
		t.Error("removed session still present in the stream index")
	}
	if handleStillThere {
		t.Error("removed session still present in the handle index")
	}
	if survivingStream != assocB || survivingHandle != assocB {
		t.Error("removing one session disturbed the other")
	}
}

// Control request IDs come from a sequential counter, so they are as guessable
// as stream IDs. Only the peer a request was sent to may answer it; otherwise
// any peer could satisfy or fail a request this agent addressed to another.
func TestAgent_ControlResponse_RejectsWrongPeer(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const requestID uint64 = 4242
	intendedPeer, _ := identity.NewAgentID()
	impostor, _ := identity.NewAgentID()

	respCh := make(chan *protocol.ControlResponse, 1)
	agent.controlMu.Lock()
	agent.pendingControl[requestID] = &pendingControlRequest{
		RequestID:   requestID,
		NextHop:     intendedPeer,
		ControlType: protocol.ControlTypeStatus,
		ResponseCh:  respCh,
		Timeout:     time.Now().Add(30 * time.Second),
	}
	agent.controlMu.Unlock()

	deliver := func(from identity.AgentID, data string) {
		resp := &protocol.ControlResponse{
			RequestID:   requestID,
			ControlType: protocol.ControlTypeStatus,
			Success:     true,
			Data:        []byte(data),
		}
		agent.processFrame(from, &protocol.Frame{
			Type:     protocol.FrameControlResponse,
			StreamID: protocol.ControlStreamID,
			Payload:  resp.Encode(),
		})
	}

	// The impostor's answer must be ignored entirely.
	deliver(impostor, "forged")
	select {
	case got := <-respCh:
		t.Fatalf("an impostor peer answered a request addressed to another peer: %q", got.Data)
	default:
	}

	// And it must not have consumed the pending entry, or the rejection would
	// be a denial of service against the real answer.
	agent.controlMu.Lock()
	_, stillPending := agent.pendingControl[requestID]
	agent.controlMu.Unlock()
	if !stillPending {
		t.Fatal("the impostor's answer consumed the pending request")
	}

	// The intended peer still completes it, with no trace of the forgery.
	deliver(intendedPeer, "genuine")
	select {
	case got := <-respCh:
		if string(got.Data) != "genuine" {
			t.Errorf("response data = %q, want %q", got.Data, "genuine")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("intended peer could not complete its own request")
	}
}

// A transit agent forwards control requests under an ID of its own, not the
// one the upstream peer chose. Every peer's request counter starts at 1, so
// reusing the upstream ID as the tracking key made two peers routing through
// the same transit agent collide in ordinary operation, silently evicting the
// earlier request. The response is mapped back to the upstream ID on the way
// out, and only the peer a request was forwarded to may answer it.
func TestAgent_ForwardedControl_NoCollisionBetweenUpstreamPeers(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	sourceA, _ := identity.NewAgentID()
	sourceB, _ := identity.NewAgentID()
	hopA, _ := identity.NewAgentID()
	hopB, _ := identity.NewAgentID()

	// Both upstream peers picked request ID 1 from their own counters.
	const sharedUpstreamID uint64 = 1
	agent.controlMu.Lock()
	agent.forwardedControl[100] = &forwardedControlRequest{
		UpstreamID: sharedUpstreamID, DownstreamID: 100,
		SourcePeer: sourceA, NextHop: hopA, CreatedAt: time.Now(),
	}
	agent.forwardedControl[200] = &forwardedControlRequest{
		UpstreamID: sharedUpstreamID, DownstreamID: 200,
		SourcePeer: sourceB, NextHop: hopB, CreatedAt: time.Now(),
	}
	entries := len(agent.forwardedControl)
	agent.controlMu.Unlock()

	if entries != 2 {
		t.Fatalf("forwardedControl holds %d entries, want 2: the two peers collided", entries)
	}

	deliver := func(from identity.AgentID, requestID uint64) {
		resp := &protocol.ControlResponse{
			RequestID:   requestID,
			ControlType: protocol.ControlTypeStatus,
			Success:     true,
			Data:        []byte("ok"),
		}
		agent.processFrame(from, &protocol.Frame{
			Type:     protocol.FrameControlResponse,
			StreamID: protocol.ControlStreamID,
			Payload:  resp.Encode(),
		})
	}

	// An answer from a peer this request was not forwarded to is discarded and
	// must not consume the entry.
	deliver(hopB, 100)
	agent.controlMu.Lock()
	_, stillThere := agent.forwardedControl[100]
	agent.controlMu.Unlock()
	if !stillThere {
		t.Error("a response from the wrong peer consumed the forwarded entry")
	}

	// The peer it was forwarded to completes it, leaving the other untouched.
	deliver(hopA, 100)
	agent.controlMu.Lock()
	_, aGone := agent.forwardedControl[100]
	_, bStillThere := agent.forwardedControl[200]
	agent.controlMu.Unlock()
	if aGone {
		t.Error("the genuine response did not consume its forwarded entry")
	}
	if !bStillThere {
		t.Error("completing one peer's request disturbed the other's")
	}
}
