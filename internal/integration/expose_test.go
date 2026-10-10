package integration

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/agent"
	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/expose"
	"github.com/postalsys/muti-metroo/internal/socks5"
)

// TestExpose_EndToEnd verifies the full ephemeral forward endpoint path over
// SOCKS5 CmdExpose on a 4-hop linear chain. The exit agent (D) accepts the
// CmdExpose stream; a fake "Mutiauk" client REGISTERs key "expose-echo"
// and echoes bytes. Agent A hosts a forward listener for "expose-echo". A
// client dials the listener; bytes round-trip via the mesh and the expose
// stream back to the in-test echo loop.
func TestExpose_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	chain := NewAgentChain(t)
	defer chain.Close()

	chain.SOCKS5AllAgents = true
	chain.ForwardListeners = map[int][]config.ForwardListener{
		0: {{Key: "expose-echo", Address: "127.0.0.1:0"}},
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.VerifyConnectivity(t)

	exitSOCKS := chain.Agents[3].SOCKS5Address()
	if exitSOCKS == nil {
		t.Fatal("exit agent (D) SOCKS5 address is nil")
	}

	// Connect a fake Mutiauk expose client to the exit agent over SOCKS5.
	conn := dialExpose(t, exitSOCKS.String())
	defer conn.Close()

	// REGISTER expose-echo.
	if err := writeExposeFrame(t, conn, expose.FrameRegister, 0, mustJSON(expose.RegisterPayload{
		Forwards: []expose.RegisterEntry{{Key: "expose-echo"}},
	})); err != nil {
		t.Fatalf("REGISTER write: %v", err)
	}
	ft, _, body, err := readExposeFrame(conn)
	if err != nil {
		t.Fatalf("REGISTER ack read: %v", err)
	}
	if ft != expose.FrameRegisterAck {
		t.Fatalf("expected REGISTER_ACK, got 0x%02x", ft)
	}
	var ack expose.RegisterAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if len(ack.Results) != 1 || !ack.Results[0].OK {
		t.Fatalf("REGISTER ack not ok: %+v", ack)
	}

	// Wait for the route to flood back to ingress A.
	if !chain.WaitForForwardRoute(t, "expose-echo", 0) {
		t.Fatal("forward route 'expose-echo' did not propagate to ingress")
	}

	// Run the operator-side echo loop until the stream closes.
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		runExposeEchoLoop(t, conn)
	}()

	// Drive client traffic through the mesh.
	listenerAddr := chain.Agents[0].ForwardListenerAddress("expose-echo")
	if listenerAddr == nil {
		t.Fatal("ingress listener bound address is nil")
	}

	payload := bytes.Repeat([]byte("expose-roundtrip "), 64) // ~1 KiB
	for i := 0; i < 5; i++ {
		c, err := net.Dial("tcp", listenerAddr.String())
		if err != nil {
			t.Fatalf("iter %d dial: %v", i, err)
		}
		c.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := c.Write(payload); err != nil {
			c.Close()
			t.Fatalf("iter %d write: %v", i, err)
		}
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(c, got); err != nil {
			c.Close()
			t.Fatalf("iter %d read: %v", i, err)
		}
		c.Close()
		if !bytes.Equal(got, payload) {
			t.Fatalf("iter %d payload mismatch", i)
		}
	}

	// Disconnecting the expose stream wipes the local route on the exit agent
	// and floods a withdrawal so peer route tables drop the row well before
	// the 5m TTL.
	conn.Close()
	<-loopDone

	exitDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(exitDeadline) {
		if chain.Agents[3].LookupForwardRoute("expose-echo") == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if route := chain.Agents[3].LookupForwardRoute("expose-echo"); route != nil {
		t.Errorf("expected exit-side expose-echo route to be removed after disconnect; got %+v", route)
	}

	// Ingress (agent A) should also drop the route via the flooded withdrawal.
	ingressDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(ingressDeadline) {
		if chain.Agents[0].LookupForwardRoute("expose-echo") == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if route := chain.Agents[0].LookupForwardRoute("expose-echo"); route != nil {
		t.Errorf("ingress should drop expose-echo via withdrawal; got %+v", route)
	}

	// New traffic at the ingress should fail: the listener accepts the TCP
	// connection but the upstream stream open errors out (forward not found).
	c, err := net.Dial("tcp", listenerAddr.String())
	if err != nil {
		t.Fatalf("post-disconnect dial: %v", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, werr := c.Write([]byte("after-disconnect")); werr == nil {
		buf := make([]byte, 4)
		_, rerr := c.Read(buf)
		if rerr == nil {
			t.Errorf("expected listener to close stream after exit-side unregister; got data")
		}
	}
}

// dialExpose performs a no-auth SOCKS5 handshake against socksAddr, issues the
// CmdExpose command, and returns the connection positioned just after the
// SOCKS5 reply, ready to exchange length-prefixed expose frames.
func dialExpose(t *testing.T, socksAddr string) net.Conn {
	t.Helper()
	conn := socks5Handshake(t, socksAddr)

	req := []byte{
		socks5.SOCKS5Version, socks5.CmdExpose, 0x00, socks5.AddrTypeIPv4,
		0, 0, 0, 0, // 0.0.0.0
		0, 0, // port 0
	}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		t.Fatalf("CmdExpose write: %v", err)
	}
	code, err := readSocks5Reply(conn, 10*time.Second)
	if err != nil {
		conn.Close()
		t.Fatalf("CmdExpose reply: %v", err)
	}
	if code != socks5.ReplySucceeded {
		conn.Close()
		t.Fatalf("CmdExpose reply = %d, want ReplySucceeded %d", code, socks5.ReplySucceeded)
	}
	return conn
}

// runExposeEchoLoop services one expose stream: ack OPEN frames and echo any
// DATA frames straight back. Returns when the stream closes.
func runExposeEchoLoop(t *testing.T, conn net.Conn) {
	t.Helper()
	for {
		ft, sid, body, err := readExposeFrame(conn)
		if err != nil {
			return
		}
		switch ft {
		case expose.FrameOpen:
			_ = writeExposeFrame(t, conn, expose.FrameOpenAck, sid, mustJSON(expose.OpenAckPayload{OK: true}))
		case expose.FrameData:
			_ = writeExposeFrame(t, conn, expose.FrameData, sid, body)
		case expose.FrameClose, expose.FrameCloseWrite:
			// Nothing to do; the upstream side has signalled it is done.
		case expose.FramePing:
			nonce, _ := expose.DecodeNonce(body)
			_ = writeExposeFrame(t, conn, expose.FramePong, 0, encodeNonce(nonce))
		}
	}
}

// encodeNonce returns the 8-byte payload of a PING/PONG frame.
func encodeNonce(nonce uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, nonce)
	return b
}

// writeExposeFrame writes a single u32-length-prefixed expose frame, matching
// the SOCKS5 CmdExpose transport framing.
func writeExposeFrame(t *testing.T, conn net.Conn, frameType uint8, streamID uint64, payload []byte) error {
	t.Helper()
	frame := expose.EncodeFrame(frameType, streamID, payload)
	buf := make([]byte, 4+len(frame))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(frame)))
	copy(buf[4:], frame)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write(buf)
	return err
}

