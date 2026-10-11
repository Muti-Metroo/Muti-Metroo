package expose

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/forward"
)

// mockAgent records register/unregister calls and stores ephemeral keys.
// Optionally returns a configurable error from Register.
type mockAgent struct {
	mu          sync.Mutex
	registered  map[string]string // key -> owner
	backends    map[string]forward.Backend
	advertises  int
	rejectKey   string
	rejectError error
}

func newMockAgent() *mockAgent {
	return &mockAgent{
		registered: make(map[string]string),
		backends:   make(map[string]forward.Backend),
	}
}

func (m *mockAgent) RegisterEphemeralForwardKey(key, owner string, b forward.Backend) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key == m.rejectKey && m.rejectError != nil {
		return m.rejectError
	}
	if existing, ok := m.registered[key]; ok {
		return fmt.Errorf("key %q already held by %s", key, existing)
	}
	m.registered[key] = owner
	m.backends[key] = b
	return nil
}

func (m *mockAgent) UnregisterEphemeralForwardKey(key, owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if got, ok := m.registered[key]; ok && got == owner {
		delete(m.registered, key)
		delete(m.backends, key)
	}
}

func (m *mockAgent) TriggerRouteAdvertise() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.advertises++
}

func (m *mockAgent) snapshot() (map[string]string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.registered))
	for k, v := range m.registered {
		out[k] = v
	}
	return out, m.advertises
}

func (m *mockAgent) backendFor(key string) forward.Backend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backends[key]
}

// testClient is the operator end of an expose session running over a
// net.Pipe. It speaks the same u32-length-prefixed framing as the SOCKS5
// CmdExpose transport (see rawConn).
type testClient struct {
	t    *testing.T
	conn net.Conn
}

func (c *testClient) writeMsg(b []byte) error {
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(b)))
	copy(buf[4:], b)
	_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := c.conn.Write(buf)
	return err
}

func (c *testClient) readMsg() ([]byte, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hdr [4]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (c *testClient) close() { _ = c.conn.Close() }

// startManager returns a Manager backed by a fresh mock agent. Sessions are
// created per-client by dialClient.
func startManager(t *testing.T, cfg ManagerConfig) (*Manager, *mockAgent) {
	t.Helper()
	agent := newMockAgent()
	mgr := NewManager(agent, cfg)
	return mgr, agent
}

// dialClient wires a net.Pipe between an operator-side testClient and a
// Manager session running HandleRawConn on the server end.
func dialClient(t *testing.T, mgr *Manager) *testClient {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go mgr.HandleRawConn(serverConn, "pipe")
	tc := &testClient{t: t, conn: clientConn}
	t.Cleanup(func() { _ = clientConn.Close() })
	return tc
}

func sendFrame(t *testing.T, conn *testClient, frameType uint8, streamID uint64, payload []byte) {
	t.Helper()
	if err := conn.writeMsg(EncodeFrame(frameType, streamID, payload)); err != nil {
		t.Fatalf("Write frame: %v", err)
	}
}

func sendJSONFrame(t *testing.T, conn *testClient, frameType uint8, streamID uint64, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sendFrame(t, conn, frameType, streamID, body)
}

func readFrame(t *testing.T, conn *testClient) (uint8, uint64, []byte) {
	t.Helper()
	data, err := conn.readMsg()
	if err != nil {
		t.Fatalf("readMsg: %v", err)
	}
	ft, sid, body, err := DecodeFrame(data)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	return ft, sid, body
}

func readNextFrameNoFatal(conn *testClient) (uint8, uint64, []byte, error) {
	data, err := conn.readMsg()
	if err != nil {
		return 0, 0, nil, err
	}
	return DecodeFrame(data)
}

func TestManager_RegisterSuccess(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{
		Forwards: []RegisterEntry{{Key: "k1"}, {Key: "k2"}},
	})

	ft, _, body := readFrame(t, conn)
	if ft != FrameRegisterAck {
		t.Fatalf("expected REGISTER_ACK, got 0x%02x", ft)
	}

	var ack RegisterAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("Unmarshal ack: %v", err)
	}
	if len(ack.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(ack.Results))
	}
	for _, r := range ack.Results {
		if !r.OK {
			t.Errorf("expected ok=true for %s, got error %q", r.Key, r.Error)
		}
	}

	// Allow background goroutines to finish processing.
	time.Sleep(50 * time.Millisecond)
	got, advertises := agent.snapshot()
	if len(got) != 2 {
		t.Errorf("expected 2 registered keys, got %d", len(got))
	}
	if advertises < 1 {
		t.Errorf("expected >= 1 advertise call, got %d", advertises)
	}
}

