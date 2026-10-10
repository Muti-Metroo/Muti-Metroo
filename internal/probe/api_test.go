package probe

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/transport"
)

func TestRequestValidate(t *testing.T) {
	caCertPEM, _, err := transport.GenerateSelfSignedCert("Test CA", time.Hour)
	if err != nil {
		t.Fatalf("failed to generate CA: %v", err)
	}
	clientCertPEM, clientKeyPEM, err := transport.GenerateSelfSignedCert("client", time.Hour)
	if err != nil {
		t.Fatalf("failed to generate client cert: %v", err)
	}

	cases := []struct {
		name    string
		req     Request
		wantErr string // substring; empty => expect success
		check   func(*testing.T, *Request)
	}{
		{
			name:    "missing address",
			req:     Request{Transport: "quic"},
			wantErr: "address is required",
		},
		{
			name:    "invalid transport",
			req:     Request{Transport: "foo", Address: "x:1"},
			wantErr: "invalid transport",
		},
		{
			name:    "plaintext with quic rejected",
			req:     Request{Transport: "quic", Address: "x:1", PlainText: true},
			wantErr: "plaintext mode is only supported for the ws transport",
		},
		{
			name: "plaintext with ws ok",
			req:  Request{Transport: "ws", Address: "x:1", PlainText: true},
		},
		{
			name:    "negative timeout",
			req:     Request{Address: "x:1", TimeoutMs: -1},
			wantErr: "timeout_ms must be between",
		},
		{
			name:    "timeout above cap",
			req:     Request{Address: "x:1", TimeoutMs: 60001},
			wantErr: "timeout_ms must be between",
		},
		{
			name:    "malformed CA pem",
			req:     Request{Address: "x:1", CACertPEM: "not pem"},
			wantErr: "ca_cert_pem must be a CERTIFICATE PEM block",
		},
		{
			name: "CA pem with wrong block type rejected",
			req: Request{
				Address:   "x:1",
				CACertPEM: "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIA==\n-----END PRIVATE KEY-----\n",
			},
			wantErr: "ca_cert_pem must be a CERTIFICATE PEM block",
		},
		{
			name: "client cert pem with wrong block type rejected",
			req: Request{
				Address:       "x:1",
				ClientCertPEM: "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIA==\n-----END PRIVATE KEY-----\n",
				ClientKeyPEM:  string(clientKeyPEM),
			},
			wantErr: "client_cert_pem must be a CERTIFICATE PEM block",
		},
		{
			name:    "client cert without key",
			req:     Request{Address: "x:1", ClientCertPEM: string(clientCertPEM)},
			wantErr: "client_cert_pem and client_key_pem must be provided together",
		},
		{
			name: "valid client cert and key",
			req: Request{
				Address:       "x:1",
				ClientCertPEM: string(clientCertPEM),
				ClientKeyPEM:  string(clientKeyPEM),
			},
		},
		{
			name: "valid CA pem",
			req:  Request{Address: "x:1", CACertPEM: string(caCertPEM)},
		},
		{
			name: "defaults applied",
			req:  Request{Address: "x:1"},
			check: func(t *testing.T, r *Request) {
				if r.Transport != "quic" {
					t.Errorf("default transport: got %q, want %q", r.Transport, "quic")
				}
				if r.TimeoutMs != defaultRequestTimeoutMs {
					t.Errorf("default timeout: got %d, want %d", r.TimeoutMs, defaultRequestTimeoutMs)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				if tc.check != nil {
					tc.check(t, &tc.req)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestOptionsFromRequest(t *testing.T) {
	req := Request{
		Transport:     "ws",
		Address:       "host:443",
		Path:          "/mesh",
		TimeoutMs:     5000,
		StrictVerify:  true,
		PlainText:     true,
		CACertPEM:     "ca",
		ClientCertPEM: "cc",
		ClientKeyPEM:  "ck",
		ALPNProtocol:  "alpn",
		HTTPHeader:    "X-H",
		WSSubprotocol: "ws-sub",
	}
	opts := OptionsFromRequest(&req)

	if opts.Transport != "ws" || opts.Address != "host:443" || opts.Path != "/mesh" {
		t.Errorf("transport/address/path mismatch: %+v", opts)
	}
	if opts.Timeout != 5*time.Second {
		t.Errorf("timeout: got %v, want 5s", opts.Timeout)
	}
	if !opts.StrictVerify || !opts.PlainText {
		t.Errorf("flags lost: strict=%v plaintext=%v", opts.StrictVerify, opts.PlainText)
	}
	if string(opts.CACertPEM) != "ca" || string(opts.ClientCertPEM) != "cc" || string(opts.ClientKeyPEM) != "ck" {
		t.Errorf("PEM fields lost: %+v", opts)
	}
	if opts.ALPNProtocol != "alpn" || opts.HTTPHeader != "X-H" || opts.WSSubprotocol != "ws-sub" {
		t.Errorf("protocol identifier fields lost: %+v", opts)
	}
}

func TestResponseFromResult(t *testing.T) {
	t.Run("success populates rtt", func(t *testing.T) {
		res := &Result{
			Success:           true,
			Transport:         "quic",
			Address:           "h:1",
			RemoteID:          "abc",
			RemoteDisplayName: "agent-x",
			RTT:               42 * time.Millisecond,
		}
		out := ResponseFromResult(res)
		if !out.Success || out.RTTMs != 42 {
			t.Errorf("unexpected response: %+v", out)
		}
		if out.Error != "" {
			t.Errorf("expected no error on success, got %q", out.Error)
		}
	})

	t.Run("failure prefers ErrorDetail", func(t *testing.T) {
		res := &Result{
			Success:     false,
			Transport:   "quic",
			Address:     "h:1",
			Error:       errors.New("raw"),
			ErrorDetail: "nice classified message",
		}
		out := ResponseFromResult(res)
		if out.Success {
			t.Error("expected success=false")
		}
		if out.Error != "nice classified message" {
			t.Errorf("expected ErrorDetail to win, got %q", out.Error)
		}
		if out.RTTMs != 0 {
			t.Errorf("expected zero rtt on failure, got %v", out.RTTMs)
		}
	})

	t.Run("failure falls back to raw error", func(t *testing.T) {
		res := &Result{
			Success:   false,
			Transport: "quic",
			Address:   "h:1",
			Error:     errors.New("raw error string"),
		}
		out := ResponseFromResult(res)
		if out.Error != "raw error string" {
			t.Errorf("expected raw error to be used, got %q", out.Error)
		}
	})
}
