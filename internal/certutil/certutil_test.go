package certutil

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateCA(t *testing.T) {
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	if ca.Certificate == nil {
		t.Fatal("Certificate is nil")
	}
	if ca.PrivateKey == nil {
		t.Fatal("PrivateKey is nil")
	}
	if len(ca.CertPEM) == 0 {
		t.Fatal("CertPEM is empty")
	}
	if len(ca.KeyPEM) == 0 {
		t.Fatal("KeyPEM is empty")
	}

	// Check CA properties
	if !ca.Certificate.IsCA {
		t.Error("Certificate is not marked as CA")
	}
	if ca.Certificate.Subject.CommonName != "Test CA" {
		t.Errorf("CommonName = %q, want %q", ca.Certificate.Subject.CommonName, "Test CA")
	}
	if ca.Certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA should have KeyUsageCertSign")
	}
}

func TestGenerateAgentCert(t *testing.T) {
	// Generate CA first
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate agent certificate
	agent, err := GenerateAgentCert("agent-1", 90*24*time.Hour, ca)
	if err != nil {
		t.Fatalf("GenerateAgentCert failed: %v", err)
	}

	if agent.Certificate == nil {
		t.Fatal("Certificate is nil")
	}
	if agent.Certificate.IsCA {
		t.Error("Agent certificate should not be CA")
	}
	if agent.Certificate.Subject.CommonName != "agent-1" {
		t.Errorf("CommonName = %q, want %q", agent.Certificate.Subject.CommonName, "agent-1")
	}

	// Check it has both server and client auth
	hasServerAuth := false
	hasClientAuth := false
	for _, usage := range agent.Certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
	}
	if !hasServerAuth {
		t.Error("Agent cert should have ServerAuth")
	}
	if !hasClientAuth {
		t.Error("Agent cert should have ClientAuth")
	}

	// Verify the certificate is signed by the CA
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate)
	_, err = agent.Certificate.Verify(x509.VerifyOptions{
		Roots: roots,
	})
	if err != nil {
		t.Errorf("Certificate verification failed: %v", err)
	}
}

func TestGenerateClientCert(t *testing.T) {
	// Generate CA first
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate client certificate
	client, err := GenerateClientCert("client-1", 90*24*time.Hour, ca)
	if err != nil {
		t.Fatalf("GenerateClientCert failed: %v", err)
	}

	if client.Certificate.IsCA {
		t.Error("Client certificate should not be CA")
	}

	// Check it has client auth only
	hasServerAuth := false
	hasClientAuth := false
	for _, usage := range client.Certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
	}
	if hasServerAuth {
		t.Error("Client cert should not have ServerAuth")
	}
	if !hasClientAuth {
		t.Error("Client cert should have ClientAuth")
	}
}

func TestGenerateCertWithOptions(t *testing.T) {
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	opts := CertOptions{
		CommonName:   "server-1",
		Organization: "Test Org",
		ValidFor:     30 * 24 * time.Hour,
		DNSNames:     []string{"server-1.example.com", "server-1.local"},
		IPAddresses:  []net.IP{net.ParseIP("192.168.1.100"), net.ParseIP("10.0.0.1")},
		CertType:     CertTypeServer,
		ParentCert:   ca.Certificate,
		ParentKey:    ca.PrivateKey,
	}

	cert, err := GenerateCert(opts)
	if err != nil {
		t.Fatalf("GenerateCert failed: %v", err)
	}

	// Check DNS names
	if len(cert.Certificate.DNSNames) != 2 {
		t.Errorf("DNSNames length = %d, want 2", len(cert.Certificate.DNSNames))
	}

	// Check IP addresses
	if len(cert.Certificate.IPAddresses) != 2 {
		t.Errorf("IPAddresses length = %d, want 2", len(cert.Certificate.IPAddresses))
	}

	// Check organization
	if len(cert.Certificate.Subject.Organization) == 0 || cert.Certificate.Subject.Organization[0] != "Test Org" {
		t.Error("Organization not set correctly")
	}
}

func TestSaveAndLoadCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")
	keyPath := filepath.Join(tmpDir, "test.key")

	// Generate and save
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	if err := ca.SaveToFiles(certPath, keyPath); err != nil {
		t.Fatalf("SaveToFiles failed: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Error("Certificate file not created")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("Key file not created")
	}

	// Check key file permissions
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Stat key file failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Key file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Load and verify
	loaded, err := LoadCert(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCert failed: %v", err)
	}

	if loaded.Certificate.Subject.CommonName != ca.Certificate.Subject.CommonName {
		t.Error("Loaded certificate CommonName mismatch")
	}
	if loaded.Fingerprint() != ca.Fingerprint() {
		t.Error("Loaded certificate fingerprint mismatch")
	}
}

func TestFingerprint(t *testing.T) {
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	fp := ca.Fingerprint()

	// Check format
	if len(fp) < 10 || fp[:7] != "sha256:" {
		t.Errorf("Fingerprint format invalid: %s", fp)
	}

	// Check consistency
	fp2 := Fingerprint(ca.Certificate)
	if fp != fp2 {
		t.Error("Fingerprint methods return different values")
	}

	// Check verification
	if !VerifyFingerprint(ca.Certificate, fp) {
		t.Error("VerifyFingerprint failed for matching fingerprint")
	}
	if VerifyFingerprint(ca.Certificate, "sha256:invalid") {
		t.Error("VerifyFingerprint should fail for non-matching fingerprint")
	}
}

func TestFingerprintFromPEM(t *testing.T) {
	ca, err := GenerateCA("Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	fp, err := FingerprintFromPEM(ca.CertPEM)
	if err != nil {
		t.Fatalf("FingerprintFromPEM failed: %v", err)
	}

	if fp != ca.Fingerprint() {
		t.Error("FingerprintFromPEM returns different fingerprint")
	}
}

func TestGetCertInfo(t *testing.T) {
	ca, err := GenerateCA("Info Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	info := GetCertInfo(ca.Certificate)

	if info.Subject == "" {
		t.Error("Subject is empty")
	}
	if info.Issuer == "" {
		t.Error("Issuer is empty")
	}
	if !info.IsCA {
		t.Error("IsCA should be true for CA cert")
	}
	if info.Fingerprint != ca.Fingerprint() {
		t.Error("Fingerprint mismatch")
	}
	if info.NotBefore.IsZero() {
		t.Error("NotBefore is zero")
	}
	if info.NotAfter.IsZero() {
		t.Error("NotAfter is zero")
	}
}

func TestIsExpired(t *testing.T) {
	// Generate a very short-lived certificate
	opts := DefaultCAOptions("Short-lived CA")
	opts.ValidFor = 1 * time.Millisecond

	ca, err := GenerateCert(opts)
	if err != nil {
		t.Fatalf("GenerateCert failed: %v", err)
	}

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	if !IsExpired(ca.Certificate) {
		t.Error("Certificate should be expired")
	}

	// Generate a long-lived certificate
	ca2, err := GenerateCA("Long-lived CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	if IsExpired(ca2.Certificate) {
		t.Error("Certificate should not be expired")
	}
}

func TestIsExpiringSoon(t *testing.T) {
	// Generate a certificate that expires in 10 days
	opts := DefaultCAOptions("Soon-expiring CA")
	opts.ValidFor = 10 * 24 * time.Hour

	ca, err := GenerateCert(opts)
	if err != nil {
		t.Fatalf("GenerateCert failed: %v", err)
	}

	// Check if expiring within 30 days (should be true)
	if !IsExpiringSoon(ca.Certificate, 30*24*time.Hour) {
		t.Error("Certificate should be expiring soon (within 30 days)")
	}

	// Check if expiring within 5 days (should be false)
	if IsExpiringSoon(ca.Certificate, 5*24*time.Hour) {
		t.Error("Certificate should not be expiring within 5 days")
	}
}

func TestClassifyExpiry(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		notAfter time.Time
		wantBand CertExpiryBand
		wantDays int
	}{
		{"valid: 1 year out", now.Add(365 * 24 * time.Hour), CertValid, 365},
		{"valid: just past 60 days", now.Add(61 * 24 * time.Hour), CertValid, 61},
		{"plan: 59 days", now.Add(59 * 24 * time.Hour), CertExpiringPlan, 59},
		{"plan: 30 days exactly", now.Add(30 * 24 * time.Hour), CertExpiringPlan, 30},
		{"soon: 29 days", now.Add(29 * 24 * time.Hour), CertExpiringSoon, 29},
		{"soon: 1 day", now.Add(24 * time.Hour), CertExpiringSoon, 1},
		{"soon: 0 days but not yet expired", now.Add(time.Hour), CertExpiringSoon, 0},
		{"expired: 1 second ago", now.Add(-time.Second), CertExpired, 0},
		{"expired: 1 day ago", now.Add(-24 * time.Hour), CertExpired, -1},
		{"expired: 30 days ago", now.Add(-30 * 24 * time.Hour), CertExpired, -30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBand, gotDays := ClassifyExpiry(tc.notAfter, now)
			if gotBand != tc.wantBand {
				t.Errorf("band = %v, want %v", gotBand, tc.wantBand)
			}
			if gotDays != tc.wantDays {
				t.Errorf("days = %d, want %d", gotDays, tc.wantDays)
			}
		})
	}
}

