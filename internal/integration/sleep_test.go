// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/agent"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/sleep"
	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// SleepMesh represents a 5-agent mesh for sleep mode testing.
// Topology: A -> B <- C -> E
//
//	C <- F
//
// A: ingress (SOCKS5)
// B: transit (hub)
// C: transit (connector to exits)
// E: exit
// F: exit
type SleepMesh struct {
	Agents   map[string]*agent.Agent
	DataDirs map[string]string
	Addrs    map[string]string
	Certs    map[string]CertPair
}

// NewSleepMesh creates a 5-agent mesh for sleep testing.
func NewSleepMesh(t *testing.T) *SleepMesh {
	mesh := &SleepMesh{
		Agents:   make(map[string]*agent.Agent),
		DataDirs: make(map[string]string),
		Addrs:    make(map[string]string),
		Certs:    make(map[string]CertPair),
	}

	names := []string{"A", "B", "C", "E", "F"}

	// Allocate free UDP ports for QUIC listeners
	ports, err := allocateFreeUDPPorts(len(names))
	if err != nil {
		t.Fatalf("Failed to allocate ports: %v", err)
	}

	for i, name := range names {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("sleep-agent-%s-", name))
		if err != nil {
			mesh.cleanup()
			t.Fatalf("Failed to create temp dir for %s: %v", name, err)
		}
		mesh.DataDirs[name] = tmpDir
		mesh.Addrs[name] = ports[i]

		// Generate TLS certificates
		certPEM, keyPEM, err := transport.GenerateSelfSignedCert("agent-"+name, 24*time.Hour)
		if err != nil {
			mesh.cleanup()
			t.Fatalf("Failed to generate cert for %s: %v", name, err)
		}

		certFile := tmpDir + "/cert.pem"
		keyFile := tmpDir + "/key.pem"
		if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
			mesh.cleanup()
			t.Fatalf("Failed to write cert for %s: %v", name, err)
		}
		if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
			mesh.cleanup()
			t.Fatalf("Failed to write key for %s: %v", name, err)
		}
		mesh.Certs[name] = CertPair{CertFile: certFile, KeyFile: keyFile}
	}

	return mesh
}

func (m *SleepMesh) cleanup() {
	for _, dir := range m.DataDirs {
		if dir != "" {
			os.RemoveAll(dir)
		}
	}
}

// Close shuts down all agents and cleans up.
func (m *SleepMesh) Close() {
	for _, a := range m.Agents {
		if a != nil {
			a.Stop()
		}
	}
	m.cleanup()
}

// buildConfig builds configuration for the named agent.
func (m *SleepMesh) buildConfig(name string) *config.Config {
	cfg := config.Default()
	cfg.Agent.DataDir = m.DataDirs[name]
	cfg.Agent.LogLevel = "info"

	// Add listener
	cfg.Listeners = []config.ListenerConfig{
		{
			Transport: "quic",
			Address:   m.Addrs[name],
			TLS: config.TLSConfig{
				Cert: m.Certs[name].CertFile,
				Key:  m.Certs[name].KeyFile,
			},
		},
	}

	// Configure peers based on topology:
	// A -> B
	// C -> B, C -> E
	// F -> C
	switch name {
	case "A":
		// A connects to B
		cfg.Peers = []config.PeerConfig{
			{ID: "auto", Transport: "quic", Address: m.Addrs["B"]},
		}
		// A is ingress with SOCKS5
		cfg.SOCKS5.Enabled = true
		cfg.SOCKS5.Address = "127.0.0.1:0"
		// Enable HTTP for management
		cfg.HTTP.Enabled = true
		cfg.HTTP.Address = "127.0.0.1:0"
		remoteAPI := true
		cfg.HTTP.RemoteAPI = &remoteAPI

	case "B":
		// B is transit hub, accepts from A and C
		// No outbound peers

	case "C":
		// C connects to B and E
		cfg.Peers = []config.PeerConfig{
			{ID: "auto", Transport: "quic", Address: m.Addrs["B"]},
			{ID: "auto", Transport: "quic", Address: m.Addrs["E"]},
		}

	case "E":
		// E is exit node
		cfg.Exit.Enabled = true
		cfg.Exit.Routes = []config.RouteConfig{{CIDR: "10.0.0.0/8"}} // Exit for 10.x.x.x

	case "F":
		// F connects to C and is exit node
		cfg.Peers = []config.PeerConfig{
			{ID: "auto", Transport: "quic", Address: m.Addrs["C"]},
		}
		cfg.Exit.Enabled = true
		cfg.Exit.Routes = []config.RouteConfig{{CIDR: "192.168.0.0/16"}} // Exit for 192.168.x.x
	}

	// Enable sleep mode on all agents.
	// PollDuration must be long enough to absorb a full dial + handshake +
	// wake message round-trip under suite-wide CPU contention. Earlier
	// versions used 1s, which raced against QUIC dial latency under load
	// and produced flaky failures where leaf agents (e.g. F) never caught
	// the wake message during their poll window.
	cfg.Sleep.Enabled = true
	cfg.Sleep.PollInterval = 2 * time.Second
	cfg.Sleep.PollIntervalJitter = 0.1
	cfg.Sleep.PollDuration = 3 * time.Second
	cfg.Sleep.PersistState = false
	cfg.Sleep.MaxQueuedMessages = 100

	// Re-advertise often so a route flood lost while the five links come up
	// at once heals within WaitForRoutes' timeout. At the default 2m the
	// periodic advertise never fires inside the wait, and one missed flood
	// fails the test.
	cfg.Routing.AdvertiseInterval = 5 * time.Second

	return cfg
}

