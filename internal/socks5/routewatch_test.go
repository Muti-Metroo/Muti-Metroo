package socks5

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// mockRouteWatch is a test RouteWatchProvider with a controllable snapshot
// body and an externally-driven change signal.
type mockRouteWatch struct {
	mu           sync.Mutex
	snap         []byte
	calls        int
	sig          chan struct{}
	unsubscribed bool
}

func newMockRouteWatch(snap string) *mockRouteWatch {
	return &mockRouteWatch{snap: []byte(snap), sig: make(chan struct{}, 8)}
}

func (m *mockRouteWatch) Snapshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.snap, nil
}

func (m *mockRouteWatch) setSnap(s string) {
	m.mu.Lock()
	m.snap = []byte(s)
	m.mu.Unlock()
}

func (m *mockRouteWatch) snapCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockRouteWatch) Subscribe() (<-chan struct{}, func()) {
	return m.sig, func() {
		m.mu.Lock()
		m.unsubscribed = true
		m.mu.Unlock()
	}
}

// startRouteWatchServer starts a SOCKS5 server with the given route-watch
// provider and returns it. When provider is nil, no handler is set.
func startRouteWatchServer(t *testing.T, provider RouteWatchProvider) *Server {
	t.Helper()
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	s := NewServer(cfg)
	if provider != nil {
		s.SetRouteWatchHandler(provider)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

// dialRouteWatch performs the no-auth greeting and sends the given command,
// returning the connection positioned just after the SOCKS5 reply. It returns
// the reply code.
func dialRouteWatch(t *testing.T, addr string, cmd byte) (net.Conn, byte) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	conn.Write([]byte{SOCKS5Version, 1, AuthMethodNoAuth})
	methodResp := make([]byte, 2)
	if _, err := io.ReadFull(conn, methodResp); err != nil {
		t.Fatalf("read method: %v", err)
	}

	req := &bytes.Buffer{}
	req.WriteByte(SOCKS5Version)
	req.WriteByte(cmd)
	req.WriteByte(0x00)
	req.WriteByte(AddrTypeIPv4)
	req.Write(net.IPv4zero.To4())
	binary.Write(req, binary.BigEndian, uint16(0))
	conn.Write(req.Bytes())

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	return conn, reply[1]
}

// readFrame reads one route-watch frame from conn.
func readFrame(t *testing.T, conn net.Conn) (frameType byte, payload []byte) {
	t.Helper()
	hdr := make([]byte, 5)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, hdr); err != nil {
		t.Fatalf("read frame header: %v", err)
	}
	length := binary.BigEndian.Uint32(hdr[1:5])
	if length == 0 {
		return hdr[0], nil
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		t.Fatalf("read frame payload: %v", err)
	}
	return hdr[0], payload
}

func TestRouteWatch_InitialSnapshot(t *testing.T) {
	mock := newMockRouteWatch(`{"hello":"world"}`)
	s := startRouteWatchServer(t, mock)

	conn, rep := dialRouteWatch(t, s.Address().String(), CmdRouteWatch)
	defer conn.Close()
	if rep != ReplySucceeded {
		t.Fatalf("reply = %d, want %d", rep, ReplySucceeded)
	}

	ft, payload := readFrame(t, conn)
	if ft != routeWatchSnapshot {
		t.Fatalf("frame type = %d, want SNAPSHOT %d", ft, routeWatchSnapshot)
	}
	if string(payload) != `{"hello":"world"}` {
		t.Fatalf("payload = %q, want initial snapshot", payload)
	}
}

func TestRouteWatch_ChangePushesSnapshot(t *testing.T) {
	mock := newMockRouteWatch(`{"v":1}`)
	s := startRouteWatchServer(t, mock)

	conn, _ := dialRouteWatch(t, s.Address().String(), CmdRouteWatch)
	defer conn.Close()

	// Consume the initial snapshot.
	if ft, _ := readFrame(t, conn); ft != routeWatchSnapshot {
		t.Fatalf("expected initial snapshot, got type %d", ft)
	}

	// Change snapshot body and signal.
	mock.setSnap(`{"v":2}`)
	mock.sig <- struct{}{}

	ft, payload := readFrame(t, conn)
	if ft != routeWatchSnapshot {
		t.Fatalf("frame type = %d, want SNAPSHOT", ft)
	}
	if string(payload) != `{"v":2}` {
		t.Fatalf("payload = %q, want updated snapshot", payload)
	}
}

