// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/socks5"
	"golang.org/x/net/dns/dnsmessage"
)

// udpEchoServer is a UDP echo server with a receive counter so tests can
// confirm the relay actually delivered datagrams (and is not bouncing
// locally as it would if internal/agent's UDP state were package-global).
type udpEchoServer struct {
	pc       net.PacketConn
	received atomic.Int64
}

func startUDPEchoServer(t *testing.T) (*udpEchoServer, func()) {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start UDP echo server: %v", err)
	}
	s := &udpEchoServer{pc: pc}

	go func() {
		buf := make([]byte, 65535)
		for {
			n, src, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			s.received.Add(1)
			_, _ = pc.WriteTo(buf[:n], src)
		}
	}()

	return s, func() { pc.Close() }
}

func (s *udpEchoServer) Addr() *net.UDPAddr { return s.pc.LocalAddr().(*net.UDPAddr) }
func (s *udpEchoServer) Received() int64    { return s.received.Load() }

// socks5UDPAssociate opens a SOCKS5 TCP connection, performs the no-auth
// method negotiation (via the shared socks5Handshake helper), sends a
// UDP_ASSOCIATE command, parses the reply, and returns the relay UDP
// address plus the TCP control connection. The caller MUST keep the
// control connection open for the lifetime of the association: per RFC
// 1928 the relay tears down when the TCP control conn closes.
func socks5UDPAssociate(t *testing.T, socksAddr string) (*net.UDPAddr, net.Conn) {
	t.Helper()

	conn := socks5Handshake(t, socksAddr)

	// UDP_ASSOCIATE: VER + CMD + RSV + ATYP + DST.ADDR(4) + DST.PORT(2)
	// Pass 0.0.0.0:0 to let the server bind freely.
	req := []byte{
		socks5.SOCKS5Version,
		socks5.CmdUDPAssociate,
		0x00,
		socks5.AddrTypeIPv4,
		0, 0, 0, 0,
		0, 0,
	}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		t.Fatalf("Failed to write UDP_ASSOCIATE: %v", err)
	}

	// IPv4 reply: VER + REP + RSV + ATYP + BND.ADDR(4) + BND.PORT(2) = 10 bytes
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		t.Fatalf("Failed to read UDP_ASSOCIATE reply: %v", err)
	}
	if reply[1] != socks5.ReplySucceeded {
		conn.Close()
		t.Fatalf("UDP_ASSOCIATE rejected: reply code %d", reply[1])
	}
	conn.SetReadDeadline(time.Time{})

	relayIP := net.IPv4(reply[4], reply[5], reply[6], reply[7])
	relayPort := binary.BigEndian.Uint16(reply[8:10])
	return &net.UDPAddr{IP: relayIP, Port: int(relayPort)}, conn
}

// wrapSOCKS5UDPDatagram builds a SOCKS5 UDP request datagram for an IPv4
// destination, using the production socks5.BuildUDPHeader so the wire
// format stays authoritative.
func wrapSOCKS5UDPDatagram(destIP net.IP, destPort uint16, payload []byte) []byte {
	ipv4 := destIP.To4()
	if ipv4 == nil {
		panic("wrapSOCKS5UDPDatagram requires an IPv4 address")
	}
	header := socks5.BuildUDPHeader(socks5.AddrTypeIPv4, ipv4, destPort)
	out := make([]byte, len(header)+len(payload))
	copy(out, header)
	copy(out[len(header):], payload)
	return out
}

