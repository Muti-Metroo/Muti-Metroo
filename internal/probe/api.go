// Package probe provides connectivity testing for Muti Metroo listeners.
//
// This file defines the JSON-friendly Request/Response types used by the
// HTTP API and the agent-side control handler. Keeping them in this package
// avoids duplicating struct definitions across internal/health and
// internal/agent.
package probe

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"
)

const (
	// Bounds the request-driven probe so a misbehaving caller cannot tie
	// up goroutines indefinitely.
	maxRequestTimeoutMs     = 60000
	defaultRequestTimeoutMs = 10000

	// Grace window added on top of the requested probe timeout when the
	// orchestration helper builds its context. Guards against any future
	// change that lets probe.Probe outlive its own internal deadline.
	probeGracePeriod = 5 * time.Second
)

// Func is the signature of probe.Probe. Used as an injection seam by the
// HTTP handler so tests can stub out the network call.
type Func func(ctx context.Context, opts Options) *Result

// Request is the JSON payload accepted by the probe HTTP endpoints.
type Request struct {
	Transport     string `json:"transport,omitempty"`       // quic|h2|ws (default: quic)
	Address       string `json:"address"`                   // required, host:port
	Path          string `json:"path,omitempty"`            // default: /mesh
	TimeoutMs     int    `json:"timeout_ms,omitempty"`      // default: 10000, max: 60000
	StrictVerify  bool   `json:"strict_verify,omitempty"`   // default: false
	PlainText     bool   `json:"plaintext,omitempty"`       // ws transport only
	CACertPEM     string `json:"ca_cert_pem,omitempty"`     // inline PEM
	ClientCertPEM string `json:"client_cert_pem,omitempty"` // inline PEM
	ClientKeyPEM  string `json:"client_key_pem,omitempty"`  // inline PEM
	ALPNProtocol  string `json:"alpn_protocol,omitempty"`   // protocol identifier override
	HTTPHeader    string `json:"http_header,omitempty"`     // protocol identifier override
	WSSubprotocol string `json:"ws_subprotocol,omitempty"`  // protocol identifier override
}

// Response is the JSON payload returned by the probe HTTP endpoints.
// Field names and types match the CLI probeJSON output exactly so that
// dashboards consuming either source see the same shape.
type Response struct {
	Success           bool    `json:"success"`
	Transport         string  `json:"transport"`
	Address           string  `json:"address"`
	RemoteID          string  `json:"remote_id,omitempty"`
	RemoteDisplayName string  `json:"remote_display_name,omitempty"`
	RTTMs             float64 `json:"rtt_ms,omitempty"`
	Error             string  `json:"error,omitempty"`
}

// Validate normalizes defaults and reports the first user-facing error.
// Callers should treat a non-nil return as HTTP 400 / control-response failure.
func (r *Request) Validate() error {
	if r.Address == "" {
		return fmt.Errorf("address is required")
	}

	if r.Transport == "" {
		r.Transport = "quic"
	}
	switch r.Transport {
	case "quic", "h2", "ws":
	default:
		return fmt.Errorf("invalid transport %q (expected quic, h2, or ws)", r.Transport)
	}

	if r.PlainText && r.Transport != "ws" {
		return fmt.Errorf("plaintext mode is only supported for the ws transport")
	}

	if r.TimeoutMs < 0 || r.TimeoutMs > maxRequestTimeoutMs {
		return fmt.Errorf("timeout_ms must be between 0 and %d", maxRequestTimeoutMs)
	}
	if r.TimeoutMs == 0 {
		r.TimeoutMs = defaultRequestTimeoutMs
	}

	if r.CACertPEM != "" {
		if !hasPEMCertificate([]byte(r.CACertPEM)) {
			return fmt.Errorf("ca_cert_pem must be a CERTIFICATE PEM block")
		}
	}
	if r.ClientCertPEM != "" {
		if !hasPEMCertificate([]byte(r.ClientCertPEM)) {
			return fmt.Errorf("client_cert_pem must be a CERTIFICATE PEM block")
		}
	}
	if r.ClientKeyPEM != "" {
		if !hasPEMBlock([]byte(r.ClientKeyPEM)) {
			return fmt.Errorf("client_key_pem is not a valid PEM block")
		}
	}
	if (r.ClientCertPEM == "") != (r.ClientKeyPEM == "") {
		return fmt.Errorf("client_cert_pem and client_key_pem must be provided together")
	}

	return nil
}

