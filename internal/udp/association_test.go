package udp

import (
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

func TestAssociationState_String(t *testing.T) {
	tests := []struct {
		state AssociationState
		want  string
	}{
		{StateOpening, "OPENING"},
		{StateOpen, "OPEN"},
		{StateClosed, "CLOSED"},
		{AssociationState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestNewAssociation(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	assoc := NewAssociation(123, 456, peerID)

	if assoc.StreamID != 123 {
		t.Errorf("StreamID = %d, want 123", assoc.StreamID)
	}
	if assoc.RequestID != 456 {
		t.Errorf("RequestID = %d, want 456", assoc.RequestID)
	}
	if assoc.PeerID != peerID {
		t.Error("PeerID mismatch")
	}
	if assoc.State != StateOpening {
		t.Errorf("State = %v, want %v", assoc.State, StateOpening)
	}
	if assoc.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if assoc.LastActivity.IsZero() {
		t.Error("LastActivity should be set")
	}
	if assoc.IsClosed() {
		t.Error("New association should not be closed")
	}
}

func TestAssociation_SetOpen(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	assoc := NewAssociation(1, 1, peerID)

	if assoc.GetState() != StateOpening {
		t.Errorf("Initial state = %v, want %v", assoc.GetState(), StateOpening)
	}

	assoc.SetOpen()

	if assoc.GetState() != StateOpen {
		t.Errorf("State after SetOpen = %v, want %v", assoc.GetState(), StateOpen)
	}

	// SetOpen should be idempotent for closed associations
	assoc.Close()
	assoc.SetOpen() // Should not change state
	if assoc.GetState() != StateClosed {
		t.Errorf("State should remain closed")
	}
}

func TestAssociation_UpdateActivity(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	assoc := NewAssociation(1, 1, peerID)

	initial := assoc.LastActivity
	time.Sleep(10 * time.Millisecond)

	assoc.UpdateActivity()

	if !assoc.LastActivity.After(initial) {
		t.Error("LastActivity should be updated")
	}
}

func TestAssociation_IsExpired(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	assoc := NewAssociation(1, 1, peerID)

	// Zero timeout means never expired
	if assoc.IsExpired(0) {
		t.Error("Should not expire with 0 timeout")
	}

	// Not expired with recent activity
	if assoc.IsExpired(1 * time.Hour) {
		t.Error("Should not be expired immediately")
	}

	// Expired with very short timeout
	time.Sleep(20 * time.Millisecond)
	if !assoc.IsExpired(1 * time.Millisecond) {
		t.Error("Should be expired with 1ms timeout after 20ms")
	}
}

func TestAssociation_Close(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	assoc := NewAssociation(1, 1, peerID)
	assoc.SetOpen()

	if assoc.IsClosed() {
		t.Error("Should not be closed before Close()")
	}

	err := assoc.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if !assoc.IsClosed() {
		t.Error("Should be closed after Close()")
	}

	if assoc.GetState() != StateClosed {
		t.Errorf("State = %v, want %v", assoc.GetState(), StateClosed)
	}

	// Double close should be safe
	err = assoc.Close()
	if err != nil {
		t.Errorf("Double Close() error = %v", err)
	}
}

func TestAssociation_Context(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	assoc := NewAssociation(1, 1, peerID)

	ctx := assoc.Context()
	if ctx == nil {
		t.Error("Context should not be nil")
	}

	// Context should be active
	select {
	case <-ctx.Done():
		t.Error("Context should not be done")
	default:
	}

	// Close association
	assoc.Close()

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be done after Close")
	}
}