// startUDPChain spins up a 4-agent chain with UDP relay enabled on the
// exit node (D). The optional udpConfigure callback is applied AFTER the
// helper sets sane defaults, so per-test overrides win. Returns A's
// SOCKS5 address. Cleanup is registered via t.Cleanup. Skips under
// -short.
func startUDPChain(t *testing.T, udpConfigure func(*config.UDPConfig)) string {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	chain.UDPConfigure = func(c *config.UDPConfig) {
		c.Enabled = true
		c.MaxDatagramSize = 1472
		c.IdleTimeout = 5 * time.Minute
		if udpConfigure != nil {
			udpConfigure(c)
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

// udpRelayFixture bundles the four handles every single-client UDP relay
// test needs: an upstream echo server (with receive counter), the SOCKS5
// UDP relay address returned by UDP_ASSOCIATE, the TCP control conn that
// owns the association, and a client UDP socket bound to 127.0.0.1.
type udpRelayFixture struct {
	Echo     *udpEchoServer
	EchoAddr *net.UDPAddr
	Relay    *net.UDPAddr
	Ctrl     net.Conn
	Client   net.PacketConn
}

// newUDPRelayFixture spins up a 4-agent chain with UDP enabled on the
// exit, opens a SOCKS5 UDP_ASSOCIATE, and prepares an upstream echo
// server plus a client UDP socket. All cleanup is registered via
// t.Cleanup so callers do not need to defer anything.
func newUDPRelayFixture(t *testing.T, udpConfigure func(*config.UDPConfig)) *udpRelayFixture {
	t.Helper()

	echo, echoCleanup := startUDPEchoServer(t)
	t.Cleanup(echoCleanup)

	socksAddr := startUDPChain(t, udpConfigure)
	relay, ctrl := socks5UDPAssociate(t, socksAddr)
	t.Cleanup(func() { ctrl.Close() })

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client udp listen: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	return &udpRelayFixture{
		Echo:     echo,
		EchoAddr: echo.Addr(),
		Relay:    relay,
		Ctrl:     ctrl,
		Client:   client,
	}
}

// readSOCKS5UDPResponse reads one datagram from clientPC, parses the
// SOCKS5 UDP header, and returns the parsed header + payload.
func readSOCKS5UDPResponse(t *testing.T, clientPC net.PacketConn, deadline time.Duration) (*socks5.UDPHeader, []byte) {
	t.Helper()

	clientPC.SetReadDeadline(time.Now().Add(deadline))
	buf := make([]byte, 65535)
	n, _, err := clientPC.ReadFrom(buf)
	if err != nil {
		t.Fatalf("UDP read failed: %v", err)
	}
	header, payload, err := socks5.ParseUDPHeader(buf[:n])
	if err != nil {
		t.Fatalf("ParseUDPHeader: %v", err)
	}
	return header, payload
}

// TestUDPRelay_BasicAssociate verifies a single UDP datagram round-trip
// through SOCKS5 UDP_ASSOCIATE: client -> ingress relay -> mesh -> exit
// -> upstream UDP echo server -> reverse path back to client. The
// receive-counter assertion proves the relay actually delivered the
// datagram to the upstream (rather than bouncing locally, which the
// previous package-global UDP state implementation did when multiple
// agents shared a process).
//
// Covers row 37 (SOCKS5 UDP_ASSOCIATE basic).
func TestUDPRelay_BasicAssociate(t *testing.T) {
	f := newUDPRelayFixture(t, nil)

	payload := []byte("hello via udp relay")
	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), payload), f.Relay); err != nil {
		t.Fatalf("write udp datagram: %v", err)
	}

	header, got := readSOCKS5UDPResponse(t, f.Client, 5*time.Second)
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
	if !header.Address.Equal(f.EchoAddr.IP) || header.Port != uint16(f.EchoAddr.Port) {
		t.Fatalf("response source mismatch: got %s:%d want %s", header.Address, header.Port, f.EchoAddr.String())
	}
	if got := f.Echo.Received(); got == 0 {
		t.Fatalf("upstream echo server received 0 datagrams; the relay may be bouncing locally")
	}
}

// TestUDPRelay_DNSQuery verifies that DNS-shaped UDP query bytes round-trip
// cleanly through the SOCKS5 UDP relay. We send a real DNS A query packet
// to a UDP echo server (which echoes it back as the "response"), then
// re-parse the echoed bytes as DNS to confirm framing survived the
// mesh path.
//
// Covers row 38 (DNS query through UDP relay).
func TestUDPRelay_DNSQuery(t *testing.T) {
	f := newUDPRelayFixture(t, nil)

	queryMsg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 0xb33f, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name:  dnsmessage.MustNewName("udp-relay-test.example."),
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
		}},
	}
	queryBytes, err := queryMsg.Pack()
	if err != nil {
		t.Fatalf("pack dns query: %v", err)
	}

	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), queryBytes), f.Relay); err != nil {
		t.Fatalf("write dns datagram: %v", err)
	}

	_, respBytes := readSOCKS5UDPResponse(t, f.Client, 5*time.Second)

	var respMsg dnsmessage.Message
	if err := respMsg.Unpack(respBytes); err != nil {
		t.Fatalf("unpack echoed query: %v", err)
	}
	if respMsg.Header.ID != 0xb33f {
		t.Fatalf("dns id mismatch: got %#x want 0xb33f", respMsg.Header.ID)
	}
	if len(respMsg.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(respMsg.Questions))
	}
	if got := respMsg.Questions[0].Name.String(); got != "udp-relay-test.example." {
		t.Fatalf("question name mismatch: got %q", got)
	}
	if got := f.Echo.Received(); got == 0 {
		t.Fatalf("upstream echo server received 0 datagrams")
	}
}

