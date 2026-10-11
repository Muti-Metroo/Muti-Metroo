// Package certutil provides TLS certificate generation and management utilities.
package certutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CertType represents the type of certificate to generate.
type CertType int

const (
	// CertTypeCA is a certificate authority certificate.
	CertTypeCA CertType = iota
	// CertTypeServer is a server certificate.
	CertTypeServer
	// CertTypeClient is a client certificate.
	CertTypeClient
	// CertTypePeer is a peer certificate (both server and client).
	CertTypePeer
)

// CertOptions configures certificate generation.
type CertOptions struct {
	// CommonName is the CN field (required).
	CommonName string

	// Organization for the certificate subject.
	Organization string

	// ValidFor is the certificate validity duration.
	ValidFor time.Duration

	// DNSNames are additional DNS SANs.
	DNSNames []string

	// IPAddresses are IP SANs.
	IPAddresses []net.IP

	// CertType determines the key usage and extensions.
	CertType CertType

	// IsCA indicates if this is a CA certificate.
	IsCA bool

	// MaxPathLen for CA certificates (-1 for no limit, 0 for end-entity only).
	MaxPathLen int

	// Parent CA certificate and key for signing (nil for self-signed).
	ParentCert *x509.Certificate
	ParentKey  *ecdsa.PrivateKey
}

// DefaultCAOptions returns default options for a CA certificate.
func DefaultCAOptions(commonName string) CertOptions {
	return CertOptions{
		CommonName:   commonName,
		Organization: "Muti Metroo",
		ValidFor:     365 * 24 * time.Hour, // 1 year
		CertType:     CertTypeCA,
		IsCA:         true,
		MaxPathLen:   1, // Can sign end-entity certs
	}
}

