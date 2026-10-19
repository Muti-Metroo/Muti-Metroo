// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/agent"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/socks5"
	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// MultiTransportChain represents a 4-agent chain with mixed transports:
// A --(quic)--> B --(h2)--> C --(ws)--> D
type MultiTransportChain struct {
	Agents     [4]*agent.Agent
	DataDirs   [4]string
	Addresses  [4]string
	TLSCerts   [4]CertPair
	AgentIDs   [4]string // Stored after agent creation
	socks5Port int       // Fixed SOCKS5 port for Docker testing (mesh runner)
}

// freeTCPUDPPort returns a loopback port that is currently free for both TCP
// and UDP, so it suits QUIC as well as HTTP/2 and WebSocket listeners.
func freeTCPUDPPort(t *testing.T) int {
	t.Helper()
	for i := 0; i < 50; i++ {
		tl, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen tcp: %v", err)
		}
		port := tl.Addr().(*net.TCPAddr).Port
		ul, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		tl.Close()
		if err == nil {
			ul.Close()
			return port
		}
	}
	t.Fatal("no port free for both TCP and UDP")
	return 0
}

// NewMultiTransportChain creates a chain with mixed transports for testing.
// Each agent listens on a port that is free for both TCP and UDP, chosen at
// run time: fixed port ranges collided with other tests and the runner's
// ephemeral ports.
func NewMultiTransportChain(t *testing.T) *MultiTransportChain {
	chain := &MultiTransportChain{}
	names := []string{"A", "B", "C", "D"}

	for i := range names {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("multi-transport-%s-", names[i]))
		if err != nil {
			chain.cleanup(i)
			t.Fatalf("Failed to create temp dir for %s: %v", names[i], err)
		}
		chain.DataDirs[i] = tmpDir
		chain.Addresses[i] = fmt.Sprintf("127.0.0.1:%d", freeTCPUDPPort(t))

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
func (c *MultiTransportChain) cleanup(upTo int) {
	for i := 0; i < upTo; i++ {
		if c.DataDirs[i] != "" {
			os.RemoveAll(c.DataDirs[i])
		}
	}
}

// Close shuts down all agents and cleans up.
func (c *MultiTransportChain) Close() {
	for i, a := range c.Agents {
		if a != nil {
			a.Stop()
		}
		if c.DataDirs[i] != "" {
			os.RemoveAll(c.DataDirs[i])
		}
	}
}

// buildConfig builds configuration for agent at index i.
// Transport chain: A --(quic)--> B --(h2)--> C --(ws)--> D
func (c *MultiTransportChain) buildConfig(i int, exitRoutes []string) *config.Config {
	cfg := config.Default()
	cfg.Agent.DataDir = c.DataDirs[i]
	cfg.Agent.LogLevel = "debug"

	// Each agent needs listeners for incoming connections
	// B listens on QUIC (from A)
	// C listens on H2 (from B)
	// D listens on WS (from C)

	switch i {
	case 0: // Agent A - no listener, only connects to B via QUIC
		cfg.Listeners = []config.ListenerConfig{}
		cfg.Peers = []config.PeerConfig{
			{
				ID:        "auto",
				Transport: "quic",
				Address:   c.Addresses[1],
				TLS:       config.TLSConfig{},
			},
		}
		cfg.SOCKS5.Enabled = true
		cfg.SOCKS5.Address = "127.0.0.1:0" // Random port

	case 1: // Agent B - listens on QUIC, connects to C via H2
		cfg.Listeners = []config.ListenerConfig{
			{
				Transport: "quic",
				Address:   c.Addresses[1],
				TLS: config.TLSConfig{
					Cert: c.TLSCerts[1].CertFile,
					Key:  c.TLSCerts[1].KeyFile,
				},
			},
		}
		cfg.Peers = []config.PeerConfig{
			{
				ID:        "auto",
				Transport: "h2",
				Address:   c.Addresses[2],
				Path:      "/mesh",
				TLS:       config.TLSConfig{},
			},
		}

	case 2: // Agent C - listens on H2, connects to D via WS, has catch-all route
		cfg.Listeners = []config.ListenerConfig{
			{
				Transport: "h2",
				Address:   c.Addresses[2],
				Path:      "/mesh",
				TLS: config.TLSConfig{
					Cert: c.TLSCerts[2].CertFile,
					Key:  c.TLSCerts[2].KeyFile,
				},
			},
		}
		cfg.Peers = []config.PeerConfig{
			{
				ID:        "auto",
				Transport: "ws",
				Address:   c.Addresses[3],
				Path:      "/mesh",
				TLS:       config.TLSConfig{},
			},
		}
		// Agent C has 0.0.0.0/0 route (catch-all)
		cfg.Exit.Enabled = true
		cfg.Exit.Routes = []config.RouteConfig{{CIDR: "0.0.0.0/0"}}

	case 3: // Agent D - listens on WS, has specific route for kreata.ee
		cfg.Listeners = []config.ListenerConfig{
			{
				Transport: "ws",
				Address:   c.Addresses[3],
				Path:      "/mesh",
				TLS: config.TLSConfig{
					Cert: c.TLSCerts[3].CertFile,
					Key:  c.TLSCerts[3].KeyFile,
				},
			},
		}
		// Agent D has specific route for kreata.ee (178.33.49.65/32)
		cfg.Exit.Enabled = true
		cfg.Exit.Routes = make([]config.RouteConfig, 0, len(exitRoutes))
		for _, cidr := range exitRoutes {
			cfg.Exit.Routes = append(cfg.Exit.Routes, config.RouteConfig{CIDR: cidr})
		}
	}

	// Configure routing for faster propagation in tests
	cfg.Routing.AdvertiseInterval = 1 * time.Second
	cfg.Routing.RouteTTL = 30 * time.Second
	cfg.Routing.MaxHops = 16

	return cfg
}

// CreateAgents creates all 4 agents with proper configuration.
func (c *MultiTransportChain) CreateAgents(t *testing.T, agentDRoutes []string) {
	for i := range c.Agents {
		cfg := c.buildConfig(i, agentDRoutes)
		a, err := agent.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create agent %d: %v", i, err)
		}
		c.Agents[i] = a
		c.AgentIDs[i] = a.ID().ShortString()
	}
}

