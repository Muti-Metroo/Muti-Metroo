// Package routing implements port forward routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// ForwardRoute represents a single port forward route entry.
type ForwardRoute struct {
	// Key is the routing key that identifies this port forward (e.g., "my-web-server")
	Key string

	// Target is the fixed destination host:port for forwarded connections.
	// This is only set on the origin agent; transit nodes don't know the target.
	Target string

	// NextHop is the peer ID to forward traffic to
	NextHop identity.AgentID

	// OriginAgent is the original advertiser of this route
	OriginAgent identity.AgentID

	// Metric is the route cost (lower is better)
	Metric uint16

	// Path is the AS-path style list of agent IDs (nil if encrypted and can't decrypt)
	Path []identity.AgentID

	// EncPath is the encrypted path data for forwarding (nil if no encryption)
	EncPath *protocol.EncryptedData

	// Sequence is used for route versioning
	Sequence uint64

	// LastUpdate is when this route was last added or refreshed
	LastUpdate time.Time
}

// String returns a human-readable representation of the port forward route.
func (r *ForwardRoute) String() string {
	return fmt.Sprintf("ForwardRoute{%s via %s, metric=%d, origin=%s}",
		r.Key, r.NextHop.ShortString(), r.Metric, r.OriginAgent.ShortString())
}

// Clone creates a deep copy of the port forward route.
func (r *ForwardRoute) Clone() *ForwardRoute {
	clone := &ForwardRoute{
		Key:         r.Key,
		Target:      r.Target,
		NextHop:     r.NextHop,
		OriginAgent: r.OriginAgent,
		Metric:      r.Metric,
		Sequence:    r.Sequence,
		LastUpdate:  r.LastUpdate,
	}
	if len(r.Path) > 0 {
		clone.Path = make([]identity.AgentID, len(r.Path))
		copy(clone.Path, r.Path)
	}
	if r.EncPath != nil {
		clone.EncPath = &protocol.EncryptedData{
			Encrypted: r.EncPath.Encrypted,
			Data:      make([]byte, len(r.EncPath.Data)),
		}
		copy(clone.EncPath.Data, r.EncPath.Data)
	}
	return clone
}

// ForwardTable is a thread-safe port forward routing table.
type ForwardTable struct {
	mu sync.RWMutex

	// routes maps forward key to route entries
	// Key: forward routing key (e.g., "my-web-server")
	routes map[string][]*ForwardRoute

	// localID is this agent's ID (for loop detection)
	localID identity.AgentID
}

// NewForwardTable creates a new port forward routing table.
func NewForwardTable(localID identity.AgentID) *ForwardTable {
	return &ForwardTable{
		routes:  make(map[string][]*ForwardRoute),
		localID: localID,
	}
}

// AddRoute adds or updates a port forward route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *ForwardTable) AddRoute(route *ForwardRoute) bool {
	if route == nil || route.Key == "" {
		return false
	}

	// Check for routing loops (is our ID in the path?)
	for _, id := range route.Path {
		if id == t.localID {
			return false // Loop detected
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	key := route.Key

	// Check if we already have a route from this origin
	for i, r := range t.routes[key] {
		if r.OriginAgent == route.OriginAgent {
			// Update if newer sequence or better metric
			if route.Sequence > r.Sequence ||
				(route.Sequence == r.Sequence && route.Metric < r.Metric) {
				cloned := route.Clone()
				cloned.LastUpdate = time.Now()
				t.routes[key][i] = cloned
				t.sortRoutes(key)
				return true
			}
			return false // Older/worse route
		}
	}

	// New route from this origin
	cloned := route.Clone()
	cloned.LastUpdate = time.Now()
	t.routes[key] = append(t.routes[key], cloned)
	t.sortRoutes(key)
	return true
}

// sortRoutes sorts routes for a key by metric (lowest first).
func (t *ForwardTable) sortRoutes(key string) {
	routes := t.routes[key]
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Metric < routes[j].Metric
	})
}

// RemoveRoute removes a port forward route from a specific origin.
func (t *ForwardTable) RemoveRoute(key string, originAgent identity.AgentID) bool {
	return t.removeRoute(key, originAgent, 0, false)
}

