// Package routing implements port forward routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
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

	// Path is the AS-path style list of agent IDs
	Path []identity.AgentID

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
	return clone
}

// forwardRouteEntryOps adapts *ForwardRoute to the generic keyedTable skeleton.
var forwardRouteEntryOps = entryOps[*ForwardRoute]{
	clone:         func(r *ForwardRoute) *ForwardRoute { return r.Clone() },
	origin:        func(r *ForwardRoute) identity.AgentID { return r.OriginAgent },
	nextHop:       func(r *ForwardRoute) identity.AgentID { return r.NextHop },
	metric:        func(r *ForwardRoute) uint16 { return r.Metric },
	sequence:      func(r *ForwardRoute) uint64 { return r.Sequence },
	path:          func(r *ForwardRoute) []identity.AgentID { return r.Path },
	lastUpdate:    func(r *ForwardRoute) time.Time { return r.LastUpdate },
	setLastUpdate: func(r *ForwardRoute, t time.Time) { r.LastUpdate = t },
}

// ForwardTable is a thread-safe port forward routing table.
// The storage skeleton is the generic keyedTable (see generic_table.go).
type ForwardTable struct {
	entries *keyedTable[string, *ForwardRoute]
}

// NewForwardTable creates a new port forward routing table.
func NewForwardTable(localID identity.AgentID) *ForwardTable {
	return &ForwardTable{
		entries: newKeyedTable[string](localID, forwardRouteEntryOps, false, false),
	}
}

// AddRoute adds or updates a port forward route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *ForwardTable) AddRoute(route *ForwardRoute) bool {
	if route == nil || route.Key == "" {
		return false
	}
	return t.entries.add(route.Key, route)
}

// RemoveRoute removes a port forward route from a specific origin.
func (t *ForwardTable) RemoveRoute(key string, originAgent identity.AgentID) bool {
	if key == "" {
		return false
	}
	return t.entries.remove(key, originAgent)
}

// RemoveRouteWithSeq removes a port forward route from a specific origin, but
// only when the installed route's sequence is not newer than seq. Guards
// against a stale ROUTE_WITHDRAW deleting a fresher re-advertised route.
func (t *ForwardTable) RemoveRouteWithSeq(key string, originAgent identity.AgentID, seq uint64) bool {
	if key == "" {
		return false
	}
	return t.entries.removeWithSeq(key, originAgent, seq)
}

// RemoveRoutesFromPeer removes all port forward routes learned from a specific peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (t *ForwardTable) RemoveRoutesFromPeer(peerID identity.AgentID) []*ForwardRoute {
	return t.entries.removeFromPeer(peerID)
}

// Lookup finds the best port forward route for a routing key.
func (t *ForwardTable) Lookup(key string) *ForwardRoute {
	route, ok := t.entries.first(key)
	if !ok {
		return nil
	}
	return route
}

// GetAllRoutes returns all port forward routes in the table.
func (t *ForwardTable) GetAllRoutes() []*ForwardRoute {
	return t.entries.all()
}

// GetRoutesFromAgent returns all port forward routes originating from a specific agent.
func (t *ForwardTable) GetRoutesFromAgent(agentID identity.AgentID) []*ForwardRoute {
	return t.entries.allFromOrigin(agentID)
}

// Size returns the number of unique port forward keys in the table.
func (t *ForwardTable) Size() int {
	return t.entries.size()
}

// TotalRoutes returns the total number of port forward route entries.
func (t *ForwardTable) TotalRoutes() int {
	return t.entries.total()
}

// Clear removes all port forward routes from the table.
func (t *ForwardTable) Clear() {
	t.entries.clear()
}

// HasRoute checks if a port forward route exists for the given key and origin.
func (t *ForwardTable) HasRoute(key string, originAgent identity.AgentID) bool {
	if key == "" {
		return false
	}
	return t.entries.has(key, originAgent)
}

// ConflictingOriginsForKey returns origin agent IDs that already advertise
// the given forward key but are different from candidateOrigin.
func (t *ForwardTable) ConflictingOriginsForKey(key string, candidateOrigin identity.AgentID) []identity.AgentID {
	if key == "" {
		return nil
	}
	var conflicts []identity.AgentID
	for _, r := range t.entries.allForKey(key) {
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
	return t.entries.cleanupStale(maxAge)
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