// CreateAgents creates all 5 agents.
func (m *SleepMesh) CreateAgents(t *testing.T) {
	names := []string{"A", "B", "C", "E", "F"}
	for _, name := range names {
		cfg := m.buildConfig(name)
		a, err := agent.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create agent %s: %v", name, err)
		}
		m.Agents[name] = a
	}
}

// StartAgents starts all agents in order (exits first, then transit, then ingress).
func (m *SleepMesh) StartAgents(t *testing.T) {
	// Start order: E, F (exits) -> B, C (transit) -> A (ingress)
	startOrder := []string{"E", "F", "B", "C", "A"}

	for _, name := range startOrder {
		if err := m.Agents[name].Start(); err != nil {
			t.Fatalf("Failed to start agent %s: %v", name, err)
		}
		t.Logf("Agent %s started (ID: %s)", name, m.Agents[name].ID().ShortString())
	}

	// Wait for connections to establish
	time.Sleep(500 * time.Millisecond)
}

// VerifyConnectivity checks that the mesh is connected properly.
func (m *SleepMesh) VerifyConnectivity(t *testing.T) bool {
	// Expected peer counts:
	// A: 1 (B)
	// B: 2 (A, C)
	// C: 3 (B, E, F)
	// E: 1 (C)
	// F: 1 (C)
	expected := map[string]int{
		"A": 1,
		"B": 2,
		"C": 3,
		"E": 1,
		"F": 1,
	}

	timeout := 30 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allConnected := true
		for name, a := range m.Agents {
			stats := a.Stats()
			if stats.PeerCount != expected[name] {
				allConnected = false
				break
			}
		}

		if allConnected {
			for name, a := range m.Agents {
				stats := a.Stats()
				t.Logf("Agent %s: %d peers (OK)", name, stats.PeerCount)
			}
			return true
		}

		time.Sleep(interval)
	}

	// Timeout - report actual state
	t.Logf("Connectivity timeout after %v", timeout)
	for name, a := range m.Agents {
		stats := a.Stats()
		t.Logf("Agent %s: expected %d peers, got %d", name, expected[name], stats.PeerCount)
	}
	return false
}