func TestManager_RegisterPartialCollision(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	agent.rejectKey = "taken"
	agent.rejectError = errors.New("collision")

	conn := dialClient(t, mgr)
	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{
		Forwards: []RegisterEntry{{Key: "ok1"}, {Key: "taken"}, {Key: "ok2"}},
	})

	_, _, body := readFrame(t, conn)
	var ack RegisterAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(ack.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(ack.Results))
	}
	results := map[string]KeyResult{}
	for _, r := range ack.Results {
		results[r.Key] = r
	}
	if !results["ok1"].OK || !results["ok2"].OK {
		t.Errorf("expected ok1 and ok2 to succeed: %+v", results)
	}
	if results["taken"].OK {
		t.Errorf("expected taken to fail")
	}
	if !strings.Contains(results["taken"].Error, "collision") {
		t.Errorf("expected collision error, got %q", results["taken"].Error)
	}
}

func TestManager_Unregister(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{
		Forwards: []RegisterEntry{{Key: "k1"}, {Key: "k2"}},
	})
	readFrame(t, conn) // register ack

	sendJSONFrame(t, conn, FrameUnregister, 0, UnregisterPayload{Keys: []string{"k1", "missing"}})
	ft, _, body := readFrame(t, conn)
	if ft != FrameUnregisterAck {
		t.Fatalf("expected UNREGISTER_ACK, got 0x%02x", ft)
	}
	var ack UnregisterAckPayload
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(ack.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(ack.Results))
	}
	results := map[string]KeyResult{}
	for _, r := range ack.Results {
		results[r.Key] = r
	}
	if !results["k1"].OK {
		t.Errorf("expected k1 unregister ok")
	}
	if results["missing"].OK {
		t.Errorf("expected missing unregister to fail")
	}

	time.Sleep(50 * time.Millisecond)
	got, _ := agent.snapshot()
	if _, ok := got["k1"]; ok {
		t.Errorf("k1 should be unregistered")
	}
	if _, ok := got["k2"]; !ok {
		t.Errorf("k2 should still be registered")
	}
}

func TestManager_DisconnectCleansUpKeys(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{
		Forwards: []RegisterEntry{{Key: "k1"}, {Key: "k2"}},
	})
	readFrame(t, conn) // ack

	conn.close()

	// Wait for the server side to notice and clean up.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := agent.snapshot()
		if len(got) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := agent.snapshot()
	if len(got) != 0 {
		t.Errorf("expected keys to be cleaned up on disconnect, got %v", got)
	}
}

func TestManager_OpenStreamRoundtrip(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{
		Forwards: []RegisterEntry{{Key: "echo"}},
	})
	readFrame(t, conn) // register ack

	// The server's wsBackend is now installed in the mock agent. Drive an
	// Open() call against it and run a small client-side echo loop in
	// parallel.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Wait for OPEN, send OPEN_ACK ok, echo any DATA back, expect CLOSE
		// after we send CLOSE_WRITE+CLOSE.
		ft, sid, body := readFrame(t, conn)
		if ft != FrameOpen {
			t.Errorf("expected OPEN, got 0x%02x", ft)
			return
		}
		var op OpenPayload
		if err := json.Unmarshal(body, &op); err != nil {
			t.Errorf("Unmarshal OPEN: %v", err)
			return
		}
		if op.Key != "echo" {
			t.Errorf("OPEN key = %q; want echo", op.Key)
			return
		}
		sendJSONFrame(t, conn, FrameOpenAck, sid, OpenAckPayload{OK: true})

		// Read a DATA frame the handler is supposed to send, echo it back.
		ft, _, body = readFrame(t, conn)
		if ft != FrameData {
			t.Errorf("expected DATA, got 0x%02x", ft)
			return
		}
		sendFrame(t, conn, FrameData, sid, body)

		// Read a CLOSE the handler sends after closing the rwc.
		for {
			ft, _, _ := readFrame(t, conn)
			if ft == FrameClose {
				return
			}
			if ft == FrameCloseWrite {
				continue
			}
		}
	}()

	// Give the registration time to complete.
	time.Sleep(50 * time.Millisecond)
	backend := agent.backendFor("echo")
	if backend == nil {
		t.Fatal("expected backend for echo")
	}
	rwc, err := backend.Open(context.Background(), 1, "echo", "127.0.0.1:1234")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := rwc.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 16)
	n, err := rwc.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("Read = %q; want hello", string(buf[:n]))
	}
	if err := rwc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	<-done
}