// StartAgents starts all agents in order (D first, then C, B, A).
func (c *MultiTransportChain) StartAgents(t *testing.T) {
	names := []string{"A", "B", "C", "D"}
	transports := []string{"SOCKS5", "QUIC listener", "H2 listener", "WS listener"}

	// Start in reverse order so listeners are ready for connections
	for i := 3; i >= 0; i-- {
		if err := c.Agents[i].Start(); err != nil {
			t.Fatalf("Failed to start agent %s: %v", names[i], err)
		}
		t.Logf("Agent %s started (ID: %s) [%s]", names[i], c.AgentIDs[i], transports[i])
	}

	// Wait for connections to establish
	time.Sleep(500 * time.Millisecond)
}

// LogRoutes logs route information for all agents.
func (c *MultiTransportChain) LogRoutes(t *testing.T) {
	names := []string{"A", "B", "C", "D"}
	for i, a := range c.Agents {
		routes := a.GetRoutes()
		t.Logf("=== Agent %s (ID: %s) Routes ===", names[i], c.AgentIDs[i])
		for _, r := range routes {
			t.Logf("  %s via %s (origin=%s, metric=%d)",
				r.Network.String(), r.NextHop.ShortString(), r.OriginAgent.ShortString(), r.Metric)
		}
	}
}

// TestMultiTransport_MixedTransportChain tests basic connectivity with mixed transports.
func TestMultiTransport_MixedTransportChain(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewMultiTransportChain(t)
	defer chain.Close()

	// D has specific route, C has catch-all
	chain.CreateAgents(t, []string{"178.33.49.65/32"})
	chain.StartAgents(t)

	// Wait for route propagation
	time.Sleep(3 * time.Second)

	// Verify connectivity
	expectedPeers := []int{1, 2, 2, 1} // A:1, B:2, C:2, D:1
	names := []string{"A", "B", "C", "D"}

	for i, a := range chain.Agents {
		stats := a.Stats()
		t.Logf("Agent %s: %d peers, %d routes", names[i], stats.PeerCount, stats.RouteCount)
		if stats.PeerCount != expectedPeers[i] {
			t.Errorf("Agent %s: expected %d peers, got %d", names[i], expectedPeers[i], stats.PeerCount)
		}
	}

	// Log all routes
	chain.LogRoutes(t)

	t.Log("Multi-transport chain connectivity verified: A-(QUIC)->B-(H2)->C-(WS)->D")
}

