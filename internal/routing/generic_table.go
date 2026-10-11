// Package routing: generic routing table skeleton.
//
// keyedTable implements the AddRoute / RemoveRoute / RemoveRoutesFromPeer /
// CleanupStaleRoutes / Clone-on-read skeleton shared by the four route
// tables (CIDR, agent presence, domain, forward). The entry-type-specific
// parts (key derivation, field access, cloning) are injected via entryOps;
// behavioral differences are the two knobs on keyedTable:
//
//   - matchNextHop: agent presence routes key on (origin, next hop) pairs --
//     an agent can be reachable via several next hops -- while the other
//     tables hold one entry per origin.
//   - removeAllOnSeqWithdraw: agent presence RemoveRouteWithSeq removes every
//     entry from the origin that passes the sequence guard; the other tables
//     remove only the first matching entry.
//
// Per-entry refresh of non-routing metadata on a same-sequence,
// same-metric re-advertisement (the CIDR operator comment) is the
// refreshInPlace hook; nil elsewhere.
package routing

import (
	"sort"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// entryOps injects entry-type-specific field access into keyedTable.
// clone must deep-copy; all reads return clones so callers can mutate freely.
type entryOps[E any] struct {
	clone         func(E) E
	origin        func(E) identity.AgentID
	nextHop       func(E) identity.AgentID
	metric        func(E) uint16
	sequence      func(E) uint64
	path          func(E) []identity.AgentID
	lastUpdate    func(E) time.Time
	setLastUpdate func(E, time.Time)

	// refreshInPlace, when non-nil, is called on a same-sequence,
	// same-metric re-advertisement from an already-known origin to update
	// non-routing metadata (the CIDR operator comment) on the stored entry.
	// Runs under the table lock.
	refreshInPlace func(stored, incoming E)
}

// keyedTable is a thread-safe routing table mapping keys to entry lists.
// The per-key list is sorted best-first (see sortRoutes).
type keyedTable[K comparable, E any] struct {
	mu     sync.RWMutex
	routes map[K][]E

	// localID is this agent's ID (for loop detection and stale cleanup)
	localID identity.AgentID

	ops entryOps[E]

	// matchNextHop makes add treat (origin, next hop) as the identity of an
	// entry (agent presence table) instead of origin alone.
	matchNextHop bool

	// removeAllOnSeqWithdraw makes removeWithSeq remove every entry from the
	// origin passing the sequence guard (agent presence table) instead of
	// stopping at the first.
	removeAllOnSeqWithdraw bool
}

func newKeyedTable[K comparable, E any](localID identity.AgentID, ops entryOps[E], matchNextHop, removeAllOnSeqWithdraw bool) *keyedTable[K, E] {
	return &keyedTable[K, E]{
		routes:                 make(map[K][]E),
		localID:                localID,
		ops:                    ops,
		matchNextHop:           matchNextHop,
		removeAllOnSeqWithdraw: removeAllOnSeqWithdraw,
	}
}

// add inserts or updates the entry under key. Returns true if the entry was
// added/updated, false if rejected (loop detected, or older/worse than the
// stored entry). The loop check rejects any entry whose path already
// contains this agent.
func (t *keyedTable[K, E]) add(key K, entry E) bool {
	// Check for routing loops (is our ID in the path?)
	for _, id := range t.ops.path(entry) {
		if id == t.localID {
			return false // Loop detected
		}
	}

	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	existing := t.routes[key]
	for i, r := range existing {
		if t.ops.origin(r) != t.ops.origin(entry) {
			continue
		}
		if t.matchNextHop && t.ops.nextHop(r) != t.ops.nextHop(entry) {
			continue
		}
		// Update if newer sequence or better metric
		if t.ops.sequence(entry) > t.ops.sequence(r) ||
			(t.ops.sequence(entry) == t.ops.sequence(r) && t.ops.metric(entry) < t.ops.metric(r)) {
			cloned := t.ops.clone(entry)
			t.ops.setLastUpdate(cloned, now)
			t.routes[key][i] = cloned
			t.sortRoutes(key)
			return true
		}
		// Same-sequence same-metric refresh of non-routing metadata (e.g.
		// the CIDR operator comment). Mutating in place is safe because we
		// hold the lock and readers clone.
		if t.ops.refreshInPlace != nil {
			t.ops.refreshInPlace(r, entry)
		}
		return false // Older/worse route (or metadata-only refresh)
	}

	// New entry
	cloned := t.ops.clone(entry)
	t.ops.setLastUpdate(cloned, now)
	t.routes[key] = append(t.routes[key], cloned)
	t.sortRoutes(key)
	return true
}

// sortRoutes sorts entries for a key by metric (lowest first), breaking ties
// on origin agent ID (and, for tables keyed on origin+next hop, on next hop)
// so equal-metric entries resolve deterministically regardless of Go map
// iteration order.
func (t *keyedTable[K, E]) sortRoutes(key K) {
	routes := t.routes[key]
	sort.Slice(routes, func(i, j int) bool {
		if t.ops.metric(routes[i]) != t.ops.metric(routes[j]) {
			return t.ops.metric(routes[i]) < t.ops.metric(routes[j])
		}
		if cmp := t.ops.origin(routes[i]).Compare(t.ops.origin(routes[j])); cmp != 0 {
			return cmp < 0
		}
		if t.matchNextHop {
			return t.ops.nextHop(routes[i]).Compare(t.ops.nextHop(routes[j])) < 0
		}
		return false
	})
}

// remove deletes the first entry under key belonging to originAgent.
func (t *keyedTable[K, E]) remove(key K, originAgent identity.AgentID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	routes := t.routes[key]
	for i, r := range routes {
		if t.ops.origin(r) == originAgent {
			t.routes[key] = append(routes[:i], routes[i+1:]...)
			if len(t.routes[key]) == 0 {
				delete(t.routes, key)
			}
			return true
		}
	}
	return false
}

// removeWithSeq deletes entries under key belonging to originAgent, but only
// when the installed entry's sequence is not newer than seq. A
// ROUTE_WITHDRAW traveling a slow path can arrive after a newer re-advertise
// from the same origin; without this guard the stale withdraw would delete
// the fresh route. With removeAllOnSeqWithdraw, all entries passing the
// guard are removed; otherwise removal stops at the first (whose failed
// guard also fails the removal).
func (t *keyedTable[K, E]) removeWithSeq(key K, originAgent identity.AgentID, seq uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.removeAllOnSeqWithdraw {
		routes := t.routes[key]
		for i, r := range routes {
			if t.ops.origin(r) == originAgent {
				if t.ops.sequence(r) > seq {
					return false // Stale withdraw; installed route is newer
				}
				t.routes[key] = append(routes[:i], routes[i+1:]...)
				if len(t.routes[key]) == 0 {
					delete(t.routes, key)
				}
				return true
			}
		}
		return false
	}

	routes := t.routes[key]
	removed := false
	filtered := routes[:0]
	for _, r := range routes {
		if t.ops.origin(r) == originAgent && t.ops.sequence(r) <= seq {
			removed = true
			continue
		}
		filtered = append(filtered, r)
	}
	if !removed {
		return false
	}
	if len(filtered) == 0 {
		delete(t.routes, key)
	} else {
		t.routes[key] = filtered
	}
	return true
}

// removeFromPeer removes all entries learned from a specific peer.
// Returns the removed (stored, uncloned) entries so the caller can propagate
// withdrawals; the table no longer references them.
func (t *keyedTable[K, E]) removeFromPeer(peerID identity.AgentID) []E {
	t.mu.Lock()
	defer t.mu.Unlock()

	var removed []E
	for key, routes := range t.routes {
		filtered := routes[:0]
		for _, r := range routes {
			if t.ops.nextHop(r) != peerID {
				filtered = append(filtered, r)
			} else {
				removed = append(removed, r)
			}
		}
		if len(filtered) == 0 {
			delete(t.routes, key)
		} else {
			t.routes[key] = filtered
		}
	}
	return removed
}

// first returns a clone of the best entry under key, if any.
func (t *keyedTable[K, E]) first(key K) (E, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var zero E
	routes := t.routes[key]
	if len(routes) == 0 {
		return zero, false
	}
	return t.ops.clone(routes[0]), true
}

// allForKey returns clones of all entries under key, best first.
func (t *keyedTable[K, E]) allForKey(key K) []E {
	t.mu.RLock()
	defer t.mu.RUnlock()

	routes := t.routes[key]
	result := make([]E, len(routes))
	for i, r := range routes {
		result[i] = t.ops.clone(r)
	}
	return result
}

// all returns clones of every entry in the table.
func (t *keyedTable[K, E]) all() []E {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []E
	for _, routes := range t.routes {
		for _, r := range routes {
			result = append(result, t.ops.clone(r))
		}
	}
	return result
}

// allFromOrigin returns clones of every entry originating from agentID.
func (t *keyedTable[K, E]) allFromOrigin(agentID identity.AgentID) []E {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []E
	for _, routes := range t.routes {
		for _, r := range routes {
			if t.ops.origin(r) == agentID {
				result = append(result, t.ops.clone(r))
			}
		}
	}
	return result
}

// has reports whether an entry under key belongs to originAgent.
func (t *keyedTable[K, E]) has(key K, originAgent identity.AgentID) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, r := range t.routes[key] {
		if t.ops.origin(r) == originAgent {
			return true
		}
	}
	return false
}