func TestManager_OpenStream_OperatorRejects(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{Session: SessionConfig{OpenTimeout: 500 * time.Millisecond}})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{{Key: "rej"}}})
	readFrame(t, conn) // ack

	go func() {
		ft, sid, _ := readFrame(t, conn)
		if ft != FrameOpen {
			return
		}
		sendJSONFrame(t, conn, FrameOpenAck, sid, OpenAckPayload{OK: false, Error: "no thanks"})
	}()

	time.Sleep(50 * time.Millisecond)
	backend := agent.backendFor("rej")
	if backend == nil {
		t.Fatal("expected backend")
	}
	rwc, err := backend.Open(context.Background(), 1, "rej", "127.0.0.1:80")
	if err == nil {
		rwc.Close()
		t.Fatal("expected error from Open after operator rejection")
	}
	if !strings.Contains(err.Error(), "no thanks") {
		t.Errorf("expected error to contain 'no thanks', got %v", err)
	}
}

func TestManager_OpenStream_Timeout(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{Session: SessionConfig{OpenTimeout: 200 * time.Millisecond}})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{{Key: "slow"}}})
	readFrame(t, conn) // ack

	// Drain frames the server sends but never reply OPEN_ACK.
	go func() {
		for {
			if _, _, _, err := readNextFrameNoFatal(conn); err != nil {
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	backend := agent.backendFor("slow")
	if backend == nil {
		t.Fatal("expected backend")
	}
	_, err := backend.Open(context.Background(), 1, "slow", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got %v", err)
	}
}

func TestManager_HeartbeatKeepsAliveOnTraffic(t *testing.T) {
	// Configure tight heartbeat so the test runs fast.
	mgr, _ := startManager(t, ManagerConfig{Session: SessionConfig{
		IdleHeartbeat:     200 * time.Millisecond,
		HeartbeatDeadline: 200 * time.Millisecond,
	}})
	conn := dialClient(t, mgr)

	// Send a frame periodically; session should not close.
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{}})
		readFrame(t, conn) // drain ack
		time.Sleep(80 * time.Millisecond)
	}
	// If the connection were dead, the next read would fail.
	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{}})
	if _, _, _, err := readNextFrameNoFatal(conn); err != nil {
		t.Errorf("expected session alive, got read error: %v", err)
	}
}

func TestManager_PingPong(t *testing.T) {
	mgr, _ := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	if err := conn.writeMsg(EncodeNonceFrame(FramePing, 0xabcd)); err != nil {
		t.Fatalf("Write ping: %v", err)
	}
	ft, _, body := readFrame(t, conn)
	if ft != FramePong {
		t.Fatalf("expected PONG, got 0x%02x", ft)
	}
	nonce, err := DecodeNonce(body)
	if err != nil {
		t.Fatalf("DecodeNonce: %v", err)
	}
	if nonce != 0xabcd {
		t.Errorf("nonce = %x; want abcd", nonce)
	}
}

// Compile-time check: io.ReadWriteCloser/forward.Backend implementations.
var _ io.ReadWriteCloser = (*muxStream)(nil)
var _ forward.Backend = (*wsBackend)(nil)

func TestBackend_Target(t *testing.T) {
	b := NewBackend(nil, "myKey")
	if b.Target() != "expose:myKey" {
		t.Errorf("Target() = %q; want expose:myKey", b.Target())
	}
}