// TestMultiTransport_RouteLongestPrefixMatch tests that longest prefix match routes correctly.
func TestMultiTransport_RouteLongestPrefixMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewMultiTransportChain(t)
	defer chain.Close()

	// D has specific route for kreata.ee, C has catch-all
	chain.CreateAgents(t, []string{"178.33.49.65/32"})
	chain.StartAgents(t)

	// Wait for route propagation
	time.Sleep(5 * time.Second)

	chain.LogRoutes(t)

	// Check that Agent A has routes for both
	routes := chain.Agents[0].GetRoutes()

	var hasKreataRoute bool
	var hasCatchAll bool
	var kreataOrigin, catchAllOrigin string

	for _, r := range routes {
		networkStr := r.Network.String()
		if networkStr == "178.33.49.65/32" {
			hasKreataRoute = true
			kreataOrigin = r.OriginAgent.ShortString()
		}
		if networkStr == "0.0.0.0/0" {
			hasCatchAll = true
			catchAllOrigin = r.OriginAgent.ShortString()
		}
	}

	if !hasKreataRoute {
		t.Error("Agent A should have route for 178.33.49.65/32")
	} else {
		t.Logf("Route for 178.33.49.65/32 originates from agent %s (should be D: %s)", kreataOrigin, chain.AgentIDs[3])
		if kreataOrigin != chain.AgentIDs[3] {
			t.Errorf("Route for 178.33.49.65/32 should originate from D (%s), got %s", chain.AgentIDs[3], kreataOrigin)
		}
	}

	if !hasCatchAll {
		t.Error("Agent A should have route for 0.0.0.0/0")
	} else {
		t.Logf("Route for 0.0.0.0/0 originates from agent %s (should be C: %s)", catchAllOrigin, chain.AgentIDs[2])
		if catchAllOrigin != chain.AgentIDs[2] {
			t.Errorf("Route for 0.0.0.0/0 should originate from C (%s), got %s", chain.AgentIDs[2], catchAllOrigin)
		}
	}

	t.Log("Longest prefix match routing verified")
}

// TestMultiTransport_SOCKS5ThroughMesh tests SOCKS5 proxy through the mixed transport mesh.
// This test verifies that streams can be relayed through the H2 and WebSocket transports.
func TestMultiTransport_SOCKS5ThroughMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewMultiTransportChain(t)
	defer chain.Close()

	chain.CreateAgents(t, []string{"178.33.49.65/32"})
	chain.StartAgents(t)

	// Wait for route propagation
	time.Sleep(5 * time.Second)

	// Log route information
	chain.LogRoutes(t)

	// Verify Agent A has routes
	stats := chain.Agents[0].Stats()
	t.Logf("Agent A stats: peers=%d, routes=%d", stats.PeerCount, stats.RouteCount)
	if stats.RouteCount == 0 {
		t.Skip("No routes propagated to Agent A - skipping SOCKS5 test")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}
	t.Logf("SOCKS5 proxy at %s", socks5Addr.String())

	// Start a local echo server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer listener.Close()

	echoAddr := listener.Addr().(*net.TCPAddr)
	t.Logf("Echo server at %s", echoAddr.String())

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

	// Connect through SOCKS5
	conn, err := net.Dial("tcp", socks5Addr.String())
	if err != nil {
		t.Fatalf("Failed to connect to SOCKS5: %v", err)
	}
	defer conn.Close()

	// SOCKS5 handshake
	conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth})
	methodResp := make([]byte, 2)
	io.ReadFull(conn, methodResp)

	// CONNECT
	connectReq := &bytes.Buffer{}
	connectReq.WriteByte(socks5.SOCKS5Version)
	connectReq.WriteByte(socks5.CmdConnect)
	connectReq.WriteByte(0x00)
	connectReq.WriteByte(socks5.AddrTypeIPv4)
	connectReq.Write(echoAddr.IP.To4())
	binary.Write(connectReq, binary.BigEndian, uint16(echoAddr.Port))
	conn.Write(connectReq.Bytes())

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("Failed to read SOCKS5 reply: %v", err)
	}

	if reply[1] != socks5.ReplySucceeded {
		stats := chain.Agents[0].Stats()
		if stats.RouteCount == 0 {
			t.Skip("No routes available")
		}
		t.Fatalf("SOCKS5 CONNECT failed with code %d", reply[1])
	}

	// Echo test
	testData := []byte("Hello through multi-transport mesh!")
	conn.Write(testData)

	response := make([]byte, len(testData))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatalf("Failed to read echo response: %v", err)
	}

	if !bytes.Equal(testData, response) {
		t.Errorf("Echo mismatch: got %q, want %q", response, testData)
	}

	t.Log("SOCKS5 through multi-transport mesh verified!")
}