// size returns the number of keys with at least one entry.
func (t *keyedTable[K, E]) size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}

// total returns the total number of entries across all keys.
func (t *keyedTable[K, E]) total() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, routes := range t.routes {
		count += len(routes)
	}
	return count
}

// clear removes all entries.
func (t *keyedTable[K, E]) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes = make(map[K][]E)
}

// keys returns all keys with at least one entry.
func (t *keyedTable[K, E]) keys() []K {
	t.mu.RLock()
	defer t.mu.RUnlock()

	keys := make([]K, 0, len(t.routes))
	for k := range t.routes {
		keys = append(keys, k)
	}
	return keys
}

// cleanupStale removes entries that haven't been updated within maxAge.
// Local entries (origin == localID) are never removed.
// Returns the number of entries removed.
func (t *keyedTable[K, E]) cleanupStale(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, routes := range t.routes {
		var kept []E
		for _, r := range routes {
			// Never remove local routes
			if t.ops.origin(r) == t.localID {
				kept = append(kept, r)
				continue
			}

			// Keep entries that are still fresh
			if now.Sub(t.ops.lastUpdate(r)) <= maxAge {
				kept = append(kept, r)
			} else {
				removed++
			}
		}

		if len(kept) > 0 {
			t.routes[key] = kept
		} else {
			delete(t.routes, key)
		}
	}

	return removed
}

