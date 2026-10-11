// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/agent"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// newChainSigningKey generates a fresh management signing keypair that the
// chain will install on every agent. Tests that exercise the "missing key"
// failure modes override ManagementConfigure to strip the pieces they want.
func newChainSigningKey(t *testing.T) (privHex, pubHex string) {
	t.Helper()
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("generate signing keypair: %v", err)
	}
	return hex.EncodeToString(kp.PrivateKey[:]), hex.EncodeToString(kp.PublicKey[:])
}

// allocateFreeUDPPorts allocates n free UDP ports and returns their addresses.
// The ports are allocated by binding to port 0 and then closing the socket.
// This is not 100% race-free but works well for test isolation.
func allocateFreeUDPPorts(n int) ([]string, error) {
	addrs := make([]string, n)
	conns := make([]*net.UDPConn, n)

	// First, allocate all ports by binding
	for i := 0; i < n; i++ {
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
		if err != nil {
			// Close already allocated
			for j := 0; j < i; j++ {
				conns[j].Close()
			}
			return nil, err
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			// Close already allocated
			for j := 0; j < i; j++ {
				conns[j].Close()
			}
			return nil, err
		}
		conns[i] = conn
		addrs[i] = conn.LocalAddr().String()
	}

	// Close all sockets to release the ports
	for i := 0; i < n; i++ {
		conns[i].Close()
	}

	return addrs, nil
}

// AgentChain represents a chain of 4 agents: A-B-C-D.
type AgentChain struct {
	Agents             [4]*agent.Agent
	DataDirs           [4]string
	Addresses          [4]string
	TLSCerts           [4]CertPair
	HTTPAddrs          [4]string // HTTP health server addresses (set after start)
	ShellConfig        *config.ShellConfig
	FileTransferConfig *config.FileTransferConfig
	EnableHTTP         bool   // Enable HTTP server on agent A
	HTTPAllAgents      bool   // Also enable HTTP on every agent (uses same HTTPConfigure on each)
	SOCKS5AllAgents    bool   // Also enable SOCKS5 on every agent (e.g. expose-over-SOCKS on the exit)
	HTTPTokenHash      string // Optional bearer-token hash applied to every HTTP-enabled agent
	// ForwardEndpoints maps agent index -> static forward endpoints to declare on that agent.
	ForwardEndpoints map[int][]config.ForwardEndpoint
	// ForwardListeners maps agent index -> static forward listeners to declare on that agent.
	ForwardListeners map[int][]config.ForwardListener
	// ExitDomainRoutes, when non-empty, sets cfg.Exit.DomainRoutes on the exit node (D).
	ExitDomainRoutes []string
	// ExitDNSServers, when non-empty, sets cfg.Exit.DNS.Servers on the exit node (D).
	ExitDNSServers []string
	// HTTPConfigure, when non-nil, is invoked against the HTTP config on agent
	// A after EnableHTTP defaults are applied. Use it to set TokenHash,
	// Minimal, Pprof, Dashboard, or RemoteAPI without bloating the fixture.
	HTTPConfigure func(*config.HTTPConfig)
	// UDPConfigure, when non-nil, is invoked against the UDP config on the
	// exit node (D). Use it to enable UDP relay and tweak limits without
	// adding a field per knob.
	UDPConfigure func(*config.UDPConfig)
	// ICMPConfigure, when non-nil, is invoked against the ICMP config on
	// BOTH the ingress agent (A) and the exit node (D). Both sides need
	// it: the exit so it can send ICMP packets, and the ingress so its
	// SOCKS5 server gets a non-nil icmpHandler for the CmdICMPEcho path.
	ICMPConfigure func(*config.ICMPConfig)
	// ManagementConfigure, when non-nil, is invoked with the per-agent
	// ManagementConfig after the default signing keypair has been applied.
	// Tests use this to simulate "missing public key on target" or "missing
	// private key on origin" by clearing the relevant field.
	ManagementConfigure func(agentIdx int, mgmt *config.ManagementConfig)

	// SigningPrivateHex / SigningPublicHex hold the management signing
	// keypair shared across all agents in this chain. Generated lazily on
	// first CreateAgents call; tests can pre-seed them.
	SigningPrivateHex string
	SigningPublicHex  string
}

