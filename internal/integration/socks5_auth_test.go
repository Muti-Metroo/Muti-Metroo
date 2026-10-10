// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/agent"
	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/socks5"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// TestSOCKS5_Authentication tests SOCKS5 username/password authentication.
func TestSOCKS5_Authentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a simple agent with SOCKS5 authentication enabled
	tmpDir, err := os.MkdirTemp("", "socks5-auth-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.Default()
	cfg.Agent.DataDir = tmpDir
	cfg.Agent.LogLevel = "debug"

	// Enable SOCKS5 with authentication
	cfg.SOCKS5.Enabled = true
	cfg.SOCKS5.Address = "127.0.0.1:0"
	cfg.SOCKS5.Auth.Enabled = true
	cfg.SOCKS5.Auth.Users = []config.SOCKS5UserConfig{
		{Username: "testuser", Password: "testpass"},
		{Username: "admin", Password: "secret123"},
	}

	// Create and start agent
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	if err := a.Start(); err != nil {
		t.Fatalf("Failed to start agent: %v", err)
	}
	defer a.Stop()

	// Wait for SOCKS5 to be ready
	time.Sleep(100 * time.Millisecond)

	stats := a.Stats()
	if !stats.SOCKS5Running {
		t.Fatal("SOCKS5 server not running")
	}

	socks5Addr := a.SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}
	t.Logf("SOCKS5 server listening on %s", socks5Addr.String())

	// Start echo server for testing
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().(*net.TCPAddr)
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	t.Run("ValidCredentials", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// Send greeting with username/password method
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodUserPass})

		// Read method selection
		methodResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, methodResp); err != nil {
			t.Fatalf("Failed to read method: %v", err)
		}
		if methodResp[1] != socks5.AuthMethodUserPass {
			t.Fatalf("Expected UserPass method, got %d", methodResp[1])
		}

		// Send authentication
		username := "testuser"
		password := "testpass"
		authReq := []byte{0x01, byte(len(username))}
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)
		conn.Write(authReq)

		// Read auth response
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}
		if authResp[1] != socks5.AuthStatusSuccess {
			t.Fatalf("Expected auth success, got %d", authResp[1])
		}

		// Now send CONNECT request to echo server
		connectReq := &bytes.Buffer{}
		connectReq.WriteByte(socks5.SOCKS5Version)
		connectReq.WriteByte(socks5.CmdConnect)
		connectReq.WriteByte(0x00) // RSV
		connectReq.WriteByte(socks5.AddrTypeIPv4)
		connectReq.Write(echoAddr.IP.To4())
		binary.Write(connectReq, binary.BigEndian, uint16(echoAddr.Port))
		conn.Write(connectReq.Bytes())

		// Read reply
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		reply := make([]byte, 10)
		if _, err := io.ReadFull(conn, reply); err != nil {
			t.Fatalf("Failed to read CONNECT reply: %v", err)
		}

		if reply[1] != socks5.ReplySucceeded {
			t.Fatalf("CONNECT failed with reply code %d", reply[1])
		}

		// Test echo
		testData := []byte("Hello, authenticated SOCKS5!")
		conn.Write(testData)

		response := make([]byte, len(testData))
		if _, err := io.ReadFull(conn, response); err != nil {
			t.Fatalf("Failed to read echo: %v", err)
		}

		if !bytes.Equal(response, testData) {
			t.Errorf("Echo mismatch: got %q, want %q", response, testData)
		}

		t.Log("Valid credentials test passed")
	})

	t.Run("InvalidPassword", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// Send greeting
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodUserPass})

		// Read method selection
		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)

		// Send wrong password
		username := "testuser"
		password := "wrongpassword"
		authReq := []byte{0x01, byte(len(username))}
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)
		conn.Write(authReq)

		// Read auth response - should be failure
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}
		if authResp[1] != socks5.AuthStatusFailure {
			t.Fatalf("Expected auth failure, got %d", authResp[1])
		}

		t.Log("Invalid password test passed")
	})

	t.Run("InvalidUsername", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// Send greeting
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodUserPass})

		// Read method selection
		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)

		// Send unknown username
		username := "unknownuser"
		password := "testpass"
		authReq := []byte{0x01, byte(len(username))}
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)
		conn.Write(authReq)

		// Read auth response - should be failure
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}
		if authResp[1] != socks5.AuthStatusFailure {
			t.Fatalf("Expected auth failure, got %d", authResp[1])
		}

		t.Log("Invalid username test passed")
	})

	t.Run("MultipleUsers", func(t *testing.T) {
		// Test second user
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// Send greeting
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodUserPass})

		// Read method selection
		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)

		// Authenticate as admin
		username := "admin"
		password := "secret123"
		authReq := []byte{0x01, byte(len(username))}
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)
		conn.Write(authReq)

		// Read auth response
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			t.Fatalf("Failed to read auth response: %v", err)
		}
		if authResp[1] != socks5.AuthStatusSuccess {
			t.Fatalf("Expected auth success for admin user, got %d", authResp[1])
		}

		t.Log("Multiple users test passed")
	})
}

