// Package routing implements agent presence routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// AgentRoute represents a single agent presence route entry.
type AgentRoute struct {
	// AgentID is the target agent this route reaches
	AgentID identity.AgentID

	// NextHop is the peer ID to forward traffic to
	NextHop identity.AgentID

	// OriginAgent is the original advertiser of this route (== AgentID)
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

// String returns a human-readable representation of the agent route.
func (r *AgentRoute) String() string {
	return fmt.Sprintf("AgentRoute{%s via %s, metric=%d, origin=%s}",
		r.AgentID.ShortString(), r.NextHop.ShortString(), r.Metric, r.OriginAgent.ShortString())
}

// Clone creates a deep copy of the agent route.
func (r *AgentRoute) Clone() *AgentRoute {
	clone := &AgentRoute{
		AgentID:     r.AgentID,
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

// agentRouteEntryOps adapts *AgentRoute to the generic keyedTable skeleton.
var agentRouteEntryOps = entryOps[*AgentRoute]{
	clone:         func(r *AgentRoute) *AgentRoute { return r.Clone() },
	origin:        func(r *AgentRoute) identity.AgentID { return r.OriginAgent },
	nextHop:       func(r *AgentRoute) identity.AgentID { return r.NextHop },
	metric:        func(r *AgentRoute) uint16 { return r.Metric },
	sequence:      func(r *AgentRoute) uint64 { return r.Sequence },
	path:          func(r *AgentRoute) []identity.AgentID { return r.Path },
	lastUpdate:    func(r *AgentRoute) time.Time { return r.LastUpdate },
	setLastUpdate: func(r *AgentRoute, t time.Time) { r.LastUpdate = t },
}

// AgentTable is a thread-safe agent presence routing table.
//
// Unlike the other tables, an agent can hold several entries per origin (one
// per next hop), so entries are keyed on (origin, next hop) pairs and a
// sequenced withdraw removes every entry from the origin that passes the
// sequence guard (see keyedTable).
type AgentTable struct {
	entries *keyedTable[identity.AgentID, *AgentRoute]
}

// NewAgentTable creates a new agent presence routing table.
func NewAgentTable(localID identity.AgentID) *AgentTable {
	return &AgentTable{
		entries: newKeyedTable[identity.AgentID](localID, agentRouteEntryOps, true, true),
	}
}

// AddRoute adds or updates an agent presence route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *AgentTable) AddRoute(route *AgentRoute) bool {
	if route == nil {
		return false
	}
	return t.entries.add(route.AgentID, route)
}

// RemoveRoute removes an agent presence route from a specific origin.
func (t *AgentTable) RemoveRoute(agentID, originAgent identity.AgentID) bool {
	return t.entries.remove(agentID, originAgent)
}

// RemoveRouteWithSeq removes agent presence routes from a specific origin, but
// only when the installed route's sequence is not newer than seq. Guards
// against a stale ROUTE_WITHDRAW deleting a fresher re-advertised route.
// Unlike the other tables, an agent can hold several entries per origin (one
// per next hop), so all entries passing the guard are removed.
func (t *AgentTable) RemoveRouteWithSeq(agentID, originAgent identity.AgentID, seq uint64) bool {
	return t.entries.removeWithSeq(agentID, originAgent, seq)
}

// RemoveRoutesFromPeer removes all agent routes learned from a specific peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (t *AgentTable) RemoveRoutesFromPeer(peerID identity.AgentID) []*AgentRoute {
	return t.entries.removeFromPeer(peerID)
}

// Lookup finds the best agent presence route for a target agent.
func (t *AgentTable) Lookup(agentID identity.AgentID) *AgentRoute {
	route, ok := t.entries.first(agentID)
	if !ok {
		return nil
	}
	return route
}

// GetAllRoutes returns all agent presence routes in the table.
func (t *AgentTable) GetAllRoutes() []*AgentRoute {
	return t.entries.all()
}

// GetRoutesForAgent returns all agent presence routes for a specific agent.
func (t *AgentTable) GetRoutesForAgent(agentID identity.AgentID) []*AgentRoute {
	return t.entries.allForKey(agentID)
}

// Size returns the number of unique agents in the table.
func (t *AgentTable) Size() int {
	return t.entries.size()
}

// TotalRoutes returns the total number of agent route entries.
func (t *AgentTable) TotalRoutes() int {
	return t.entries.total()
}

// Clear removes all agent routes from the table.
func (t *AgentTable) Clear() {
	t.entries.clear()
}

// CleanupStaleRoutes removes agent routes that haven't been updated within maxAge.
// Local routes (where OriginAgent == localID) are never removed.
// Returns the number of routes removed.
func (t *AgentTable) CleanupStaleRoutes(maxAge time.Duration) int {
	return t.entries.cleanupStale(maxAge)
}

// GetAllAgentIDs returns all unique agent IDs in the table.
func (t *AgentTable) GetAllAgentIDs() []identity.AgentID {
	return t.entries.keys()
}
