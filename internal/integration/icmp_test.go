// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/socks5"
)

// tryICMPSession opens a SOCKS5 TCP connection, performs the no-auth method
// negotiation, sends a CONNECT-like request with the custom CmdICMPEcho
// command targeting destIP, and returns the connected net.Conn ready for
// reading and writing echo frames in the `[id(2)][seq(2)][len(2)][payload]`
// format. On rejection it returns (nil, error) instead of failing the test,
// so tests that intentionally probe failure paths can inspect the rejection.
// Note: handshake-level failures (TCP dial, method negotiation) still call
// t.Fatalf via socks5Handshake -- those are real bugs, not the failures
// this helper is designed to test.
func tryICMPSession(t *testing.T, socksAddr string, destIP net.IP) (net.Conn, error) {
	t.Helper()

	ipv4 := destIP.To4()
	if ipv4 == nil {
		return nil, fmt.Errorf("tryICMPSession requires an IPv4 destination, got %s", destIP)
	}

	conn := socks5Handshake(t, socksAddr)

	req := []byte{
		socks5.SOCKS5Version,
		socks5.CmdICMPEcho,
		0x00,
		socks5.AddrTypeIPv4,
		ipv4[0], ipv4[1], ipv4[2], ipv4[3],
		0, 0, // port (unused for ICMP)
	}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write ICMP_ECHO request: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read ICMP_ECHO reply: %w", err)
	}
	if reply[1] != socks5.ReplySucceeded {
		conn.Close()
		return nil, fmt.Errorf("ICMP_ECHO rejected with code %d", reply[1])
	}
	conn.SetReadDeadline(time.Time{})
	return conn, nil
}

// socks5ICMPSession is the fatal-on-error wrapper around tryICMPSession for
// happy-path tests that don't want to handle rejection inline.
func socks5ICMPSession(t *testing.T, socksAddr string, destIP net.IP) net.Conn {
	t.Helper()
	conn, err := tryICMPSession(t, socksAddr, destIP)
	if err != nil {
		t.Fatalf("socks5ICMPSession: %v", err)
	}
	return conn
}

// writeICMPEcho sends one echo request frame on an established ICMP session.
func writeICMPEcho(t *testing.T, conn net.Conn, identifier, sequence uint16, payload []byte) {
	t.Helper()

	header := make([]byte, 6)
	binary.BigEndian.PutUint16(header[0:2], identifier)
	binary.BigEndian.PutUint16(header[2:4], sequence)
	binary.BigEndian.PutUint16(header[4:6], uint16(len(payload)))
	if _, err := conn.Write(header); err != nil {
		t.Fatalf("write echo header: %v", err)
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			t.Fatalf("write echo payload: %v", err)
		}
	}
}

// readICMPEcho reads one echo reply frame from an established ICMP session
// with the given deadline. Returns the parsed identifier, sequence, payload.
func readICMPEcho(t *testing.T, conn net.Conn, deadline time.Duration) (uint16, uint16, []byte) {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(deadline))
	header := make([]byte, 6)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatalf("read echo header: %v", err)
	}
	identifier := binary.BigEndian.Uint16(header[0:2])
	sequence := binary.BigEndian.Uint16(header[2:4])
	payloadLen := binary.BigEndian.Uint16(header[4:6])
	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			t.Fatalf("read echo payload: %v", err)
		}
	}
	return identifier, sequence, payload
}

// startICMPChain spins up a 4-agent chain with ICMP enabled on both agent A
// (so the ingress SOCKS5 server has an icmpHandler) and agent D (so the
// exit can actually send ICMP packets). The optional configure callback
// is applied AFTER the helper sets sane defaults; per-test overrides win.
// Returns A's SOCKS5 address. Cleanup is registered via t.Cleanup. Skips
// under -short.
func startICMPChain(t *testing.T, configure func(*config.ICMPConfig)) string {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	chain.ICMPConfigure = func(c *config.ICMPConfig) {
		c.Enabled = true
		c.MaxSessions = 100
		c.IdleTimeout = 60 * time.Second
		c.EchoTimeout = 5 * time.Second
		if configure != nil {
			configure(c)
		}
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	t.Cleanup(chain.Close)

	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}
	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}
	return socks5Addr.String()
}

// TestICMPEcho_Basic verifies a single ICMP echo round-trip through the
// mesh: client -> SOCKS5 ingress -> 4-agent chain -> exit -> upstream
// 127.0.0.1 (loopback ICMP) -> reverse path back to client. The reply's
// sequence and payload must match the request.
//
// Note: the identifier field is NOT round-trip preserved. The exit uses
// unprivileged ICMP sockets (icmp.ListenPacket "udp4"), and the kernel
// overwrites the identifier with the local source port. The production
// CLI ping has the same property and matches replies by sequence + payload.
//
// Covers row 128 (ICMP echo basic).
func TestICMPEcho_Basic(t *testing.T) {
	socksAddr := startICMPChain(t, nil)

	conn := socks5ICMPSession(t, socksAddr, net.IPv4(127, 0, 0, 1))
	defer conn.Close()

	payload := []byte("hello-icmp")
	writeICMPEcho(t, conn, 0x1234, 1, payload)

	_, seq, body := readICMPEcho(t, conn, 5*time.Second)
	if seq != 1 {
		t.Errorf("sequence mismatch: got %d, want 1", seq)
	}
	if !bytes.Equal(body, payload) {
		t.Errorf("payload mismatch: got %q, want %q", body, payload)
	}
}

