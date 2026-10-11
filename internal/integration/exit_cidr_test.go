// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/agent"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/socks5"
	"github.com/Muti-Metroo/Muti-Metroo/internal/transport"
)

// CIDRFilterChain is a specialized chain for testing CIDR filtering.
type CIDRFilterChain struct {
	AgentChain
	AllowedCIDRs []string
}

// NewCIDRFilterChain creates a chain with specific CIDR filtering on exit node.
func NewCIDRFilterChain(t *testing.T, allowedCIDRs []string) *CIDRFilterChain {
	chain := &CIDRFilterChain{
		AllowedCIDRs: allowedCIDRs,
	}

	names := []string{"A", "B", "C", "D"}

	// Allocate free UDP ports for QUIC listeners
	ports, err := allocateFreeUDPPorts(4)
	if err != nil {
		t.Fatalf("Failed to allocate ports: %v", err)
	}

	for i := range names {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("cidr-agent-%s-", names[i]))
		if err != nil {
			chain.cleanup(i)
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		chain.DataDirs[i] = tmpDir
		chain.Addresses[i] = ports[i]

		certPEM, keyPEM, err := transport.GenerateSelfSignedCert("agent-"+names[i], 24*time.Hour)
		if err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to generate cert: %v", err)
		}

		certFile := tmpDir + "/cert.pem"
		keyFile := tmpDir + "/key.pem"
		if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to write cert: %v", err)
		}
		if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to write key: %v", err)
		}
		chain.TLSCerts[i] = CertPair{CertFile: certFile, KeyFile: keyFile}
	}

	return chain
}

// CreateAgentsWithCIDR creates agents with CIDR filtering on exit node.
func (c *CIDRFilterChain) CreateAgentsWithCIDR(t *testing.T) {
	for i := range c.Agents {
		cfg := c.buildConfig(i)

		// Override exit routes on D
		if i == 3 {
			cfg.Exit.Routes = make([]config.RouteConfig, 0, len(c.AllowedCIDRs))
			for _, cidr := range c.AllowedCIDRs {
				cfg.Exit.Routes = append(cfg.Exit.Routes, config.RouteConfig{CIDR: cidr})
			}
		}

		a, err := agent.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create agent %d: %v", i, err)
		}
		c.Agents[i] = a
	}
}

// TestExitCIDRFiltering tests that exit node respects CIDR filtering.
func TestExitCIDRFiltering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Only allow connections to 127.0.0.0/8 (localhost)
	chain := NewCIDRFilterChain(t, []string{"127.0.0.0/8"})
	defer chain.Close()

	chain.CreateAgentsWithCIDR(t)
	chain.StartAgents(t)

	// Wait for routes with polling
	if !chain.WaitForRoutes(t) {
		t.Fatal("Routes did not propagate in time")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	// Start allowed echo server on localhost
	allowedListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start allowed server: %v", err)
	}

	allowedAddr := allowedListener.Addr().(*net.TCPAddr)
	t.Logf("Allowed server on %s", allowedAddr.String())

	// Track echo server connections for cleanup
	var echoConns []net.Conn
	var echoMu sync.Mutex

	go func() {
		for {
			conn, err := allowedListener.Accept()
			if err != nil {
				return
			}
			echoMu.Lock()
			echoConns = append(echoConns, conn)
			echoMu.Unlock()
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Cleanup function to close all echo connections
	defer func() {
		allowedListener.Close()
		echoMu.Lock()
		for _, c := range echoConns {
			c.Close()
		}
		echoMu.Unlock()
		// Small delay for connection cleanup
		time.Sleep(100 * time.Millisecond)
	}()

	t.Run("AllowedDestination", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// SOCKS5 handshake
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth})
		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)

		// CONNECT to allowed address
		connectReq := &bytes.Buffer{}
		connectReq.WriteByte(socks5.SOCKS5Version)
		connectReq.WriteByte(socks5.CmdConnect)
		connectReq.WriteByte(0x00)
		connectReq.WriteByte(socks5.AddrTypeIPv4)
		connectReq.Write(allowedAddr.IP.To4())
		binary.Write(connectReq, binary.BigEndian, uint16(allowedAddr.Port))
		conn.Write(connectReq.Bytes())

		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		reply := make([]byte, 10)
		_, err = io.ReadFull(conn, reply)
		if err != nil {
			t.Fatalf("Failed to read reply: %v", err)
		}

		if reply[1] != socks5.ReplySucceeded {
			stats := chain.Agents[0].Stats()
			if stats.RouteCount == 0 {
				t.Skip("No routes available")
			}
			t.Fatalf("Expected success for allowed destination, got %d", reply[1])
		}

		// Echo test
		testData := []byte("Allowed destination!")
		conn.Write(testData)
		response := make([]byte, len(testData))
		io.ReadFull(conn, response)

		if !bytes.Equal(response, testData) {
			t.Errorf("Echo mismatch: got %q, want %q", response, testData)
		}

		t.Log("Allowed destination test passed")
	})

	t.Run("BlockedDestination", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// SOCKS5 handshake
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth})
		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)

		// CONNECT to blocked address (10.0.0.1 - not in 127.0.0.0/8)
		blockedIP := net.ParseIP("10.0.0.1").To4()
		connectReq := &bytes.Buffer{}
		connectReq.WriteByte(socks5.SOCKS5Version)
		connectReq.WriteByte(socks5.CmdConnect)
		connectReq.WriteByte(0x00)
		connectReq.WriteByte(socks5.AddrTypeIPv4)
		connectReq.Write(blockedIP)
		binary.Write(connectReq, binary.BigEndian, uint16(8080))
		conn.Write(connectReq.Bytes())

		// Use shorter timeout - blocked should respond quickly or timeout
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		reply := make([]byte, 10)
		_, err = io.ReadFull(conn, reply)

		// Either timeout (connection refused/blocked) or explicit rejection is acceptable
		if err != nil {
			// Timeout or EOF means the connection was blocked
			t.Logf("Blocked destination: connection error (expected): %v", err)
			return
		}

		// Should NOT succeed - destination is blocked
		if reply[1] == socks5.ReplySucceeded {
			stats := chain.Agents[0].Stats()
			if stats.RouteCount == 0 {
				t.Skip("No routes available - can't test blocking")
			}
			t.Error("Expected blocked destination to be rejected, but got success")
		} else {
			t.Logf("Blocked destination correctly rejected with code %d", reply[1])
		}
	})
}

