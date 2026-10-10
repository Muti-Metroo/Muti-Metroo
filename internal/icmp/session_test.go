package icmp

import (
	"net"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

func TestNewSession(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")

	session := NewSession(12345, 67890, peerID, destIP)

	if session.StreamID != 12345 {
		t.Errorf("StreamID = %d, want 12345", session.StreamID)
	}
	if session.RequestID != 67890 {
		t.Errorf("RequestID = %d, want 67890", session.RequestID)
	}
	if session.PeerID != peerID {
		t.Error("PeerID mismatch")
	}
	if !session.DestIP.Equal(destIP) {
		t.Errorf("DestIP = %v, want %v", session.DestIP, destIP)
	}
	if session.State != StateOpening {
		t.Errorf("State = %v, want OPENING", session.State)
	}
	if session.IsClosed() {
		t.Error("Session should not be closed")
	}
	if session.ctx == nil {
		t.Error("Context should not be nil")
	}
}

func TestSession_StateTransitions(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")
	session := NewSession(1, 1, peerID, destIP)

	// Initial state
	if session.GetState() != StateOpening {
		t.Errorf("Initial state = %v, want OPENING", session.GetState())
	}
	if session.GetState().String() != "OPENING" {
		t.Errorf("State string = %q, want OPENING", session.GetState().String())
	}

	// Transition to open
	session.SetOpen()
	if session.GetState() != StateOpen {
		t.Errorf("After SetOpen, state = %v, want OPEN", session.GetState())
	}
	if session.GetState().String() != "OPEN" {
		t.Errorf("State string = %q, want OPEN", session.GetState().String())
	}

	// Close
	session.Close()
	if session.GetState() != StateClosed {
		t.Errorf("After Close, state = %v, want CLOSED", session.GetState())
	}
	if !session.IsClosed() {
		t.Error("IsClosed() should return true after Close()")
	}
}

func TestSession_Activity(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")
	session := NewSession(1, 1, peerID, destIP)

	// Initial activity time
	initialTime := session.LastActivity

	// Wait a bit and update
	time.Sleep(10 * time.Millisecond)
	session.UpdateActivity()

	if !session.LastActivity.After(initialTime) {
		t.Error("LastActivity should be updated after UpdateActivity()")
	}
}

func TestSession_IsExpired(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")
	session := NewSession(1, 1, peerID, destIP)

	// Not expired immediately
	if session.IsExpired(time.Second) {
		t.Error("Session should not be expired immediately")
	}

	// Zero timeout means never expires
	if session.IsExpired(0) {
		t.Error("Session should not expire with zero timeout")
	}

	// Wait and check expiration
	time.Sleep(20 * time.Millisecond)
	if !session.IsExpired(10 * time.Millisecond) {
		t.Error("Session should be expired after timeout")
	}

	// Update activity and check again
	session.UpdateActivity()
	if session.IsExpired(time.Second) {
		t.Error("Session should not be expired after activity update")
	}
}

func TestSession_Close(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")
	session := NewSession(1, 1, peerID, destIP)

	// First close should succeed
	err := session.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Second close should be idempotent
	err = session.Close()
	if err != nil {
		t.Errorf("Second Close() error = %v", err)
	}

	// Context should be cancelled
	select {
	case <-session.Context().Done():
		// Good
	default:
		t.Error("Context should be cancelled after Close()")
	}
}

func TestSession_EncryptDecrypt_NoKey(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")
	session := NewSession(1, 1, peerID, destIP)

	// Without session key, data should pass through unchanged
	plaintext := []byte("test data")

	encrypted, err := session.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if string(encrypted) != string(plaintext) {
		t.Error("Without key, Encrypt() should return original data")
	}

	decrypted, err := session.Decrypt(plaintext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Error("Without key, Decrypt() should return original data")
	}
}

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{StateOpening, "OPENING"},
		{StateOpen, "OPEN"},
		{StateClosed, "CLOSED"},
		{SessionState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestSession_SetOpen_OnlyFromOpening(t *testing.T) {
	peerID, _ := identity.NewAgentID()
	destIP := net.ParseIP("8.8.8.8")
	session := NewSession(1, 1, peerID, destIP)

	// Close immediately
	session.Close()

	// SetOpen should not work when already closed
	session.SetOpen()
	if session.GetState() != StateClosed {
		t.Error("SetOpen() should not change state from CLOSED")
	}
}
