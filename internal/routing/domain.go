// Package routing implements domain-based routing for Muti Metroo mesh network.
package routing

import (
	"fmt"
	"strings"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
)

// DomainRoute represents a single domain route entry.
type DomainRoute struct {
	// Pattern is the original pattern (e.g., "example.com" or "*.example.com")
	Pattern string

	// IsWildcard indicates if this is a wildcard pattern (*.domain)
	IsWildcard bool

	// BaseDomain is the domain without the wildcard prefix
	// For exact matches, this equals Pattern
	// For wildcards, "*.example.com" -> "example.com"
	BaseDomain string

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

// String returns a human-readable representation of the domain route.
func (r *DomainRoute) String() string {
	return fmt.Sprintf("DomainRoute{%s via %s, metric=%d, origin=%s}",
		r.Pattern, r.NextHop.ShortString(), r.Metric, r.OriginAgent.ShortString())
}

// Clone creates a deep copy of the domain route.
func (r *DomainRoute) Clone() *DomainRoute {
	clone := &DomainRoute{
		Pattern:     r.Pattern,
		IsWildcard:  r.IsWildcard,
		BaseDomain:  r.BaseDomain,
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

// domainRouteEntryOps adapts *DomainRoute to the generic keyedTable skeleton.
var domainRouteEntryOps = entryOps[*DomainRoute]{
	clone:         func(r *DomainRoute) *DomainRoute { return r.Clone() },
	origin:        func(r *DomainRoute) identity.AgentID { return r.OriginAgent },
	nextHop:       func(r *DomainRoute) identity.AgentID { return r.NextHop },
	metric:        func(r *DomainRoute) uint16 { return r.Metric },
	sequence:      func(r *DomainRoute) uint64 { return r.Sequence },
	path:          func(r *DomainRoute) []identity.AgentID { return r.Path },
	lastUpdate:    func(r *DomainRoute) time.Time { return r.LastUpdate },
	setLastUpdate: func(r *DomainRoute, t time.Time) { r.LastUpdate = t },
}

// DomainTable is a thread-safe domain routing table.
//
// The storage skeleton is the generic keyedTable (see generic_table.go).
// Exact and wildcard routes share one map under distinct key prefixes
// ("e:" for exact domains, "w:" for wildcard base domains) so an exact route
// for "example.com" never collides with the wildcard "*.example.com".
// Lookup tries the exact key first, then the single-level wildcard key.
type DomainTable struct {
	entries *keyedTable[string, *DomainRoute]
}

// domainRouteKey derives the map key for a route: wildcard routes key on the
// lowercase base domain, exact routes on the lowercase pattern.
func domainRouteKey(pattern string, isWildcard bool, baseDomain string) string {
	if isWildcard {
		return "w:" + strings.ToLower(baseDomain)
	}
	return "e:" + strings.ToLower(pattern)
}

// NewDomainTable creates a new domain routing table.
func NewDomainTable(localID identity.AgentID) *DomainTable {
	return &DomainTable{
		entries: newKeyedTable[string](localID, domainRouteEntryOps, false, false),
	}
}

// AddRoute adds or updates a domain route in the table.
// Returns true if the route was added/updated, false if rejected (e.g., loop detected).
func (t *DomainTable) AddRoute(route *DomainRoute) bool {
	if route == nil || route.Pattern == "" {
		return false
	}
	return t.entries.add(domainRouteKey(route.Pattern, route.IsWildcard, route.BaseDomain), route)
}

// RemoveRoute removes a domain route from a specific origin.
func (t *DomainTable) RemoveRoute(pattern string, originAgent identity.AgentID) bool {
	if pattern == "" {
		return false
	}
	isWildcard, baseDomain := ParseDomainPattern(pattern)
	return t.entries.remove(domainRouteKey(pattern, isWildcard, baseDomain), originAgent)
}

// RemoveRouteWithSeq removes a domain route from a specific origin, but only
// when the installed route's sequence is not newer than seq. Guards against a
// stale ROUTE_WITHDRAW deleting a fresher re-advertised route.
func (t *DomainTable) RemoveRouteWithSeq(pattern string, originAgent identity.AgentID, seq uint64) bool {
	if pattern == "" {
		return false
	}
	isWildcard, baseDomain := ParseDomainPattern(pattern)
	return t.entries.removeWithSeq(domainRouteKey(pattern, isWildcard, baseDomain), originAgent, seq)
}

// RemoveRoutesFromPeer removes all domain routes learned from a specific peer.
// Returns the removed routes so the caller can propagate withdrawals.
func (t *DomainTable) RemoveRoutesFromPeer(peerID identity.AgentID) []*DomainRoute {
	return t.entries.removeFromPeer(peerID)
}

// Lookup finds the best domain route for a domain name.
// First checks exact matches, then single-level wildcards.
func (t *DomainTable) Lookup(domain string) *DomainRoute {
	domain = strings.ToLower(domain)

	// 1. Check exact match first
	if route, ok := t.entries.first("e:" + domain); ok {
		return route
	}

	// 2. Check single-level wildcard
	// Split at first dot to get parent domain
	idx := strings.Index(domain, ".")
	if idx > 0 && idx < len(domain)-1 {
		baseDomain := domain[idx+1:]
		if route, ok := t.entries.first("w:" + baseDomain); ok {
			return route
		}
	}

	return nil
}

// GetAllRoutes returns all domain routes in the table.
func (t *DomainTable) GetAllRoutes() []*DomainRoute {
	return t.entries.all()
}

// GetRoutesFromAgent returns all domain routes originating from a specific agent.
func (t *DomainTable) GetRoutesFromAgent(agentID identity.AgentID) []*DomainRoute {
	return t.entries.allFromOrigin(agentID)
}

// Size returns the number of unique domain patterns in the table.
func (t *DomainTable) Size() int {
	return t.entries.size()
}

// TotalRoutes returns the total number of domain route entries.
func (t *DomainTable) TotalRoutes() int {
	return t.entries.total()
}

// Clear removes all domain routes from the table.
func (t *DomainTable) Clear() {
	t.entries.clear()
}

// HasRoute checks if a domain route exists for the given pattern and origin.
func (t *DomainTable) HasRoute(pattern string, originAgent identity.AgentID) bool {
	if pattern == "" {
		return false
	}
	isWildcard, baseDomain := ParseDomainPattern(pattern)
	return t.entries.has(domainRouteKey(pattern, isWildcard, baseDomain), originAgent)
}

// CleanupStaleRoutes removes domain routes that haven't been updated within maxAge.
// Local routes (where OriginAgent == localID) are never removed.
// Returns the number of routes removed.
func (t *DomainTable) CleanupStaleRoutes(maxAge time.Duration) int {
	return t.entries.cleanupStale(maxAge)
}

// ParseDomainPattern parses a domain pattern and returns whether it's a wildcard
// and the base domain.
func ParseDomainPattern(pattern string) (isWildcard bool, baseDomain string) {
	pattern = strings.TrimSpace(pattern)
	if strings.HasPrefix(pattern, "*.") {
		return true, pattern[2:]
	}
	return false, pattern
}

// ValidateDomainPattern validates a domain pattern.
// Returns nil if valid, or an error describing the issue.
func ValidateDomainPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty domain pattern")
	}

	isWildcard, baseDomain := ParseDomainPattern(pattern)

	// Validate base domain
	domain := baseDomain
	if !isWildcard {
		domain = pattern
	}

	if domain == "" {
		return fmt.Errorf("empty domain after wildcard")
	}

	// Basic domain validation
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("domain cannot start or end with a dot")
	}

	if strings.Contains(domain, "..") {
		return fmt.Errorf("domain cannot contain consecutive dots")
	}

	// Check for valid characters
	for _, r := range domain {
		if !isValidDomainChar(r) {
			return fmt.Errorf("invalid character in domain: %c", r)
		}
	}

	// Must have at least one dot (TLD)
	if !strings.Contains(domain, ".") {
		return fmt.Errorf("domain must have at least one dot (e.g., example.com)")
	}

	return nil
}

// isValidDomainChar checks if a character is valid in a domain name.
func isValidDomainChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '.'
}

// LocalDomainRoute represents a locally-announced domain route.
type LocalDomainRoute struct {
	Pattern    string
	IsWildcard bool
	BaseDomain string
	Metric     uint16
}

// DomainRouteChange represents a domain route update.
type DomainRouteChange struct {
	Type  RouteChangeType
	Route *DomainRoute
}