func TestRouteWatch_CoalescesBurst(t *testing.T) {
	mock := newMockRouteWatch(`{"v":1}`)
	s := startRouteWatchServer(t, mock)

	conn, _ := dialRouteWatch(t, s.Address().String(), CmdRouteWatch)
	defer conn.Close()

	readFrame(t, conn) // initial
	callsAfterInitial := mock.snapCalls()

	// Fire a burst of signals within the coalesce window.
	for i := 0; i < 5; i++ {
		mock.sig <- struct{}{}
	}

	ft, _ := readFrame(t, conn)
	if ft != routeWatchSnapshot {
		t.Fatalf("frame type = %d, want SNAPSHOT", ft)
	}
	// The burst should collapse into a single extra Snapshot() call.
	if got := mock.snapCalls() - callsAfterInitial; got != 1 {
		t.Fatalf("Snapshot() called %d times for burst, want 1 (coalesced)", got)
	}
}

func TestRouteWatch_Heartbeat(t *testing.T) {
	// Lower the heartbeat interval so the test does not wait the production
	// default. Restore it afterward.
	orig := routeWatchHeartbeatInterval
	routeWatchHeartbeatInterval = 200 * time.Millisecond
	defer func() { routeWatchHeartbeatInterval = orig }()

	mock := newMockRouteWatch(`{}`)
	s := startRouteWatchServer(t, mock)

	conn, _ := dialRouteWatch(t, s.Address().String(), CmdRouteWatch)
	defer conn.Close()

	readFrame(t, conn) // initial snapshot

	// With no changes, the next frame should be a heartbeat.
	ft, payload := readFrame(t, conn)
	if ft != routeWatchHeartbeat {
		t.Fatalf("frame type = %d, want HEARTBEAT %d", ft, routeWatchHeartbeat)
	}
	if len(payload) != 0 {
		t.Fatalf("heartbeat payload len = %d, want 0", len(payload))
	}
}

func TestRouteWatch_UnsupportedWhenNoProvider(t *testing.T) {
	s := startRouteWatchServer(t, nil)

	conn, rep := dialRouteWatch(t, s.Address().String(), CmdRouteWatch)
	defer conn.Close()
	if rep != ReplyCmdNotSupported {
		t.Fatalf("reply = %d, want ReplyCmdNotSupported %d", rep, ReplyCmdNotSupported)
	}
}

func TestSnapshot_OneShot(t *testing.T) {
	mock := newMockRouteWatch(`{"trace":true}`)
	s := startRouteWatchServer(t, mock)

	conn, rep := dialRouteWatch(t, s.Address().String(), CmdSnapshot)
	defer conn.Close()
	if rep != ReplySucceeded {
		t.Fatalf("reply = %d, want %d", rep, ReplySucceeded)
	}

	ft, payload := readFrame(t, conn)
	if ft != routeWatchSnapshot {
		t.Fatalf("frame type = %d, want SNAPSHOT", ft)
	}
	if string(payload) != `{"trace":true}` {
		t.Fatalf("payload = %q", payload)
	}

	// The connection should close after the single frame (EOF on next read).
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("expected EOF after one-shot snapshot, got %v", err)
	}
}

func TestSnapshot_UnsupportedWhenNoProvider(t *testing.T) {
	s := startRouteWatchServer(t, nil)

	conn, rep := dialRouteWatch(t, s.Address().String(), CmdSnapshot)
	defer conn.Close()
	if rep != ReplyCmdNotSupported {
		t.Fatalf("reply = %d, want ReplyCmdNotSupported %d", rep, ReplyCmdNotSupported)
	}
}
