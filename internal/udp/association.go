package udp

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// AssociationState represents the state of a UDP association.
type AssociationState int

const (
	// StateOpening means the association is being established.
	StateOpening AssociationState = iota
	// StateOpen means the association is active and can relay datagrams.
	StateOpen
	// StateClosed means the association has been terminated.
	StateClosed
)

// String returns a human-readable name for the state.
func (s AssociationState) String() string {
	switch s {
	case StateOpening:
		return "OPENING"
	case StateOpen:
		return "OPEN"
	case StateClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

// Association represents an active UDP tunnel through the mesh.
type Association struct {
	mu sync.RWMutex

	// Identifiers
	StreamID  uint64           // Changes per hop (local to this agent)
	RequestID uint64           // Stable across hops for correlation
	PeerID    identity.AgentID // Direct peer that sent the UDP_OPEN

	// State
	State        AssociationState
	CreatedAt    time.Time
	LastActivity time.Time

	// UDP socket (only on exit node)
	UDPConn   *net.UDPConn
	RelayAddr *net.UDPAddr // Local address the UDP socket is bound to

	// Encryption
	SessionKey *crypto.SessionKey

	// Client tracking (for return path)
	ClientAddr *net.UDPAddr // SOCKS5 client's address (for ingress)

	// Cleanup
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

// NewAssociation creates a new UDP association.
func NewAssociation(streamID, requestID uint64, peerID identity.AgentID) *Association {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()

	return &Association{
		StreamID:     streamID,
		RequestID:    requestID,
		PeerID:       peerID,
		State:        StateOpening,
		CreatedAt:    now,
		LastActivity: now,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// SetOpen transitions the association to the open state.
func (a *Association) SetOpen() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.State == StateOpening {
		a.State = StateOpen
		a.LastActivity = time.Now()
	}
}

// UpdateActivity updates the last activity timestamp.
func (a *Association) UpdateActivity() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.LastActivity = time.Now()
}

// IsExpired checks if the association has been idle longer than the timeout.
func (a *Association) IsExpired(timeout time.Duration) bool {
	if timeout == 0 {
		return false
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	return time.Since(a.LastActivity) > timeout
}

// IsClosed returns true if the association has been closed.
func (a *Association) IsClosed() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.closed
}

// GetState returns the current state.
func (a *Association) GetState() AssociationState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.State
}

// Context returns the association's context.
func (a *Association) Context() context.Context {
	return a.ctx
}

// Close terminates the association and releases resources.
func (a *Association) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return nil
	}

	a.closed = true
	a.State = StateClosed
	a.cancel()

	// Close UDP socket if present
	if a.UDPConn != nil {
		if err := a.UDPConn.Close(); err != nil {
			return err
		}
		a.UDPConn = nil
	}

	// Zero and clear session key
	if a.SessionKey != nil {
		a.SessionKey.Zero()
		a.SessionKey = nil
	}

	return nil
}

// SetUDPConn sets the UDP connection for this association (exit node).
func (a *Association) SetUDPConn(conn *net.UDPConn) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.UDPConn = conn
	if conn != nil {
		a.RelayAddr = conn.LocalAddr().(*net.UDPAddr)
	}
}

// SetSessionKey sets the E2E encryption session key.
func (a *Association) SetSessionKey(key *crypto.SessionKey) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.SessionKey = key
}

// GetSessionKey returns the E2E encryption session key.
func (a *Association) GetSessionKey() *crypto.SessionKey {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.SessionKey
}

// Encrypt encrypts data using the session key.
// Returns the original data if no session key is set.
//
// The RLock is held for the duration of the crypto operation so that a
// concurrent Close (which zeros the key bytes) cannot race a use of the
// key. This matters when frame dispatch is parallel: a UDP_DATAGRAM and
// a UDP_CLOSE for the same stream can otherwise hit Encrypt/Decrypt and
// Close at the same time.
func (a *Association) Encrypt(plaintext []byte) ([]byte, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.SessionKey == nil {
		return plaintext, nil
	}

	return a.SessionKey.Encrypt(plaintext)
}

// Decrypt decrypts data using the session key.
// Returns the original data if no session key is set.
//
// See Encrypt for the lock-holding rationale.
func (a *Association) Decrypt(ciphertext []byte) ([]byte, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.SessionKey == nil {
		return ciphertext, nil
	}

	return a.SessionKey.Decrypt(ciphertext)
}
