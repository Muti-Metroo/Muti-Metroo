package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"net"
	"testing"
	"time"
)

// Per-stream flow control: a slow reader or a slow destination must slow the
// sender down, never make an endpoint drop frames and reset the stream.
//
// flowPayloadSize is well over one flow-control window (128 frames of 16 KB,
// 2 MB) plus the socket buffers on both ends, so a sender that ignored the
// window would overflow a stream buffer while the far side stalls.
const flowPayloadSize = 24 << 20

// flowStall is how long the slow side waits before it starts reading.
const flowStall = 2 * time.Second

// startFlowChain starts the A-B-C-D chain with legacy agents (no flow
// control) at the given indexes and waits for routes.
func startFlowChain(t *testing.T, legacy ...int) *AgentChain {
	t.Helper()
	chain := NewAgentChain(t)
	t.Cleanup(chain.Close)
	if len(legacy) > 0 {
		chain.LegacyAgents = map[int]bool{}
		for _, i := range legacy {
			chain.LegacyAgents[i] = true
		}
	}
	chain.CreateAgents(t)
	chain.StartAgents(t)
	if !chain.WaitForRoutes(t) {
		t.Fatal("routes did not propagate")
	}
	return chain
}

// socksConnectTCP opens a SOCKS5 CONNECT to addr through the agent at
// socksAddr.
func socksConnectTCP(t *testing.T, socksAddr string, addr *net.TCPAddr) net.Conn {
	t.Helper()
	return socksConnect(t, socksAddr, buildIPv4ConnectRequest(addr), addr.String())
}

func randomPayload(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

// serveOnce accepts one connection on a fresh listener and runs handle on it.
func serveOnce(t *testing.T, handle func(net.Conn)) *net.TCPAddr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		handle(conn)
	}()
	return ln.Addr().(*net.TCPAddr)
}

// downloadStalled has a server send payload as fast as it can while the
// SOCKS client waits flowStall before reading, and returns what the client
// read and the error that ended the read.
func downloadStalled(t *testing.T, chain *AgentChain, payload []byte) ([]byte, error) {
	t.Helper()
	server := serveOnce(t, func(conn net.Conn) {
		conn.Write(payload)
	})
	conn := socksConnectTCP(t, chain.Agents[0].SOCKS5Address().String(), server)
	defer conn.Close()

	time.Sleep(flowStall)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return io.ReadAll(conn)
}

// A client that stops reading must get every byte once it resumes.
func TestFlowControl_SlowReader(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	chain := startFlowChain(t)
	payload := randomPayload(t, flowPayloadSize)

	got, err := downloadStalled(t, chain, payload)
	if err != nil {
		t.Fatalf("read after %d of %d bytes: %v", len(got), len(payload), err)
	}
	if sha256.Sum256(got) != sha256.Sum256(payload) {
		t.Fatalf("received %d bytes, want %d identical bytes", len(got), len(payload))
	}
}

// A destination that stops reading must get every byte once it resumes.
func TestFlowControl_SlowDestination(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	chain := startFlowChain(t)
	payload := randomPayload(t, flowPayloadSize)

	type result struct {
		data []byte
		err  error
	}
	received := make(chan result, 1)
	server := serveOnce(t, func(conn net.Conn) {
		time.Sleep(flowStall)
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		data, err := io.ReadAll(conn)
		received <- result{data, err}
	})

	conn := socksConnectTCP(t, chain.Agents[0].SOCKS5Address().String(), server)
	defer conn.Close()
	conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.(*net.TCPConn).CloseWrite()

	select {
	case r := <-received:
		if r.err != nil {
			t.Fatalf("destination read after %d of %d bytes: %v", len(r.data), len(payload), r.err)
		}
		if sha256.Sum256(r.data) != sha256.Sum256(payload) {
			t.Fatalf("destination got %d bytes, want %d identical bytes", len(r.data), len(payload))
		}
	case <-time.After(90 * time.Second):
		t.Fatal("destination never finished reading")
	}
}

// A mesh with an agent that predates flow control falls back to running
// without it, and streams through it still carry data intact.
func TestFlowControl_LegacyAgentFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	for _, tc := range []struct {
		name   string
		legacy int
	}{
		{"legacy transit", 1},
		{"legacy exit", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := startFlowChain(t, tc.legacy)
			payload := randomPayload(t, 1<<20)

			server, stop := startEchoServer(t)
			defer stop()
			conn := socksConnectTCP(t, chain.Agents[0].SOCKS5Address().String(), server)
			defer conn.Close()

			go func() {
				conn.Write(payload)
				conn.(*net.TCPConn).CloseWrite()
			}()
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			got, err := io.ReadAll(conn)
			if err != nil {
				t.Fatalf("read after %d bytes: %v", len(got), err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("echoed %d bytes, want %d identical bytes", len(got), len(payload))
			}
		})
	}
}

// Without flow control a stalled reader can still overflow the ingress
// stream buffer. The stream is then reset, and the client must see an error,
// never a clean end of a truncated stream.
func TestFlowControl_LegacyOverflowIsAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	chain := startFlowChain(t, 3)
	payload := randomPayload(t, flowPayloadSize)

	got, err := downloadStalled(t, chain, payload)
	if err == nil && len(got) != len(payload) {
		t.Fatalf("stream ended cleanly after %d of %d bytes: truncation must be an error", len(got), len(payload))
	}
	if err == nil && sha256.Sum256(got) != sha256.Sum256(payload) {
		t.Fatal("stream ended cleanly with corrupted data")
	}
	t.Logf("legacy path: read %d of %d bytes, err=%v", len(got), len(payload), err)
}