func TestManager_HalfCloseAndFullClose(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{{Key: "k"}}})
	readFrame(t, conn) // ack

	// Operator-side scripted behaviour: ack the OPEN, then send CLOSE_WRITE
	// to half-close the read side, then send CLOSE.
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		ft, sid, _ := readFrame(t, conn)
		if ft != FrameOpen {
			t.Errorf("expected OPEN, got 0x%02x", ft)
			return
		}
		sendJSONFrame(t, conn, FrameOpenAck, sid, OpenAckPayload{OK: true})

		// Half-close the read side.
		sendFrame(t, conn, FrameCloseWrite, sid, nil)
		// Then full close.
		sendFrame(t, conn, FrameClose, sid, nil)
	}()

	time.Sleep(50 * time.Millisecond)
	rwc, err := agent.backendFor("k").Open(context.Background(), 1, "k", "1.2.3.4:5")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	<-clientDone

	// The scripted operator has stopped reading. net.Pipe never reports EOF on
	// its own, so leaving the conn open models a peer that is alive but will
	// never drain again -- a state no real transport produces, and one where a
	// racing teardown CLOSE blocks until its write deadline. Close the conn to
	// model the operator actually going away, which is what a real transport
	// surfaces as an error on the next write.
	conn.close()

	// Read should now return EOF (CLOSE_WRITE seen) or net.ErrClosed (CLOSE
	// arrived first). Either is acceptable.
	buf := make([]byte, 4)
	_, err = rwc.Read(buf)
	if err == nil {
		t.Errorf("expected error from Read after operator CLOSE; got nil")
	}

	// Closing the rwc twice is safe.
	_ = rwc.Close()
	_ = rwc.Close()
}

func TestManager_StreamWriteChunking(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{{Key: "big"}}})
	readFrame(t, conn) // ack

	gotBytes := make(chan int, 4)
	go func() {
		// Wait for OPEN, ACK, count DATA bytes until CLOSE.
		ft, sid, _ := readFrame(t, conn)
		if ft != FrameOpen {
			return
		}
		sendJSONFrame(t, conn, FrameOpenAck, sid, OpenAckPayload{OK: true})
		for {
			ft, _, body := readFrame(t, conn)
			if ft == FrameClose {
				close(gotBytes)
				return
			}
			if ft == FrameData {
				gotBytes <- len(body)
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	rwc, err := agent.backendFor("big").Open(context.Background(), 1, "big", "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Write a buffer larger than MaxDataPayload. Expect at least 2 frames.
	buf := make([]byte, MaxDataPayload+512)
	for i := range buf {
		buf[i] = byte(i)
	}
	if n, err := rwc.Write(buf); err != nil || n != len(buf) {
		t.Fatalf("Write: n=%d err=%v", n, err)
	}
	rwc.Close()

	// Drain the gotBytes channel.
	totalFrames := 0
	totalBytes := 0
	for n := range gotBytes {
		totalFrames++
		totalBytes += n
	}
	if totalFrames < 2 {
		t.Errorf("expected >= 2 DATA frames, got %d", totalFrames)
	}
	if totalBytes != len(buf) {
		t.Errorf("total DATA bytes = %d; want %d", totalBytes, len(buf))
	}
}

func TestManager_Shutdown(t *testing.T) {
	mgr, _ := startManager(t, ManagerConfig{Logger: nil})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{{Key: "x"}}})
	readFrame(t, conn) // ack

	mgr.Shutdown()

	// After Shutdown, the next read should fail (server closed the conn).
	if _, _, _, err := readNextFrameNoFatal(conn); err == nil {
		t.Error("expected read to fail after Shutdown")
	}
}

func TestManager_HeartbeatTimeoutClosesSession(t *testing.T) {
	mgr, agent := startManager(t, ManagerConfig{Session: SessionConfig{
		IdleHeartbeat:     150 * time.Millisecond,
		HeartbeatDeadline: 150 * time.Millisecond,
	}})
	conn := dialClient(t, mgr)

	sendJSONFrame(t, conn, FrameRegister, 0, RegisterPayload{Forwards: []RegisterEntry{{Key: "live"}}})
	readFrame(t, conn) // ack

	// Drop into a goroutine that explicitly does NOT respond to PINGs,
	// while still draining frames so reads don't accumulate. The session
	// should time out after IdleHeartbeat + HeartbeatDeadline.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			msg, err := conn.readMsg()
			if err != nil {
				return
			}
			ft, _, body, _ := DecodeFrame(msg)
			if ft == FramePing {
				// Intentionally do not pong.
				_ = body
			}
		}
	}()
	defer close(stop)

	// Wait for the session to be cleaned up. Give it up to 1.5s.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, _ := agent.snapshot()
		if len(got) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := agent.snapshot()
	if len(got) != 0 {
		t.Errorf("expected key cleanup after heartbeat timeout; got %v", got)
	}
}
