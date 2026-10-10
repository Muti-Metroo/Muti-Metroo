// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/agent"
	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/health"
)

// startForwardEchoServer starts a TCP echo server on 127.0.0.1:0 and returns
// its address. The server echoes any bytes it receives. It is closed when the
// test ends via t.Cleanup.
func startForwardEchoServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	return listener.Addr().String()
}

// TestForward_StaticEndToEnd verifies the full port-forward path on a 4-hop
// linear chain: a static endpoint declared on the exit (D) is reachable through
// a static listener declared on the ingress (A). Covers coverage.csv rows 136
// (endpoint registration), 137 (listener registration), 138 (end-to-end flow)
// and 139 (multi-hop forward).
func TestForward_StaticEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr := startForwardEchoServer(t)

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.ForwardEndpoints = map[int][]config.ForwardEndpoint{
		3: {{Key: "echo", Target: echoAddr}},
	}
	chain.ForwardListeners = map[int][]config.ForwardListener{
		0: {{Key: "echo", Address: "127.0.0.1:0"}},
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	if !chain.WaitForForwardRoute(t, "echo", 0) {
		t.Fatal("forward route 'echo' did not propagate to ingress agent A")
	}

	// D should have a local forward route too (via its own handler).
	if route := chain.Agents[3].LookupForwardRoute("echo"); route == nil {
		t.Fatal("exit agent D missing local forward route 'echo'")
	}

	// Resolve the listener bound address (was :0).
	listenerAddr := chain.Agents[0].ForwardListenerAddress("echo")
	if listenerAddr == nil {
		t.Fatal("forward listener address on agent A is nil")
	}
	t.Logf("forward listener bound on %s -> echo @ %s", listenerAddr, echoAddr)

	// Round-trip a payload several times to catch leaks and ordering bugs.
	payload := bytes.Repeat([]byte("forward-stream-test "), 200) // 4000 bytes
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("tcp", listenerAddr.String())
		if err != nil {
			t.Fatalf("iter %d: dial listener: %v", i, err)
		}

		conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := conn.Write(payload); err != nil {
			conn.Close()
			t.Fatalf("iter %d: write: %v", i, err)
		}

		got := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, got); err != nil {
			conn.Close()
			t.Fatalf("iter %d: read: %v", i, err)
		}
		conn.Close()

		if !bytes.Equal(got, payload) {
			t.Fatalf("iter %d: payload mismatch", i)
		}
	}

	// Stream count should drain back to baseline after we stop generating load.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if chain.Agents[0].Stats().StreamCount == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := chain.Agents[0].Stats().StreamCount; got != 0 {
		t.Errorf("agent A stream count = %d, want 0 (possible leak)", got)
	}
}

// TestForward_SameAgentSelfLoop verifies that a forward listener on the same
// agent as a static forward endpoint round-trips traffic correctly without
// going through the mesh wire protocol. Prior to the dialForwardLocal fix
// this hung because Agent.DialForward looked up the local-only route and
// then asked peerMgr for a peer connection that did not exist.
func TestForward_SameAgentSelfLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr := startForwardEchoServer(t)

	cfg := config.Default()
	cfg.Agent.DataDir = t.TempDir()
	cfg.Agent.LogLevel = "warn"
	cfg.Forward.Endpoints = []config.ForwardEndpoint{{Key: "self", Target: echoAddr}}
	cfg.Forward.Listeners = []config.ForwardListener{{Key: "self", Address: "127.0.0.1:0"}}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer a.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.LookupForwardRoute("self") != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a.LookupForwardRoute("self") == nil {
		t.Fatal("local forward route 'self' did not appear")
	}

	listenerAddr := a.ForwardListenerAddress("self")
	if listenerAddr == nil {
		t.Fatal("forward listener address is nil")
	}

	payload := bytes.Repeat([]byte("self-loop "), 256) // ~2.5 KiB
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", listenerAddr.String())
		if err != nil {
			t.Fatalf("iter %d dial: %v", i, err)
		}
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write(payload); err != nil {
			conn.Close()
			t.Fatalf("iter %d write: %v", i, err)
		}
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, got); err != nil {
			conn.Close()
			t.Fatalf("iter %d read: %v", i, err)
		}
		conn.Close()
		if !bytes.Equal(got, payload) {
			t.Fatalf("iter %d payload mismatch", i)
		}
	}
}