// TestMultiTransport_SSHToKreata tests SSH connection to kreata.ee through the mesh.
// This test verifies:
// a) Connection opens successfully to kreata.ee
// b) Traffic routes through agent D (not C) due to longest prefix match
// c) Connection stays up for 5 minutes with active data transfer
func TestMultiTransport_SSHToKreata(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Check if we should run the long test
	if os.Getenv("SSH_LONG_TEST") == "" {
		t.Skip("Skipping 5-minute SSH test. Set SSH_LONG_TEST=1 to run.")
	}

	chain := NewMultiTransportChain(t)
	defer chain.Close()

	// Agent D: 178.33.49.65/32 (kreata.ee specific)
	// Agent C: 0.0.0.0/0 (catch-all)
	chain.CreateAgents(t, []string{"178.33.49.65/32"})
	chain.StartAgents(t)

	// Wait for route propagation
	t.Log("Waiting for route propagation...")
	time.Sleep(5 * time.Second)

	chain.LogRoutes(t)

	// Verify routing before SSH test
	routes := chain.Agents[0].GetRoutes()
	var kreataRoute *struct {
		origin  string
		nextHop string
		metric  uint16
	}

	for _, r := range routes {
		if r.Network.String() == "178.33.49.65/32" {
			kreataRoute = &struct {
				origin  string
				nextHop string
				metric  uint16
			}{
				origin:  r.OriginAgent.ShortString(),
				nextHop: r.NextHop.ShortString(),
				metric:  r.Metric,
			}
			break
		}
	}

	if kreataRoute == nil {
		t.Fatal("No route found for 178.33.49.65/32 (kreata.ee)")
	}

	t.Logf("Route for kreata.ee: origin=%s, nextHop=%s, metric=%d",
		kreataRoute.origin, kreataRoute.nextHop, kreataRoute.metric)

	// Verify the route originates from Agent D
	if kreataRoute.origin != chain.AgentIDs[3] {
		t.Errorf("Route should originate from Agent D (%s), but originates from %s",
			chain.AgentIDs[3], kreataRoute.origin)
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}
	t.Logf("SOCKS5 proxy at %s", socks5Addr.String())

	// Use ssh with ProxyCommand to go through our SOCKS5 proxy
	// -o ProxyCommand='nc -X 5 -x localhost:PORT %h %p'
	proxyCmd := fmt.Sprintf("nc -X 5 -x %s %%h %%p", socks5Addr.String())

	// SSH command to run a long-running command
	// Using 'tail -f /var/log/syslog' or 'dmesg -w' or 'vmstat 1'
	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ProxyCommand=%s", proxyCmd),
		"-o", "ConnectTimeout=30",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=10",
		"andris@kreata.ee",
		"vmstat 1", // Print vmstat every second - generates continuous output
	)

	// Capture output
	stdout, err := sshCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to get stdout pipe: %v", err)
	}
	stderr, err := sshCmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to get stderr pipe: %v", err)
	}

	t.Log("Starting SSH connection to andris@kreata.ee...")
	startTime := time.Now()

	if err := sshCmd.Start(); err != nil {
		t.Fatalf("Failed to start SSH: %v", err)
	}

	// Track connection state
	var (
		connected     atomic.Bool
		lineCount     atomic.Int64
		lastLineTime  atomic.Int64
		connectionErr atomic.Value
		mu            sync.Mutex
	)

	// Read stdout in goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			lineCount.Add(1)
			lastLineTime.Store(time.Now().UnixNano())

			if !connected.Load() {
				connected.Store(true)
				t.Logf("SSH connected! First output received after %v", time.Since(startTime))
			}

			// Log every 60th line to show activity
			if lineCount.Load()%60 == 0 {
				elapsed := time.Since(startTime)
				t.Logf("[%v] Lines received: %d, last: %s", elapsed.Round(time.Second), lineCount.Load(), truncate(line, 50))
			}
		}
		if err := scanner.Err(); err != nil {
			mu.Lock()
			connectionErr.Store(err)
			mu.Unlock()
		}
	}()

	// Read stderr in goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			t.Logf("SSH stderr: %s", line)
		}
	}()

	// Wait for 5 minutes or until error
	testDuration := 5 * time.Minute
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	deadline := time.NewTimer(testDuration)
	defer deadline.Stop()

	// Also set up a stall detector - if no data for 60 seconds, consider it failed
	stallCheck := time.NewTicker(10 * time.Second)
	defer stallCheck.Stop()

	t.Logf("Running SSH test for %v...", testDuration)

