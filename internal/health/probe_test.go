package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/probe"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"

	"golang.org/x/crypto/bcrypt"
)

// newProbeServer returns a server with the dashboard endpoint enabled and an
// injected probeFunc. Tests pass a stub to assert what was called and shape
// the response without needing a real listener.
func newProbeServer(t *testing.T, fn ProbeFunc) *Server {
	t.Helper()
	cfg := DefaultServerConfig()
	s := NewServer(cfg, &mockStatsProvider{running: true})
	if fn != nil {
		s.probeFunc = fn
	}
	return s
}

func decodeProbeResponse(t *testing.T, rec *httptest.ResponseRecorder) probe.Response {
	t.Helper()
	var got probe.Response
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v (raw=%q)", err, rec.Body.String())
	}
	return got
}

func decodeProbeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v (raw=%q)", err, rec.Body.String())
	}
	return body["error"]
}

func TestHandleProbe_Success(t *testing.T) {
	var calledOpts probe.Options
	stub := func(ctx context.Context, opts probe.Options) *probe.Result {
		calledOpts = opts
		return &probe.Result{
			Success:           true,
			Transport:         opts.Transport,
			Address:           opts.Address,
			RemoteID:          "abcd1234",
			RemoteDisplayName: "agent-x",
			RTT:               7 * time.Millisecond,
		}
	}
	s := newProbeServer(t, stub)

	body := strings.NewReader(`{"transport":"quic","address":"agent2:4433"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeProbeResponse(t, rec)
	if !got.Success || got.RTTMs != 7 || got.RemoteID != "abcd1234" || got.RemoteDisplayName != "agent-x" {
		t.Errorf("unexpected response: %+v", got)
	}
	if calledOpts.Transport != "quic" || calledOpts.Address != "agent2:4433" {
		t.Errorf("probeFunc received wrong opts: %+v", calledOpts)
	}
}

func TestHandleProbe_HandshakeFailure(t *testing.T) {
	stub := func(ctx context.Context, opts probe.Options) *probe.Result {
		return &probe.Result{
			Success:     false,
			Transport:   opts.Transport,
			Address:     opts.Address,
			ErrorDetail: "Connection refused - listener not running or port blocked",
		}
	}
	s := newProbeServer(t, stub)

	body := strings.NewReader(`{"transport":"quic","address":"127.0.0.1:1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	// Probe ran to completion; failure lives in the body, not the HTTP status.
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	got := decodeProbeResponse(t, rec)
	if got.Success {
		t.Error("expected success=false")
	}
	if !strings.Contains(got.Error, "Connection refused") {
		t.Errorf("expected classified error, got %q", got.Error)
	}
}

func TestHandleProbe_InvalidTransport(t *testing.T) {
	s := newProbeServer(t, nil)

	body := strings.NewReader(`{"transport":"foo","address":"x:1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
	if !strings.Contains(decodeProbeError(t, rec), "invalid transport") {
		t.Errorf("expected invalid-transport error, got %q", rec.Body.String())
	}
}

func TestHandleProbe_PlaintextRequiresWS(t *testing.T) {
	s := newProbeServer(t, nil)

	body := strings.NewReader(`{"transport":"quic","address":"x:1","plaintext":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestHandleProbe_MalformedPEM(t *testing.T) {
	s := newProbeServer(t, nil)

	body := strings.NewReader(`{"transport":"quic","address":"x:1","ca_cert_pem":"not pem"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestHandleProbe_MalformedJSON(t *testing.T) {
	s := newProbeServer(t, nil)

	body := strings.NewReader(`{not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestHandleProbe_MethodNotAllowed(t *testing.T) {
	s := newProbeServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/probe", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want 405", rec.Code)
	}
}

func TestHandleProbe_DisabledReturns404(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.EnableDashboard = false
	s := NewServer(cfg, &mockStatsProvider{running: true})

	body := strings.NewReader(`{"transport":"quic","address":"x:1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/probe", body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

func TestHandleProbe_AuthRequired(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	cfg := DefaultServerConfig()
	cfg.TokenHash = string(hash)
	s := NewServer(cfg, &mockStatsProvider{running: true})

	t.Run("no token rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/probe", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", rec.Code)
		}
	})

	t.Run("valid token allowed", func(t *testing.T) {
		s.probeFunc = func(ctx context.Context, opts probe.Options) *probe.Result {
			return &probe.Result{Success: true, Transport: opts.Transport, Address: opts.Address}
		}
		req := httptest.NewRequest(http.MethodPost, "/api/probe",
			strings.NewReader(`{"transport":"quic","address":"x:1"}`))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandleRemoteProbe_ForwardsControl(t *testing.T) {
	s := newProbeServer(t, nil)

	stubResp := `{"success":true,"transport":"quic","address":"agent2:4433","remote_id":"deadbeef","rtt_ms":3}`
	captured := &capturedRemoteProvider{
		response: &protocol.ControlResponse{Success: true, Data: []byte(stubResp)},
	}
	s.SetRemoteProvider(captured)

	target, _ := identity.NewAgentID()
	url := "/agents/" + target.String() + "/probe"
	body := strings.NewReader(`{"transport":"quic","address":"agent2:4433"}`)
	req := httptest.NewRequest(http.MethodPost, url, body)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if captured.controlType != protocol.ControlTypeProbe {
		t.Errorf("controlType: got 0x%02x, want 0x%02x", captured.controlType, protocol.ControlTypeProbe)
	}
	if captured.targetID != target {
		t.Errorf("targetID mismatch: got %s, want %s", captured.targetID, target)
	}
	if rec.Body.String() != stubResp {
		t.Errorf("body relay mismatch: got %q, want %q", rec.Body.String(), stubResp)
	}
}

func TestHandleRemoteProbe_DisabledReturns404(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.EnableRemoteAPI = false
	s := NewServer(cfg, &mockStatsProvider{running: true})

	target, _ := identity.NewAgentID()
	url := "/agents/" + target.String() + "/probe"
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

// capturedRemoteProvider is a RemoteStatusProvider that records the last
// SendControlRequestWithData call so handler tests can assert that the right
// control type and target were dispatched.
type capturedRemoteProvider struct {
	mockRemoteStatusProvider
	response    *protocol.ControlResponse
	controlType uint8
	targetID    identity.AgentID
	data        []byte
}

func (c *capturedRemoteProvider) SendControlRequestWithData(ctx context.Context, targetID identity.AgentID, controlType uint8, data []byte) (*protocol.ControlResponse, error) {
	c.controlType = controlType
	c.targetID = targetID
	c.data = data
	return c.response, nil
}