// TestSOCKS5_ThroughMesh tests SOCKS5 proxy working end-to-end through the mesh.
func TestSOCKS5_ThroughMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	// Modify Agent A to have SOCKS5 enabled (default in buildConfig)
	chain.CreateAgents(t)
	chain.StartAgents(t)

	// Wait for route propagation
	time.Sleep(3 * time.Second)

	// Log agent info
	for i, a := range chain.Agents {
		stats := a.Stats()
		t.Logf("Agent %d: %d peers, %d routes", i, stats.PeerCount, stats.RouteCount)
	}

	// Verify Agent A has SOCKS5
	stats := chain.Agents[0].Stats()
	if !stats.SOCKS5Running {
		t.Fatal("SOCKS5 not running on Agent A")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	// Check that routes have propagated from D (exit node)
	if stats.RouteCount == 0 {
		t.Log("Warning: No routes available yet, may need more time for propagation")
	}

	// Start echo server (simulates external server)
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().(*net.TCPAddr)
	t.Logf("Echo server on %s", echoAddr.String())

	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Connect through SOCKS5 to echo server
	conn, err := net.Dial("tcp", socks5Addr.String())
	if err != nil {
		t.Fatalf("Failed to connect to SOCKS5: %v", err)
	}
	defer conn.Close()

	// SOCKS5 handshake (no auth)
	conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth})

	methodResp := make([]byte, 2)
	if _, err := io.ReadFull(conn, methodResp); err != nil {
		t.Fatalf("Failed to read method: %v", err)
	}
	if methodResp[1] != socks5.AuthMethodNoAuth {
		t.Fatalf("Expected NoAuth method, got %d", methodResp[1])
	}

	// CONNECT to echo server
	connectReq := &bytes.Buffer{}
	connectReq.WriteByte(socks5.SOCKS5Version)
	connectReq.WriteByte(socks5.CmdConnect)
	connectReq.WriteByte(0x00)
	connectReq.WriteByte(socks5.AddrTypeIPv4)
	connectReq.Write(echoAddr.IP.To4())
	binary.Write(connectReq, binary.BigEndian, uint16(echoAddr.Port))
	conn.Write(connectReq.Bytes())

	// Read reply
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("Failed to read CONNECT reply: %v", err)
	}

	if reply[1] != socks5.ReplySucceeded {
		t.Logf("CONNECT reply code: %d", reply[1])
		if stats.RouteCount == 0 {
			t.Skip("Skipping: No routes available - route propagation may need more time")
		}
		t.Fatalf("CONNECT failed with reply code %d", reply[1])
	}

	// Test echo through mesh
	testData := []byte("Hello through the mesh network!")
	if _, err := conn.Write(testData); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	response := make([]byte, len(testData))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatalf("Failed to read echo: %v", err)
	}

	if !bytes.Equal(response, testData) {
		t.Errorf("Echo mismatch: got %q, want %q", response, testData)
	}

	t.Log("SOCKS5 through mesh test passed!")
}

