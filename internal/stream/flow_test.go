package stream

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

func available(w *Window) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.avail
}

func flowControlled(s *Stream) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendWindow != nil
}

func TestWindow_AcquireUntilEmpty(t *testing.T) {
	w := NewWindow(3)
	for i := 0; i < 3; i++ {
		if err := w.Acquire(nil); err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}
	if got := available(w); got != 0 {
		t.Fatalf("Available() = %d, want 0", got)
	}

	acquired := make(chan error, 1)
	go func() { acquired <- w.Acquire(nil) }()
	select {
	case err := <-acquired:
		t.Fatalf("Acquire on empty window returned %v, want it to wait", err)
	case <-time.After(50 * time.Millisecond):
	}

	w.Grant(2)
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("Acquire after Grant: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire did not wake up after Grant")
	}
	if got := available(w); got != 1 {
		t.Fatalf("Available() = %d, want 1", got)
	}
}

func TestWindow_CloseWakesWaiters(t *testing.T) {
	w := NewWindow(0)
	acquired := make(chan error, 1)
	go func() { acquired <- w.Acquire(nil) }()
	time.Sleep(20 * time.Millisecond)

	w.Close()
	select {
	case err := <-acquired:
		if !errors.Is(err, ErrWindowClosed) {
			t.Fatalf("Acquire = %v, want ErrWindowClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire did not wake up after Close")
	}

	// Closed for good: grants are ignored, Acquire fails at once, and a
	// second Close is harmless.
	w.Grant(5)
	w.Close()
	if err := w.Acquire(nil); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("Acquire after Close = %v, want ErrWindowClosed", err)
	}
}

func TestWindow_DoneChannel(t *testing.T) {
	w := NewWindow(0)
	done := make(chan struct{})
	acquired := make(chan error, 1)
	go func() { acquired <- w.Acquire(done) }()
	close(done)
	select {
	case err := <-acquired:
		if !errors.Is(err, ErrWindowClosed) {
			t.Fatalf("Acquire = %v, want ErrWindowClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire did not return after done closed")
	}
}

func TestWindow_ConcurrentSendersNeverExceedGrants(t *testing.T) {
	w := NewWindow(10)
	const senders, perSender = 8, 50

	var mu sync.Mutex
	sent := 0
	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perSender; j++ {
				if err := w.Acquire(nil); err != nil {
					return
				}
				mu.Lock()
				sent++
				mu.Unlock()
			}
		}()
	}

	granted := 10
	for granted < senders*perSender {
		time.Sleep(time.Millisecond)
		mu.Lock()
		if sent > granted {
			mu.Unlock()
			t.Fatalf("sent %d frames with only %d granted", sent, granted)
		}
		mu.Unlock()
		w.Grant(7)
		granted += 7
	}
	wg.Wait()
	if sent != senders*perSender {
		t.Fatalf("sent = %d, want %d", sent, senders*perSender)
	}
}

func TestCredit_BatchesAtThreshold(t *testing.T) {
	var c Credit
	for i := 1; i < protocol.StreamWindowUpdateThreshold; i++ {
		if n := c.Consumed(1); n != 0 {
			t.Fatalf("Consumed after %d frames = %d, want 0", i, n)
		}
	}
	if n := c.Consumed(1); n != protocol.StreamWindowUpdateThreshold {
		t.Fatalf("Consumed at threshold = %d, want %d", n, protocol.StreamWindowUpdateThreshold)
	}
	if n := c.Consumed(1); n != 0 {
		t.Fatalf("Consumed after a grant = %d, want 0 (batch restarted)", n)
	}
}

// The batching threshold must leave the sender window to keep sending while
// a grant is in flight, or every batch would stall the stream for a round
// trip.
func TestWindowConstants(t *testing.T) {
	if protocol.StreamWindowUpdateThreshold >= protocol.StreamWindowFrames {
		t.Fatalf("threshold %d must be below the window %d",
			protocol.StreamWindowUpdateThreshold, protocol.StreamWindowFrames)
	}
}

func newFlowStream(t *testing.T) (*Stream, *[]uint32) {
	t.Helper()
	local, _ := identity.NewAgentID()
	remote, _ := identity.NewAgentID()
	s := NewStream(1, local, remote, 1)
	var mu sync.Mutex
	grants := []uint32{}
	s.EnableFlowControl(func(n uint32) {
		mu.Lock()
		grants = append(grants, n)
		mu.Unlock()
	})
	s.Open()
	return s, &grants
}

