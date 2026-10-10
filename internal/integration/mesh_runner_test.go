// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/agent"
	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// TestMeshRunner starts the multi-transport mesh with a fixed SOCKS5 port.
// Run from Docker with port mapping for SSH testing from host.
// Usage: docker run -p 51080:51080 -e SSH_MESH_RUNNER=1 muti-metroo-test ...
func TestMeshRunner(t *testing.T) {
	if os.Getenv("SSH_MESH_RUNNER") == "" {
		t.Skip("Set SSH_MESH_RUNNER=1 to run the mesh for manual SSH testing")
	}

	// Use fixed ports
	portBase := 50000
	socks5Port := 51080
	chain := NewMultiTransportChainFixed(t, portBase, socks5Port)
	defer chain.Close()

	// D has specific route for kreata.ee, C has catch-all
	chain.CreateAgentsFixed(t, []string{"178.33.49.65/32"})
	chain.StartAgents(t)

	// Wait for route propagation
	t.Log("Waiting for route propagation...")
	time.Sleep(5 * time.Second)

	chain.LogRoutes(t)

	// Verify routing
	routes := chain.Agents[0].GetRoutes()
	for _, r := range routes {
		if r.Network.String() == "178.33.49.65/32" {
			if r.OriginAgent.ShortString() == chain.AgentIDs[3] {
				t.Logf("Verified: 178.33.49.65/32 routes through Agent D")
			}
		}
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	fmt.Printf("\n")
	fmt.Printf("=================================================================\n")
	fmt.Printf("MULTI-TRANSPORT MESH RUNNING\n")
	fmt.Printf("=================================================================\n")
	fmt.Printf("\n")
	fmt.Printf("SOCKS5 proxy address: %s\n", socks5Addr.String())
	fmt.Printf("\n")
	fmt.Printf("To connect via SSH from host terminal, run:\n")
	fmt.Printf("\n")
	fmt.Printf("  ssh -o ProxyCommand='nc -X 5 -x localhost:%d %%h %%p' andris@kreata.ee\n", socks5Port)
	fmt.Printf("\n")
	fmt.Printf("For the 5-minute test:\n")
	fmt.Printf("\n")
	fmt.Printf("  ssh -o ProxyCommand='nc -X 5 -x localhost:%d %%h %%p' andris@kreata.ee 'vmstat 1'\n", socks5Port)
	fmt.Printf("\n")
	fmt.Printf("Mesh will run for 10 minutes. Press Ctrl+C to stop earlier.\n")
	fmt.Printf("=================================================================\n")

	// Run for 10 minutes or until killed
	time.Sleep(10 * time.Minute)

	t.Log("Stopping mesh...")
}

// NewMultiTransportChainFixed creates a chain with a fixed SOCKS5 port.
func NewMultiTransportChainFixed(t *testing.T, portBase, socks5Port int) *MultiTransportChain {
	chain := &MultiTransportChain{}
	names := []string{"A", "B", "C", "D"}

	for i := range names {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("multi-transport-%s-", names[i]))
		if err != nil {
			chain.cleanup(i)
			t.Fatalf("Failed to create temp dir for %s: %v", names[i], err)
		}
		chain.DataDirs[i] = tmpDir
		chain.Addresses[i] = fmt.Sprintf("0.0.0.0:%d", portBase+i)

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

	// Store SOCKS5 port for fixed config
	chain.socks5Port = socks5Port

	return chain
}

// socks5Port is stored for fixed port configuration
func (c *MultiTransportChain) buildConfigFixed(i int, exitRoutes []string, socks5Port int) *config.Config {
	cfg := c.buildConfig(i, exitRoutes)
	if i == 0 {
		// Agent A - use fixed SOCKS5 port bound to all interfaces for Docker
		cfg.SOCKS5.Address = fmt.Sprintf("0.0.0.0:%d", socks5Port)
	}
	return cfg
}

// CreateAgentsFixed creates agents with fixed ports.
func (c *MultiTransportChain) CreateAgentsFixed(t *testing.T, agentDRoutes []string) {
	for i := range c.Agents {
		cfg := c.buildConfigFixed(i, agentDRoutes, c.socks5Port)
		a, err := agent.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create agent %d: %v", i, err)
		}
		c.Agents[i] = a
		c.AgentIDs[i] = a.ID().ShortString()
	}
}