// WaitForRoutes waits for routes to propagate to agent A.
//
// The 60s timeout is generous on purpose: under suite-wide load the QUIC
// dial / handshake / route flood pipeline can take several seconds, and
// brief mid-flight peer churn can drop a single flood. We rely on the
// per-connection SendFullTable plus the periodic route advertise loop
// (5s in this mesh, see buildConfig) to converge.
func (m *SleepMesh) WaitForRoutes(t *testing.T) bool {
	timeout := 60 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		stats := m.Agents["A"].Stats()
		// A should have at least 2 routes (from E and F)
		if stats.RouteCount >= 2 {
			t.Logf("Agent A has %d routes (OK)", stats.RouteCount)
			return true
		}
		time.Sleep(interval)
	}

	stats := m.Agents["A"].Stats()
	t.Logf("Route propagation timeout: Agent A has %d routes", stats.RouteCount)
	// Dump every agent's table so a failure shows which hop lost the route.
	for _, name := range []string{"A", "B", "C", "E", "F"} {
		a := m.Agents[name]
		t.Logf("Agent %s (%s): %d peers", name, a.ID().ShortString(), a.Stats().PeerCount)
		for _, r := range a.GetRoutes() {
			t.Logf("  route: %s via %s (origin=%s, metric=%d)",
				r.Network, r.NextHop.ShortString(), r.OriginAgent.ShortString(), r.Metric)
		}
	}
	return false
}

// VerifyAllSleeping checks that all agents are in sleep mode with no peers.
func (m *SleepMesh) VerifyAllSleeping(t *testing.T) bool {
	timeout := 10 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allSleeping := true
		for name, a := range m.Agents {
			state := a.GetSleepState()
			stats := a.Stats()
			if state != sleep.StateSleeping && state != sleep.StatePolling {
				allSleeping = false
				t.Logf("Agent %s: state=%s, peers=%d (not sleeping yet)", name, state, stats.PeerCount)
				break
			}
			if stats.PeerCount > 0 {
				allSleeping = false
				t.Logf("Agent %s: state=%s, peers=%d (still has peers)", name, state, stats.PeerCount)
				break
			}
		}

		if allSleeping {
			for name, a := range m.Agents {
				state := a.GetSleepState()
				stats := a.Stats()
				t.Logf("Agent %s: state=%s, peers=%d (OK)", name, state, stats.PeerCount)
			}
			return true
		}

		time.Sleep(interval)
	}

	// Final state report
	t.Logf("Sleep verification timeout")
	for name, a := range m.Agents {
		state := a.GetSleepState()
		stats := a.Stats()
		t.Logf("Agent %s: state=%s, peers=%d", name, state, stats.PeerCount)
	}
	return false
}

// VerifyAllAwake checks that all agents are awake with expected peer counts.
func (m *SleepMesh) VerifyAllAwake(t *testing.T) bool {
	expected := map[string]int{
		"A": 1,
		"B": 2,
		"C": 3,
		"E": 1,
		"F": 1,
	}

	// Longer timeout to allow for poll cycles to complete
	timeout := 60 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allAwake := true
		for name, a := range m.Agents {
			state := a.GetSleepState()
			stats := a.Stats()
			if state != sleep.StateAwake {
				allAwake = false
				break
			}
			if stats.PeerCount != expected[name] {
				allAwake = false
				break
			}
		}

		if allAwake {
			for name, a := range m.Agents {
				state := a.GetSleepState()
				stats := a.Stats()
				t.Logf("Agent %s: state=%s, peers=%d (OK)", name, state, stats.PeerCount)
			}
			return true
		}

		time.Sleep(interval)
	}

	// Final state report
	t.Logf("Wake verification timeout")
	for name, a := range m.Agents {
		state := a.GetSleepState()
		stats := a.Stats()
		t.Logf("Agent %s: state=%s, peers=%d (expected %d)", name, state, stats.PeerCount, expected[name])
	}
	return false
}