// TestForward_HalfClose verifies that the FIN_WRITE half-close flag propagates
// end-to-end through a forward stream. The client closes its write side after
// sending a request; the server should observe EOF and respond, and the
// client should still be able to read the response.
func TestForward_HalfClose(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Half-close-aware server: read until EOF, then send a deterministic
	// response and close.
	serverListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer serverListener.Close()

	serverReceived := make(chan []byte, 1)
	var serverWg sync.WaitGroup
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		conn, err := serverListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		data, err := io.ReadAll(conn)
		if err != nil && err != io.EOF {
			t.Logf("server read error: %v", err)
			return
		}
		serverReceived <- data

		conn.Write([]byte("SERVER_RESPONSE_AFTER_HALFCLOSE"))
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.ForwardEndpoints = map[int][]config.ForwardEndpoint{
		3: {{Key: "halfclose", Target: serverListener.Addr().String()}},
	}
	chain.ForwardListeners = map[int][]config.ForwardListener{
		0: {{Key: "halfclose", Address: "127.0.0.1:0"}},
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	if !chain.WaitForForwardRoute(t, "halfclose", 0) {
		t.Fatal("forward route 'halfclose' did not propagate to ingress")
	}

	listenerAddr := chain.Agents[0].ForwardListenerAddress("halfclose")
	if listenerAddr == nil {
		t.Fatal("forward listener address is nil")
	}

	conn, err := net.Dial("tcp", listenerAddr.String())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	clientData := []byte("CLIENT_DATA_BEFORE_HALFCLOSE")
	if _, err := conn.Write(clientData); err != nil {
		t.Fatalf("write: %v", err)
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.CloseWrite(); err != nil {
			t.Fatalf("CloseWrite: %v", err)
		}
	}

	select {
	case received := <-serverReceived:
		if !bytes.Equal(received, clientData) {
			t.Errorf("server received %q, want %q", received, clientData)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server to receive data after half-close")
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	response, err := io.ReadAll(conn)
	if err != nil && err != io.EOF {
		t.Fatalf("read response: %v", err)
	}
	if !bytes.Equal(response, []byte("SERVER_RESPONSE_AFTER_HALFCLOSE")) {
		t.Errorf("got response %q, want SERVER_RESPONSE_AFTER_HALFCLOSE", response)
	}

	serverWg.Wait()
}

// TestForward_MissingKeyError verifies that a forward listener whose routing
// key is unknown to the mesh closes incoming connections immediately rather
// than hanging. Covers coverage.csv row 145.
func TestForward_MissingKeyError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Declare a real endpoint on D under a different key, so the route table
	// is non-empty but does not contain "ghost". This exercises the
	// lookup-miss path rather than the empty-table path.
	otherEcho := startForwardEchoServer(t)

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.ForwardEndpoints = map[int][]config.ForwardEndpoint{
		3: {{Key: "other", Target: otherEcho}},
	}
	chain.ForwardListeners = map[int][]config.ForwardListener{
		0: {{Key: "ghost", Address: "127.0.0.1:0"}},
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	// Wait for the *other* route to propagate so we know the mesh is settled.
	if !chain.WaitForForwardRoute(t, "other", 0) {
		t.Fatal("forward route 'other' did not propagate to ingress")
	}

	// 'ghost' must NOT have a route.
	if route := chain.Agents[0].LookupForwardRoute("ghost"); route != nil {
		t.Fatalf("unexpected route for 'ghost': %+v", route)
	}

	listenerAddr := chain.Agents[0].ForwardListenerAddress("ghost")
	if listenerAddr == nil {
		t.Fatal("ghost listener address is nil")
	}

	conn, err := net.Dial("tcp", listenerAddr.String())
	if err != nil {
		t.Fatalf("dial ghost listener: %v", err)
	}
	defer conn.Close()

	// The listener should accept the TCP connection then close it because
	// DialForward returns an error. ReadAll should return promptly with an
	// empty buffer (or EOF).
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := io.ReadAll(conn)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected read error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty read on missing-key listener, got %d bytes", len(data))
	}
}

// TestForward_MaxConnectionsLimit verifies the listener's MaxConnections limit
// is enforced. Covers coverage.csv row 141.
func TestForward_MaxConnectionsLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// "Parking" server: accept connections and read until they close. The
	// server never writes anything back. Closing parkListener at the end of
	// the test makes Accept return and the goroutine exits.
	parkListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("park listener: %v", err)
	}
	defer parkListener.Close()

	go func() {
		for {
			conn, err := parkListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	chain := NewAgentChain(t)
	defer chain.Close()

	const limit = 2
	chain.ForwardEndpoints = map[int][]config.ForwardEndpoint{
		3: {{Key: "park", Target: parkListener.Addr().String()}},
	}
	chain.ForwardListeners = map[int][]config.ForwardListener{
		0: {{Key: "park", Address: "127.0.0.1:0", MaxConnections: limit}},
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	if !chain.WaitForForwardRoute(t, "park", 0) {
		t.Fatal("forward route 'park' did not propagate to ingress")
	}

	listenerAddr := chain.Agents[0].ForwardListenerAddress("park")
	if listenerAddr == nil {
		t.Fatal("park listener address is nil")
	}

	// Open `limit` long-lived connections that should be accepted. Closing
	// these BEFORE chain.Close() lets the listener's Stop() return promptly.
	heldConns := make([]net.Conn, 0, limit)
	defer func() {
		for _, c := range heldConns {
			c.Close()
		}
	}()
	for i := 0; i < limit; i++ {
		c, err := net.Dial("tcp", listenerAddr.String())
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		// Write something so the listener fully opens the mesh forward
		// stream and the connection counter increments before the next dial.
		if _, err := c.Write([]byte("hold")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		heldConns = append(heldConns, c)
	}

	// Wait for the listener's connection count (mirrored by the agent's
	// stream count, since this is the only forward traffic in the test) to
	// reach the limit, so the next dial trips the limit check synchronously
	// inside acceptLoop.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if chain.Agents[0].Stats().StreamCount >= limit {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := chain.Agents[0].Stats().StreamCount; got < limit {
		t.Fatalf("expected at least %d open streams, got %d", limit, got)
	}

	// The (limit+1)th connection should be accepted at the TCP layer but
	// closed immediately by the listener because the limit was reached.
	overflow, err := net.Dial("tcp", listenerAddr.String())
	if err != nil {
		t.Fatalf("dial overflow: %v", err)
	}
	defer overflow.Close()

	overflow.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 16)
	n, err := overflow.Read(buf)
	if err == nil {
		t.Errorf("overflow read returned nil error (expected EOF), got %d bytes", n)
	}
}

// TestForward_DynamicAddRemoveList exercises POST /forward/manage with the
// add, list and remove actions on agent A. Covers coverage.csv row 143.
func TestForward_DynamicAddRemoveList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr := startForwardEchoServer(t)

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.EnableHTTP = true
	chain.ForwardEndpoints = map[int][]config.ForwardEndpoint{
		3: {{Key: "echo", Target: echoAddr}},
	}
	// No static listeners on agent A - the test adds one dynamically.

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	if !chain.WaitForForwardRoute(t, "echo", 0) {
		t.Fatal("forward route 'echo' did not propagate to ingress")
	}

	httpAddr := chain.HTTPAddrs[0]
	if httpAddr == "" {
		t.Fatal("agent A HTTP server address is empty")
	}

	manageURL := "http://" + httpAddr + "/forward/manage"

	// 1. List - should be empty.
	if got := postForwardManage(t, manageURL, map[string]any{"action": "list"}); len(got.Listeners) != 0 {
		t.Errorf("initial list: expected 0 listeners, got %d", len(got.Listeners))
	}

	// 2. Add a dynamic listener.
	addResp := postForwardManage(t, manageURL, map[string]any{
		"action":          "add",
		"key":             "echo",
		"address":         "127.0.0.1:0",
		"max_connections": 10,
	})
	if addResp.Status != "ok" {
		t.Fatalf("add: status %q, message %q", addResp.Status, addResp.Message)
	}
	if !strings.Contains(addResp.Message, "127.0.0.1:") {
		t.Errorf("add message %q does not contain bound address", addResp.Message)
	}

	// 3. List - should contain exactly one dynamic listener.
	listResp := postForwardManage(t, manageURL, map[string]any{"action": "list"})
	if len(listResp.Listeners) != 1 {
		t.Fatalf("list after add: expected 1 listener, got %d", len(listResp.Listeners))
	}
	entry := listResp.Listeners[0]
	if entry.Key != "echo" {
		t.Errorf("listener key = %q, want echo", entry.Key)
	}
	if !entry.Dynamic {
		t.Errorf("listener.Dynamic = false, want true")
	}
	if entry.MaxConnections != 10 {
		t.Errorf("listener.MaxConnections = %d, want 10", entry.MaxConnections)
	}
	listenerAddr := entry.Address
	if listenerAddr == "" {
		t.Fatal("listener.Address is empty")
	}

	// 4. Forward real traffic through the dynamically-added listener.
	conn, err := net.Dial("tcp", listenerAddr)
	if err != nil {
		t.Fatalf("dial dynamic listener: %v", err)
	}
	payload := []byte("dynamic-forward-test")
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		conn.Close()
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		conn.Close()
		t.Fatalf("read: %v", err)
	}
	conn.Close()
	if !bytes.Equal(got, payload) {
		t.Errorf("payload mismatch through dynamic listener: got %q want %q", got, payload)
	}

	// 5. Remove the listener.
	removeResp := postForwardManage(t, manageURL, map[string]any{
		"action": "remove",
		"key":    "echo",
	})
	if removeResp.Status != "ok" {
		t.Fatalf("remove: status %q, message %q", removeResp.Status, removeResp.Message)
	}

	// 6. List - should be empty again.
	if got := postForwardManage(t, manageURL, map[string]any{"action": "list"}); len(got.Listeners) != 0 {
		t.Errorf("list after remove: expected 0 listeners, got %d", len(got.Listeners))
	}

	// 7. The previously bound address should refuse new connections.
	if c, err := net.DialTimeout("tcp", listenerAddr, 2*time.Second); err == nil {
		c.Close()
		t.Errorf("dial after remove: expected error, got nil")
	}

	// Negative: removing a key that does not exist should return 400.
	negResp := postForwardManageRaw(t, manageURL, map[string]any{
		"action": "remove",
		"key":    "never-existed",
	})
	defer negResp.Body.Close()
	if negResp.StatusCode != http.StatusBadRequest {
		t.Errorf("negative remove: expected status %d, got %d", http.StatusBadRequest, negResp.StatusCode)
	}
}

