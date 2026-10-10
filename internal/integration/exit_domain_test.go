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

	"github.com/postalsys/muti-metroo/internal/socks5"
)

// startEchoServer starts a TCP echo server on 127.0.0.1 with an ephemeral
// port. Closing the listener stops the accept loop; per-connection cleanup
// happens via the goroutine's deferred Close.
func startEchoServer(t *testing.T) (*net.TCPAddr, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}

	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()

	return listener.Addr().(*net.TCPAddr), func() { listener.Close() }
}

// socks5Handshake performs the no-auth method negotiation against socksAddr
// and returns the connected net.Conn ready for a CONNECT request.
func socks5Handshake(t *testing.T, socksAddr string) net.Conn {
	t.Helper()

	conn, err := net.Dial("tcp", socksAddr)
	if err != nil {
		t.Fatalf("Failed to dial SOCKS5 %s: %v", socksAddr, err)
	}
	if _, err := conn.Write([]byte{socks5.SOCKS5Version, 1, socks5.AuthMethodNoAuth}); err != nil {
		conn.Close()
		t.Fatalf("Failed to write method negotiation: %v", err)
	}
	methodResp := make([]byte, 2)
	if _, err := io.ReadFull(conn, methodResp); err != nil {
		conn.Close()
		t.Fatalf("Failed to read method response: %v", err)
	}
	if methodResp[1] != socks5.AuthMethodNoAuth {
		conn.Close()
		t.Fatalf("Server selected method %d, want %d", methodResp[1], socks5.AuthMethodNoAuth)
	}
	return conn
}

// buildDomainConnectRequest builds a SOCKS5 CONNECT request frame for a
// domain destination using AddrTypeDomain.
func buildDomainConnectRequest(domain string, port uint16) []byte {
	if len(domain) > 255 {
		panic("domain too long for SOCKS5")
	}
	buf := &bytes.Buffer{}
	buf.WriteByte(socks5.SOCKS5Version)
	buf.WriteByte(socks5.CmdConnect)
	buf.WriteByte(0x00) // RSV
	buf.WriteByte(socks5.AddrTypeDomain)
	buf.WriteByte(byte(len(domain)))
	buf.WriteString(domain)
	_ = binary.Write(buf, binary.BigEndian, port)
	return buf.Bytes()
}

// readSocks5Reply reads the standard 10-byte IPv4-form reply that the
// Muti Metroo SOCKS5 server returns for both successful and failed CONNECTs.
// (See sendReply in internal/socks5/handler.go: replies always use the IPv4
// or IPv6 address-type, never AddrTypeDomain.)
func readSocks5Reply(conn net.Conn, deadline time.Duration) (byte, error) {
	conn.SetReadDeadline(time.Now().Add(deadline))
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return 0, err
	}
	return reply[1], nil
}

// socksConnectDomain performs a SOCKS5 no-auth handshake and issues a CONNECT
// to (domain, port) using AddrTypeDomain. On success, returns the connected
// net.Conn. On non-success reply or read error, fails the test.
func socksConnectDomain(t *testing.T, socksAddr string, domain string, port uint16) net.Conn {
	t.Helper()

	conn := socks5Handshake(t, socksAddr)
	if _, err := conn.Write(buildDomainConnectRequest(domain, port)); err != nil {
		conn.Close()
		t.Fatalf("Failed to write CONNECT for %s: %v", domain, err)
	}
	code, err := readSocks5Reply(conn, 10*time.Second)
	if err != nil {
		conn.Close()
		t.Fatalf("Failed to read CONNECT reply for %s: %v", domain, err)
	}
	if code != socks5.ReplySucceeded {
		conn.Close()
		t.Fatalf("CONNECT to %s rejected with reply code %d", domain, code)
	}
	return conn
}

// echoRoundTrip writes payload through conn and asserts the same bytes come
// back, confirming the upstream TCP session was actually established.
func echoRoundTrip(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()

	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("Echo write failed: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("Echo read failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Echo mismatch: got %q, want %q", got, payload)
	}
}