// TestSleepMesh_FullCycle tests the complete sleep/wake cycle.
func TestSleepMesh_FullCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	mesh := NewSleepMesh(t)
	defer mesh.Close()

	// Create and start agents
	mesh.CreateAgents(t)
	mesh.StartAgents(t)

	// Step 1: Verify mesh is up
	t.Log("=== Step 1: Verify mesh connectivity ===")
	if !mesh.VerifyConnectivity(t) {
		t.Fatal("Mesh connectivity verification failed")
	}

	// Step 2: Wait for route propagation
	t.Log("=== Step 2: Wait for route propagation ===")
	if !mesh.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	// Log route information
	t.Log("Routes on Agent A:")
	for _, r := range mesh.Agents["A"].GetRoutes() {
		t.Logf("  %s via %s (metric=%d)", r.Network, r.NextHop.ShortString(), r.Metric)
	}

	// Step 3: Trigger sleep from agent A
	t.Log("=== Step 3: Trigger sleep from agent A ===")
	sleepStart := time.Now()
	if err := mesh.Agents["A"].TriggerSleep(); err != nil {
		t.Fatalf("Failed to trigger sleep: %v", err)
	}
	t.Logf("Sleep triggered at %v", sleepStart)

	// Step 4: Verify all agents enter sleep mode
	t.Log("=== Step 4: Verify all agents are sleeping ===")
	if !mesh.VerifyAllSleeping(t) {
		t.Fatal("Not all agents entered sleep mode")
	}
	sleepPropagationTime := time.Since(sleepStart)
	t.Logf("Sleep propagation completed in %v", sleepPropagationTime)

	// Step 5: Verify no peer connections while sleeping
	t.Log("=== Step 5: Verify no peer connections ===")
	for name, a := range mesh.Agents {
		stats := a.Stats()
		if stats.PeerCount > 0 {
			t.Errorf("Agent %s still has %d peers while sleeping", name, stats.PeerCount)
		}
	}

	// Wait a moment to ensure sleep is stable
	time.Sleep(1 * time.Second)

	// Step 6: Trigger wake from agent A
	t.Log("=== Step 6: Trigger wake from agent A ===")
	wakeStart := time.Now()
	if err := mesh.Agents["A"].TriggerWake(); err != nil {
		t.Fatalf("Failed to trigger wake: %v", err)
	}
	t.Logf("Wake triggered at %v", wakeStart)

	// Wait for poll cycles to allow wake to propagate
	// Since A dials to B and B is sleeping, we need B to poll first
	// Poll interval is 2s, so wait a bit for cycles to complete
	t.Log("Waiting for poll cycles to propagate wake...")
	time.Sleep(5 * time.Second)

	// Step 7: Measure wake propagation time
	t.Log("=== Step 7: Verify all agents are awake ===")
	if !mesh.VerifyAllAwake(t) {
		t.Fatal("Not all agents woke up")
	}
	wakePropagationTime := time.Since(wakeStart)
	t.Logf("Wake propagation completed in %v", wakePropagationTime)

	// Step 8: Wait for routes to re-propagate.
	// On wake, peers reconnect and exchange routes via SendFullTable. The
	// initial exchange may miss routes that were learned later from other
	// peers (timing race in the connection ordering). Trigger an explicit
	// route advertise on each agent so the local routes re-flood through
	// the chain. This matches the e2e shell test's pattern (commit c96b40b)
	// and converges faster than waiting for the periodic 2-minute interval.
	t.Log("=== Step 8: Wait for routes to re-propagate ===")
	for _, name := range []string{"E", "F", "C", "B", "A"} {
		mesh.Agents[name].TriggerRouteAdvertise()
	}
	if !mesh.WaitForRoutes(t) {
		t.Fatal("Route re-propagation failed after wake")
	}

	// Step 9: Verify traffic can flow A -> E (10.x.x.x)
	t.Log("=== Step 9: Verify traffic flow A -> E ===")
	if !testTrafficFlow(t, mesh.Agents["A"], "10.0.0.1") {
		t.Error("Traffic flow to E (10.x.x.x) failed")
	}

	// Step 10: Verify traffic can flow A -> F (192.168.x.x)
	t.Log("=== Step 10: Verify traffic flow A -> F ===")
	if !testTrafficFlow(t, mesh.Agents["A"], "192.168.1.1") {
		t.Error("Traffic flow to F (192.168.x.x) failed")
	}

	// Summary
	t.Log("=== Summary ===")
	t.Logf("Sleep propagation time: %v", sleepPropagationTime)
	t.Logf("Wake propagation time: %v", wakePropagationTime)
	t.Log("Full sleep/wake cycle completed successfully")
}

