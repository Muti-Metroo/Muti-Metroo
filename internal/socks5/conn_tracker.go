package socks5

import (
	"io"
	"sync"
	"sync/atomic"
)

// connCloser combines io.Closer with comparable for map key usage.
type connCloser interface {
	comparable
	io.Closer
}

// connTracker manages active connections with thread-safe tracking and counting.
// It provides a reusable component for both TCP and WebSocket listeners.
type connTracker[T connCloser] struct {
	mu          sync.Mutex
	connections map[T]struct{}
	connCount   atomic.Int64
}

// newConnTracker creates a new connection tracker.
func newConnTracker[T connCloser]() *connTracker[T] {
	return &connTracker[T]{
		connections: make(map[T]struct{}),
	}
}

// add registers a connection for tracking.
func (t *connTracker[T]) add(conn T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connections[conn] = struct{}{}
	t.connCount.Add(1)
}

// remove unregisters a connection from tracking.
// Safe to call multiple times for the same connection.
func (t *connTracker[T]) remove(conn T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.connections[conn]; exists {
		delete(t.connections, conn)
		t.connCount.Add(-1)
	}
}

// count returns the number of active connections.
func (t *connTracker[T]) count() int64 {
	return t.connCount.Load()
}

// closeAll closes all tracked connections and resets the tracker state.
func (t *connTracker[T]) closeAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for conn := range t.connections {
		conn.Close()
	}
	// Clear the map and reset counter to prevent stale references
	// and counter inconsistency if remove() is called after closeAll()
	t.connections = make(map[T]struct{})
	t.connCount.Store(0)
}
