package agent

import (
	"testing"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// Stream IDs are allocated per peer connection, so two upstream peers relaying
// through this agent both open stream 1. Keying the index on the numeric ID
// alone lets the second insert evict the first: that peer's stream goes dead,
// the evicted entry survives in only one index, and DeleteByPeer -- which
// walks a single map -- can never reclaim it.
func TestRelayTable_UpstreamIDCollisionAcrossPeers(t *testing.T) {
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()
	nextHop, _ := identity.NewAgentID()

	rt := newRelayTable()
	entryA := &relayEntry{UpstreamPeer: peerA, UpstreamID: 1, DownstreamPeer: nextHop, DownstreamID: 100}
	entryB := &relayEntry{UpstreamPeer: peerB, UpstreamID: 1, DownstreamPeer: nextHop, DownstreamID: 102}
	rt.Insert(entryA)
	rt.Insert(entryB)

	// Each peer's stream 1 must still resolve to that peer's own entry.
	if up, _ := rt.LookupBoth(peerA, 1); up != entryA {
		t.Errorf("peer A stream 1 resolved to %v, want peer A's entry", up)
	}
	if up, _ := rt.LookupBoth(peerB, 1); up != entryB {
		t.Errorf("peer B stream 1 resolved to %v, want peer B's entry", up)
	}

	// A peer that holds neither side of the stream must not match at all.
	if up, down := rt.LookupBoth(nextHop, 1); up != nil || down != nil {
		t.Errorf("unrelated peer matched stream 1: up=%v down=%v", up, down)
	}

	// Removing one peer must leave the other peer's entry intact and fully
	// indexed, and must not strand anything behind.
	if n := rt.DeleteByPeer(peerB); n != 1 {
		t.Errorf("DeleteByPeer(B) removed %d entries, want 1", n)
	}
	if up, _ := rt.LookupBoth(peerA, 1); up != entryA {
		t.Error("peer A's entry lost when peer B was cleaned up")
	}
	if e := rt.LookupDownstream(nextHop, 100); e != entryA {
		t.Error("peer A's downstream index entry lost when peer B was cleaned up")
	}

	if n := rt.DeleteByPeer(peerA); n != 1 {
		t.Errorf("DeleteByPeer(A) removed %d entries, want 1", n)
	}
	if len(rt.byUpstream) != 0 || len(rt.byDownstream) != 0 {
		t.Errorf("entries leaked: byUpstream=%d byDownstream=%d, want 0 and 0",
			len(rt.byUpstream), len(rt.byDownstream))
	}
}

// A close or reset naming a stream ID must only affect the sending peer's own
// stream. Without peer-scoped keys, peer B could tear down peer A's stream by
// quoting a small, entirely guessable ID.
func TestRelayTable_PopMatchingPeerIsolatesPeers(t *testing.T) {
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()
	nextHop, _ := identity.NewAgentID()

	rt := newRelayTable()
	entryA := &relayEntry{UpstreamPeer: peerA, UpstreamID: 1, DownstreamPeer: nextHop, DownstreamID: 100}
	rt.Insert(entryA)

	// Peer B has no stream 1 here and must not be able to pop peer A's.
	if entry, _ := rt.PopMatchingPeer(peerB, 1); entry != nil {
		t.Error("peer B popped a relay entry belonging to peer A")
	}
	if up, _ := rt.LookupBoth(peerA, 1); up != entryA {
		t.Fatal("peer A's entry disappeared after peer B's close attempt")
	}

	// The owning peer still pops it, and the direction flag is upstream.
	entry, fromUpstream := rt.PopMatchingPeer(peerA, 1)
	if entry != entryA {
		t.Fatalf("peer A could not pop its own entry: got %v", entry)
	}
	if !fromUpstream {
		t.Error("fromUpstream = false, want true for an upstream-side match")
	}
	if len(rt.byUpstream) != 0 || len(rt.byDownstream) != 0 {
		t.Errorf("entry not removed from both indices: byUpstream=%d byDownstream=%d",
			len(rt.byUpstream), len(rt.byDownstream))
	}
}

// The downstream side is matched by (peer, ID) too, so an OPEN_ERR from an
// unrelated peer cannot cancel a relay it does not own.
func TestRelayTable_PopDownstreamIsolatesPeers(t *testing.T) {
	upstream, _ := identity.NewAgentID()
	nextHop, _ := identity.NewAgentID()
	stranger, _ := identity.NewAgentID()

	rt := newRelayTable()
	entry := &relayEntry{UpstreamPeer: upstream, UpstreamID: 7, DownstreamPeer: nextHop, DownstreamID: 100}
	rt.Insert(entry)

	if got := rt.PopDownstream(stranger, 100); got != nil {
		t.Error("an unrelated peer popped the downstream entry")
	}
	if got := rt.PopDownstream(nextHop, 100); got != entry {
		t.Fatalf("downstream peer could not pop its own entry: got %v", got)
	}
	if len(rt.byUpstream) != 0 || len(rt.byDownstream) != 0 {
		t.Error("entry not removed from both indices")
	}
}
