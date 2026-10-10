package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// RouteWatchProvider backs the CmdRouteWatch (0x05) and CmdSnapshot (0x06)
// SOCKS5 commands. It lets a Mutiauk client receive the agent's dashboard
// state over the same endpoint it uses for the data plane, replacing HTTP
// polling of GET /api/dashboard with a server-pushed stream.
type RouteWatchProvider interface {
	// Snapshot returns the current dashboard state as JSON. The bytes are
	// byte-identical to the body of GET /api/dashboard, so the client reuses
	// its existing DashboardResponse parser.
	Snapshot() ([]byte, error)

	// Subscribe returns a coalesced change-signal channel and an unsubscribe
	// func. The channel receives a value whenever routes, mesh hosts, or
	// suffixes change. It is signal-only: the handler reacts by calling
	// Snapshot(), so deltas never cross this boundary. The caller must invoke
	// the returned func when done.
	Subscribe() (<-chan struct{}, func())
}

// Route-watch frame types (server -> client). Each frame is
// [type:1][length:u32 BE][payload:length].
const (
	routeWatchHeartbeat = 0x00 // length 0, no payload
	routeWatchSnapshot  = 0x01 // payload is DashboardResponse JSON
)

const (
	// routeWatchCoalesceWindow bounds how long a burst of change signals is
	// collapsed before a fresh snapshot is sent.
	routeWatchCoalesceWindow = 250 * time.Millisecond
	// maxRouteWatchFrame caps a single frame payload. The client enforces the
	// same bound; the server refuses to emit anything larger.
	maxRouteWatchFrame = 8 << 20 // 8 MiB
)

// routeWatchHeartbeatInterval is how often a heartbeat frame is sent when idle,
// so the client can detect a dead connection. It is a var so tests can lower
// it.
var routeWatchHeartbeatInterval = 20 * time.Second

// ErrRouteWatchDisabled is returned when a route-watch command arrives but no
// provider is configured.
var ErrRouteWatchDisabled = errors.New("route watch not enabled")

// writeRouteWatchFrame writes a single length-prefixed frame. Header and
// payload are sent as one Write so a partial frame cannot interleave.
func writeRouteWatchFrame(conn net.Conn, frameType byte, payload []byte) error {
	buf := make([]byte, 5+len(payload))
	buf[0] = frameType
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	_, err := conn.Write(buf)
	return err
}

// handleRouteWatch handles CmdRouteWatch (0x05): a long-lived stream that
// pushes the full dashboard snapshot on every relevant change, debounced,
// interleaved with periodic heartbeats. The client never sends frames after
// the request, so any read result is treated as a disconnect.
func (h *Handler) handleRouteWatch(conn net.Conn, req *Request) error {
	if h.routeWatch == nil {
		h.sendReply(conn, ReplyCmdNotSupported, nil, 0)
		return ErrRouteWatchDisabled
	}

	snap, err := h.routeWatch.Snapshot()
	if err != nil {
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("route watch snapshot: %w", err)
	}
	if len(snap) > maxRouteWatchFrame {
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("route watch snapshot too large: %d bytes", len(snap))
	}

	if err := h.sendReply(conn, ReplySucceeded, nil, 0); err != nil {
		return err
	}
	conn.SetDeadline(time.Time{})

	if err := writeRouteWatchFrame(conn, routeWatchSnapshot, snap); err != nil {
		return err
	}

	sig, cancel := h.routeWatch.Subscribe()
	defer cancel()

	// Detect client disconnect: clients send no frames after the request, so
	// any read result (data or error) means we should stop. The read unblocks
	// when the caller closes the connection after this method returns.
	closed := make(chan struct{})
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(closed)
				return
			}
		}
	}()

	// Coalescing timer: starts on the first signal of a burst, then a single
	// snapshot is sent when it fires. Last-write-wins -- only ever the latest
	// snapshot, never a queue.
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	pending := false

	heartbeat := time.NewTicker(routeWatchHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-closed:
			return nil
		case <-sig:
			if !pending {
				pending = true
				timer.Reset(routeWatchCoalesceWindow)
			}
		case <-timer.C:
			pending = false
			snap, err := h.routeWatch.Snapshot()
			if err != nil {
				continue
			}
			if len(snap) > maxRouteWatchFrame {
				continue
			}
			if err := writeRouteWatchFrame(conn, routeWatchSnapshot, snap); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := writeRouteWatchFrame(conn, routeWatchHeartbeat, nil); err != nil {
				return err
			}
		}
	}
}

// handleSnapshot handles CmdSnapshot (0x06): a one-shot reply carrying a single
// dashboard snapshot frame, after which the connection closes. Used by the
// client's route-trace path.
func (h *Handler) handleSnapshot(conn net.Conn, req *Request) error {
	if h.routeWatch == nil {
		h.sendReply(conn, ReplyCmdNotSupported, nil, 0)
		return ErrRouteWatchDisabled
	}

	snap, err := h.routeWatch.Snapshot()
	if err != nil {
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("route watch snapshot: %w", err)
	}
	if len(snap) > maxRouteWatchFrame {
		h.sendReply(conn, ReplyServerFailure, nil, 0)
		return fmt.Errorf("route watch snapshot too large: %d bytes", len(snap))
	}

	if err := h.sendReply(conn, ReplySucceeded, nil, 0); err != nil {
		return err
	}
	conn.SetDeadline(time.Time{})
	return writeRouteWatchFrame(conn, routeWatchSnapshot, snap)
}