// testTrafficFlow tests that traffic can flow through the mesh to a destination.
// Since we don't have actual servers at 10.x.x.x or 192.168.x.x, we test route lookup.
func testTrafficFlow(t *testing.T, a *agent.Agent, destIP string) bool {
	// Check that agent has a route for the destination
	routes := a.GetRoutes()
	for _, r := range routes {
		if r.Network.Contains(net.ParseIP(destIP)) {
			t.Logf("Route found for %s: %s via %s", destIP, r.Network, r.NextHop.ShortString())
			return true
		}
	}
	t.Logf("No route found for %s", destIP)
	return false
}

// TestSleepMesh_EchoThroughMesh tests actual traffic through the mesh before and after sleep.
func TestSleepMesh_EchoThroughMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	mesh := NewSleepMesh(t)
	defer mesh.Close()

	// Modify E to be a default exit (0.0.0.0/0) for this test
	mesh.CreateAgents(t)

	// Override E's exit config to allow all traffic
	cfg := mesh.buildConfig("E")
	cfg.Exit.Routes = []config.RouteConfig{{CIDR: "0.0.0.0/0"}}
	mesh.Agents["E"].Stop()
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("Failed to recreate agent E: %v", err)
	}
	mesh.Agents["E"] = a

	mesh.StartAgents(t)

	// Verify connectivity and routes
	if !mesh.VerifyConnectivity(t) {
		t.Fatal("Mesh connectivity failed")
	}
	if !mesh.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	// Start echo server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer listener.Close()

	// Echo server goroutine
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

	// Test echo before sleep
	t.Log("=== Testing echo BEFORE sleep ===")
	if !testEcho(t, mesh.Agents["A"], listener.Addr().String()) {
		t.Error("Echo failed before sleep")
	}

	// Sleep
	t.Log("=== Putting mesh to sleep ===")
	if err := mesh.Agents["A"].TriggerSleep(); err != nil {
		t.Fatalf("Sleep failed: %v", err)
	}
	if !mesh.VerifyAllSleeping(t) {
		t.Fatal("Not all agents sleeping")
	}

	// Wake
	t.Log("=== Waking mesh ===")
	if err := mesh.Agents["A"].TriggerWake(); err != nil {
		t.Fatalf("Wake failed: %v", err)
	}
	if !mesh.VerifyAllAwake(t) {
		t.Fatal("Not all agents awake")
	}

	// Wait for routes. See WaitForRoutes / TestSleepMesh_FullCycle for the
	// rationale behind triggering an explicit advertise here.
	for _, name := range []string{"E", "F", "C", "B", "A"} {
		mesh.Agents[name].TriggerRouteAdvertise()
	}
	if !mesh.WaitForRoutes(t) {
		t.Fatal("Route re-propagation failed")
	}

	// Test echo after wake. WaitForRoutes only checks route counts, so a
	// transit agent on the path may still be re-learning its routes on a
	// loaded machine; retry until the path works or the deadline passes.
	t.Log("=== Testing echo AFTER wake ===")
	deadline := time.Now().Add(15 * time.Second)
	for !testEcho(t, mesh.Agents["A"], listener.Addr().String()) {
		if time.Now().After(deadline) {
			t.Fatal("Echo failed after wake")
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Log("Echo test completed successfully")
}

// testEcho tests echoing data through an agent's Dial method.
func testEcho(t *testing.T, a *agent.Agent, serverAddr string) bool {
	conn, err := a.Dial("tcp", serverAddr)
	if err != nil {
		t.Logf("Dial failed: %v", err)
		return false
	}
	defer conn.Close()

	testMsg := []byte("hello from sleep test!")
	if _, err := conn.Write(testMsg); err != nil {
		t.Logf("Write failed: %v", err)
		return false
	}

	buf := make([]byte, len(testMsg))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Logf("Read failed: %v", err)
		return false
	}

	if !bytes.Equal(testMsg, buf) {
		t.Logf("Echo mismatch: sent %q, got %q", testMsg, buf)
		return false
	}

	t.Logf("Echo successful: %q", testMsg)
	return true
}