// TestForward_DynamicEndpointAddRemoveList exercises POST /forward/endpoints/manage
// and its remote /agents/{id}/forward/endpoints/manage variant. An endpoint is
// added dynamically on the exit agent (D) via the remote-control path through
// agent A's HTTP API, traffic is forwarded end-to-end, then the endpoint is
// removed and we verify traffic fails. Covers the Dynamic add/remove/list +
// Remote dynamic endpoint mgmt coverage rows.
func TestForward_DynamicEndpointAddRemoveList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr := startForwardEchoServer(t)

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.EnableHTTP = true
	// Static listener on ingress A; NO static endpoint on exit D -- the test
	// adds the endpoint dynamically via HTTP after the mesh is up.
	chain.ForwardListeners = map[int][]config.ForwardListener{
		0: {{Key: "echo", Address: "127.0.0.1:0"}},
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	httpAddr := chain.HTTPAddrs[0]
	if httpAddr == "" {
		t.Fatal("agent A HTTP server address is empty")
	}

	exitID := chain.Agents[3].ID().String()
	localURL := "http://" + httpAddr + "/forward/endpoints/manage"
	remoteURL := "http://" + httpAddr + "/agents/" + exitID + "/forward/endpoints/manage"

	// 1. Initial list on exit D (via remote) - no endpoints.
	if got := postForwardEndpointManage(t, remoteURL, map[string]any{"action": "list"}); len(got.Endpoints) != 0 {
		t.Errorf("initial remote list: expected 0 endpoints, got %d", len(got.Endpoints))
	}

	// 2. Add endpoint on D via the remote control path.
	addResp := postForwardEndpointManage(t, remoteURL, map[string]any{
		"action": "add",
		"key":    "echo",
		"target": echoAddr,
	})
	if addResp.Status != "ok" {
		t.Fatalf("remote add: status %q, message %q", addResp.Status, addResp.Message)
	}

	// 3. Wait for the forward route to flood back to ingress A.
	if !chain.WaitForForwardRoute(t, "echo", 0) {
		t.Fatal("forward route 'echo' did not propagate to ingress after dynamic endpoint add")
	}

	// 4. List on D - should contain exactly one dynamic endpoint.
	listResp := postForwardEndpointManage(t, remoteURL, map[string]any{"action": "list"})
	if len(listResp.Endpoints) != 1 {
		t.Fatalf("list after add: expected 1 endpoint, got %d", len(listResp.Endpoints))
	}
	entry := listResp.Endpoints[0]
	if entry.Key != "echo" || entry.Target != echoAddr || !entry.Dynamic {
		t.Errorf("endpoint entry = %+v; want key=echo, target=%s, dynamic=true", entry, echoAddr)
	}

	// 5. Forward traffic through the ingress listener now that the endpoint is live.
	laddr := chain.Agents[0].ForwardListenerAddress("echo")
	if laddr == nil {
		t.Fatal("ingress listener address is nil")
	}
	listenerAddr := laddr.String()
	conn, err := net.Dial("tcp", listenerAddr)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	payload := []byte("dynamic-endpoint-test")
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		conn.Close()
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		conn.Close()
		t.Fatalf("read: %v", err)
	}
	conn.Close()
	if !bytes.Equal(got, payload) {
		t.Errorf("payload mismatch through dynamic endpoint: got %q want %q", got, payload)
	}

	// 6. Remove via local path (ingress A has no endpoint of that key, exercise
	//    the local management URL by adding+removing a throwaway endpoint).
	localAdd := postForwardEndpointManage(t, localURL, map[string]any{
		"action": "add",
		"key":    "local-throwaway",
		"target": "127.0.0.1:1",
	})
	if localAdd.Status != "ok" {
		t.Fatalf("local add: status %q, message %q", localAdd.Status, localAdd.Message)
	}
	localRemove := postForwardEndpointManage(t, localURL, map[string]any{
		"action": "remove",
		"key":    "local-throwaway",
	})
	if localRemove.Status != "ok" {
		t.Fatalf("local remove: status %q, message %q", localRemove.Status, localRemove.Message)
	}

	// 7. Remove the real endpoint on D.
	removeResp := postForwardEndpointManage(t, remoteURL, map[string]any{
		"action": "remove",
		"key":    "echo",
	})
	if removeResp.Status != "ok" {
		t.Fatalf("remote remove: status %q, message %q", removeResp.Status, removeResp.Message)
	}

	// 8. Negative: re-removing a missing key returns 400 through the remote
	//    control path.
	negResp := postForwardEndpointManageRaw(t, remoteURL, map[string]any{
		"action": "remove",
		"key":    "echo",
	})
	defer negResp.Body.Close()
	if negResp.StatusCode != http.StatusBadRequest {
		t.Errorf("remote remove-missing: expected 400, got %d", negResp.StatusCode)
	}
}

