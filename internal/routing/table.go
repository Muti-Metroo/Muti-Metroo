// Package routing implements CIDR-based routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// Route represents a single route entry in the routing table.
type Route struct {
	// Network is the destination CIDR block
	Network *net.IPNet

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

	// Comment is operator metadata propagated from the originating
	// agent. It is not used for route selection; same-sequence
	// re-advertisements with a different comment refresh it in place.
	Comment string
}

// String returns a human-readable representation of the route.
func (r *Route) String() string {
	return fmt.Sprintf("Route{%s via %s, metric=%d, origin=%s}",
		r.Network.String(), r.NextHop.ShortString(), r.Metric, r.OriginAgent.ShortString())
}

// Clone creates a deep copy of the route.
func (r *Route) Clone() *Route {
	clone := &Route{
		Network:     &net.IPNet{IP: make(net.IP, len(r.Network.IP)), Mask: make(net.IPMask, len(r.Network.Mask))},
		NextHop:     r.NextHop,
		OriginAgent: r.OriginAgent,
		Metric:      r.Metric,
		Sequence:    r.Sequence,
		LastUpdate:  r.LastUpdate,
		Comment:     r.Comment,
	}
	copy(clone.Network.IP, r.Network.IP)
	copy(clone.Network.Mask, r.Network.Mask)
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

// Table is a thread-safe routing table with longest-prefix match support.
type Table struct {
	mu sync.RWMutex

	// routes maps CIDR string to route entries (may have multiple routes per prefix)
	routes map[string][]*Route

	// localID is this agent's ID (for loop detection)
	localID identity.AgentID
}

// NewTable creates a new routing table.
func NewTable(localID identity.AgentID) *Table {
	return &Table{
		routes:  make(map[string][]*Route),
		localID: localID,
	}
}

// AddRoute adds or updates a route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *Table) AddRoute(route *Route) bool {
	if route == nil || route.Network == nil {
		return false
	}

	// Check for routing loops (is our ID in the path?)
	for _, id := range route.Path {
		if id == t.localID {
			return false // Loop detected
		}
	}

	key := route.Network.String()
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if we already have a route from this origin
	existing := t.routes[key]
	for i, r := range existing {
		if r.OriginAgent == route.OriginAgent {
			// Update if newer sequence or better metric
			if route.Sequence > r.Sequence ||
				(route.Sequence == r.Sequence && route.Metric < r.Metric) {
				cloned := route.Clone()
				cloned.LastUpdate = now
				t.routes[key][i] = cloned
				t.sortRoutes(key)
				return true
			}
			// Same-sequence same-metric refresh: only the operator
			// comment may have changed. Mutating in place is safe
			// because we hold t.mu.Lock() and readers RLock + Clone.
			if route.Comment != r.Comment {
				r.Comment = route.Comment
			}
			return false // Older/worse route (or comment-only refresh)
		}
	}

	// New route from this origin
	cloned := route.Clone()
	cloned.LastUpdate = now
	t.routes[key] = append(t.routes[key], cloned)
	t.sortRoutes(key)
	return true
}

// sortRoutes sorts routes for a key by metric (lowest first).
func (t *Table) sortRoutes(key string) {
	routes := t.routes[key]
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Metric < routes[j].Metric
	})
}

// SetLocalComment updates the operator comment on the route from origin
// for network, in place. Used after a comment-only edit on a locally
// originated route so the table-level entry stays in sync with the
// operator-visible value.
func (t *Table) SetLocalComment(network *net.IPNet, origin identity.AgentID, comment string) {
	if network == nil {
		return
	}
	key := network.String()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.routes[key] {
		if r.OriginAgent == origin {
			r.Comment = comment
			return
		}
	}
}

// RemoveRoute removes a route from a specific origin.
func (t *Table) RemoveRoute(network *net.IPNet, originAgent identity.AgentID) bool {
	if network == nil {
		return false
	}

	key := network.String()

	t.mu.Lock()
	defer t.mu.Unlock()

	routes := t.routes[key]
	for i, r := range routes {
		if r.OriginAgent == originAgent {
			// Remove this route
			t.routes[key] = append(routes[:i], routes[i+1:]...)
			if len(t.routes[key]) == 0 {
				delete(t.routes, key)
			}
			return true
		}
	}
	return false
}

// RemoveRoutesFromPeer removes all routes learned from a specific peer.
func (t *Table) RemoveRoutesFromPeer(peerID identity.AgentID) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for key, routes := range t.routes {
		filtered := routes[:0]
		for _, r := range routes {
			if r.NextHop != peerID {
				filtered = append(filtered, r)
			} else {
				count++
			}
		}
		if len(filtered) == 0 {
			delete(t.routes, key)
		} else {
			t.routes[key] = filtered
		}
	}
	return count
}

// Lookup finds the best route for an IP address using longest-prefix match.
func (t *Table) Lookup(ip net.IP) *Route {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.lookupUnlocked(ip)
}