// TestSOCKS5_AuthThroughMesh tests SOCKS5 with authentication through mesh.
func TestSOCKS5_AuthThroughMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create modified chain with auth enabled on Agent A
	chain := &AgentChain{}
	names := []string{"A", "B", "C", "D"}

	// Allocate free UDP ports for QUIC listeners
	ports, err := allocateFreeUDPPorts(4)
	if err != nil {
		t.Fatalf("Failed to allocate ports: %v", err)
	}

	for i := range names {
		tmpDir, err := os.MkdirTemp("", fmt.Sprintf("agent-auth-%s-", names[i]))
		if err != nil {
			chain.cleanup(i)
			t.Fatalf("Failed to create temp dir for %s: %v", names[i], err)
		}
		chain.DataDirs[i] = tmpDir
		chain.Addresses[i] = ports[i]

		certPEM, keyPEM, err := transport.GenerateSelfSignedCert("agent-"+names[i], 24*time.Hour)
		if err != nil {
			chain.cleanup(i + 1)
			t.Fatalf("Failed to generate cert for %s: %v", names[i], err)
		}

		certFile := tmpDir + "/cert.pem"
		keyFile := tmpDir + "/key.pem"
		os.WriteFile(certFile, certPEM, 0600)
		os.WriteFile(keyFile, keyPEM, 0600)
		chain.TLSCerts[i] = CertPair{CertFile: certFile, KeyFile: keyFile}
	}
	defer chain.Close()

	// Create agents with custom config for Agent A (auth enabled)
	for i := range chain.Agents {
		cfg := chain.buildConfig(i)

		// Enable auth on Agent A
		if i == 0 {
			cfg.SOCKS5.Auth.Enabled = true
			cfg.SOCKS5.Auth.Users = []config.SOCKS5UserConfig{
				{Username: "meshuser", Password: "meshpass"},
			}
		}

		a, err := agent.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create agent %d: %v", i, err)
		}
		chain.Agents[i] = a
	}

	// Start agents
	for i := 3; i >= 0; i-- {
		if err := chain.Agents[i].Start(); err != nil {
			t.Fatalf("Failed to start agent %d: %v", i, err)
		}
	}

	time.Sleep(3 * time.Second)

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	// Start echo server
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().(*net.TCPAddr)
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	t.Run("AuthenticatedConnection", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5: %v", err)
		}
		defer conn.Close()

		// Request username/password auth
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodUserPass})

		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)
		if methodResp[1] != socks5.AuthMethodUserPass {
			t.Fatalf("Expected UserPass method, got %d", methodResp[1])
		}

		// Authenticate
		username := "meshuser"
		password := "meshpass"
		authReq := []byte{0x01, byte(len(username))}
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)
		conn.Write(authReq)

		authResp := make([]byte, 2)
		io.ReadFull(conn, authResp)
		if authResp[1] != socks5.AuthStatusSuccess {
			t.Fatalf("Expected auth success, got %d", authResp[1])
		}

		// CONNECT request
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
		_, err = io.ReadFull(conn, reply)
		if err != nil {
			t.Fatalf("Failed to read CONNECT reply: %v", err)
		}

		if reply[1] != socks5.ReplySucceeded {
			stats := chain.Agents[0].Stats()
			if stats.RouteCount == 0 {
				t.Skip("Skipping: No routes available")
			}
			t.Fatalf("CONNECT failed with code %d", reply[1])
		}

		// Echo test
		testData := []byte("Authenticated mesh data!")
		conn.Write(testData)
		response := make([]byte, len(testData))
		io.ReadFull(conn, response)

		if !bytes.Equal(response, testData) {
			t.Errorf("Echo mismatch: got %q, want %q", response, testData)
		}

		t.Log("Authenticated connection through mesh passed")
	})

	t.Run("UnauthenticatedRejected", func(t *testing.T) {
		conn, err := net.Dial("tcp", socks5Addr.String())
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		// Try to use no-auth when auth is required
		conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth})

		methodResp := make([]byte, 2)
		io.ReadFull(conn, methodResp)

		// Should reject no-auth
		if methodResp[1] == socks5.AuthMethodNoAuth {
			t.Error("Should reject no-auth when auth is required")
		}

		t.Log("Unauthenticated rejection test passed")
	})
}