// OptionsFromRequest converts a validated Request into probe.Options.
// Validate must be called first.
func OptionsFromRequest(r *Request) Options {
	return Options{
		Transport:     r.Transport,
		Address:       r.Address,
		Path:          r.Path,
		Timeout:       time.Duration(r.TimeoutMs) * time.Millisecond,
		StrictVerify:  r.StrictVerify,
		PlainText:     r.PlainText,
		CACertPEM:     []byte(r.CACertPEM),
		ClientCertPEM: []byte(r.ClientCertPEM),
		ClientKeyPEM:  []byte(r.ClientKeyPEM),
		ALPNProtocol:  r.ALPNProtocol,
		HTTPHeader:    r.HTTPHeader,
		WSSubprotocol: r.WSSubprotocol,
	}
}

// ResponseFromResult converts a probe.Result into the wire Response.
func ResponseFromResult(res *Result) Response {
	out := Response{
		Success:           res.Success,
		Transport:         res.Transport,
		Address:           res.Address,
		RemoteID:          res.RemoteID,
		RemoteDisplayName: res.RemoteDisplayName,
	}
	if res.Success {
		out.RTTMs = float64(res.RTT.Milliseconds())
	}
	if res.ErrorDetail != "" {
		out.Error = res.ErrorDetail
	} else if res.Error != nil {
		out.Error = res.Error.Error()
	}
	return out
}

// hasPEMBlock reports whether the input contains at least one parsable PEM
// block. Callers may supply CERTIFICATE, PRIVATE KEY, EC PRIVATE KEY, etc.,
// so the block type is not constrained. Private key blocks are not deeply
// parsed here because tls.X509KeyPair handles every key format the standard
// library knows about and will surface its own error during buildTLSConfig.
func hasPEMBlock(data []byte) bool {
	block, _ := pem.Decode(data)
	return block != nil
}

// hasPEMCertificate reports whether the input begins with a PEM block whose
// Type is CERTIFICATE and whose bytes parse as an x509 certificate. Used for
// inputs that feed x509.NewCertPool().AppendCertsFromPEM or tls.X509KeyPair,
// which both require CERTIFICATE blocks and would otherwise emit a less
// specific error from inside buildTLSConfig.
func hasPEMCertificate(data []byte) bool {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return false
	}
	_, err := x509.ParseCertificate(block.Bytes)
	return err == nil
}

// HandleRequest is the shared orchestration used by both the HTTP handler
// and the agent-side control handler. It decodes the JSON body into a
// Request, validates it, and runs the probe via fn.
//
// Returns (responseBody, true) when the probe ran to completion -- including
// the case where the probe itself failed; that failure is reflected by
// Response.Success=false in the body. Returns (errorBody, false) when the
// request itself was malformed or invalid; the body is a {"error": "..."}
// object suitable for an HTTP 4xx response.
func HandleRequest(ctx context.Context, body []byte, fn Func) ([]byte, bool) {
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		out, _ := json.Marshal(map[string]string{"error": "invalid request: " + err.Error()})
		return out, false
	}
	if err := req.Validate(); err != nil {
		out, _ := json.Marshal(map[string]string{"error": err.Error()})
		return out, false
	}

	opts := OptionsFromRequest(&req)
	probeCtx, cancel := context.WithTimeout(ctx, opts.Timeout+probeGracePeriod)
	defer cancel()

	result := fn(probeCtx, opts)
	out, _ := json.Marshal(ResponseFromResult(result))
	return out, true
}
