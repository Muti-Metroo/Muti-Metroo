// Package agent provides the relay-table primitive used by the TCP, UDP,
// and ICMP transit handlers. All three protocols only need
// (upstream peer, upstream stream id, downstream peer, downstream stream id)
// to forward frames in either direction, so the relay-table type and the
// indexing logic are shared.
package agent

import (
	"sync"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
)

// relayEntry tracks a stream/association/session being relayed through this
// agent (transit). The same struct shape is used for TCP streams, UDP
// associations, and ICMP sessions; lifecycle management is identical.
//
// Entries are immutable once inserted into a relayTable: callers may safely
// dereference fields after a Lookup* method returns even if a concurrent
// goroutine deletes the entry, because pointer values returned to callers
// outlive the table's index entries.
type relayEntry struct {
	UpstreamPeer   identity.AgentID
	UpstreamID     uint64 // ID space of the upstream peer connection
	DownstreamPeer identity.AgentID
	DownstreamID   uint64 // ID space of the downstream peer connection (allocated locally)
}

// streamKey identifies a stream by its peer as well as its ID. Stream IDs are
// allocated per peer connection, so the ID alone is not a unique identity --
// see "Stream Identification" in Architecture.md. Used by the relay table and
// by the agent-level maps tracking streams this agent terminates or originates
// (file transfers, shell client sessions).
type streamKey struct {
	peer     identity.AgentID
	streamID uint64
}

// relayTable is a thread-safe bidirectional index of relay entries keyed
// by both upstream and downstream (peer, stream ID). It is used by the TCP,
// UDP, and ICMP relay handlers, which all share the same shape.
//
// Invariant: every entry is always indexed under BOTH byUpstream and
// byDownstream. All mutators preserve this; future mutators must too.
// DeleteByPeer relies on it to walk only one map.
type relayTable struct {
	mu           sync.RWMutex
	byUpstream   map[streamKey]*relayEntry
	byDownstream map[streamKey]*relayEntry
}

// newRelayTable returns an empty relay table ready for use.
func newRelayTable() *relayTable {
	return &relayTable{
		byUpstream:   make(map[streamKey]*relayEntry),
		byDownstream: make(map[streamKey]*relayEntry),
	}
}

func upstreamKey(e *relayEntry) streamKey {
	return streamKey{peer: e.UpstreamPeer, streamID: e.UpstreamID}
}

func downstreamKey(e *relayEntry) streamKey {
	return streamKey{peer: e.DownstreamPeer, streamID: e.DownstreamID}
}

// Insert adds an entry under both upstream and downstream keys.
func (r *relayTable) Insert(e *relayEntry) {
	r.mu.Lock()
	r.byUpstream[upstreamKey(e)] = e
	r.byDownstream[downstreamKey(e)] = e
	r.mu.Unlock()
}

// Delete removes the entry from both indices. Idempotent.
func (r *relayTable) Delete(e *relayEntry) {
	r.mu.Lock()
	delete(r.byUpstream, upstreamKey(e))
	delete(r.byDownstream, downstreamKey(e))
	r.mu.Unlock()
}

// LookupBoth returns the entries (if any) for which peer is the upstream and
// the downstream side of streamID respectively. A frame arriving from peer on
// streamID can match at most one of the two, which is what tells the caller
// which way to forward it.
func (r *relayTable) LookupBoth(peer identity.AgentID, streamID uint64) (up, down *relayEntry) {
	key := streamKey{peer: peer, streamID: streamID}
	r.mu.RLock()
	up = r.byUpstream[key]
	down = r.byDownstream[key]
	r.mu.RUnlock()
	return up, down
}

// LookupDownstream returns the entry whose downstream side is (peer, streamID),
// or nil. Used by ACK handlers, where the response always comes back over the
// downstream peer connection.
func (r *relayTable) LookupDownstream(peer identity.AgentID, streamID uint64) *relayEntry {
	r.mu.RLock()
	e := r.byDownstream[streamKey{peer: peer, streamID: streamID}]
	r.mu.RUnlock()
	return e
}

// PopDownstream atomically looks up the entry whose downstream side is
// (peer, streamID) and removes it from both indices. Returns nil if no such
// entry exists. Used by *_OPEN_ERR handlers.
func (r *relayTable) PopDownstream(peer identity.AgentID, streamID uint64) *relayEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.byDownstream[streamKey{peer: peer, streamID: streamID}]
	if e == nil {
		return nil
	}
	delete(r.byUpstream, upstreamKey(e))
	delete(r.byDownstream, downstreamKey(e))
	return e
}

// PopMatchingPeer atomically looks up the entry that (peer, streamID) names,
// removes it from both indices, and returns it. The fromUpstream return flag
// is true when the pair matched on the upstream side (i.e., the close/reset
// originated from the upstream peer), false when it matched on the downstream
// side. Returns (nil, false) if neither side matches.
func (r *relayTable) PopMatchingPeer(peer identity.AgentID, streamID uint64) (entry *relayEntry, fromUpstream bool) {
	key := streamKey{peer: peer, streamID: streamID}
	r.mu.Lock()
	defer r.mu.Unlock()
	if up := r.byUpstream[key]; up != nil {
		delete(r.byUpstream, upstreamKey(up))
		delete(r.byDownstream, downstreamKey(up))
		return up, true
	}
	if down := r.byDownstream[key]; down != nil {
		delete(r.byUpstream, upstreamKey(down))
		delete(r.byDownstream, downstreamKey(down))
		return down, false
	}
	return nil, false
}

// DeleteByPeer removes every entry where either the upstream or downstream
// peer is `peer`. Returns the number of entries removed. Used during peer
// disconnect cleanup.
func (r *relayTable) DeleteByPeer(peer identity.AgentID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int
	for key, e := range r.byUpstream {
		if e.UpstreamPeer == peer || e.DownstreamPeer == peer {
			delete(r.byUpstream, key)
			delete(r.byDownstream, downstreamKey(e))
			n++
		}
	}
	return n
}