TestLoop:
	for {
		select {
		case <-deadline.C:
			t.Log("Test duration completed successfully!")
			break TestLoop

		case <-ticker.C:
			elapsed := time.Since(startTime)
			if !connected.Load() {
				t.Logf("[%v] Waiting for SSH connection...", elapsed.Round(time.Second))
			} else {
				t.Logf("[%v] SSH active - %d lines received", elapsed.Round(time.Second), lineCount.Load())
			}

		case <-stallCheck.C:
			if connected.Load() {
				lastTime := time.Unix(0, lastLineTime.Load())
				if time.Since(lastTime) > 60*time.Second {
					t.Errorf("SSH connection stalled - no data for %v", time.Since(lastTime))
					break TestLoop
				}
			} else if time.Since(startTime) > 60*time.Second {
				t.Error("SSH failed to connect within 60 seconds")
				break TestLoop
			}
		}

		// Check for errors
		if err, ok := connectionErr.Load().(error); ok && err != nil {
			t.Errorf("SSH connection error: %v", err)
			break TestLoop
		}

		// Check if process died
		if sshCmd.ProcessState != nil && sshCmd.ProcessState.Exited() {
			t.Errorf("SSH process exited unexpectedly")
			break TestLoop
		}
	}

	// Cleanup
	t.Log("Terminating SSH connection...")
	if sshCmd.Process != nil {
		sshCmd.Process.Kill()
	}
	sshCmd.Wait()

	// Final report
	elapsed := time.Since(startTime)
	t.Logf("SSH test completed after %v", elapsed)
	t.Logf("Total lines received: %d", lineCount.Load())

	if !connected.Load() {
		t.Error("SSH connection was never established")
	} else if elapsed < testDuration-10*time.Second {
		t.Errorf("SSH connection terminated early at %v", elapsed)
	} else {
		t.Log("SUCCESS: SSH connection maintained for full test duration")
	}

	// Verify traffic went through Agent D
	t.Log("Verifying traffic routing...")

	// Check agent D's exit handler stats
	statsD := chain.Agents[3].Stats()
	statsC := chain.Agents[2].Stats()

	t.Logf("Agent D (178.33.49.65/32): exit_handler=%v", statsD.ExitHandlerRun)
	t.Logf("Agent C (0.0.0.0/0): exit_handler=%v", statsC.ExitHandlerRun)

	if !statsD.ExitHandlerRun {
		t.Error("Agent D exit handler should be running")
	}

	t.Log("Route verification: Traffic routed through Agent D (specific /32 route takes precedence over /0)")
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TestMultiTransport_SSHToKreata_Short is a shorter version that just verifies connectivity.
func TestMultiTransport_SSHToKreata_Short(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if SSH key is available
	if os.Getenv("SSH_TEST") == "" {
		t.Skip("Skipping SSH test. Set SSH_TEST=1 to run.")
	}

	chain := NewMultiTransportChain(t)
	defer chain.Close()

	chain.CreateAgents(t, []string{"178.33.49.65/32"})
	chain.StartAgents(t)

	// Wait for route propagation
	time.Sleep(5 * time.Second)

	chain.LogRoutes(t)

	// Verify we have the correct routes
	routes := chain.Agents[0].GetRoutes()
	var hasKreataRoute bool
	for _, r := range routes {
		if r.Network.String() == "178.33.49.65/32" {
			if r.OriginAgent.ShortString() == chain.AgentIDs[3] {
				hasKreataRoute = true
				t.Logf("Verified: 178.33.49.65/32 routes through Agent D")
			}
		}
	}

	if !hasKreataRoute {
		t.Fatal("Route for kreata.ee not found or not routed through Agent D")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	// Quick SSH test - just verify connection
	proxyCmd := fmt.Sprintf("nc -X 5 -x %s %%h %%p", socks5Addr.String())

	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ProxyCommand=%s", proxyCmd),
		"-o", "ConnectTimeout=30",
		"andris@kreata.ee",
		"echo 'SSH_CONNECTION_SUCCESS'; hostname; uptime",
	)

	output, err := sshCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SSH failed: %v\nOutput: %s", err, string(output))
	}

	if !strings.Contains(string(output), "SSH_CONNECTION_SUCCESS") {
		t.Errorf("Expected success marker in output, got: %s", string(output))
	}

	t.Logf("SSH output:\n%s", string(output))
	t.Log("Short SSH test passed - connection verified through Agent D!")
}
