package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// mockSetupDefaultsProvider returns a fixed payload for handler tests.
type mockSetupDefaultsProvider struct {
	resp SetupDefaultsResponse
}

func (m *mockSetupDefaultsProvider) GetSetupDefaults() SetupDefaultsResponse {
	return m.resp
}

func TestServer_handleSetupDefaults_Success(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})

	want := SetupDefaultsResponse{
		AgentID:     "a1b2c3d4e5f60718a1b2c3d4e5f60718",
		DisplayName: "test-agent",
		Version:     "9.9.9",
		TLS: SetupDefaultsTLS{
			CAPEM:      "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
			Strict:     true,
			MTLS:       false,
			SelfSigned: false,
		},
		Management: SetupDefaultsManagement{
			Enabled:              true,
			PublicKey:            strings.Repeat("a", 64),
			HasPrivateKey:        true,
			SigningPublicKey:     strings.Repeat("b", 64),
			HasSigningPrivateKey: true,
		},
		Listener: SetupDefaultsListener{
			Transport: "quic",
			Address:   "0.0.0.0:4433",
			Path:      "/mesh",
			Plaintext: false,
		},
		SuggestedPeer: SetupDefaultsSuggestedPeer{
			Address:    "198.51.100.1:4433",
			Transport:  "quic",
			Path:       "/mesh",
			ExpectedID: "a1b2c3d4e5f60718a1b2c3d4e5f60718",
		},
	}
	s.SetSetupDefaultsProvider(&mockSetupDefaultsProvider{resp: want})

	req := httptest.NewRequest(http.MethodGet, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got SetupDefaultsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("response mismatch:\n got  = %+v\n want = %+v", got, want)
	}
}

func TestServer_handleSetupDefaults_Minimal(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})

	want := SetupDefaultsResponse{
		AgentID:     "0000000000000000a1b2c3d4e5f60718",
		DisplayName: "0000000000000000",
		Version:     "dev",
	}
	s.SetSetupDefaultsProvider(&mockSetupDefaultsProvider{resp: want})

	req := httptest.NewRequest(http.MethodGet, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got SetupDefaultsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TLS.CAPEM != "" {
		t.Errorf("TLS.CAPEM = %q, want empty", got.TLS.CAPEM)
	}
	if got.Management.Enabled {
		t.Errorf("Management.Enabled = true, want false")
	}
	if got.Listener.Transport != "" {
		t.Errorf("Listener.Transport = %q, want empty", got.Listener.Transport)
	}
	if got.SuggestedPeer.Address != "" {
		t.Errorf("SuggestedPeer.Address = %q, want empty", got.SuggestedPeer.Address)
	}
}

// TestServer_handleSetupDefaults_NoSecrets asserts that the JSON response
// never contains keys used to represent private material, regardless of
// provider-side bugs. bcrypt hashes (token_hash, password_hash) are
// intentionally excluded from this list: they are safe to propagate.
func TestServer_handleSetupDefaults_NoSecrets(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})

	resp := SetupDefaultsResponse{
		AgentID:     "a1b2c3d4e5f60718a1b2c3d4e5f60718",
		DisplayName: "test-agent",
		Version:     "test",
		TLS: SetupDefaultsTLS{
			CAPEM: "ca",
		},
		Management: SetupDefaultsManagement{
			Enabled:          true,
			PublicKey:        strings.Repeat("a", 64),
			SigningPublicKey: strings.Repeat("b", 64),
		},
	}
	s.SetSetupDefaultsProvider(&mockSetupDefaultsProvider{resp: resp})

	req := httptest.NewRequest(http.MethodGet, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	// Check for JSON-key occurrences ("name":) to avoid false positives from
	// unrelated fields that happen to contain the substring (e.g.
	// has_private_key vs private_key).
	forbidden := []string{
		`"key_pem"`,
		`"cert_pem"`,
		`"private_key"`,
		`"signing_private_key"`,
	}
	for _, key := range forbidden {
		if strings.Contains(body, key) {
			t.Errorf("response body contains forbidden key %s: %s", key, body)
		}
	}
}

func TestServer_handleSetupDefaults_HTTPEnabledWithToken(t *testing.T) {
	hash := "$2a$10$abcdefghijklmnopqrstuvABCDEFGHIJKLMNOPQRSTUVWXYZ123456"
	s := newSetupDefaultsServer(t, SetupDefaultsResponse{
		HTTP: SetupDefaultsHTTP{Enabled: true, TokenHash: hash},
	})

	var got SetupDefaultsResponse
	body := doSetupDefaultsRequest(t, s, &got)
	if !got.HTTP.Enabled {
		t.Errorf("HTTP.Enabled = false, want true")
	}
	if got.HTTP.TokenHash != hash {
		t.Errorf("HTTP.TokenHash = %q, want %q", got.HTTP.TokenHash, hash)
	}
	if !strings.Contains(body, `"token_hash"`) {
		t.Errorf("expected token_hash key in body, got %s", body)
	}
}