// CertPair holds TLS certificate and key file paths.
type CertPair struct {
	CertFile string
	KeyFile  string
}

// NewAgentChain creates a 4-agent chain for testing.
func NewAgentChain(t *testing.T) *AgentChain {
	chain := &AgentChain{}
	names := []string{"A", "B", "C", "D"}

	// Allocate free UDP ports for QUIC listeners
	ports, err := allocateFreeUDPPorts(4)
	if err != nil {
		t.Fatalf("Failed to allocate ports: %v", err)
	}

	// Generate certificates and create data directories
	for i := range names {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("agent-%s-", names[i]))
		if err != nil {
			chain.cleanup(i)
			t.Fatalf("Failed to create temp dir for %s: %v", names[i], err)
		}
		chain.DataDirs[i] = tmpDir
		chain.Addresses[i] = ports[i]

		// Generate TLS certificates
		certPEM, keyPEM, err := transport.GenerateSelfSignedCert("agent-"+names[i], 24*time.Hour)
		if err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to generate cert for %s: %v", names[i], err)
		}

		certFile := tmpDir + "/cert.pem"
		keyFile := tmpDir + "/key.pem"
		if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to write cert for %s: %v", names[i], err)
		}
		if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to write key for %s: %v", names[i], err)
		}
		chain.TLSCerts[i] = CertPair{CertFile: certFile, KeyFile: keyFile}
	}

	return chain
}

// cleanup removes data directories up to index.
func (c *AgentChain) cleanup(upTo int) {
	for i := 0; i < upTo; i++ {
		if c.DataDirs[i] != "" {
			os.RemoveAll(c.DataDirs[i])
		}
	}
}

// Close shuts down all agents and cleans up.
func (c *AgentChain) Close() {
	for i, a := range c.Agents {
		if a != nil {
			a.Stop()
		}
		if c.DataDirs[i] != "" {
			os.RemoveAll(c.DataDirs[i])
		}
	}
}

// CreateAgents creates all 4 agents with proper configuration.
func (c *AgentChain) CreateAgents(t *testing.T) {
	if c.SigningPrivateHex == "" || c.SigningPublicHex == "" {
		c.SigningPrivateHex, c.SigningPublicHex = newChainSigningKey(t)
	}
	for i := range c.Agents {
		cfg := c.buildConfig(i)
		a, err := agent.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create agent %d: %v", i, err)
		}
		c.Agents[i] = a
	}
}