// TestExitDomainRouting_ExactMatch verifies an exact-match exit.domain_routes
// entry: a propagated domain route causes the ingress to forward the domain
// string verbatim to the exit, which then dials the resolved address.
//
// Uses a fully-qualified test domain (and the in-process DNS responder)
// because routing.ValidateDomainPattern rejects bare hostnames without a dot,
// and because the host's resolver does not know about it.
func TestExitDomainRouting_ExactMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr, echoCleanup := startEchoServer(t)
	defer echoCleanup()

	dns := newTestDNSServer(t, map[string]net.IP{
		"allowed.example": net.ParseIP("127.0.0.1"),
	})
	defer dns.Close()

	chain := NewAgentChain(t)
	chain.ExitDomainRoutes = []string{"allowed.example"}
	chain.ExitDNSServers = []string{dns.Addr()}
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}
	if !chain.WaitForDomainRoute(t, 0) {
		t.Fatal("Domain route propagation failed")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	conn := socksConnectDomain(t, socks5Addr.String(), "allowed.example", uint16(echoAddr.Port))
	defer conn.Close()
	echoRoundTrip(t, conn, []byte("hello via exact domain"))
}

// TestExitDomainRouting_Wildcard verifies that a *.base wildcard pattern in
// exit.domain_routes matches multiple distinct single-label subdomains end to
// end. Both subdomains share the same propagated domain route entry, which
// the ingress matches against the QNAME before sending STREAM_OPEN.
//
// Negative matching (different base domain, bare base) is covered by unit
// tests in internal/routing/domain_test.go and internal/exit -- there is no
// way to assert the exit-side denial through the SOCKS5 path because the
// ingress-side LookupDomain short-circuits before the request ever leaves
// the ingress agent.
func TestExitDomainRouting_Wildcard(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr, echoCleanup := startEchoServer(t)
	defer echoCleanup()

	dns := newTestDNSServer(t, map[string]net.IP{
		"foo.test.example": net.ParseIP("127.0.0.1"),
		"bar.test.example": net.ParseIP("127.0.0.1"),
	})
	defer dns.Close()

	chain := NewAgentChain(t)
	chain.ExitDomainRoutes = []string{"*.test.example"}
	chain.ExitDNSServers = []string{dns.Addr()}
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}
	if !chain.WaitForDomainRoute(t, 0) {
		t.Fatal("Domain route propagation failed")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	for _, name := range []string{"foo.test.example", "bar.test.example"} {
		t.Run("Allow_"+name, func(t *testing.T) {
			conn := socksConnectDomain(t, socks5Addr.String(), name, uint16(echoAddr.Port))
			defer conn.Close()
			echoRoundTrip(t, conn, []byte(fmt.Sprintf("hello %s", name)))
		})
	}
}

// TestExitDomainRouting_DNSResolvedAtExit proves two related claims:
//  1. DNS resolution happens at the exit, not the ingress (the ingress passes
//     the domain string verbatim across the wire).
//  2. exit.dns.servers is honored: the configured resolver receives the query.
//
// The in-process DNS server is the only place that knows how to resolve
// "only-at-exit.test", so a successful round-trip can only happen if the exit
// queried it.
func TestExitDomainRouting_DNSResolvedAtExit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	echoAddr, echoCleanup := startEchoServer(t)
	defer echoCleanup()

	const targetDomain = "only-at-exit.test"
	dns := newTestDNSServer(t, map[string]net.IP{
		targetDomain: net.ParseIP("127.0.0.1"),
	})
	defer dns.Close()

	chain := NewAgentChain(t)
	chain.ExitDomainRoutes = []string{targetDomain}
	chain.ExitDNSServers = []string{dns.Addr()}
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	if !chain.WaitForRoutes(t) {
		t.Fatal("Route propagation failed")
	}
	if !chain.WaitForDomainRoute(t, 0) {
		t.Fatal("Domain route propagation failed")
	}

	socks5Addr := chain.Agents[0].SOCKS5Address()
	if socks5Addr == nil {
		t.Fatal("SOCKS5 address is nil")
	}

	beforeQueries := dns.QueryCount()
	conn := socksConnectDomain(t, socks5Addr.String(), targetDomain, uint16(echoAddr.Port))
	defer conn.Close()

	echoRoundTrip(t, conn, []byte("payload via exit-side dns"))

	if got := dns.QueryCount(); got <= beforeQueries {
		t.Fatalf("expected exit to query the configured DNS server (count=%d, before=%d)", got, beforeQueries)
	}
	if got := dns.LastName(); got != targetDomain {
		t.Fatalf("DNS server saw qname %q, want %q", got, targetDomain)
	}
}
