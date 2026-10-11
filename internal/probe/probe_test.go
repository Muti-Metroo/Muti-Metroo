package probe

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// TestBuildTLSConfig_InlinePEMPreferredOverPath verifies that when both an
// inline PEM and a file path are provided, the inline PEM takes precedence.
// The path here points to a non-existent file, so a path-only loader would
// fail; success implies inline took precedence.
func TestBuildTLSConfig_InlinePEMPreferredOverPath(t *testing.T) {
	caCertPEM, _, err := transport.GenerateSelfSignedCert("Test CA", time.Hour)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}
	clientCertPEM, clientKeyPEM, err := transport.GenerateSelfSignedCert("client", time.Hour)
	if err != nil {
		t.Fatalf("failed to generate client cert: %v", err)
	}

	opts := Options{
		CACert:        "/nonexistent/ca.crt",
		ClientCert:    "/nonexistent/client.crt",
		ClientKey:     "/nonexistent/client.key",
		CACertPEM:     caCertPEM,
		ClientCertPEM: clientCertPEM,
		ClientKeyPEM:  clientKeyPEM,
		StrictVerify:  true,
	}

	cfg, err := buildTLSConfig(opts)
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v (inline PEM should win over bad paths)", err)
	}
	if cfg.RootCAs == nil {
		t.Error("RootCAs not populated from inline PEM")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("Certificates: got %d, want 1", len(cfg.Certificates))
	}
}

// TestBuildTLSConfig_FilePathFallback verifies the legacy file-path loader
// still works for the CLI.
func TestBuildTLSConfig_FilePathFallback(t *testing.T) {
	tmp, err := os.MkdirTemp("", "probe-tls-fallback-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmp)

	caPath := filepath.Join(tmp, "ca.pem")
	caPEM, _, err := transport.GenerateSelfSignedCert("Test CA", time.Hour)
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	cfg, err := buildTLSConfig(Options{CACert: caPath, StrictVerify: true})
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v", err)
	}
	if cfg.RootCAs == nil {
		t.Error("RootCAs not populated from file path")
	}
}

// TestBuildTLSConfig_MalformedInlinePEM ensures parse failures surface as
// clear errors rather than silently producing a bad TLS config.
func TestBuildTLSConfig_MalformedInlinePEM(t *testing.T) {
	_, err := buildTLSConfig(Options{CACertPEM: []byte("not pem at all")})
	if err == nil {
		t.Fatal("expected error for malformed inline PEM, got nil")
	}
}