// updateInPlace mutates the first stored entry under key matching match, in
// place, under the table lock (the CIDR comment refresh). Readers clone, so
// in-place mutation under the write lock cannot race them.
func (t *keyedTable[K, E]) updateInPlace(key K, match func(E) bool, update func(E)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.routes[key] {
		if match(r) {
			update(r)
			return
		}
	}
}

// lookupBest iterates the best (first) entry of every key, keeps those
// satisfying match, and returns a clone of the winner according to
// better(candidate, current). Used for longest-prefix match in the CIDR
// table.
func (t *keyedTable[K, E]) lookupBest(match func(E) bool, better func(candidate, current E) bool) (E, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var best E
	found := false
	for _, routes := range t.routes {
		if len(routes) == 0 {
			continue
		}
		first := routes[0]
		if !match(first) {
			continue
		}
		if !found || better(first, best) {
			best = first
			found = true
		}
	}
	if !found {
		var zero E
		return zero, false
	}
	return t.ops.clone(best), true
}

// collectFirsts returns clones of the best (first) entry of every key whose
// entry satisfies match. Used by CIDR LookupAll.
func (t *keyedTable[K, E]) collectFirsts(match func(E) bool) []E {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []E
	for _, routes := range t.routes {
		if len(routes) == 0 {
			continue
		}
		if match(routes[0]) {
			result = append(result, t.ops.clone(routes[0]))
		}
	}
	return result
}
