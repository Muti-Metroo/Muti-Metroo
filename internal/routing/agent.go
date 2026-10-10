// Package routing implements agent presence routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
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

	// Path is the AS-path style list of agent IDs (nil if encrypted and can't decrypt)
	Path []identity.AgentID

	// EncPath is the encrypted path data for forwarding (nil if no encryption)
	EncPath *protocol.EncryptedData

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
	if r.EncPath != nil {
		clone.EncPath = &protocol.EncryptedData{
			Encrypted: r.EncPath.Encrypted,
			Data:      make([]byte, len(r.EncPath.Data)),
		}
		copy(clone.EncPath.Data, r.EncPath.Data)
	}
	return clone
}

// AgentTable is a thread-safe agent presence routing table.
type AgentTable struct {
	mu sync.RWMutex

	// routes maps agent ID to route entries
	routes map[identity.AgentID][]*AgentRoute

	// localID is this agent's ID (for loop detection)
	localID identity.AgentID
}

// NewAgentTable creates a new agent presence routing table.
func NewAgentTable(localID identity.AgentID) *AgentTable {
	return &AgentTable{
		routes:  make(map[identity.AgentID][]*AgentRoute),
		localID: localID,
	}
}

// AddRoute adds or updates an agent presence route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *AgentTable) AddRoute(route *AgentRoute) bool {
	if route == nil {
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

	key := route.AgentID

	// Check if we already have a route from this origin via this next hop
	for i, r := range t.routes[key] {
		if r.OriginAgent == route.OriginAgent && r.NextHop == route.NextHop {
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

	// New route from this origin/nexthop
	cloned := route.Clone()
	cloned.LastUpdate = time.Now()
	t.routes[key] = append(t.routes[key], cloned)
	t.sortRoutes(key)
	return true
}

// sortRoutes sorts routes for an agent by metric (lowest first).
func (t *AgentTable) sortRoutes(key identity.AgentID) {
	routes := t.routes[key]
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Metric < routes[j].Metric
	})
}

// RemoveRoute removes an agent presence route from a specific origin.
func (t *AgentTable) RemoveRoute(agentID, originAgent identity.AgentID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	routes := t.routes[agentID]
	for i, r := range routes {
		if r.OriginAgent == originAgent {
			t.routes[agentID] = append(routes[:i], routes[i+1:]...)
			if len(t.routes[agentID]) == 0 {
				delete(t.routes, agentID)
			}
			return true
		}
	}
	return false
}

// RemoveRoutesFromPeer removes all agent routes learned from a specific peer.
func (t *AgentTable) RemoveRoutesFromPeer(peerID identity.AgentID) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for agentID, routes := range t.routes {
		filtered := routes[:0]
		for _, r := range routes {
			if r.NextHop != peerID {
				filtered = append(filtered, r)
			} else {
				count++
			}
		}
		if len(filtered) == 0 {
			delete(t.routes, agentID)
		} else {
			t.routes[agentID] = filtered
		}
	}
	return count
}

// Lookup finds the best agent presence route for a target agent.
func (t *AgentTable) Lookup(agentID identity.AgentID) *AgentRoute {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if routes, ok := t.routes[agentID]; ok && len(routes) > 0 {
		return routes[0].Clone() // First is best due to sorting by metric
	}
	return nil
}

// GetAllRoutes returns all agent presence routes in the table.
func (t *AgentTable) GetAllRoutes() []*AgentRoute {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var all []*AgentRoute
	for _, routes := range t.routes {
		for _, r := range routes {
			all = append(all, r.Clone())
		}
	}
	return all
}

// GetRoutesForAgent returns all agent presence routes for a specific agent.
func (t *AgentTable) GetRoutesForAgent(agentID identity.AgentID) []*AgentRoute {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var matching []*AgentRoute
	for _, r := range t.routes[agentID] {
		matching = append(matching, r.Clone())
	}
	return matching
}

// Size returns the number of unique agents in the table.
func (t *AgentTable) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}

// TotalRoutes returns the total number of agent route entries.
func (t *AgentTable) TotalRoutes() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, routes := range t.routes {
		count += len(routes)
	}
	return count
}

// Clear removes all agent routes from the table.
func (t *AgentTable) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes = make(map[identity.AgentID][]*AgentRoute)
}

// CleanupStaleRoutes removes agent routes that haven't been updated within maxAge.
// Local routes (where OriginAgent == localID) are never removed.
// Returns the number of routes removed.
func (t *AgentTable) CleanupStaleRoutes(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	removed := 0

	for agentID, routes := range t.routes {
		var kept []*AgentRoute
		for _, r := range routes {
			if r.OriginAgent == t.localID || now.Sub(r.LastUpdate) <= maxAge {
				kept = append(kept, r)
			} else {
				removed++
			}
		}
		if len(kept) > 0 {
			t.routes[agentID] = kept
		} else {
			delete(t.routes, agentID)
		}
	}
	return removed
}

// GetAllAgentIDs returns all unique agent IDs in the table.
func (t *AgentTable) GetAllAgentIDs() []identity.AgentID {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ids := make([]identity.AgentID, 0, len(t.routes))
	for id := range t.routes {
		ids = append(ids, id)
	}
	return ids
}