func TestServer_handleSetupDefaults_HTTPDisabledNoToken(t *testing.T) {
	s := newSetupDefaultsServer(t, SetupDefaultsResponse{
		HTTP: SetupDefaultsHTTP{Enabled: false},
	})

	var got SetupDefaultsResponse
	body := doSetupDefaultsRequest(t, s, &got)
	if got.HTTP.Enabled {
		t.Errorf("HTTP.Enabled = true, want false")
	}
	if got.HTTP.TokenHash != "" {
		t.Errorf("HTTP.TokenHash = %q, want empty", got.HTTP.TokenHash)
	}
	if strings.Contains(body, `"token_hash"`) {
		t.Errorf("token_hash should be omitted when empty, body: %s", body)
	}
}

func TestServer_handleSetupDefaults_ShellFields(t *testing.T) {
	hash := "$2a$10$shellhashABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghij"
	want := SetupDefaultsShell{
		Enabled:      true,
		Whitelist:    []string{"whoami", "ls"},
		PasswordHash: hash,
	}
	s := newSetupDefaultsServer(t, SetupDefaultsResponse{Shell: want})

	var got SetupDefaultsResponse
	doSetupDefaultsRequest(t, s, &got)
	if got.Shell.Enabled != want.Enabled {
		t.Errorf("Shell.Enabled = %v, want %v", got.Shell.Enabled, want.Enabled)
	}
	if !reflect.DeepEqual(got.Shell.Whitelist, want.Whitelist) {
		t.Errorf("Shell.Whitelist = %v, want %v", got.Shell.Whitelist, want.Whitelist)
	}
	if got.Shell.PasswordHash != want.PasswordHash {
		t.Errorf("Shell.PasswordHash = %q, want %q", got.Shell.PasswordHash, want.PasswordHash)
	}
}

func TestServer_handleSetupDefaults_FileTransferFields(t *testing.T) {
	hash := "$2a$10$fthashABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijkl"
	want := SetupDefaultsFileTransfer{
		Enabled:      true,
		AllowedPaths: []string{"/tmp", "/home/*/uploads"},
		PasswordHash: hash,
	}
	s := newSetupDefaultsServer(t, SetupDefaultsResponse{FileTransfer: want})

	var got SetupDefaultsResponse
	doSetupDefaultsRequest(t, s, &got)
	if got.FileTransfer.Enabled != want.Enabled {
		t.Errorf("FileTransfer.Enabled = %v, want %v", got.FileTransfer.Enabled, want.Enabled)
	}
	if !reflect.DeepEqual(got.FileTransfer.AllowedPaths, want.AllowedPaths) {
		t.Errorf("FileTransfer.AllowedPaths = %v, want %v", got.FileTransfer.AllowedPaths, want.AllowedPaths)
	}
	if got.FileTransfer.PasswordHash != want.PasswordHash {
		t.Errorf("FileTransfer.PasswordHash = %q, want %q", got.FileTransfer.PasswordHash, want.PasswordHash)
	}
}

func newSetupDefaultsServer(t *testing.T, resp SetupDefaultsResponse) *Server {
	t.Helper()
	s := NewServer(DefaultServerConfig(), &mockStatsProvider{running: true})
	s.SetSetupDefaultsProvider(&mockSetupDefaultsProvider{resp: resp})
	return s
}

func doSetupDefaultsRequest(t *testing.T, s *Server, out *SetupDefaultsResponse) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestServer_handleSetupDefaults_ProviderNotSet(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})

	req := httptest.NewRequest(http.MethodGet, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestServer_handleSetupDefaults_MethodNotAllowed(t *testing.T) {
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	s.SetSetupDefaultsProvider(&mockSetupDefaultsProvider{})

	req := httptest.NewRequest(http.MethodPost, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_handleSetupDefaults_DisabledWhenDashboardDisabled(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.EnableDashboard = false
	s := NewServer(cfg, &mockStatsProvider{running: true})
	s.SetSetupDefaultsProvider(&mockSetupDefaultsProvider{})

	req := httptest.NewRequest(http.MethodGet, "/api/setup-defaults", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