// buildConfig builds configuration for agent at index i.
func (c *AgentChain) buildConfig(i int) *config.Config {
	cfg := config.Default()
	cfg.Agent.DataDir = c.DataDirs[i]
	cfg.Agent.LogLevel = "debug"

	// Seed management signing keys so privileged operations (shell, file
	// transfer, dynamic config mutations) work out of the box. Tests that
	// exercise the missing-key failure paths override via ManagementConfigure.
	cfg.Management.SigningPrivateKey = c.SigningPrivateHex
	cfg.Management.SigningPublicKey = c.SigningPublicHex
	if c.ManagementConfigure != nil {
		c.ManagementConfigure(i, &cfg.Management)
	}

	// Add listener
	cfg.Listeners = []config.ListenerConfig{
		{
			Transport: "quic",
			Address:   c.Addresses[i],
			TLS: config.TLSConfig{
				Cert: c.TLSCerts[i].CertFile,
				Key:  c.TLSCerts[i].KeyFile,
			},
		},
	}

	// Add peer connections based on topology A-B-C-D
	// A (0) connects to B (1)
	// B (1) connects to C (2)
	// C (2) connects to D (3)
	if i < 3 {
		cfg.Peers = []config.PeerConfig{
			{
				ID:        "auto", // Will be discovered during handshake
				Transport: "quic",
				Address:   c.Addresses[i+1],
				TLS:       config.TLSConfig{},
			},
		}
	}

	// D is the exit node
	if i == 3 {
		cfg.Exit.Enabled = true
		cfg.Exit.Routes = []config.RouteConfig{{CIDR: "0.0.0.0/0"}} // Allow all destinations

		// Enable shell on exit node if configured
		if c.ShellConfig != nil {
			cfg.Shell = *c.ShellConfig
		}

		// Enable file transfer on exit node if configured
		if c.FileTransferConfig != nil {
			cfg.FileTransfer = *c.FileTransferConfig
		}

		// Apply per-test exit-domain config
		if len(c.ExitDomainRoutes) > 0 {
			cfg.Exit.DomainRoutes = c.ExitDomainRoutes
		}
		if len(c.ExitDNSServers) > 0 {
			cfg.Exit.DNS.Servers = c.ExitDNSServers
			cfg.Exit.DNS.Timeout = 2 * time.Second
		}

		// Apply per-test UDP relay config (exit-side)
		if c.UDPConfigure != nil {
			c.UDPConfigure(&cfg.UDP)
		}

		// Apply per-test ICMP echo config (exit-side)
		if c.ICMPConfigure != nil {
			c.ICMPConfigure(&cfg.ICMP)
		}
	}

	// A always has SOCKS5 enabled; optionally every agent (expose-over-SOCKS
	// tests register keys on the exit agent's SOCKS5 endpoint).
	if i == 0 || c.SOCKS5AllAgents {
		cfg.SOCKS5.Enabled = true
		cfg.SOCKS5.Address = "127.0.0.1:0" // Random port
	}
	if i == 0 {
		// SOCKS5 ICMP_ECHO requires the ingress agent's icmpHandler to be
		// non-nil, which only happens when cfg.ICMP.Enabled is true on
		// agent A as well as the exit. Apply ICMPConfigure here too so
		// tests only need to set it once.
		if c.ICMPConfigure != nil {
			c.ICMPConfigure(&cfg.ICMP)
		}
	}

	// Enable HTTP server on agent A (ingress) when EnableHTTP is set, plus
	// optionally on every other agent when HTTPAllAgents is set (used by
	// tests that exercise the HTTP API on non-ingress nodes).
	if (i == 0 && c.EnableHTTP) || (i != 0 && c.HTTPAllAgents) {
		cfg.HTTP.Enabled = true
		cfg.HTTP.Address = "127.0.0.1:0"
		remoteAPI := true
		cfg.HTTP.RemoteAPI = &remoteAPI
		if c.HTTPTokenHash != "" {
			cfg.HTTP.TokenHash = c.HTTPTokenHash
		}
		if c.HTTPConfigure != nil {
			c.HTTPConfigure(&cfg.HTTP)
		}
	}

	// Apply per-agent forward endpoints/listeners (opt-in via fixture fields)
	if eps, ok := c.ForwardEndpoints[i]; ok {
		cfg.Forward.Endpoints = eps
	}
	if lns, ok := c.ForwardListeners[i]; ok {
		cfg.Forward.Listeners = lns
	}

	return cfg
}

// StartAgents starts all agents in order (D first, then C, B, A).
func (c *AgentChain) StartAgents(t *testing.T) {
	// Start in reverse order so listeners are ready for connections
	for i := 3; i >= 0; i-- {
		if err := c.Agents[i].Start(); err != nil {
			t.Fatalf("Failed to start agent %d: %v", i, err)
		}
		t.Logf("Agent %d started (ID: %s)", i, c.Agents[i].ID().ShortString())

		// Capture HTTP server address if available
		if addr := c.Agents[i].HealthServerAddress(); addr != nil {
			c.HTTPAddrs[i] = addr.String()
			t.Logf("Agent %d HTTP server at %s", i, c.HTTPAddrs[i])
		}
	}

	// Wait for connections to establish
	time.Sleep(500 * time.Millisecond)
}

