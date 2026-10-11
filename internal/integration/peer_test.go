// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/agent"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/health"
	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// TestPeer_DynamicAddRemoveList exercises POST /peers/manage with the add,
// list, and remove actions. Covers the dynamic peer management feature:
// starting agent A with NO peers configured, then instructing it over HTTP
// to dial agent B, verifying the handshake completes, and tearing the peer
// down via remove.
func TestPeer_DynamicAddRemoveList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Allocate QUIC ports for both agents.
	ports, err := allocateFreeUDPPorts(2)
	if err != nil {
		t.Fatalf("allocate ports: %v", err)
	}
	addrA, addrB := ports[0], ports[1]

	// Temp data dirs + certs.
	tmpDirA, err := os.MkdirTemp("", "peer-dyn-A-")
	if err != nil {
		t.Fatalf("mkdir A: %v", err)
	}
	defer os.RemoveAll(tmpDirA)
	tmpDirB, err := os.MkdirTemp("", "peer-dyn-B-")
	if err != nil {
		t.Fatalf("mkdir B: %v", err)
	}
	defer os.RemoveAll(tmpDirB)

	certPEMA, keyPEMA, err := transport.GenerateSelfSignedCert("agent-A", 24*time.Hour)
	if err != nil {
		t.Fatalf("gen cert A: %v", err)
	}
	certPEMB, keyPEMB, err := transport.GenerateSelfSignedCert("agent-B", 24*time.Hour)
	if err != nil {
		t.Fatalf("gen cert B: %v", err)
	}
	certFileA := tmpDirA + "/cert.pem"
	keyFileA := tmpDirA + "/key.pem"
	certFileB := tmpDirB + "/cert.pem"
	keyFileB := tmpDirB + "/key.pem"
	for _, w := range []struct {
		path string
		data []byte
	}{
		{certFileA, certPEMA}, {keyFileA, keyPEMA},
		{certFileB, certPEMB}, {keyFileB, keyPEMB},
	} {
		if err := os.WriteFile(w.path, w.data, 0600); err != nil {
			t.Fatalf("write %s: %v", w.path, err)
		}
	}

	// Agent B: listener only, no peers configured. This is the agent we'll
	// dynamically dial from A.
	cfgB := config.Default()
	cfgB.Agent.DataDir = tmpDirB
	cfgB.Agent.LogLevel = "error"
	cfgB.Listeners = []config.ListenerConfig{{
		Transport: "quic",
		Address:   addrB,
		TLS:       config.TLSConfig{Cert: certFileB, Key: keyFileB},
	}}

	agentB, err := agent.New(cfgB)
	if err != nil {
		t.Fatalf("new agent B: %v", err)
	}
	if err := agentB.Start(); err != nil {
		t.Fatalf("start agent B: %v", err)
	}
	defer agentB.Stop()

	// Agent A: listener + HTTP API, NO peers configured initially.
	cfgA := config.Default()
	cfgA.Agent.DataDir = tmpDirA
	cfgA.Agent.LogLevel = "error"
	cfgA.Listeners = []config.ListenerConfig{{
		Transport: "quic",
		Address:   addrA,
		TLS:       config.TLSConfig{Cert: certFileA, Key: keyFileA},
	}}
	cfgA.HTTP.Enabled = true
	cfgA.HTTP.Address = "127.0.0.1:0"
	remoteAPI := true
	cfgA.HTTP.RemoteAPI = &remoteAPI
	// Mutation endpoints (/peers/manage add/remove) gate on the local agent
	// having a management signing private key configured -- otherwise they
	// return 403. The chain test helper sets this on every agent; this
	// standalone test does it inline.
	privHex, pubHex := newChainSigningKey(t)
	cfgA.Management.SigningPrivateKey = privHex
	cfgA.Management.SigningPublicKey = pubHex

	agentA, err := agent.New(cfgA)
	if err != nil {
		t.Fatalf("new agent A: %v", err)
	}
	if err := agentA.Start(); err != nil {
		t.Fatalf("start agent A: %v", err)
	}
	defer agentA.Stop()

	// Sanity: A has 0 peers before we add anything.
	if stats := agentA.Stats(); stats.PeerCount != 0 {
		t.Fatalf("pre-add peer count = %d, want 0", stats.PeerCount)
	}

	httpAddrRaw := agentA.HealthServerAddress()
	if httpAddrRaw == nil {
		t.Fatal("agent A HTTP server address is nil")
	}
	manageURL := "http://" + httpAddrRaw.String() + "/peers/manage"

	// 1. List before add: only the B peer should not appear (nothing configured yet).
	if got := postPeerManage(t, manageURL, map[string]any{"action": "list"}); len(got.Peers) != 0 {
		t.Errorf("initial list: expected 0 peers, got %d (%+v)", len(got.Peers), got.Peers)
	}

	// 2. Dynamically add B as a peer of A.
	addResp := postPeerManage(t, manageURL, map[string]any{
		"action":    "add",
		"address":   addrB,
		"transport": "quic",
	})
	if addResp.Status != "ok" {
		t.Fatalf("add: status %q, message %q", addResp.Status, addResp.Message)
	}

	// 3. Wait for A to complete handshake with B.
	if !waitForPeerCount(agentA, 1, 10*time.Second) {
		t.Fatalf("A did not connect to B within timeout (peer count stayed %d)", agentA.Stats().PeerCount)
	}

	// 4. List should show exactly one dynamic peer, connected, with a real remote ID.
	listResp := postPeerManage(t, manageURL, map[string]any{"action": "list"})
	if len(listResp.Peers) != 1 {
		t.Fatalf("list after add: expected 1 peer, got %d (%+v)", len(listResp.Peers), listResp.Peers)
	}
	p := listResp.Peers[0]
	if p.Address != addrB {
		t.Errorf("peer.Address = %q, want %q", p.Address, addrB)
	}
	if p.Transport != "quic" {
		t.Errorf("peer.Transport = %q, want quic", p.Transport)
	}
	if !p.Dynamic {
		t.Error("peer.Dynamic = false, want true")
	}
	if !p.Connected {
		t.Error("peer.Connected = false, want true (handshake should be complete)")
	}
	if p.RemoteID == "" {
		t.Error("peer.RemoteID is empty, want B's agent ID")
	}
	if got, want := p.RemoteID, agentB.ID().String(); got != want {
		t.Errorf("peer.RemoteID = %q, want %q", got, want)
	}

	// 5. Remove the dynamic peer.
	removeResp := postPeerManage(t, manageURL, map[string]any{
		"action":  "remove",
		"address": addrB,
	})
	if removeResp.Status != "ok" {
		t.Fatalf("remove: status %q, message %q", removeResp.Status, removeResp.Message)
	}

	// 6. A should drop to 0 peers.
	if !waitForPeerCount(agentA, 0, 5*time.Second) {
		t.Errorf("A still has peers after remove: %d", agentA.Stats().PeerCount)
	}

	// 7. List should be empty again.
	if got := postPeerManage(t, manageURL, map[string]any{"action": "list"}); len(got.Peers) != 0 {
		t.Errorf("list after remove: expected 0 peers, got %d", len(got.Peers))
	}

	// Negative: removing an address that is not dynamic should return 400.
	negResp := postPeerManageRaw(t, manageURL, map[string]any{
		"action":  "remove",
		"address": addrB,
	})
	defer negResp.Body.Close()
	if negResp.StatusCode != http.StatusBadRequest {
		t.Errorf("negative remove: expected status %d, got %d", http.StatusBadRequest, negResp.StatusCode)
	}

	// Negative: unknown transport should return 400.
	badTransportResp := postPeerManageRaw(t, manageURL, map[string]any{
		"action":    "add",
		"address":   addrB,
		"transport": "nonsense",
	})
	defer badTransportResp.Body.Close()
	if badTransportResp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad transport: expected status %d, got %d", http.StatusBadRequest, badTransportResp.StatusCode)
	}
}

// waitForPeerCount polls agent.Stats() until PeerCount reaches want or timeout
// elapses. Returns true if the target count was observed.
func waitForPeerCount(a *agent.Agent, want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.Stats().PeerCount == want {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return a.Stats().PeerCount == want
}

// postPeerManage POSTs a JSON body to /peers/manage and decodes the result.
// Fails the test on transport errors or non-OK responses.
func postPeerManage(t *testing.T, url string, body map[string]any) *health.PeerManageResult {
	t.Helper()
	resp := postPeerManageRaw(t, url, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: status %d, body %s", url, resp.StatusCode, respBody)
	}
	var out health.PeerManageResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &out
}

// postPeerManageRaw POSTs a JSON body to /peers/manage and returns the raw
// response without asserting status. Caller must close the body.
func postPeerManageRaw(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}
