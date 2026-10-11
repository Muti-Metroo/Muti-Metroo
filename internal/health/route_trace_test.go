package health

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockRouteTraceProvider struct {
	called bool
	dest   string
	result *RouteTraceResult
	err    error
}

func (m *mockRouteTraceProvider) TraceRoute(dest string) (*RouteTraceResult, error) {
	m.called = true
	m.dest = dest
	return m.result, m.err
}

func TestServer_handleRouteTrace_Success(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)
	mock := &mockRouteTraceProvider{
		result: &RouteTraceResult{
			Status: "ok",
			Dest:   "8.8.8.8",
			IPs: []*RouteTraceIP{{
				IP: "8.8.8.8",
				Candidates: []*RouteTraceCandidate{
					{Network: "8.8.8.0/24", PrefixLen: 24, Metric: 2, Winner: true},
					{Network: "0.0.0.0/0", PrefixLen: 0, Metric: 1, Reason: "shorter prefix"},
				},
			}},
		},
	}
	s.SetRouteTraceProvider(mock)

	body, _ := json.Marshal(map[string]string{"dest": "8.8.8.8"})
	req := httptest.NewRequest(http.MethodPost, "/route/trace", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	if !mock.called {
		t.Error("provider.TraceRoute was not called")
	}
	if mock.dest != "8.8.8.8" {
		t.Errorf("dest passed = %q, want %q", mock.dest, "8.8.8.8")
	}

	var resp RouteTraceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if len(resp.IPs) != 1 || len(resp.IPs[0].Candidates) != 2 {
		t.Fatalf("unexpected response shape: %+v", resp)
	}
	if !resp.IPs[0].Candidates[0].Winner {
		t.Error("first candidate should be winner")
	}
}

func TestServer_handleRouteTrace_MissingDest(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)
	s.SetRouteTraceProvider(&mockRouteTraceProvider{})

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/route/trace", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
}

func TestServer_handleRouteTrace_ProviderNotSet(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)
	// No SetRouteTraceProvider call.

	body, _ := json.Marshal(map[string]string{"dest": "1.2.3.4"})
	req := httptest.NewRequest(http.MethodPost, "/route/trace", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503. body=%s", rec.Code, rec.Body.String())
	}
}

func TestServer_handleRouteTrace_WrongMethod(t *testing.T) {
	cfg := DefaultServerConfig()
	provider := &mockStatsProvider{running: true}
	s := NewServer(cfg, provider)
	s.SetRouteTraceProvider(&mockRouteTraceProvider{})

	req := httptest.NewRequest(http.MethodGet, "/route/trace", nil)
	rec := httptest.NewRecorder()

	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