// TestUDPRelay_ConcurrentAssociations verifies multiple SOCKS5 UDP
// associations from the same ingress can deliver datagrams in parallel,
// and that the upstream echo server actually saw N >= associations.
//
// Covers row 39 (multiple concurrent UDP associations).
func TestUDPRelay_ConcurrentAssociations(t *testing.T) {
	echoServer, echoCleanup := startUDPEchoServer(t)
	defer echoCleanup()
	echoAddr := echoServer.Addr()

	socksAddr := startUDPChain(t, nil)

	// UDP_DATAGRAM frames take the per-connection fast lane (see
	// internal/peer/connection.go::fastLaneWorker), so head-of-line
	// blocking on the sequential processor no longer caps the in-flight
	// burst. 10 parallel associations is a comfortable smoke test.
	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

			relayAddr, ctrl := socks5UDPAssociate(t, socksAddr)
			defer ctrl.Close()

			clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				errs <- fmt.Errorf("association %d: client listen: %w", i, err)
				return
			}
			defer clientPC.Close()

			payload := []byte(fmt.Sprintf("hello-from-%d", i))
			if _, err := clientPC.WriteTo(wrapSOCKS5UDPDatagram(echoAddr.IP, uint16(echoAddr.Port), payload), relayAddr); err != nil {
				errs <- fmt.Errorf("association %d: write: %w", i, err)
				return
			}

			clientPC.SetReadDeadline(time.Now().Add(5 * time.Second))
			buf := make([]byte, 65535)
			read, _, err := clientPC.ReadFrom(buf)
			if err != nil {
				errs <- fmt.Errorf("association %d: read: %w", i, err)
				return
			}
			_, body, err := socks5.ParseUDPHeader(buf[:read])
			if err != nil {
				errs <- fmt.Errorf("association %d: parse: %w", i, err)
				return
			}
			if !bytes.Equal(body, payload) {
				errs <- fmt.Errorf("association %d: payload mismatch %q vs %q", i, body, payload)
				return
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := echoServer.Received(); got < n {
		t.Errorf("upstream echo server received %d datagrams, want >= %d", got, n)
	}
}

// TestUDPRelay_FragRejection verifies that a SOCKS5 UDP datagram with a
// non-zero FRAG byte is silently dropped at the ingress relay (RFC 1928
// disallows fragmentation; this relay does not implement reassembly).
// We assert by sending a fragmented sentinel followed by a non-fragmented
// good datagram and verifying only the good payload echoes back.
//
// Covers row 43 (Frag rejection).
func TestUDPRelay_FragRejection(t *testing.T) {
	f := newUDPRelayFixture(t, nil)

	// Build a fragmented datagram (FRAG=1) by hand:
	// RSV(2)=0,0  FRAG(1)=1  ATYP(1)=IPv4  ADDR(4)  PORT(2)  PAYLOAD
	ipv4 := f.EchoAddr.IP.To4()
	frag := []byte{0, 0, 1, socks5.AddrTypeIPv4, ipv4[0], ipv4[1], ipv4[2], ipv4[3]}
	frag = binary.BigEndian.AppendUint16(frag, uint16(f.EchoAddr.Port))
	frag = append(frag, []byte("FRAGMENTED-PAYLOAD")...)
	if _, err := f.Client.WriteTo(frag, f.Relay); err != nil {
		t.Fatalf("write fragmented datagram: %v", err)
	}

	beforeReceived := f.Echo.Received()

	goodPayload := []byte("GOOD-PAYLOAD")
	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), goodPayload), f.Relay); err != nil {
		t.Fatalf("write good datagram: %v", err)
	}

	_, body := readSOCKS5UDPResponse(t, f.Client, 5*time.Second)
	if bytes.Contains(body, []byte("FRAGMENTED")) {
		t.Fatalf("fragmented datagram was relayed (got %q)", body)
	}
	if !bytes.Equal(body, goodPayload) {
		t.Fatalf("expected good payload echo, got %q", body)
	}
	// Echo server should have received exactly the GOOD payload (the fragmented
	// one was dropped at the ingress relay before it ever hit the mesh).
	if got := f.Echo.Received(); got != beforeReceived+1 {
		t.Errorf("echo server received %d datagrams since fragment send; expected exactly 1 (the good one)", got-beforeReceived)
	}
}