// lookupUnlocked performs longest-prefix-match with metric and OriginAgent
// tiebreakers. Caller must hold the lock. The OriginAgent tiebreaker keeps
// selection deterministic across process restarts regardless of Go map
// iteration order.
func (t *Table) lookupUnlocked(ip net.IP) *Route {
	var bestRoute *Route
	var bestPrefixLen = -1

	ip = ip.To16()

	for _, routes := range t.routes {
		if len(routes) == 0 {
			continue
		}

		// routes[0] is the best entry for this CIDR key (sorted by metric).
		first := routes[0]
		if !first.Network.Contains(ip) {
			continue
		}

		ones, _ := first.Network.Mask.Size()

		switch {
		case ones > bestPrefixLen:
			bestPrefixLen = ones
			bestRoute = first
		case ones == bestPrefixLen && bestRoute != nil:
			if first.Metric < bestRoute.Metric {
				bestRoute = first
			} else if first.Metric == bestRoute.Metric &&
				first.OriginAgent.Compare(bestRoute.OriginAgent) < 0 {
				bestRoute = first
			}
		}
	}

	if bestRoute != nil {
		return bestRoute.Clone()
	}
	return nil
}

// LookupAll returns all routes for an IP address, sorted by prefix length then metric.
func (t *Table) LookupAll(ip net.IP) []*Route {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ip = ip.To16()
	var matches []*Route

	for _, routes := range t.routes {
		if len(routes) == 0 {
			continue
		}

		first := routes[0]
		if !first.Network.Contains(ip) {
			continue
		}

		// Add best route from each matching prefix
		matches = append(matches, first.Clone())
	}

	// Sort by prefix length (longest first), then by metric
	sort.Slice(matches, func(i, j int) bool {
		onesI, _ := matches[i].Network.Mask.Size()
		onesJ, _ := matches[j].Network.Mask.Size()
		if onesI != onesJ {
			return onesI > onesJ
		}
		return matches[i].Metric < matches[j].Metric
	})

	return matches
}

// GetRoute returns the best route for a specific network.
func (t *Table) GetRoute(network *net.IPNet) *Route {
	if network == nil {
		return nil
	}

	key := network.String()

	t.mu.RLock()
	defer t.mu.RUnlock()

	routes := t.routes[key]
	if len(routes) > 0 {
		return routes[0].Clone()
	}
	return nil
}

// GetAllRoutesForNetwork returns all routes for a specific network.
func (t *Table) GetAllRoutesForNetwork(network *net.IPNet) []*Route {
	if network == nil {
		return nil
	}

	key := network.String()

	t.mu.RLock()
	defer t.mu.RUnlock()

	routes := t.routes[key]
	result := make([]*Route, len(routes))
	for i, r := range routes {
		result[i] = r.Clone()
	}
	return result
}

// GetAllRoutes returns all routes in the table.
func (t *Table) GetAllRoutes() []*Route {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var all []*Route
	for _, routes := range t.routes {
		for _, r := range routes {
			all = append(all, r.Clone())
		}
	}
	return all
}

// GetRoutesFromAgent returns all routes originating from a specific agent.
func (t *Table) GetRoutesFromAgent(agentID identity.AgentID) []*Route {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var matching []*Route
	for _, routes := range t.routes {
		for _, r := range routes {
			if r.OriginAgent == agentID {
				matching = append(matching, r.Clone())
			}
		}
	}
	return matching
}

// Size returns the number of unique prefixes in the table.
func (t *Table) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}

// TotalRoutes returns the total number of route entries.
func (t *Table) TotalRoutes() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, routes := range t.routes {
		count += len(routes)
	}
	return count
}

// Clear removes all routes from the table.
func (t *Table) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes = make(map[string][]*Route)
}

// HasRoute checks if a route exists for the given network and origin.
func (t *Table) HasRoute(network *net.IPNet, originAgent identity.AgentID) bool {
	if network == nil {
		return false
	}

	key := network.String()

	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, r := range t.routes[key] {
		if r.OriginAgent == originAgent {
			return true
		}
	}
	return false
}

// ParseCIDR parses a CIDR string into a net.IPNet.
func ParseCIDR(cidr string) (*net.IPNet, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}
	return network, nil
}

// MustParseCIDR parses a CIDR string or panics.
func MustParseCIDR(cidr string) *net.IPNet {
	network, err := ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}

// CleanupStaleRoutes removes routes that haven't been updated within maxAge.
// Local routes (where OriginAgent == localID) are never removed.
// Returns the number of routes removed.
func (t *Table) CleanupStaleRoutes(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, routes := range t.routes {
		var kept []*Route
		for _, r := range routes {
			// Never remove local routes
			if r.OriginAgent == t.localID {
				kept = append(kept, r)
				continue
			}

			// Keep routes that are still fresh
			if now.Sub(r.LastUpdate) <= maxAge {
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