// TestICMPEcho_CountAndInterval verifies that 4 echo requests sent with a
// 50ms interval all produce matching replies.
//
// Covers row 129 (Echo with count + interval).
func TestICMPEcho_CountAndInterval(t *testing.T) {
	socksAddr := startICMPChain(t, nil)

	conn := socks5ICMPSession(t, socksAddr, net.IPv4(127, 0, 0, 1))
	defer conn.Close()

	const n = 4
	for seq := uint16(1); seq <= n; seq++ {
		payload := []byte(fmt.Sprintf("ping-%d", seq))
		writeICMPEcho(t, conn, 0xabcd, seq, payload)
		time.Sleep(50 * time.Millisecond)
	}

	for i := uint16(1); i <= n; i++ {
		_, seq, body := readICMPEcho(t, conn, 5*time.Second)
		if seq != i {
			t.Errorf("echo %d: sequence mismatch: got %d, want %d", i, seq, i)
		}
		want := []byte(fmt.Sprintf("ping-%d", i))
		if !bytes.Equal(body, want) {
			t.Errorf("echo %d: payload mismatch: got %q, want %q", i, body, want)
		}
	}
}

// TestICMPEcho_E2EEncrypted verifies that the round-trip works when E2E
// encryption is in effect (which is the default for SOCKS5 ICMP echo
// sessions). The fact that the payload comes back unchanged confirms that
// the encrypt-on-send / decrypt-on-receive path is functional end to end.
// Cryptographic correctness is unit-tested in internal/crypto.
//
// Covers row 132 (E2E encrypted echoes).
func TestICMPEcho_E2EEncrypted(t *testing.T) {
	socksAddr := startICMPChain(t, nil)

	conn := socks5ICMPSession(t, socksAddr, net.IPv4(127, 0, 0, 1))
	defer conn.Close()

	// 256-byte payload exercises full block boundaries of ChaCha20-Poly1305.
	payload := bytes.Repeat([]byte{0xab, 0xcd, 0xef, 0x01}, 64)
	writeICMPEcho(t, conn, 0xfeed, 7, payload)

	_, seq, body := readICMPEcho(t, conn, 5*time.Second)
	if seq != 7 {
		t.Errorf("sequence mismatch: got %d, want 7", seq)
	}
	if !bytes.Equal(body, payload) {
		t.Errorf("payload mismatch: %d bytes vs %d bytes", len(body), len(payload))
	}
}

// TestICMPEcho_MaxSessionsLimit verifies that the exit's MaxSessions limit
// causes additional ICMP_OPEN requests beyond the cap to be rejected.
// Sessions are opened sequentially and held open simultaneously (no
// goroutines, no closes) so the exit's session count actually reaches
// the cap before the (limit+1)-th request arrives.
//
// Covers row 133 (Max sessions limit).
func TestICMPEcho_MaxSessionsLimit(t *testing.T) {
	const limit = 2
	socksAddr := startICMPChain(t, func(c *config.ICMPConfig) {
		c.MaxSessions = limit
	})

	// Open limit+1 sessions sequentially. The first `limit` should succeed;
	// the over-cap one should be rejected by the exit handler with
	// ErrICMPSessionLimit, which surfaces as a non-success SOCKS5 reply.
	var openConns []net.Conn
	defer func() {
		for _, c := range openConns {
			c.Close()
		}
	}()

	successes := 0
	for i := 0; i < limit+1; i++ {
		conn, err := tryICMPSession(t, socksAddr, net.IPv4(127, 0, 0, 1))
		if err != nil {
			t.Logf("session %d rejected (as expected for over-cap): %v", i, err)
			continue
		}
		openConns = append(openConns, conn)
		successes++
	}

	if successes != limit {
		t.Fatalf("expected exactly %d sessions to succeed, got %d", limit, successes)
	}
	t.Logf("%d/%d sessions succeeded under limit=%d", successes, limit+1, limit)
}

// TestICMPEcho_DisabledRejection verifies that when ICMP is disabled at
// the ingress, the SOCKS5 server rejects the ICMP_ECHO command with a
// non-success reply (typically ReplyCmdNotSupported = 0x07).
//
// Note: config.Default() sets ICMP.Enabled = true, so the test must
// explicitly disable it via ICMPConfigure.
//
// Covers row 135 (Disabled state rejection).
func TestICMPEcho_DisabledRejection(t *testing.T) {
	socksAddr := startICMPChain(t, func(c *config.ICMPConfig) {
		c.Enabled = false
	})

	conn, err := tryICMPSession(t, socksAddr, net.IPv4(127, 0, 0, 1))
	if err == nil {
		conn.Close()
		t.Fatalf("expected ICMP_ECHO to be rejected when ICMP disabled")
	}
	t.Logf("ICMP disabled correctly rejected: %v", err)
}