// DefaultServerOptions returns default options for a server certificate.
func DefaultServerOptions(commonName string) CertOptions {
	return CertOptions{
		CommonName:   commonName,
		Organization: "Muti Metroo",
		ValidFor:     90 * 24 * time.Hour, // 90 days
		DNSNames:     []string{commonName, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		CertType:     CertTypeServer,
	}
}

// DefaultClientOptions returns default options for a client certificate.
func DefaultClientOptions(commonName string) CertOptions {
	return CertOptions{
		CommonName:   commonName,
		Organization: "Muti Metroo",
		ValidFor:     90 * 24 * time.Hour, // 90 days
		CertType:     CertTypeClient,
	}
}

// DefaultPeerOptions returns default options for a peer certificate (server+client).
func DefaultPeerOptions(commonName string) CertOptions {
	return CertOptions{
		CommonName:   commonName,
		Organization: "Muti Metroo",
		ValidFor:     90 * 24 * time.Hour, // 90 days
		DNSNames:     []string{commonName, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		CertType:     CertTypePeer,
	}
}

// GeneratedCert contains the generated certificate and private key.
type GeneratedCert struct {
	// Certificate is the parsed X.509 certificate.
	Certificate *x509.Certificate

	// PrivateKey is the ECDSA private key.
	PrivateKey *ecdsa.PrivateKey

	// CertPEM is the PEM-encoded certificate.
	CertPEM []byte

	// KeyPEM is the PEM-encoded private key.
	KeyPEM []byte
}

// Fingerprint returns the SHA256 fingerprint of the certificate.
func (gc *GeneratedCert) Fingerprint() string {
	hash := sha256.Sum256(gc.Certificate.Raw)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// TLSCertificate returns a tls.Certificate from the generated cert.
func (gc *GeneratedCert) TLSCertificate() (tls.Certificate, error) {
	return tls.X509KeyPair(gc.CertPEM, gc.KeyPEM)
}

// SaveToFiles saves the certificate and key to files.
func (gc *GeneratedCert) SaveToFiles(certPath, keyPath string) error {
	// Ensure directories exist
	if dir := filepath.Dir(certPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create cert directory: %w", err)
		}
	}
	if dir := filepath.Dir(keyPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create key directory: %w", err)
		}
	}

	// Write certificate (readable)
	if err := os.WriteFile(certPath, gc.CertPEM, 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write key (private)
	if err := os.WriteFile(keyPath, gc.KeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// GenerateCert generates a certificate with the given options.
func GenerateCert(opts CertOptions) (*GeneratedCert, error) {
	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   opts.CommonName,
			Organization: []string{opts.Organization},
		},
		NotBefore:             now,
		NotAfter:              now.Add(opts.ValidFor),
		BasicConstraintsValid: true,
		DNSNames:              opts.DNSNames,
		IPAddresses:           opts.IPAddresses,
	}

	// Set key usage based on certificate type
	switch opts.CertType {
	case CertTypeCA:
		template.IsCA = true
		template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
		template.MaxPathLen = opts.MaxPathLen
		template.MaxPathLenZero = opts.MaxPathLen == 0
	case CertTypeServer:
		template.KeyUsage = x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	case CertTypeClient:
		template.KeyUsage = x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	case CertTypePeer:
		template.KeyUsage = x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	}

	// Determine parent certificate and key
	parent := &template
	signingKey := privateKey
	if opts.ParentCert != nil && opts.ParentKey != nil {
		parent = opts.ParentCert
		signingKey = opts.ParentKey
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, parent, &privateKey.PublicKey, signingKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Parse the created certificate
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	return &GeneratedCert{
		Certificate: cert,
		PrivateKey:  privateKey,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
	}, nil
}

// GenerateCA generates a CA certificate.
func GenerateCA(commonName string, validFor time.Duration) (*GeneratedCert, error) {
	opts := DefaultCAOptions(commonName)
	opts.ValidFor = validFor
	return GenerateCert(opts)
}

// GenerateAgentCert generates an agent certificate signed by a CA.
func GenerateAgentCert(commonName string, validFor time.Duration, ca *GeneratedCert) (*GeneratedCert, error) {
	opts := DefaultPeerOptions(commonName)
	opts.ValidFor = validFor
	opts.ParentCert = ca.Certificate
	opts.ParentKey = ca.PrivateKey
	return GenerateCert(opts)
}

// GenerateClientCert generates a client certificate signed by a CA.
func GenerateClientCert(commonName string, validFor time.Duration, ca *GeneratedCert) (*GeneratedCert, error) {
	opts := DefaultClientOptions(commonName)
	opts.ValidFor = validFor
	opts.ParentCert = ca.Certificate
	opts.ParentKey = ca.PrivateKey
	return GenerateCert(opts)
}

// LoadCert loads a certificate and key from files.
func LoadCert(certPath, keyPath string) (*GeneratedCert, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	return ParseCert(certPEM, keyPEM)
}

// ParseCert parses PEM-encoded certificate and key.
func ParseCert(certPEM, keyPEM []byte) (*GeneratedCert, error) {
	// Parse certificate
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	var privateKey *ecdsa.PrivateKey
	switch keyBlock.Type {
	case "EC PRIVATE KEY":
		privateKey, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not ECDSA")
		}
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", keyBlock.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return &GeneratedCert{
		Certificate: cert,
		PrivateKey:  privateKey,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
	}, nil
}

// Fingerprint calculates the SHA256 fingerprint of a certificate.
func Fingerprint(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// FingerprintFromPEM calculates the fingerprint from PEM-encoded certificate.
func FingerprintFromPEM(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse certificate: %w", err)
	}

	return Fingerprint(cert), nil
}

// FingerprintFromFile calculates the fingerprint from a certificate file.
func FingerprintFromFile(certPath string) (string, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("failed to read certificate: %w", err)
	}

	return FingerprintFromPEM(certPEM)
}

// VerifyFingerprint verifies that a certificate matches the expected fingerprint.
func VerifyFingerprint(cert *x509.Certificate, expectedFingerprint string) bool {
	actual := Fingerprint(cert)
	return strings.EqualFold(actual, expectedFingerprint)
}

// ValidateECCertificate validates that a certificate uses ECDSA (EC) public key.
// Returns an error if the certificate uses RSA or another algorithm.
func ValidateECCertificate(certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	switch cert.PublicKeyAlgorithm {
	case x509.ECDSA:
		return nil
	case x509.RSA:
		return fmt.Errorf("RSA certificates are not supported; use EC (ECDSA) certificates")
	case x509.Ed25519:
		return fmt.Errorf("Ed25519 certificates are not supported; use EC (ECDSA) certificates")
	default:
		return fmt.Errorf("unsupported certificate algorithm: %v", cert.PublicKeyAlgorithm)
	}
}

// ValidateECPrivateKey validates that a private key is an ECDSA key.
// Returns an error if the key is RSA or another type.
func ValidateECPrivateKey(keyPEM []byte) error {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return fmt.Errorf("failed to decode private key PEM")
	}

	// Try EC PRIVATE KEY format
	if block.Type == "EC PRIVATE KEY" {
		_, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse EC private key: %w", err)
		}
		return nil
	}

	// Try PKCS#8 format (PRIVATE KEY)
	if block.Type == "PRIVATE KEY" {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse PKCS#8 private key: %w", err)
		}

		switch key.(type) {
		case *ecdsa.PrivateKey:
			return nil
		default:
			return fmt.Errorf("private key is not ECDSA; use EC (ECDSA) keys only")
		}
	}

	// RSA PRIVATE KEY
	if block.Type == "RSA PRIVATE KEY" {
		return fmt.Errorf("RSA private keys are not supported; use EC (ECDSA) keys")
	}

	return fmt.Errorf("unsupported private key type: %s", block.Type)
}