// RemoveRouteWithSeq removes a port forward route from a specific origin, but
// only when the installed route's sequence is not newer than seq. Guards
// against a stale ROUTE_WITHDRAW deleting a fresher re-advertised route.
func (t *ForwardTable) RemoveRouteWithSeq(key string, originAgent identity.AgentID, seq uint64) bool {
	return t.removeRoute(key, originAgent, seq, true)
}

func (t *ForwardTable) removeRoute(key string, originAgent identity.AgentID, seq uint64, checkSeq bool) bool {
	if key == "" {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	routes := t.routes[key]
	for i, r := range routes {
		if r.OriginAgent == originAgent {
			if checkSeq && r.Sequence > seq {
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

// RemoveRoutesFromPeer removes all port forward routes learned from a specific peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (t *ForwardTable) RemoveRoutesFromPeer(peerID identity.AgentID) []*ForwardRoute {
	t.mu.Lock()
	defer t.mu.Unlock()

	var removed []*ForwardRoute
	for key, routes := range t.routes {
		filtered := routes[:0]
		for _, r := range routes {
			if r.NextHop != peerID {
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

// Lookup finds the best port forward route for a routing key.
func (t *ForwardTable) Lookup(key string) *ForwardRoute {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if routes, ok := t.routes[key]; ok && len(routes) > 0 {
		return routes[0].Clone() // First is best due to sorting by metric
	}
	return nil
}

// GetAllRoutes returns all port forward routes in the table.
func (t *ForwardTable) GetAllRoutes() []*ForwardRoute {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var all []*ForwardRoute
	for _, routes := range t.routes {
		for _, r := range routes {
			all = append(all, r.Clone())
		}
	}
	return all
}

// GetRoutesFromAgent returns all port forward routes originating from a specific agent.
func (t *ForwardTable) GetRoutesFromAgent(agentID identity.AgentID) []*ForwardRoute {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var matching []*ForwardRoute
	for _, routes := range t.routes {
		for _, r := range routes {
			if r.OriginAgent == agentID {
				matching = append(matching, r.Clone())
			}
		}
	}
	return matching
}

// Size returns the number of unique port forward keys in the table.
func (t *ForwardTable) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}

// TotalRoutes returns the total number of port forward route entries.
func (t *ForwardTable) TotalRoutes() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, routes := range t.routes {
		count += len(routes)
	}
	return count
}

// Clear removes all port forward routes from the table.
func (t *ForwardTable) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes = make(map[string][]*ForwardRoute)
}

// HasRoute checks if a port forward route exists for the given key and origin.
func (t *ForwardTable) HasRoute(key string, originAgent identity.AgentID) bool {
	if key == "" {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, r := range t.routes[key] {
		if r.OriginAgent == originAgent {
			return true
		}
	}
	return false
}

// ConflictingOriginsForKey returns origin agent IDs that already advertise
// the given forward key but are different from candidateOrigin.
func (t *ForwardTable) ConflictingOriginsForKey(key string, candidateOrigin identity.AgentID) []identity.AgentID {
	if key == "" {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	var conflicts []identity.AgentID
	for _, r := range t.routes[key] {
		if r.OriginAgent != candidateOrigin {
			conflicts = append(conflicts, r.OriginAgent)
		}
	}
	return conflicts
}

// CleanupStaleRoutes removes port forward routes that haven't been updated within maxAge.
// Local routes (where OriginAgent == localID) are never removed.
// Returns the number of routes removed.
func (t *ForwardTable) CleanupStaleRoutes(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, routes := range t.routes {
		var kept []*ForwardRoute
		for _, r := range routes {
			if r.OriginAgent == t.localID || now.Sub(r.LastUpdate) <= maxAge {
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

// LocalForwardRoute represents a locally-announced port forward route.
type LocalForwardRoute struct {
	Key    string // Routing key
	Target string // Fixed target host:port
	Metric uint16
}

// ForwardRouteChange represents a port forward route update.
type ForwardRouteChange struct {
	Type  RouteChangeType
	Route *ForwardRoute
}
