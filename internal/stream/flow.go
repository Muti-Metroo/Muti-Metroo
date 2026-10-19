package stream

import (
	"errors"
	"sync"

	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// ErrWindowClosed is returned by Window.Acquire when the stream ended while
// the sender was waiting for window.
var ErrWindowClosed = errors.New("stream closed while waiting for flow-control window")

// Window is the send side of a stream's flow control: how many more data
// frames the sender may send before the receiver returns window. Senders call
// Acquire before every data frame with a payload and block at zero, which in
// turn stops them reading their source, so a slow receiver slows the whole
// path instead of overflowing a buffer.
type Window struct {
	mu      sync.Mutex
	avail   int
	closed  bool
	changed chan struct{} // closed and replaced on every Grant and on Close
}

// NewWindow returns a window that allows initial frames.
func NewWindow(initial int) *Window {
	return &Window{avail: initial, changed: make(chan struct{})}
}

// Acquire takes one frame from the window, waiting until one is available.
// It returns ErrWindowClosed when the window is closed or done is closed.
func (w *Window) Acquire(done <-chan struct{}) error {
	for {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return ErrWindowClosed
		}
		if w.avail > 0 {
			w.avail--
			w.mu.Unlock()
			return nil
		}
		changed := w.changed
		w.mu.Unlock()

		select {
		case <-changed:
		case <-done:
			return ErrWindowClosed
		}
	}
}

// Grant adds n frames to the window and wakes waiting senders.
func (w *Window) Grant(n int) {
	if n <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.avail += n
	close(w.changed)
	w.changed = make(chan struct{})
}

// Close fails current and future Acquire calls.
func (w *Window) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	close(w.changed)
}

// Credit collects the frames a receiver has consumed and decides when to
// return them to the sender in a STREAM_WINDOW frame. Batching keeps the
// update traffic to one frame per StreamWindowUpdateThreshold data frames.
type Credit struct {
	mu      sync.Mutex
	pending int
}

// Consumed records n consumed frames and returns how many to grant back now:
// zero until StreamWindowUpdateThreshold frames have accumulated, then all of
// them.
func (c *Credit) Consumed(n int) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending += n
	if c.pending < protocol.StreamWindowUpdateThreshold {
		return 0
	}
	grant := c.pending
	c.pending = 0
	return uint32(grant)
}
