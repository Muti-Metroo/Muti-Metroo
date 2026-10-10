// Package transport provides network transport implementations for Muti Metroo.
package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	utls "github.com/refraction-networking/utls"
)

// FingerprintPreset represents a TLS fingerprint preset name.
type FingerprintPreset string

const (
	// FingerprintDisabled uses Go standard library TLS (no fingerprint customization).
	FingerprintDisabled FingerprintPreset = "disabled"
	// FingerprintGo uses Go standard library TLS (same as disabled).
	FingerprintGo FingerprintPreset = "go"
	// FingerprintChrome selects a Chrome-like TLS ClientHello profile.
	FingerprintChrome FingerprintPreset = "chrome"
	// FingerprintFirefox selects a Firefox-like TLS ClientHello profile.
	FingerprintFirefox FingerprintPreset = "firefox"
	// FingerprintSafari selects a Safari-like TLS ClientHello profile.
	FingerprintSafari FingerprintPreset = "safari"
	// FingerprintEdge selects a Microsoft Edge-like TLS ClientHello profile.
	FingerprintEdge FingerprintPreset = "edge"
	// FingerprintIOS selects an iOS Safari-like TLS ClientHello profile.
	FingerprintIOS FingerprintPreset = "ios"
	// FingerprintAndroid selects an Android Chrome-like TLS ClientHello profile.
	FingerprintAndroid FingerprintPreset = "android"
	// FingerprintRandom randomizes the ClientHello profile per connection.
	FingerprintRandom FingerprintPreset = "random"
)

// fingerprintClientHelloIDs maps preset names to uTLS ClientHelloIDs.
var fingerprintClientHelloIDs = map[FingerprintPreset]utls.ClientHelloID{
	FingerprintChrome:   utls.HelloChrome_Auto,
	FingerprintFirefox:  utls.HelloFirefox_Auto,
	FingerprintSafari:   utls.HelloSafari_Auto,
	FingerprintEdge:     utls.HelloEdge_Auto,
	FingerprintIOS:      utls.HelloIOS_Auto,
	FingerprintAndroid:  utls.HelloAndroid_11_OkHttp,
	FingerprintRandom:   utls.HelloRandomized,
	FingerprintGo:       utls.HelloGolang,
	FingerprintDisabled: utls.HelloGolang,
	"":                  utls.HelloGolang,
}

// GetClientHelloID returns the uTLS ClientHelloID for the given preset.
// Returns HelloGolang (standard Go TLS) for unknown presets.
func GetClientHelloID(preset string) utls.ClientHelloID {
	if id, ok := fingerprintClientHelloIDs[FingerprintPreset(preset)]; ok {
		return id
	}
	return utls.HelloGolang
}

// IsFingerprintEnabled returns true if the preset enables TLS fingerprinting.
func IsFingerprintEnabled(preset string) bool {
	return preset != "" && preset != string(FingerprintDisabled) && preset != string(FingerprintGo)
}

// utlsConn wraps a uTLS connection to implement net.Conn.
// This wrapper ensures proper behavior for HTTP/2 transport.
type utlsConn struct {
	*utls.UConn
	rawConn net.Conn
}

// ConnectionState returns the TLS connection state.
// Required for http2.Transport to recognize the connection as TLS.
func (c *utlsConn) ConnectionState() tls.ConnectionState {
	// Convert uTLS state to standard TLS state
	state := c.UConn.ConnectionState()
	return tls.ConnectionState{
		Version:                     state.Version,
		HandshakeComplete:           state.HandshakeComplete,
		DidResume:                   state.DidResume,
		CipherSuite:                 state.CipherSuite,
		NegotiatedProtocol:          state.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  state.NegotiatedProtocolIsMutual,
		ServerName:                  state.ServerName,
		PeerCertificates:            state.PeerCertificates,
		VerifiedChains:              state.VerifiedChains,
		SignedCertificateTimestamps: state.SignedCertificateTimestamps,
		OCSPResponse:                state.OCSPResponse,
	}
}

// NetConn returns the underlying network connection.
func (c *utlsConn) NetConn() net.Conn {
	return c.rawConn
}

// DialUTLS dials a TLS connection using uTLS with the specified fingerprint preset.
// If the preset is empty, disabled, or go, it returns nil (caller should use standard TLS).
// The ctx parameter is used for the initial TCP dial timeout.
func DialUTLS(ctx context.Context, network, addr string, tlsConfig *tls.Config, preset string) (net.Conn, error) {
	// Delegate to DialUTLSWithALPN with the existing NextProtos
	return DialUTLSWithALPN(ctx, nil, network, addr, tlsConfig, preset, tlsConfig.NextProtos)
}

// DialUTLSWithALPN dials a TLS connection using uTLS with ALPN protocols specified.
// This is the preferred method for HTTP/2 connections that need h2 ALPN.
// dialer carries the underlying TCP dial settings (e.g. SO_KEEPALIVE); pass nil
// for defaults.
func DialUTLSWithALPN(ctx context.Context, dialer *net.Dialer, network, addr string, tlsConfig *tls.Config, preset string, alpn []string) (net.Conn, error) {
	if !IsFingerprintEnabled(preset) {
		return nil, nil // Signal to caller to use standard TLS
	}

	// Dial the raw TCP connection
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	// Create uTLS config
	utlsConfig := &utls.Config{
		ServerName:         tlsConfig.ServerName,
		InsecureSkipVerify: tlsConfig.InsecureSkipVerify,
		RootCAs:            tlsConfig.RootCAs,
		MinVersion:         tlsConfig.MinVersion,
		MaxVersion:         tlsConfig.MaxVersion,
	}

	// Handle client certificates for mTLS
	if len(tlsConfig.Certificates) > 0 {
		utlsConfig.Certificates = make([]utls.Certificate, len(tlsConfig.Certificates))
		for i, cert := range tlsConfig.Certificates {
			utlsConfig.Certificates[i] = utls.Certificate{
				Certificate: cert.Certificate,
				PrivateKey:  cert.PrivateKey,
				Leaf:        cert.Leaf,
			}
		}
	}

	// Get the ClientHelloID for the preset
	clientHelloID := GetClientHelloID(preset)

	// Create uTLS connection with the specified fingerprint
	uconn := utls.UClient(rawConn, utlsConfig, clientHelloID)

	// Apply ALPN by modifying the extensions before handshake
	// Most browser fingerprints already include common ALPN values (h2, http/1.1)
	// but we set them explicitly to ensure HTTP/2 negotiation works
	if len(alpn) > 0 {
		// Build handshake state to access extensions
		if err := uconn.BuildHandshakeState(); err != nil {
			rawConn.Close()
			return nil, fmt.Errorf("failed to build handshake state: %w", err)
		}

		// Find and modify ALPN extension
		alpnFound := false
		for _, ext := range uconn.Extensions {
			if alpnExt, ok := ext.(*utls.ALPNExtension); ok {
				alpnExt.AlpnProtocols = alpn
				alpnFound = true
				break
			}
		}

		// Add ALPN extension if not present in the preset
		if !alpnFound {
			uconn.Extensions = append(uconn.Extensions, &utls.ALPNExtension{
				AlpnProtocols: alpn,
			})
		}
	}

	// Perform the TLS handshake
	if err := uconn.Handshake(); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("uTLS handshake failed: %w", err)
	}

	return &utlsConn{
		UConn:   uconn,
		rawConn: rawConn,
	}, nil
}
