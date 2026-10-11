package socks5

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// mockExpose is a test ExposeOverSocks. On HandleRawConn it reads one
// u32-length-prefixed message off the raw stream and echoes it straight back,
// proving the connection was handed off authenticated and full-duplex.
type mockExpose struct {
	called chan string
}

func newMockExpose() *mockExpose { return &mockExpose{called: make(chan string, 1)} }

func (m *mockExpose) HandleRawConn(conn net.Conn, remoteAddr string) {
	select {
	case m.called <- remoteAddr:
	default:
	}
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return
	}
	out := make([]byte, 4+len(buf))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(buf)))
	copy(out[4:], buf)
	_, _ = conn.Write(out)
}

func startExposeServer(t *testing.T, handler ExposeOverSocks) *Server {
	t.Helper()
	cfg := DefaultServerConfig()
	cfg.Address = "127.0.0.1:0"
	s := NewServer(cfg)
	if handler != nil {
		s.SetExposeHandler(handler)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestExpose_UnsupportedWhenNoHandler(t *testing.T) {
	s := startExposeServer(t, nil)

	conn, rep := dialRouteWatch(t, s.Address().String(), CmdExpose)
	defer conn.Close()
	if rep != ReplyCmdNotSupported {
		t.Fatalf("reply = %d, want ReplyCmdNotSupported %d", rep, ReplyCmdNotSupported)
	}
}

func TestExpose_HandsOffRawStream(t *testing.T) {
	mock := newMockExpose()
	s := startExposeServer(t, mock)

	conn, rep := dialRouteWatch(t, s.Address().String(), CmdExpose)
	defer conn.Close()
	if rep != ReplySucceeded {
		t.Fatalf("reply = %d, want ReplySucceeded %d", rep, ReplySucceeded)
	}

	select {
	case <-mock.called:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleRawConn was not invoked")
	}

	// Drive a framed message through the handed-off stream and read the echo.
	payload := []byte("expose round-trip")
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(payload)))
	copy(frame[4:], payload)

	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		t.Fatalf("read echo len: %v", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	got := make([]byte, n)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo body: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("echo = %q; want %q", got, payload)
	}
}