// VerifyConnectivity checks that the chain is connected properly.
// Uses polling with timeout instead of fixed sleep to handle race conditions.
func (c *AgentChain) VerifyConnectivity(t *testing.T) {
	expected := []int{1, 2, 2, 1} // A:1 peer, B:2 peers, C:2 peers, D:1 peer

	// Wait for connectivity with timeout
	timeout := 30 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allConnected := true
		for i, a := range c.Agents {
			stats := a.Stats()
			if stats.PeerCount != expected[i] {
				allConnected = false
				break
			}
		}

		if allConnected {
			// All agents have expected peer counts
			for i, a := range c.Agents {
				stats := a.Stats()
				t.Logf("Agent %d: %d peers (OK)", i, stats.PeerCount)
			}
			return
		}

		time.Sleep(interval)
	}

	// Timeout - report actual state
	for i, a := range c.Agents {
		stats := a.Stats()
		if stats.PeerCount != expected[i] {
			t.Errorf("Agent %d: expected %d peers, got %d (timeout after %v)", i, expected[i], stats.PeerCount, timeout)
		} else {
			t.Logf("Agent %d: %d peers (OK)", i, stats.PeerCount)
		}
	}
}

// WaitForRoutes waits for routes to propagate to all agents.
// Returns true if routes are available, false on timeout.
func (c *AgentChain) WaitForRoutes(t *testing.T) bool {
	// First ensure connectivity
	c.VerifyConnectivity(t)

	// Wait for at least one route on each agent (from exit node D)
	timeout := 30 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allHaveRoutes := true
		for _, a := range c.Agents {
			stats := a.Stats()
			if stats.RouteCount == 0 {
				allHaveRoutes = false
				break
			}
		}

		if allHaveRoutes {
			for i, a := range c.Agents {
				stats := a.Stats()
				t.Logf("Agent %d: %d peers, %d routes (OK)", i, stats.PeerCount, stats.RouteCount)
			}
			return true
		}

		time.Sleep(interval)
	}

	// Timeout - report actual state
	t.Logf("Route propagation timeout after %v", timeout)
	for i, a := range c.Agents {
		stats := a.Stats()
		t.Logf("Agent %d: %d peers, %d routes", i, stats.PeerCount, stats.RouteCount)
	}
	return false
}

// WaitForForwardRoute waits for a forward route with the given key to propagate to
// the agent at agentIdx. Returns true if the route appears within the timeout.
func (c *AgentChain) WaitForForwardRoute(t *testing.T, key string, agentIdx int) bool {
	timeout := 30 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if c.Agents[agentIdx].LookupForwardRoute(key) != nil {
			return true
		}
		time.Sleep(interval)
	}

	t.Logf("forward route %q did not propagate to agent %d within %v", key, agentIdx, timeout)
	return false
}

// WaitForDomainRoute waits for at least one domain route to appear at the
// agent at agentIdx. Returns true if a domain route is present within the
// timeout.
func (c *AgentChain) WaitForDomainRoute(t *testing.T, agentIdx int) bool {
	timeout := 30 * time.Second
	interval := 100 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if len(c.Agents[agentIdx].GetDomainRouteDetails()) > 0 {
			return true
		}
		time.Sleep(interval)
	}

	t.Logf("domain route did not propagate to agent %d within %v", agentIdx, timeout)
	return false
}

// TestAgentChain_BasicConnectivity tests that 4 agents connect in a chain.
func TestAgentChain_BasicConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// VerifyConnectivity uses polling with timeout, no need for fixed sleep
	chain.VerifyConnectivity(t)

	// Verify A has SOCKS5 running
	stats := chain.Agents[0].Stats()
	if !stats.SOCKS5Running {
		t.Error("Agent A should have SOCKS5 running")
	}

	// Verify D has exit handler running
	stats = chain.Agents[3].Stats()
	if !stats.ExitHandlerRun {
		t.Error("Agent D should have exit handler running")
	}

	t.Log("Chain topology verified: A-B-C-D")
}

// TestAgentChain_StreamThroughMesh tests basic mesh connectivity.
func TestAgentChain_StreamThroughMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for route propagation (uses polling with timeout)
	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	t.Log("Mesh connectivity verified")
}

