package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

const (
	// DefaultALPNProtocol is the default ALPN protocol identifier.
	// This can be overridden via config to match local network policy.
	DefaultALPNProtocol = "muti-metroo/1"

	// DefaultHTTPHeader is the default HTTP header for protocol identification.
	DefaultHTTPHeader = "X-Muti-Metroo-Protocol"

	// DefaultWSSubprotocol is the default WebSocket subprotocol.
	DefaultWSSubprotocol = "muti-metroo/1"

	// ALPNProtocol is an alias for DefaultALPNProtocol for backward compatibility.
	// Deprecated: Use DefaultALPNProtocol instead.
	ALPNProtocol = DefaultALPNProtocol
)

// LoadTLSConfig loads a TLS configuration from certificate and key files.
func LoadTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{ALPNProtocol},
	}, nil
}

// LoadClientTLSConfig loads a TLS configuration for client connections.
// If strictVerify is true, peer certificates are validated against the CA.
// If strictVerify is false (default), certificate verification is skipped because
// Muti Metroo uses an E2E encryption layer that provides security.
func LoadClientTLSConfig(caFile string, strictVerify bool) (*tls.Config, error) {
	config := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{ALPNProtocol},
		InsecureSkipVerify: !strictVerify, // Invert: strict=true means verify, strict=false means skip
	}

	if caFile != "" {
		caPool, err := LoadCAPool(caFile)
		if err != nil {
			return nil, err
		}
		config.RootCAs = caPool
	}

	return config, nil
}

// LoadCAPool loads a CA certificate pool from a file.
func LoadCAPool(caFile string) (*x509.CertPool, error) {
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return pool, nil
}

// LoadMutualTLSConfig loads a TLS configuration with client certificate verification.
func LoadMutualTLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	config, err := LoadTLSConfig(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	if clientCAFile != "" {
		clientCAPool, err := LoadCAPool(clientCAFile)
		if err != nil {
			return nil, err
		}
		config.ClientCAs = clientCAPool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return config, nil
}

// GenerateSelfSignedCert generates a self-signed certificate for development.
func GenerateSelfSignedCert(commonName string, validFor time.Duration) (certPEM, keyPEM []byte, err error) {
	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Muti Metroo"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{commonName, "localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	return certPEM, keyPEM, nil
}

// GenerateAndSaveCert generates a self-signed certificate and saves it to files.
func GenerateAndSaveCert(certFile, keyFile, commonName string, validFor time.Duration) error {
	certPEM, keyPEM, err := GenerateSelfSignedCert(commonName, validFor)
	if err != nil {
		return err
	}

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write certificate file: %w", err)
	}

	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}

// TLSConfigFromBytes creates a TLS config from PEM-encoded certificate and key.
func TLSConfigFromBytes(certPEM, keyPEM []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{ALPNProtocol},
	}, nil
}

// CloneTLSConfig creates a copy of a TLS config.
func CloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return nil
	}
	return cfg.Clone()
}

// prepareTLSConfigForDial prepares a TLS config for dialing.
// If tlsConfig is nil, creates a config based on strictVerify setting.
// If strictVerify is false (default), certificate verification is skipped.
// The nextProtos parameter specifies the ALPN protocols to use.
func prepareTLSConfigForDial(tlsConfig *tls.Config, strictVerify bool, nextProtos []string) (*tls.Config, error) {
	if tlsConfig == nil {
		// No TLS config provided - create one based on strict setting
		// Default (strictVerify=false) skips verification, which is safe because
		// the E2E encryption layer provides security
		return &tls.Config{
			InsecureSkipVerify: !strictVerify,
			NextProtos:         nextProtos,
			MinVersion:         tls.VersionTLS13,
		}, nil
	}

	// Clone and set NextProtos on provided config
	cfg := tlsConfig.Clone()
	cfg.NextProtos = nextProtos
	return cfg, nil
}

// ensureH2InNextProtos ensures "h2" is present in TLS NextProtos.
// Returns a cloned config with "h2" prepended if not already present.
func ensureH2InNextProtos(tlsConfig *tls.Config) *tls.Config {
	cfg := tlsConfig.Clone()
	for _, proto := range cfg.NextProtos {
		if proto == "h2" {
			return cfg
		}
	}
	cfg.NextProtos = append([]string{"h2"}, cfg.NextProtos...)
	return cfg
}
