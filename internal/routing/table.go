// Package routing implements CIDR-based routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
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

	// Path is the AS-path style list of agent IDs
	Path []identity.AgentID

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
	return clone
}

// routeEntryOps adapts *Route to the generic keyedTable skeleton.
var routeEntryOps = entryOps[*Route]{
	clone:         func(r *Route) *Route { return r.Clone() },
	origin:        func(r *Route) identity.AgentID { return r.OriginAgent },
	nextHop:       func(r *Route) identity.AgentID { return r.NextHop },
	metric:        func(r *Route) uint16 { return r.Metric },
	sequence:      func(r *Route) uint64 { return r.Sequence },
	path:          func(r *Route) []identity.AgentID { return r.Path },
	lastUpdate:    func(r *Route) time.Time { return r.LastUpdate },
	setLastUpdate: func(r *Route, t time.Time) { r.LastUpdate = t },
	refreshInPlace: func(stored, incoming *Route) {
		if stored.Comment != incoming.Comment {
			stored.Comment = incoming.Comment
		}
	},
}

// Table is a thread-safe routing table with longest-prefix match support.
// The storage skeleton (add/remove/cleanup/clone-on-read) is the generic
// keyedTable; longest-prefix match over CIDR keys stays here.
type Table struct {
	entries *keyedTable[string, *Route]
}

// NewTable creates a new routing table.
func NewTable(localID identity.AgentID) *Table {
	return &Table{
		entries: newKeyedTable[string](localID, routeEntryOps, false, false),
	}
}

// AddRoute adds or updates a route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *Table) AddRoute(route *Route) bool {
	if route == nil || route.Network == nil {
		return false
	}
	return t.entries.add(route.Network.String(), route)
}

// SetLocalComment updates the operator comment on the route from origin
// for network, in place. Used after a comment-only edit on a locally
// originated route so the table-level entry stays in sync with the
// operator-visible value.
func (t *Table) SetLocalComment(network *net.IPNet, origin identity.AgentID, comment string) {
	if network == nil {
		return
	}
	t.entries.updateInPlace(network.String(),
		func(r *Route) bool { return r.OriginAgent == origin },
		func(r *Route) { r.Comment = comment })
}

// RemoveRoute removes a route from a specific origin.
func (t *Table) RemoveRoute(network *net.IPNet, originAgent identity.AgentID) bool {
	if network == nil {
		return false
	}
	return t.entries.remove(network.String(), originAgent)
}

// RemoveRouteWithSeq removes a route from a specific origin, but only when the
// installed route's sequence is not newer than seq. A ROUTE_WITHDRAW traveling
// a slow path can arrive after a newer re-advertise from the same origin;
// without this guard the stale withdraw would delete the fresh route.
func (t *Table) RemoveRouteWithSeq(network *net.IPNet, originAgent identity.AgentID, seq uint64) bool {
	if network == nil {
		return false
	}
	return t.entries.removeWithSeq(network.String(), originAgent, seq)
}

// RemoveRoutesFromPeer removes all routes learned from a specific peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (t *Table) RemoveRoutesFromPeer(peerID identity.AgentID) []*Route {
	return t.entries.removeFromPeer(peerID)
}

// Lookup finds the best route for an IP address using longest-prefix match.
// Equal-prefix matches break ties on metric, then on origin agent ID, so
// selection is deterministic across process restarts regardless of Go map
// iteration order. Each per-key list is already sorted best-first (see
// keyedTable.sortRoutes), so only the first entry of each key competes.
func (t *Table) Lookup(ip net.IP) *Route {
	ip = ip.To16()

	best, ok := t.entries.lookupBest(
		func(r *Route) bool { return r.Network.Contains(ip) },
		func(candidate, current *Route) bool {
			candOnes, _ := candidate.Network.Mask.Size()
			curOnes, _ := current.Network.Mask.Size()
			if candOnes != curOnes {
				return candOnes > curOnes
			}
			if candidate.Metric != current.Metric {
				return candidate.Metric < current.Metric
			}
			return candidate.OriginAgent.Compare(current.OriginAgent) < 0
		})
	if !ok {
		return nil
	}
	return best
}

// LookupAll returns all routes for an IP address, sorted by prefix length then metric.
func (t *Table) LookupAll(ip net.IP) []*Route {
	ip = ip.To16()

	matches := t.entries.collectFirsts(func(r *Route) bool {
		return r.Network.Contains(ip)
	})

	// Sort by prefix length (longest first), then by metric, then by
	// origin agent ID so equal-metric matches resolve deterministically.
	sort.Slice(matches, func(i, j int) bool {
		onesI, _ := matches[i].Network.Mask.Size()
		onesJ, _ := matches[j].Network.Mask.Size()
		if onesI != onesJ {
			return onesI > onesJ
		}
		if matches[i].Metric != matches[j].Metric {
			return matches[i].Metric < matches[j].Metric
		}
		return matches[i].OriginAgent.Compare(matches[j].OriginAgent) < 0
	})

	return matches
}

// GetRoute returns the best route for a specific network.
func (t *Table) GetRoute(network *net.IPNet) *Route {
	if network == nil {
		return nil
	}
	route, ok := t.entries.first(network.String())
	if !ok {
		return nil
	}
	return route
}

// GetAllRoutesForNetwork returns all routes for a specific network.
func (t *Table) GetAllRoutesForNetwork(network *net.IPNet) []*Route {
	if network == nil {
		return nil
	}
	return t.entries.allForKey(network.String())
}

// GetAllRoutes returns all routes in the table.
func (t *Table) GetAllRoutes() []*Route {
	return t.entries.all()
}

// GetRoutesFromAgent returns all routes originating from a specific agent.
func (t *Table) GetRoutesFromAgent(agentID identity.AgentID) []*Route {
	return t.entries.allFromOrigin(agentID)
}

// Size returns the number of unique prefixes in the table.
func (t *Table) Size() int {
	return t.entries.size()
}

// TotalRoutes returns the total number of route entries.
func (t *Table) TotalRoutes() int {
	return t.entries.total()
}

// Clear removes all routes from the table.
func (t *Table) Clear() {
	t.entries.clear()
}

// HasRoute checks if a route exists for the given network and origin.
func (t *Table) HasRoute(network *net.IPNet, originAgent identity.AgentID) bool {
	if network == nil {
		return false
	}
	return t.entries.has(network.String(), originAgent)
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
	return t.entries.cleanupStale(maxAge)
}