func TestTLSCertificate(t *testing.T) {
	ca, err := GenerateCA("TLS Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	tlsCert, err := ca.TLSCertificate()
	if err != nil {
		t.Fatalf("TLSCertificate failed: %v", err)
	}

	if tlsCert.PrivateKey == nil {
		t.Error("TLS certificate PrivateKey is nil")
	}
	if len(tlsCert.Certificate) == 0 {
		t.Error("TLS certificate has no certificate data")
	}
}

func TestCreateCertPool(t *testing.T) {
	ca1, err := GenerateCA("CA 1", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	ca2, err := GenerateCA("CA 2", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	pool, err := CreateCertPool(ca1.CertPEM, ca2.CertPEM)
	if err != nil {
		t.Fatalf("CreateCertPool failed: %v", err)
	}

	if pool == nil {
		t.Error("Pool is nil")
	}

	// Verify certs can be verified using the pool
	agent, err := GenerateAgentCert("agent", 90*24*time.Hour, ca1)
	if err != nil {
		t.Fatalf("GenerateAgentCert failed: %v", err)
	}

	_, err = agent.Certificate.Verify(x509.VerifyOptions{
		Roots: pool,
	})
	if err != nil {
		t.Errorf("Certificate verification with pool failed: %v", err)
	}
}

func TestParseCert(t *testing.T) {
	ca, err := GenerateCA("Parse Test CA", 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	parsed, err := ParseCert(ca.CertPEM, ca.KeyPEM)
	if err != nil {
		t.Fatalf("ParseCert failed: %v", err)
	}

	if parsed.Certificate.Subject.CommonName != ca.Certificate.Subject.CommonName {
		t.Error("Parsed certificate CommonName mismatch")
	}
}

func TestDefaultOptions(t *testing.T) {
	tests := []struct {
		name     string
		opts     CertOptions
		wantType CertType
		wantCA   bool
	}{
		{
			name:     "DefaultCAOptions",
			opts:     DefaultCAOptions("Test CA"),
			wantType: CertTypeCA,
			wantCA:   true,
		},
		{
			name:     "DefaultServerOptions",
			opts:     DefaultServerOptions("server"),
			wantType: CertTypeServer,
			wantCA:   false,
		},
		{
			name:     "DefaultClientOptions",
			opts:     DefaultClientOptions("client"),
			wantType: CertTypeClient,
			wantCA:   false,
		},
		{
			name:     "DefaultPeerOptions",
			opts:     DefaultPeerOptions("peer"),
			wantType: CertTypePeer,
			wantCA:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.opts.CertType != tt.wantType {
				t.Errorf("CertType = %v, want %v", tt.opts.CertType, tt.wantType)
			}
			if tt.opts.IsCA != tt.wantCA {
				t.Errorf("IsCA = %v, want %v", tt.opts.IsCA, tt.wantCA)
			}
			if tt.opts.Organization != "Muti Metroo" {
				t.Errorf("Organization = %q, want %q", tt.opts.Organization, "Muti Metroo")
			}
		})
	}
}

func TestSelfSignedCert(t *testing.T) {
	// Generate a self-signed server cert (no parent CA)
	opts := DefaultServerOptions("self-signed")
	opts.ParentCert = nil
	opts.ParentKey = nil

	cert, err := GenerateCert(opts)
	if err != nil {
		t.Fatalf("GenerateCert failed: %v", err)
	}

	// Self-signed cert should have same subject and issuer
	if cert.Certificate.Subject.String() != cert.Certificate.Issuer.String() {
		t.Error("Self-signed cert should have same subject and issuer")
	}
}