// TestExitCIDRFiltering_MultipleRanges tests CIDR filtering with multiple allowed ranges.
func TestExitCIDRFiltering_MultipleRanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Allow 127.0.0.0/8 and 192.168.0.0/16
	chain := NewCIDRFilterChain(t, []string{"127.0.0.0/8", "192.168.0.0/16"})
	defer chain.Close()

	chain.CreateAgentsWithCIDR(t)
	chain.StartAgents(t)

	// Wait for routes with polling
	if !chain.WaitForRoutes(t) {
		t.Fatal("Routes did not propagate in time")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	// Start echo server on localhost
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}

	echoAddr := echoListener.Addr().(*net.TCPAddr)

	// Track echo server connections for cleanup
	var echoConns []net.Conn
	var echoMu sync.Mutex

	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			echoMu.Lock()
			echoConns = append(echoConns, conn)
			echoMu.Unlock()
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Cleanup function to close all echo connections
	defer func() {
		echoListener.Close()
		echoMu.Lock()
		for _, c := range echoConns {
			c.Close()
		}
		echoMu.Unlock()
		// Small delay for connection cleanup
		time.Sleep(100 * time.Millisecond)
	}()

	tests := []struct {
		name    string
		ip      string
		allowed bool
	}{
		{"localhost", "127.0.0.1", true},
		{"private_192_168", "192.168.1.1", true},
		{"blocked_10_0", "10.0.0.1", false},
		{"blocked_8_8", "8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", socks5Addr.String())
			if err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}
			defer conn.Close()

			// SOCKS5 handshake
			conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth})
			methodResp := make([]byte, 2)
			io.ReadFull(conn, methodResp)

			// For allowed IPs, use actual echo server port
			// For blocked IPs, use any port (connection won't succeed anyway)
			port := uint16(8080)
			ip := net.ParseIP(tt.ip).To4()

			if tt.ip == "127.0.0.1" {
				port = uint16(echoAddr.Port)
			}

			connectReq := &bytes.Buffer{}
			connectReq.WriteByte(socks5.SOCKS5Version)
			connectReq.WriteByte(socks5.CmdConnect)
			connectReq.WriteByte(0x00)
			connectReq.WriteByte(socks5.AddrTypeIPv4)
			connectReq.Write(ip)
			binary.Write(connectReq, binary.BigEndian, port)
			conn.Write(connectReq.Bytes())

			// Use shorter timeout for blocked destinations
			timeout := 10 * time.Second
			if !tt.allowed {
				timeout = 3 * time.Second
			}
			conn.SetReadDeadline(time.Now().Add(timeout))
			reply := make([]byte, 10)
			_, err = io.ReadFull(conn, reply)

			stats := chain.Agents[0].Stats()
			if stats.RouteCount == 0 {
				t.Skip("No routes available")
			}

			if tt.allowed && tt.ip == "127.0.0.1" {
				// Only localhost can actually succeed (we have a server there)
				if err != nil {
					t.Fatalf("Failed to read reply: %v", err)
				}
				if reply[1] != socks5.ReplySucceeded {
					t.Errorf("Expected success for %s, got %d", tt.ip, reply[1])
				}
			} else if !tt.allowed {
				// Blocked - either timeout, EOF, or explicit rejection is acceptable
				if err != nil {
					t.Logf("Blocked IP %s: connection error (expected): %v", tt.ip, err)
					return
				}
				if reply[1] == socks5.ReplySucceeded {
					t.Errorf("Expected rejection for %s, but got success", tt.ip)
				} else {
					t.Logf("Blocked IP %s correctly rejected with code %d", tt.ip, reply[1])
				}
			}
			// 192.168.x.x may fail with connection refused (no server) but shouldn't
			// be rejected by CIDR filter
		})
	}
}
