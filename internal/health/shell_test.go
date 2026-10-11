package health

import (
	"sync"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

type recordingSender struct {
	mu     sync.Mutex
	frames []sentFrame
}

type sentFrame struct {
	peerID identity.AgentID
	frame  *protocol.Frame
}

func (r *recordingSender) SendToPeer(peerID identity.AgentID, frame *protocol.Frame) error {
	r.mu.Lock()
	r.frames = append(r.frames, sentFrame{peerID: peerID, frame: frame})
	r.mu.Unlock()
	return nil
}

func (r *recordingSender) snapshot() []sentFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sentFrame, len(r.frames))
	copy(out, r.frames)
	return out
}

func nonZeroAgentID(b byte) identity.AgentID {
	var id identity.AgentID
	id[0] = b
	return id
}

func TestShellStreamAdapter_CloseSendsStreamClose(t *testing.T) {
	var closeFuncCalled int
	adapter := NewShellStreamAdapter(42, nonZeroAgentID(0xaa), func() { closeFuncCalled++ })

	sender := &recordingSender{}
	nextHop := nonZeroAgentID(0xbb)
	adapter.SetNextHop(nextHop, sender)

	adapter.Close()

	frames := sender.snapshot()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame sent, got %d", len(frames))
	}
	if frames[0].peerID != nextHop {
		t.Errorf("frame sent to wrong peer: got %v, want %v", frames[0].peerID, nextHop)
	}
	if frames[0].frame.Type != protocol.FrameStreamClose {
		t.Errorf("frame type: got %d, want FrameStreamClose (%d)", frames[0].frame.Type, protocol.FrameStreamClose)
	}
	if frames[0].frame.StreamID != 42 {
		t.Errorf("stream ID: got %d, want 42", frames[0].frame.StreamID)
	}
	if closeFuncCalled != 1 {
		t.Errorf("closeFunc called %d times, want 1", closeFuncCalled)
	}
}

func TestShellStreamAdapter_CloseFromRemoteSkipsStreamClose(t *testing.T) {
	adapter := NewShellStreamAdapter(7, nonZeroAgentID(0xaa), nil)

	sender := &recordingSender{}
	adapter.SetNextHop(nonZeroAgentID(0xbb), sender)

	adapter.CloseFromRemote()

	if frames := sender.snapshot(); len(frames) != 0 {
		t.Fatalf("expected 0 frames sent from CloseFromRemote, got %d", len(frames))
	}
}

func TestShellStreamAdapter_CloseSkipsBeforeSetNextHop(t *testing.T) {
	adapter := NewShellStreamAdapter(11, nonZeroAgentID(0xaa), nil)

	// Deliberately no SetNextHop -- mirrors early error paths in OpenShellStream.
	adapter.Close()

	// No sender to check because SetNextHop was never called; the test is
	// simply that Close() does not panic and does not try to emit a frame.
}

func TestShellStreamAdapter_CloseIdempotent(t *testing.T) {
	adapter := NewShellStreamAdapter(99, nonZeroAgentID(0xaa), nil)

	sender := &recordingSender{}
	adapter.SetNextHop(nonZeroAgentID(0xbb), sender)

	adapter.Close()
	adapter.Close()
	adapter.Close()

	if frames := sender.snapshot(); len(frames) != 1 {
		t.Fatalf("expected exactly 1 frame across repeated Close calls, got %d", len(frames))
	}
}

func TestShellStreamAdapter_NextHop(t *testing.T) {
	adapter := NewShellStreamAdapter(1, nonZeroAgentID(0xaa), nil)
	if adapter.NextHop() != (identity.AgentID{}) {
		t.Fatalf("expected zero NextHop before SetNextHop")
	}

	nextHop := nonZeroAgentID(0xcc)
	adapter.SetNextHop(nextHop, &recordingSender{})
	if adapter.NextHop() != nextHop {
		t.Errorf("NextHop after set: got %v, want %v", adapter.NextHop(), nextHop)
	}
}