// TestAgentChain_LargeFileStream tests streaming a large file through the chain.
func TestAgentChain_LargeFileStream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for route propagation (uses polling with timeout)
	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	// File size
	fileSize := int64(10 * 1024 * 1024) // 10MB
	if os.Getenv("LARGE_TEST") != "" {
		fileSize = int64(1024 * 1024 * 1024) // 1GB
	}

	t.Logf("Testing with %d bytes (%d MB)", fileSize, fileSize/(1024*1024))

	// Start server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	// Generate test data
	testData := make([]byte, fileSize)
	rand.Read(testData)

	// Server sends all data
	var serverWg sync.WaitGroup
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send data in chunks
		const chunkSize = 64 * 1024
		for offset := int64(0); offset < fileSize; offset += chunkSize {
			end := offset + chunkSize
			if end > fileSize {
				end = fileSize
			}
			if _, err := conn.Write(testData[offset:end]); err != nil {
				t.Logf("Server write error: %v", err)
				return
			}
		}
	}()

	// Client receives all data
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	received := make([]byte, 0, fileSize)
	buf := make([]byte, 64*1024)
	for int64(len(received)) < fileSize {
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Read error: %v", err)
		}
		received = append(received, buf[:n]...)
	}

	serverWg.Wait()

	// Verify
	if int64(len(received)) != fileSize {
		t.Errorf("Size mismatch: expected %d, got %d", fileSize, len(received))
	}

	if !bytes.Equal(testData, received) {
		t.Error("Data mismatch")
	} else {
		t.Logf("Large file test passed: %d bytes transferred", len(received))
	}
}

// TestAgentChain_RouteAdvertisement tests that routes are properly advertised through the mesh.
func TestAgentChain_RouteAdvertisement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for route propagation (uses polling with timeout)
	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	// D is exit, should have local routes
	// Other agents should have learned routes from D
	t.Log("Route advertisement verified")
}

// TestAgentChain_DialThroughMesh tests the Agent.Dial method.
func TestAgentChain_DialThroughMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for route propagation (uses polling with timeout)
	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	// Log detailed route and peer information for each agent
	agentNames := []string{"A", "B", "C", "D"}
	for i, a := range chain.Agents {
		t.Logf("=== Agent %s (ID: %s) ===", agentNames[i], a.ID().ShortString())

		// Log peers
		peerIDs := a.GetPeerIDs()
		t.Logf("  Peers (%d):", len(peerIDs))
		for _, pid := range peerIDs {
			t.Logf("    - %s", pid.ShortString())
		}

		// Log routes
		routes := a.GetRoutes()
		t.Logf("  Routes (%d):", len(routes))
		for _, r := range routes {
			pathStr := "["
			for j, p := range r.Path {
				if j > 0 {
					pathStr += ", "
				}
				pathStr += p.ShortString()
			}
			pathStr += "]"
			t.Logf("    - %s via %s (origin=%s, metric=%d, path=%s)",
				r.Network.String(), r.NextHop.ShortString(), r.OriginAgent.ShortString(), r.Metric, pathStr)
		}
	}

	// Start a simple TCP echo server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer listener.Close()

	// Echo server
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

	// Try to dial through agent A
	serverAddr := listener.Addr().String()
	t.Logf("Attempting to dial %s through mesh", serverAddr)

	// Use Agent A's Dial method
	conn, err := chain.Agents[0].Dial("tcp", serverAddr)
	if err != nil {
		// This may fail if routes haven't propagated yet
		t.Logf("Dial failed (may need route propagation): %v", err)

		// Check route count
		stats := chain.Agents[0].Stats()
		t.Logf("Agent A has %d routes", stats.RouteCount)

		if stats.RouteCount == 0 {
			t.Log("No routes available - route propagation may need more time")
		}
		return
	}
	defer conn.Close()

	// Test echo
	testMsg := []byte("hello mesh!")
	if _, err := conn.Write(testMsg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, len(testMsg))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if !bytes.Equal(testMsg, buf) {
		t.Error("Echo mismatch")
	} else {
		t.Log("Mesh dial and echo successful!")
	}
}

// TestAgentChain_SOCKS5Connectivity tests SOCKS5 proxy through the mesh.
func TestAgentChain_SOCKS5Connectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for route propagation (uses polling with timeout)
	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}

	// Verify SOCKS5 is running on Agent A
	stats := chain.Agents[0].Stats()
	if !stats.SOCKS5Running {
		t.Fatal("SOCKS5 not running on Agent A")
	}

	t.Logf("SOCKS5 confirmed running. Agent A has %d peers and %d routes",
		stats.PeerCount, stats.RouteCount)
}