// TestUDPRelay_AssociationTiedToTCPClose verifies that closing the SOCKS5
// TCP control connection tears down the UDP relay socket: subsequent
// datagrams from the client to the (now-stale) relay address are not
// echoed.
//
// Covers row 44 (Association tied to TCP control conn).
func TestUDPRelay_AssociationTiedToTCPClose(t *testing.T) {
	f := newUDPRelayFixture(t, nil)

	// Sanity: the association works before we close the control conn.
	payload := []byte("before-close")
	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), payload), f.Relay); err != nil {
		t.Fatalf("write datagram: %v", err)
	}
	if _, body := readSOCKS5UDPResponse(t, f.Client, 5*time.Second); !bytes.Equal(body, payload) {
		t.Fatalf("pre-close echo mismatch: %q vs %q", body, payload)
	}
	receivedBeforeClose := f.Echo.Received()

	// Close the TCP control connection. The relay should tear down its UDP
	// listener as a result.
	f.Ctrl.Close()
	time.Sleep(200 * time.Millisecond)

	// Send a follow-up datagram to the same relay address.
	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), []byte("after-close")), f.Relay); err != nil {
		t.Logf("post-close write: %v (acceptable)", err)
	}
	f.Client.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 65535)
	if n, _, err := f.Client.ReadFrom(buf); err == nil {
		t.Fatalf("expected no response after control conn close, got %d bytes: %q", n, buf[:n])
	}
	// And the echo server must not have seen any new traffic after we closed.
	if got := f.Echo.Received(); got != receivedBeforeClose {
		t.Errorf("echo server saw additional traffic (%d > %d) after control conn close", got, receivedBeforeClose)
	}
}

// TestUDPRelay_MaxDatagramSize verifies that a datagram whose payload
// exceeds the exit's MaxDatagramSize is dropped at the exit (no response
// arrives at the client). A small follow-up datagram still works to prove
// the relay itself is healthy.
//
// Covers row 42 (Max datagram size enforcement).
func TestUDPRelay_MaxDatagramSize(t *testing.T) {
	f := newUDPRelayFixture(t, func(c *config.UDPConfig) {
		c.MaxDatagramSize = 64
	})

	// 256-byte payload exceeds the 64-byte exit limit.
	oversize := bytes.Repeat([]byte("X"), 256)
	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), oversize), f.Relay); err != nil {
		t.Fatalf("write oversize: %v", err)
	}
	f.Client.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 65535)
	if n, _, err := f.Client.ReadFrom(buf); err == nil {
		_, body, parseErr := socks5.ParseUDPHeader(buf[:n])
		if parseErr == nil && bytes.Contains(body, []byte("X")) {
			t.Fatalf("oversize datagram was relayed (got %d-byte echo)", len(body))
		}
	}
	if got := f.Echo.Received(); got != 0 {
		t.Errorf("echo server received %d oversize datagrams; expected 0", got)
	}

	// Now send a small datagram and verify the relay still works.
	small := []byte("ok")
	if _, err := f.Client.WriteTo(wrapSOCKS5UDPDatagram(f.EchoAddr.IP, uint16(f.EchoAddr.Port), small), f.Relay); err != nil {
		t.Fatalf("write small: %v", err)
	}
	if _, body := readSOCKS5UDPResponse(t, f.Client, 5*time.Second); !bytes.Equal(body, small) {
		t.Fatalf("small payload echo mismatch: %q", body)
	}
	if got := f.Echo.Received(); got != 1 {
		t.Errorf("echo server saw %d total datagrams after small send; expected 1", got)
	}
}

// TestUDPRelay_MaxAssociationsLimit verifies that the exit's
// MaxAssociations limit causes additional UDP_OPEN frames to be rejected.
//
// Covers row 41 (Max associations limit).
func TestUDPRelay_MaxAssociationsLimit(t *testing.T) {
	echoServer, echoCleanup := startUDPEchoServer(t)
	defer echoCleanup()
	echoAddr := echoServer.Addr()

	const limit = 2
	socksAddr := startUDPChain(t, func(c *config.UDPConfig) {
		c.MaxAssociations = limit
	})

	type result struct {
		idx int
		ok  bool
	}
	results := make(chan result, limit+1)
	var wg sync.WaitGroup

	for i := 0; i < limit+1; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()

			relayAddr, ctrl := socks5UDPAssociate(t, socksAddr)
			defer ctrl.Close()

			clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				results <- result{i, false}
				return
			}
			defer clientPC.Close()

			payload := []byte(fmt.Sprintf("assoc-%d", i))
			if _, err := clientPC.WriteTo(wrapSOCKS5UDPDatagram(echoAddr.IP, uint16(echoAddr.Port), payload), relayAddr); err != nil {
				results <- result{i, false}
				return
			}

			clientPC.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 65535)
			if _, _, err := clientPC.ReadFrom(buf); err != nil {
				results <- result{i, false}
				return
			}
			results <- result{i, true}
		}()
	}

	wg.Wait()
	close(results)

	successes := 0
	for r := range results {
		if r.ok {
			successes++
		}
	}
	if successes > limit {
		t.Fatalf("expected at most %d associations to succeed, got %d", limit, successes)
	}
	if successes == 0 {
		t.Fatalf("expected at least one association to succeed under limit=%d", limit)
	}
	t.Logf("%d/%d associations succeeded under limit=%d", successes, limit+1, limit)
}
