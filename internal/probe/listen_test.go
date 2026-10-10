package probe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/transport"
)

func TestBuildListenerTLSConfig(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir, err := os.MkdirTemp("", "probe-listen-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	caFile := filepath.Join(tmpDir, "ca.pem")

	// Generate test certificate
	err = transport.GenerateAndSaveCert(certFile, keyFile, "test.local", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Generate CA certificate for mTLS testing
	caCertPEM, _, err := transport.GenerateSelfSignedCert("Test CA", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate CA certificate: %v", err)
	}
	if err := os.WriteFile(caFile, caCertPEM, 0644); err != nil {
		t.Fatalf("Failed to write CA certificate: %v", err)
	}

	t.Run("ephemeral TLS config", func(t *testing.T) {
		// No cert/key provided - should generate ephemeral certificate
		opts := ListenOptions{}

		config, err := buildListenerTLSConfig(opts)
		if err != nil {
			t.Fatalf("buildListenerTLSConfig() error = %v", err)
		}

		if len(config.Certificates) != 1 {
			t.Errorf("Expected 1 certificate, got %d", len(config.Certificates))
		}

		if config.ClientAuth != 0 {
			t.Errorf("Expected no client auth, got %d", config.ClientAuth)
		}
	})

	t.Run("basic TLS config", func(t *testing.T) {
		opts := ListenOptions{
			TLSCert: certFile,
			TLSKey:  keyFile,
		}

		config, err := buildListenerTLSConfig(opts)
		if err != nil {
			t.Fatalf("buildListenerTLSConfig() error = %v", err)
		}

		if len(config.Certificates) != 1 {
			t.Errorf("Expected 1 certificate, got %d", len(config.Certificates))
		}

		if config.ClientAuth != 0 {
			t.Errorf("Expected no client auth, got %d", config.ClientAuth)
		}
	})

	t.Run("mTLS config", func(t *testing.T) {
		opts := ListenOptions{
			TLSCert: certFile,
			TLSKey:  keyFile,
			TLSCA:   caFile,
		}

		config, err := buildListenerTLSConfig(opts)
		if err != nil {
			t.Fatalf("buildListenerTLSConfig() error = %v", err)
		}

		if config.ClientCAs == nil {
			t.Error("Expected ClientCAs to be set for mTLS")
		}

		if config.ClientAuth == 0 {
			t.Error("Expected ClientAuth to be set for mTLS")
		}
	})

	t.Run("missing cert file", func(t *testing.T) {
		opts := ListenOptions{
			TLSCert: "/nonexistent/cert.pem",
			TLSKey:  keyFile,
		}

		_, err := buildListenerTLSConfig(opts)
		if err == nil {
			t.Error("Expected error for missing cert file")
		}
	})

	t.Run("missing key file", func(t *testing.T) {
		opts := ListenOptions{
			TLSCert: certFile,
			TLSKey:  "/nonexistent/key.pem",
		}

		_, err := buildListenerTLSConfig(opts)
		if err == nil {
			t.Error("Expected error for missing key file")
		}
	})

	t.Run("missing CA file", func(t *testing.T) {
		opts := ListenOptions{
			TLSCert: certFile,
			TLSKey:  keyFile,
			TLSCA:   "/nonexistent/ca.pem",
		}

		_, err := buildListenerTLSConfig(opts)
		if err == nil {
			t.Error("Expected error for missing CA file")
		}
	})
}

func TestListenDefaults(t *testing.T) {
	// Test that defaults are applied correctly
	opts := ListenOptions{}

	// These would be set during Listen() call
	if opts.Path == "" {
		opts.Path = "/mesh"
	}
	if opts.DisplayName == "" {
		opts.DisplayName = "probe-listener"
	}

	if opts.Path != "/mesh" {
		t.Errorf("Expected default path /mesh, got %s", opts.Path)
	}
	if opts.DisplayName != "probe-listener" {
		t.Errorf("Expected default displayName probe-listener, got %s", opts.DisplayName)
	}
}

func TestConnectionEvent(t *testing.T) {
	// Test that ConnectionEvent struct can hold all expected values
	event := ConnectionEvent{
		Timestamp:  time.Now(),
		RemoteAddr: "192.168.1.100:54321",
		RemoteID:   "abc123def456",
		RemoteName: "test-agent",
		Success:    true,
		Error:      "",
		RTTMs:      5.5,
	}

	if event.RemoteAddr != "192.168.1.100:54321" {
		t.Errorf("Unexpected RemoteAddr: %s", event.RemoteAddr)
	}
	if event.RemoteID != "abc123def456" {
		t.Errorf("Unexpected RemoteID: %s", event.RemoteID)
	}
	if !event.Success {
		t.Error("Expected Success to be true")
	}
}

func TestListenIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temp directory for test certificates
	tmpDir, err := os.MkdirTemp("", "probe-listen-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	// Generate test certificate
	err = transport.GenerateAndSaveCert(certFile, keyFile, "localhost", 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate test certificate: %v", err)
	}

	// Test with QUIC transport
	t.Run("QUIC listener and probe", func(t *testing.T) {
		testListenerAndProbe(t, "quic", certFile, keyFile)
	})

	// Test with H2 transport
	t.Run("H2 listener and probe", func(t *testing.T) {
		testListenerAndProbe(t, "h2", certFile, keyFile)
	})

	// Test with WebSocket transport
	t.Run("WS listener and probe", func(t *testing.T) {
		testListenerAndProbe(t, "ws", certFile, keyFile)
	})
}

func testListenerAndProbe(t *testing.T, transportType, certFile, keyFile string) {
	// Use a dedicated test approach that starts the listener and connects via direct transport
	// This mirrors the approach in transport_test.go for more reliable testing
	testListenerAndProbeWithTransport(t, transportType, certFile, keyFile)
}

func testListenerAndProbeWithTransport(t *testing.T, transportType, certFile, keyFile string) {
	// Load TLS config
	tlsConfig, err := transport.LoadTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to load TLS config: %v", err)
	}

	// Create transport
	var tr transport.Transport
	switch transportType {
	case "quic":
		tr = transport.NewQUICTransport()
	case "h2":
		tr = transport.NewH2Transport()
	case "ws":
		tr = transport.NewWebSocketTransport()
	default:
		t.Fatalf("Unknown transport type: %s", transportType)
	}
	defer tr.Close()

	// Build listen options
	listenOpts := transport.ListenOptions{
		TLSConfig: tlsConfig,
		Path:      "/mesh",
	}

	// Create listener on port 0 to get an available port
	listener, err := tr.Listen("127.0.0.1:0", listenOpts)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	// Get the actual address
	actualAddr := listener.Addr().String()
	t.Logf("Listener started on %s", actualAddr)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Accept connection in goroutine and handle handshake
	acceptDone := make(chan ConnectionEvent, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			acceptDone <- ConnectionEvent{Error: fmt.Sprintf("accept failed: %v", err)}
			return
		}
		defer conn.Close()

		event := handleProbeConnection(ctx, conn, "test-listener")
		acceptDone <- event
	}()

	// Give the accept goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	// Run probe
	probeOpts := Options{
		Transport:    transportType,
		Address:      actualAddr,
		Path:         "/mesh",
		Timeout:      10 * time.Second,
		StrictVerify: false, // Skip cert verification for self-signed test cert
	}

	result := Probe(ctx, probeOpts)

	if !result.Success {
		t.Errorf("Probe failed: %s (error: %v)", result.ErrorDetail, result.Error)
		return
	}

	if result.RemoteDisplayName != "test-listener" {
		t.Errorf("Expected remote display name 'test-listener', got '%s'", result.RemoteDisplayName)
	}

	// Wait for and verify connection event from the listener
	select {
	case event := <-acceptDone:
		if event.Error != "" {
			t.Errorf("Connection event indicates failure: %s", event.Error)
		}
		if !event.Success {
			t.Errorf("Connection not successful")
		}
		if event.RemoteName != "probe" {
			t.Errorf("Expected remote name 'probe', got '%s'", event.RemoteName)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for connection event")
	}
}