func TestStream_FlowControlReturnsCreditOnRead(t *testing.T) {
	s, grants := newFlowStream(t)
	if !flowControlled(s) {
		t.Fatal("flow control on = false after EnableFlowControl")
	}

	// A full window fits the read buffer: a sender honoring the window can
	// never hit ErrReadBufferFull.
	for i := 0; i < protocol.StreamWindowFrames; i++ {
		if err := s.PushData([]byte{byte(i)}); err != nil {
			t.Fatalf("PushData %d: %v", i, err)
		}
	}

	ctx := context.Background()
	for i := 0; i < protocol.StreamWindowUpdateThreshold-1; i++ {
		if _, err := s.Read(ctx); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if len(*grants) != 0 {
		t.Fatalf("grants before threshold = %v, want none", *grants)
	}
	if _, ok := s.TryRead(); !ok {
		t.Fatal("TryRead found no buffered frame")
	}
	if len(*grants) != 1 || (*grants)[0] != protocol.StreamWindowUpdateThreshold {
		t.Fatalf("grants = %v, want [%d]", *grants, protocol.StreamWindowUpdateThreshold)
	}
}

func TestStream_AcquireSendBlocksUntilGrant(t *testing.T) {
	s, _ := newFlowStream(t)
	for i := 0; i < protocol.StreamWindowFrames; i++ {
		if err := s.AcquireSend(); err != nil {
			t.Fatalf("AcquireSend %d: %v", i, err)
		}
	}

	acquired := make(chan error, 1)
	go func() { acquired <- s.AcquireSend() }()
	select {
	case err := <-acquired:
		t.Fatalf("AcquireSend past the window returned %v, want it to wait", err)
	case <-time.After(50 * time.Millisecond):
	}
	s.GrantSend(1)
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("AcquireSend after grant: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AcquireSend did not wake up after GrantSend")
	}

	// Closing the stream releases a blocked sender with an error.
	go func() { acquired <- s.AcquireSend() }()
	time.Sleep(20 * time.Millisecond)
	s.Close()
	select {
	case err := <-acquired:
		if !errors.Is(err, ErrWindowClosed) {
			t.Fatalf("AcquireSend after Close = %v, want ErrWindowClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AcquireSend did not return after Close")
	}
}

func TestStream_NoFlowControlNeverBlocks(t *testing.T) {
	local, _ := identity.NewAgentID()
	remote, _ := identity.NewAgentID()
	s := NewStream(1, local, remote, 1)
	s.Open()
	if flowControlled(s) {
		t.Fatal("flow control on = true without EnableFlowControl")
	}
	for i := 0; i < 4*protocol.StreamWindowFrames; i++ {
		if err := s.AcquireSend(); err != nil {
			t.Fatalf("AcquireSend: %v", err)
		}
	}
}

func TestStream_ResetFailsReaders(t *testing.T) {
	local, _ := identity.NewAgentID()
	remote, _ := identity.NewAgentID()
	s := NewStream(1, local, remote, 1)
	s.Open()
	if err := s.PushData([]byte("lost")); err != nil {
		t.Fatalf("PushData: %v", err)
	}

	readErr := make(chan error, 1)
	s.Reset(&ResetError{Code: protocol.ErrResourceLimit})
	go func() {
		// Drain whatever the reader gets until it fails.
		for {
			if _, err := s.Read(context.Background()); err != nil {
				readErr <- err
				return
			}
		}
	}()

	select {
	case err := <-readErr:
		var re *ResetError
		if !errors.As(err, &re) {
			t.Fatalf("Read error = %v, want *ResetError", err)
		}
		if re.Code != protocol.ErrResourceLimit {
			t.Fatalf("ResetError.Code = %d, want %d", re.Code, protocol.ErrResourceLimit)
		}
		if errors.Is(err, io.EOF) {
			t.Fatal("reset must not look like a clean EOF")
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not fail after Reset")
	}
}

func TestManager_FlowControlNegotiation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ackFlag   bool
		wantFlow  bool
		setSender bool
	}{
		{"ack with flag", true, true, true},
		{"legacy ack", false, false, true},
		{"no window sender", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, _ := identity.NewAgentID()
			remote, _ := identity.NewAgentID()
			m := NewManager(DefaultManagerConfig(), local)

			type grant struct {
				peer identity.AgentID
				id   uint64
				n    uint32
			}
			grants := make(chan grant, 4)
			if tc.setSender {
				m.SetWindowSender(func(peer identity.AgentID, id uint64, n uint32) {
					grants <- grant{peer, id, n}
				})
			}

			pending := m.OpenStream(1, remote, "127.0.0.1", 80, time.Minute)
			var eph [32]byte
			s, err := m.HandleStreamOpenAck(remote, pending.RequestID, nil, 0, eph, tc.ackFlag)
			if err != nil {
				t.Fatalf("HandleStreamOpenAck: %v", err)
			}
			if got := flowControlled(s); got != tc.wantFlow {
				t.Fatalf("flow control on = %v, want %v", got, tc.wantFlow)
			}
			if !tc.wantFlow {
				return
			}

			// STREAM_WINDOW for the stream reaches its send window.
			if !m.HandleStreamWindow(remote, s.ID, 3) {
				t.Fatal("HandleStreamWindow did not find the stream")
			}
			if m.HandleStreamWindow(local, s.ID, 3) {
				t.Fatal("HandleStreamWindow matched a stream of a different peer")
			}

			// Consuming data sends grants addressed to the stream's peer.
			for i := 0; i < protocol.StreamWindowUpdateThreshold; i++ {
				if err := m.HandleStreamData(remote, s.ID, 0, []byte{1}); err != nil {
					t.Fatalf("HandleStreamData: %v", err)
				}
				if _, err := s.Read(context.Background()); err != nil {
					t.Fatalf("Read: %v", err)
				}
			}
			select {
			case g := <-grants:
				if g.peer != remote || g.id != s.ID || g.n != protocol.StreamWindowUpdateThreshold {
					t.Fatalf("grant = %+v, want peer %s stream %d n %d", g, remote, s.ID, protocol.StreamWindowUpdateThreshold)
				}
			case <-time.After(time.Second):
				t.Fatal("no window grant sent")
			}
		})
	}
}
