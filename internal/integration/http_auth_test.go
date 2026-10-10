// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"net/http"
	"testing"

	"github.com/postalsys/muti-metroo/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// httpGetWithToken issues a GET against url. If token is non-empty, it is
// sent in the Authorization: Bearer <token> header. Returns the status code.
func httpGetWithToken(t *testing.T, url string, token string) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to build request for %s: %v", url, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// makeBcryptHash returns the bcrypt hash of plaintext at MinCost (= 4),
// which is sufficient for tests and ~10x faster than DefaultCost.
func makeBcryptHash(t *testing.T, plaintext string) string {
	t.Helper()

	h, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	return string(h)
}

// startHTTPChain spins up a 4-agent chain with the HTTP server enabled on
// agent A, applies the optional HTTP config callback, and returns the base
// URL "http://host:port". Cleanup is registered via t.Cleanup. Skips under
// -short. Does NOT wait for route propagation -- the HTTP server is local
// to agent A and ready as soon as StartAgents returns.
func startHTTPChain(t *testing.T, configure func(*config.HTTPConfig)) string {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	chain.EnableHTTP = true
	chain.HTTPConfigure = configure

	chain.CreateAgents(t)
	chain.StartAgents(t)
	t.Cleanup(chain.Close)

	if chain.HTTPAddrs[0] == "" {
		t.Fatal("HTTP server address not captured")
	}
	return "http://" + chain.HTTPAddrs[0]
}

// TestHTTPAuth_BearerHeaderAllowed verifies that a correct token in the
// Authorization: Bearer header lets a request through to a protected endpoint.
// Covers row 169 (Bearer via Authorization header).
func TestHTTPAuth_BearerHeaderAllowed(t *testing.T) {
	const token = "correct-horse-battery-staple"
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		h.TokenHash = makeBcryptHash(t, token)
	})

	if code := httpGetWithToken(t, base+"/api/dashboard", token); code != http.StatusOK {
		t.Fatalf("expected 200 with valid bearer token, got %d", code)
	}
}

// TestHTTPAuth_QueryTokenAllowed verifies that ?token=<correct> is also accepted.
// Covers row 170 (Bearer via ?token= query).
func TestHTTPAuth_QueryTokenAllowed(t *testing.T) {
	const token = "query-string-token"
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		h.TokenHash = makeBcryptHash(t, token)
	})

	if code := httpGetWithToken(t, base+"/api/dashboard?token="+token, ""); code != http.StatusOK {
		t.Fatalf("expected 200 with valid query-string token, got %d", code)
	}
}

// TestHTTPAuth_InvalidTokenRejected verifies that wrong tokens return 401,
// both via header and via query string.
// Covers row 172 (Invalid token rejection).
func TestHTTPAuth_InvalidTokenRejected(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		h.TokenHash = makeBcryptHash(t, "the-real-token")
	})

	if code := httpGetWithToken(t, base+"/api/dashboard", "the-wrong-token"); code != http.StatusUnauthorized {
		t.Errorf("Bearer header with wrong token: expected 401, got %d", code)
	}
	if code := httpGetWithToken(t, base+"/api/dashboard?token=also-wrong", ""); code != http.StatusUnauthorized {
		t.Errorf("Query string with wrong token: expected 401, got %d", code)
	}
}

// TestHTTPAuth_MissingTokenRejected verifies that no token at all returns 401
// when TokenHash is configured.
// Covers row 172 (Invalid token rejection, missing-token subcase).
func TestHTTPAuth_MissingTokenRejected(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		h.TokenHash = makeBcryptHash(t, "any-token")
	})

	if code := httpGetWithToken(t, base+"/api/dashboard", ""); code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no token supplied, got %d", code)
	}
}

// TestHTTPAuth_HealthExempt verifies that health/ready/splash endpoints
// bypass the bearer-token gate even when TokenHash is configured.
// Covers row 171 (Health endpoints exempt from auth).
func TestHTTPAuth_HealthExempt(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		h.TokenHash = makeBcryptHash(t, "ignored-by-health")
	})

	for _, path := range []string{"/health", "/healthz", "/ready", "/", "/logo.png"} {
		if code := httpGetWithToken(t, base+path, ""); code != http.StatusOK {
			t.Errorf("%s with no token: expected 200 (exempt from auth), got %d", path, code)
		}
	}
}

// TestHTTPAuth_MinimalMode verifies that http.minimal=true disables non-health
// endpoints while leaving /health responsive.
// Covers row 173 (Minimal mode).
func TestHTTPAuth_MinimalMode(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		h.Minimal = true
	})

	if code := httpGetWithToken(t, base+"/api/dashboard", ""); code != http.StatusNotFound {
		t.Errorf("/api/dashboard in minimal mode: expected 404, got %d", code)
	}
	if code := httpGetWithToken(t, base+"/health", ""); code != http.StatusOK {
		t.Errorf("/health in minimal mode: expected 200, got %d", code)
	}
}

// TestHTTPAuth_PprofDisabled verifies that http.pprof=false returns 404 for
// pprof endpoints while leaving other endpoint groups intact.
// Covers row 174 (pprof endpoint group toggle).
func TestHTTPAuth_PprofDisabled(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		f := false
		h.Pprof = &f
	})

	if code := httpGetWithToken(t, base+"/debug/pprof/", ""); code != http.StatusNotFound {
		t.Errorf("/debug/pprof/ with pprof=false: expected 404, got %d", code)
	}
	if code := httpGetWithToken(t, base+"/api/dashboard", ""); code != http.StatusOK {
		t.Errorf("/api/dashboard should still be 200 when only pprof is disabled, got %d", code)
	}
}

// TestHTTPAuth_DashboardDisabled verifies that http.dashboard=false returns
// 404 for /api/* endpoints while leaving /agents/* intact.
// Covers row 175 (dashboard endpoint group toggle).
func TestHTTPAuth_DashboardDisabled(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		f := false
		h.Dashboard = &f
	})

	if code := httpGetWithToken(t, base+"/api/dashboard", ""); code != http.StatusNotFound {
		t.Errorf("/api/dashboard with dashboard=false: expected 404, got %d", code)
	}
	if code := httpGetWithToken(t, base+"/agents", ""); code != http.StatusOK {
		t.Errorf("/agents should still be 200 when only dashboard is disabled, got %d", code)
	}
}

// TestHTTPAuth_RemoteAPIDisabled verifies that http.remote_api=false returns
// 404 for /agents/* endpoints while leaving /api/* intact.
// Covers row 176 (remote_api endpoint group toggle).
func TestHTTPAuth_RemoteAPIDisabled(t *testing.T) {
	base := startHTTPChain(t, func(h *config.HTTPConfig) {
		f := false
		h.RemoteAPI = &f
	})

	if code := httpGetWithToken(t, base+"/agents", ""); code != http.StatusNotFound {
		t.Errorf("/agents with remote_api=false: expected 404, got %d", code)
	}
	if code := httpGetWithToken(t, base+"/api/dashboard", ""); code != http.StatusOK {
		t.Errorf("/api/dashboard should still be 200 when only remote_api is disabled, got %d", code)
	}
}