func readExposeFrame(conn net.Conn) (uint8, uint64, []byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return 0, 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	data := make([]byte, n)
	if _, err := io.ReadFull(conn, data); err != nil {
		return 0, 0, nil, err
	}
	return expose.DecodeFrame(data)
}

func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return out
}

// TestExpose_SingleAgentLoopback verifies the self-loop case: one agent both
// accepts the CmdExpose stream (operator side) AND runs the forward listener
// for the claimed key. The fix in Agent.DialForward short-circuits to the
// local backend when the resolved next hop is the agent itself; this test
// guards against regressing that path.
func TestExpose_SingleAgentLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := config.Default()
	cfg.Agent.DataDir = t.TempDir()
	cfg.Agent.LogLevel = "warn"
	cfg.SOCKS5.Enabled = true
	cfg.SOCKS5.Address = "127.0.0.1:0"
	cfg.Forward.Listeners = []config.ForwardListener{
		{Key: "loop", Address: "127.0.0.1:0"},
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer a.Stop()

	socksAddr := a.SOCKS5Address()
	if socksAddr == nil {
		t.Fatal("agent SOCKS5 address is nil")
	}

	conn := dialExpose(t, socksAddr.String())
	defer conn.Close()

	if err := writeExposeFrame(t, conn, expose.FrameRegister, 0, mustJSON(expose.RegisterPayload{
		Forwards: []expose.RegisterEntry{{Key: "loop"}},
	})); err != nil {
		t.Fatalf("REGISTER: %v", err)
	}
	ft, _, body, err := readExposeFrame(conn)
	if err != nil {
		t.Fatalf("REGISTER ack: %v", err)
	}
	if ft != expose.FrameRegisterAck {
		t.Fatalf("expected REGISTER_ACK, got 0x%02x", ft)
	}
	var ack expose.RegisterAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if len(ack.Results) != 1 || !ack.Results[0].OK {
		t.Fatalf("REGISTER ack not ok: %+v", ack)
	}

	// Wait for the local forward route to land in the routing table.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.LookupForwardRoute("loop") != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a.LookupForwardRoute("loop") == nil {
		t.Fatal("local forward route 'loop' was not added")
	}

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		runExposeEchoLoop(t, conn)
	}()

	listenerAddr := a.ForwardListenerAddress("loop")
	if listenerAddr == nil {
		t.Fatal("forward listener address is nil")
	}

	payload := bytes.Repeat([]byte("loopback "), 128) // ~1 KiB
	for i := 0; i < 3; i++ {
		c, err := net.Dial("tcp", listenerAddr.String())
		if err != nil {
			t.Fatalf("iter %d dial: %v", i, err)
		}
		c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Write(payload); err != nil {
			c.Close()
			t.Fatalf("iter %d write: %v", i, err)
		}
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(c, got); err != nil {
			c.Close()
			t.Fatalf("iter %d read: %v", i, err)
		}
		c.Close()
		if !bytes.Equal(got, payload) {
			t.Fatalf("iter %d payload mismatch", i)
		}
	}

	// Disconnecting the expose stream must drop the local backend; subsequent
	// dials surface an error to the listener.
	conn.Close()
	<-loopDone

	gone := time.Now().Add(2 * time.Second)
	for time.Now().Before(gone) {
		if a.LookupForwardRoute("loop") == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a.LookupForwardRoute("loop") != nil {
		t.Errorf("local forward route 'loop' should be removed after disconnect")
	}
}