// ValidateECKeyPair validates that both certificate and private key use ECDSA.
func ValidateECKeyPair(certPEM, keyPEM []byte) error {
	if err := ValidateECCertificate(certPEM); err != nil {
		return fmt.Errorf("certificate: %w", err)
	}
	if err := ValidateECPrivateKey(keyPEM); err != nil {
		return fmt.Errorf("private key: %w", err)
	}
	return nil
}

// CertInfo contains certificate information for display.
type CertInfo struct {
	Subject      string
	Issuer       string
	SerialNumber string
	NotBefore    time.Time
	NotAfter     time.Time
	Fingerprint  string
	IsCA         bool
	DNSNames     []string
	IPAddresses  []string
	KeyUsage     []string
	ExtKeyUsage  []string
}

// GetCertInfo extracts information from a certificate.
func GetCertInfo(cert *x509.Certificate) CertInfo {
	info := CertInfo{
		Subject:      cert.Subject.String(),
		Issuer:       cert.Issuer.String(),
		SerialNumber: cert.SerialNumber.Text(16),
		NotBefore:    cert.NotBefore,
		NotAfter:     cert.NotAfter,
		Fingerprint:  Fingerprint(cert),
		IsCA:         cert.IsCA,
		DNSNames:     cert.DNSNames,
	}

	for _, ip := range cert.IPAddresses {
		info.IPAddresses = append(info.IPAddresses, ip.String())
	}

	// Key usage
	if cert.KeyUsage&x509.KeyUsageDigitalSignature != 0 {
		info.KeyUsage = append(info.KeyUsage, "DigitalSignature")
	}
	if cert.KeyUsage&x509.KeyUsageKeyEncipherment != 0 {
		info.KeyUsage = append(info.KeyUsage, "KeyEncipherment")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign != 0 {
		info.KeyUsage = append(info.KeyUsage, "CertSign")
	}
	if cert.KeyUsage&x509.KeyUsageCRLSign != 0 {
		info.KeyUsage = append(info.KeyUsage, "CRLSign")
	}

	// Extended key usage
	for _, eku := range cert.ExtKeyUsage {
		switch eku {
		case x509.ExtKeyUsageServerAuth:
			info.ExtKeyUsage = append(info.ExtKeyUsage, "ServerAuth")
		case x509.ExtKeyUsageClientAuth:
			info.ExtKeyUsage = append(info.ExtKeyUsage, "ClientAuth")
		}
	}

	return info
}

// GetCertInfoFromFile extracts information from a certificate file.
func GetCertInfoFromFile(certPath string) (*CertInfo, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	info := GetCertInfo(cert)
	return &info, nil
}

// IsExpired checks if a certificate is expired.
func IsExpired(cert *x509.Certificate) bool {
	return time.Now().After(cert.NotAfter)
}

// IsExpiringSoon checks if a certificate is expiring within the given duration.
func IsExpiringSoon(cert *x509.Certificate, within time.Duration) bool {
	return time.Now().Add(within).After(cert.NotAfter)
}

// CertExpiryBand classifies how urgently a certificate needs rotation.
// Used by both `muti-metroo cert info` (to render a Status: line) and
// `muti-metroo run` (to print a startup warning) so the bands stay in sync.
type CertExpiryBand int

const (
	// CertValid: more than 60 days remaining.
	CertValid CertExpiryBand = iota
	// CertExpiringPlan: less than 60 days remaining; rotation should be scheduled.
	CertExpiringPlan
	// CertExpiringSoon: less than 30 days remaining; rotation is urgent.
	CertExpiringSoon
	// CertExpired: NotAfter is in the past.
	CertExpired
)

// ClassifyExpiry returns the band and the integer days remaining (negative
// when expired). Pass time.Now() unless you're testing. Both warning bands
// (60 and 30 days) are deliberately fixed -- callers that want different
// thresholds should compute their own bands rather than parameterize this.
func ClassifyExpiry(notAfter time.Time, now time.Time) (CertExpiryBand, int) {
	daysLeft := int(notAfter.Sub(now).Hours() / 24)
	switch {
	case now.After(notAfter):
		return CertExpired, daysLeft
	case daysLeft < 30:
		return CertExpiringSoon, daysLeft
	case daysLeft < 60:
		return CertExpiringPlan, daysLeft
	default:
		return CertValid, daysLeft
	}
}

// CreateCertPool creates a certificate pool from PEM data.
func CreateCertPool(certPEMs ...[]byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	for _, certPEM := range certPEMs {
		if !pool.AppendCertsFromPEM(certPEM) {
			return nil, fmt.Errorf("failed to add certificate to pool")
		}
	}
	return pool, nil
}

// CreateCertPoolFromFiles creates a certificate pool from files.
func CreateCertPoolFromFiles(certPaths ...string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	for _, path := range certPaths {
		certPEM, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		if !pool.AppendCertsFromPEM(certPEM) {
			return nil, fmt.Errorf("failed to add certificate from %s to pool", path)
		}
	}
	return pool, nil
}

// ParsePrivateKeyPEM parses an EC private key from PEM data.
func ParsePrivateKeyPEM(pemData []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PKCS#8 private key: %w", err)
		}
		ecKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not ECDSA")
		}
		return ecKey, nil
	case "RSA PRIVATE KEY":
		return nil, fmt.Errorf("RSA private keys are not supported; use EC (ECDSA) keys")
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
	}
}

// GenerateCACertFromKey generates a self-signed CA certificate from an existing private key.
func GenerateCACertFromKey(key *ecdsa.PrivateKey, commonName string, validityDays int) (*GeneratedCert, error) {
	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	validFor := time.Duration(validityDays) * 24 * time.Hour
	now := time.Now()

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Muti Metroo"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(validFor),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		MaxPathLen:            1,
	}

	// Self-signed: parent is the template itself
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	certPEM := EncodeCertPEM(cert)
	keyPEM := EncodeKeyPEM(key)

	return &GeneratedCert{
		Certificate: cert,
		PrivateKey:  key,
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
	}, nil
}

// EncodeCertPEM encodes a certificate to PEM format.
func EncodeCertPEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
}

// EncodeKeyPEM encodes an EC private key to PEM format.
func EncodeKeyPEM(key *ecdsa.PrivateKey) []byte {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		// This should never happen for valid keys
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})
}
