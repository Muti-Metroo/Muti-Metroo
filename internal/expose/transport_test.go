package expose

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// pipePair returns a connected rawConn (server side) and the raw client end of
// a net.Pipe so a test can drive bytes against the rawConn framing.
func pipePair(t *testing.T, maxRead int64) (*rawConn, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	return newRawConn(c1, maxRead), c2
}

func TestRawConn_RoundTrip(t *testing.T) {
	rc, client := pipePair(t, defaultReadLimit)

	want := EncodeFrame(FrameData, 7, []byte("hello world"))

	go func() {
		// Read the [u32 len][bytes] frame the rawConn writes.
		var hdr [4]byte
		if _, err := io.ReadFull(client, hdr[:]); err != nil {
			t.Errorf("read len: %v", err)
			return
		}
		n := binary.BigEndian.Uint32(hdr[:])
		buf := make([]byte, n)
		if _, err := io.ReadFull(client, buf); err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		// Echo it straight back in the same framing.
		out := make([]byte, 4+len(buf))
		binary.BigEndian.PutUint32(out[0:4], uint32(len(buf)))
		copy(out[4:], buf)
		if _, err := client.Write(out); err != nil {
			t.Errorf("echo write: %v", err)
		}
	}()

	if err := rc.WriteMessage(want); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := rc.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("round-trip mismatch: got %x want %x", got, want)
	}
}

func TestRawConn_OversizeRejected(t *testing.T) {
	const maxRead = 64
	rc, client := pipePair(t, maxRead)

	go func() {
		// Announce a frame larger than maxRead; the body never needs to be sent.
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], maxRead+1)
		_, _ = client.Write(hdr[:])
	}()

	_, err := rc.ReadMessage()
	if err == nil {
		t.Fatal("expected oversize frame to be rejected")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("expected 'too large' error, got %v", err)
	}
}

func TestRawConn_PartialReads(t *testing.T) {
	rc, client := pipePair(t, defaultReadLimit)

	payload := []byte("partial frame delivery")
	frame := EncodeFrame(FrameData, 1, payload)

	go func() {
		// Dribble the length prefix one byte at a time, then the body in two
		// chunks, to exercise io.ReadFull's looping.
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(frame)))
		for _, b := range hdr {
			_, _ = client.Write([]byte{b})
			time.Sleep(2 * time.Millisecond)
		}
		mid := len(frame) / 2
		_, _ = client.Write(frame[:mid])
		time.Sleep(2 * time.Millisecond)
		_, _ = client.Write(frame[mid:])
	}()

	got, err := rc.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(got) != string(frame) {
		t.Errorf("got %x want %x", got, frame)
	}
}

func TestRawConn_CloseUnblocksRead(t *testing.T) {
	rc, _ := pipePair(t, defaultReadLimit)
	done := make(chan error, 1)
	go func() {
		_, err := rc.ReadMessage()
		done <- err
	}()
	// Give the read a moment to block, then close.
	time.Sleep(20 * time.Millisecond)
	_ = rc.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error after Close, got nil")
		}
	case <-time.After(time.Second):
		t.Fatal("ReadMessage did not unblock after Close")
	}
}
