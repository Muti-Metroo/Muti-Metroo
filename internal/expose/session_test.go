package expose

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// fakeRegistrar is a no-op keyRegistrar used in unit tests that exercise
// muxStream / Session methods directly without a transport.
type fakeRegistrar struct{}

func (fakeRegistrar) register(string, *Session) error { return nil }
func (fakeRegistrar) unregister(string, *Session)     {}
func (fakeRegistrar) triggerAdvertise()               {}

// freshSessionForTest builds a minimal Session with a nil conn for tests that
// only exercise muxStream methods that do not write to the wire. Calls that
// would write (CloseWrite, Close, Write) are routed through a stub via
// shadowing where appropriate.
func freshSessionForTest(t *testing.T) *Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &Session{
		id:                "test",
		rootCtx:           ctx,
		cancelFn:          cancel,
		register:          fakeRegistrar{},
		keys:              make(map[string]struct{}),
		streams:           make(map[uint64]*muxStream),
		pending:           make(map[uint64]chan openResult),
		openTimeout:       time.Second,
		idleHeartbeat:     time.Second,
		heartbeatDeadline: time.Second,
	}
	return s
}

func TestMuxStream_ReadDrainsBufferThenEOF(t *testing.T) {
	sess := freshSessionForTest(t)
	st := newMuxStream(sess, 1)

	// Deliver two payloads, then close the read side -> Read should drain
	// then return EOF.
	st.deliver([]byte("hello "))
	st.deliver([]byte("world"))
	st.closeReadSide()

	got := make([]byte, 0, 32)
	buf := make([]byte, 4)
	for {
		n, err := st.Read(buf)
		got = append(got, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if string(got) != "hello world" {
		t.Errorf("got %q; want hello world", string(got))
	}
}

func TestMuxStream_CloseReturnsErrClosed(t *testing.T) {
	sess := freshSessionForTest(t)
	st := newMuxStream(sess, 1)

	// Use tearDown(false) so we don't try to send CLOSE on a nil conn.
	_ = st.tearDown(false)

	buf := make([]byte, 4)
	_, err := st.Read(buf)
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("Read after tearDown: err = %v; want net.ErrClosed", err)
	}

	_, err = st.Write([]byte("x"))
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("Write after tearDown: err = %v; want net.ErrClosed", err)
	}
}

func TestMuxStream_DeliverEmptyIsNoOp(t *testing.T) {
	sess := freshSessionForTest(t)
	st := newMuxStream(sess, 1)
	st.deliver(nil)
	st.deliver([]byte{})
	st.closeReadSide()
	buf := make([]byte, 1)
	_, err := st.Read(buf)
	if err != io.EOF {
		t.Errorf("expected EOF from empty stream; got %v", err)
	}
}

func TestSessionConfig_Defaults(t *testing.T) {
	cfg := SessionConfig{}.withDefaults()
	if cfg.OpenTimeout != 30*time.Second {
		t.Errorf("OpenTimeout default = %v", cfg.OpenTimeout)
	}
	if cfg.IdleHeartbeat != 60*time.Second {
		t.Errorf("IdleHeartbeat default = %v", cfg.IdleHeartbeat)
	}
	if cfg.HeartbeatDeadline != 30*time.Second {
		t.Errorf("HeartbeatDeadline default = %v", cfg.HeartbeatDeadline)
	}
	if cfg.Logger == nil {
		t.Error("Logger default = nil")
	}
}

func TestManagerConfig_Defaults(t *testing.T) {
	cfg := ManagerConfig{}.withDefaults()
	if cfg.ReadLimit <= 0 {
		t.Errorf("ReadLimit = %d", cfg.ReadLimit)
	}
	if cfg.Logger == nil {
		t.Error("Logger nil")
	}
}