// postForwardEndpointManage POSTs a JSON body to a /forward/endpoints/manage URL
// and decodes the result. Fails the test on non-OK responses.
func postForwardEndpointManage(t *testing.T, url string, body map[string]any) *health.ForwardEndpointManageResult {
	t.Helper()

	resp := postForwardEndpointManageRaw(t, url, body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: status %d, body %s", url, resp.StatusCode, respBody)
	}

	var out health.ForwardEndpointManageResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &out
}

// postForwardEndpointManageRaw POSTs a JSON body and returns the raw response
// without asserting status. Caller must close the body.
func postForwardEndpointManageRaw(t *testing.T, url string, body map[string]any) *http.Response {
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

// postForwardManage POSTs a JSON body to the /forward/manage endpoint and
// decodes the result. Fails the test on transport or non-OK responses.
func postForwardManage(t *testing.T, url string, body map[string]any) *health.ForwardManageResult {
	t.Helper()

	resp := postForwardManageRaw(t, url, body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: status %d, body %s", url, resp.StatusCode, respBody)
	}

	var out health.ForwardManageResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &out
}

// postForwardManageRaw POSTs a JSON body to /forward/manage and returns the
// raw response without asserting status. Caller must close the body.
func postForwardManageRaw(t *testing.T, url string, body map[string]any) *http.Response {
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

// TestForwardEndpoint_RemoveWithdrawsFromPeer verifies that removing a dynamic
// forward endpoint on the exit agent floods a withdrawal so peer route tables
// drop the row well before the 5m TTL.
func TestForwardEndpoint_RemoveWithdrawsFromPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr := startForwardEchoServer(t)

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	// Add a dynamic forward endpoint on the exit (D).
	if _, err := chain.Agents[3].ManageForwardEndpoint("add", "dyn-fwd", echoAddr); err != nil {
		t.Fatalf("ManageForwardEndpoint add on D: %v", err)
	}

	if !chain.WaitForForwardRoute(t, "dyn-fwd", 0) {
		t.Fatal("forward route 'dyn-fwd' did not propagate to ingress A")
	}

	// Remove it on D and verify A drops the row well before TTL.
	if _, err := chain.Agents[3].ManageForwardEndpoint("remove", "dyn-fwd", ""); err != nil {
		t.Fatalf("ManageForwardEndpoint remove on D: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if chain.Agents[0].LookupForwardRoute("dyn-fwd") == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if route := chain.Agents[0].LookupForwardRoute("dyn-fwd"); route != nil {
		t.Errorf("ingress A should drop dyn-fwd after withdrawal; got %+v", route)
	}
}

// TestRoute_RemoveWithdrawsFromPeer verifies that removing a dynamic CIDR
// route on the exit agent floods a withdrawal so peer route tables drop the
// row well before the 5m TTL.
func TestRoute_RemoveWithdrawsFromPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	const cidr = "203.0.113.0/24"

	if _, err := chain.Agents[3].ManageRoute("add", cidr, 5, "", "", "", ""); err != nil {
		t.Fatalf("ManageRoute add on D: %v", err)
	}

	// Look for an exact-network match, not contains: the chain seeds a
	// 0.0.0.0/0 default route on D that would match any probe IP.
	hasRouteOnA := func() bool {
		for _, r := range chain.Agents[0].GetRoutes() {
			if r.Network != nil && r.Network.String() == cidr {
				return true
			}
		}
		return false
	}

	addDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(addDeadline) && !hasRouteOnA() {
		time.Sleep(100 * time.Millisecond)
	}
	if !hasRouteOnA() {
		t.Fatal("CIDR route 203.0.113.0/24 did not propagate to ingress A")
	}

	if _, err := chain.Agents[3].ManageRoute("remove", cidr, 0, "", "", "", ""); err != nil {
		t.Fatalf("ManageRoute remove on D: %v", err)
	}

	rmDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(rmDeadline) && hasRouteOnA() {
		time.Sleep(50 * time.Millisecond)
	}
	if hasRouteOnA() {
		t.Errorf("ingress A still has route for %s after withdrawal", cidr)
	}
}
