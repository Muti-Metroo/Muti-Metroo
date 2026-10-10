// Package routing: trace helpers for explaining route selection decisions.
package routing

import (
	"net"
	"sort"
	"strings"
)

// TraceCandidate describes one CIDR route considered during an Explain.
// Exactly one candidate in a Trace has Winner=true (if any match); the rest
// carry a Reason describing why they lost.
type TraceCandidate struct {
	Route     *Route
	PrefixLen int
	Winner    bool
	Reason    string
}

// Trace holds the result of explaining a CIDR routing decision for one IP.
// Candidates is sorted winner-first (by prefix length desc, then metric asc,
// then origin ID asc). Candidates is nil when no route matches.
type Trace struct {
	IP         net.IP
	Candidates []*TraceCandidate
}

// Explain returns all CIDR routes that match ip along with which one would
// win under the same selection rules as Lookup (longest prefix, then lowest
// metric, then lowest origin ID). Uses the best-per-CIDR (routes[0]) so the
// result mirrors what the live code path would pick.
func (t *Table) Explain(ip net.IP) *Trace {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ip16 := ip.To16()

	var candidates []*TraceCandidate
	for _, routes := range t.routes {
		if len(routes) == 0 {
			continue
		}
		first := routes[0]
		if !first.Network.Contains(ip16) {
			continue
		}
		ones, _ := first.Network.Mask.Size()
		candidates = append(candidates, &TraceCandidate{
			Route:     first.Clone(),
			PrefixLen: ones,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if ci.PrefixLen != cj.PrefixLen {
			return ci.PrefixLen > cj.PrefixLen
		}
		if ci.Route.Metric != cj.Route.Metric {
			return ci.Route.Metric < cj.Route.Metric
		}
		return ci.Route.OriginAgent.Compare(cj.Route.OriginAgent) < 0
	})

	if len(candidates) > 0 {
		candidates[0].Winner = true
		winner := candidates[0]
		for _, c := range candidates[1:] {
			switch {
			case c.PrefixLen < winner.PrefixLen:
				c.Reason = "shorter prefix"
			case c.Route.Metric > winner.Route.Metric:
				c.Reason = "higher metric"
			default:
				c.Reason = "lost origin tiebreaker"
			}
		}
	}

	return &Trace{
		IP:         ip,
		Candidates: candidates,
	}
}

// DomainTrace holds the result of explaining a domain routing decision.
// Winner is nil if no domain route matches.
type DomainTrace struct {
	Domain string
	Winner *DomainRoute
}

// Explain returns which domain route (if any) would match domain. The match
// rule mirrors Lookup: exact match first, then single-level wildcard on the
// parent domain.
func (t *DomainTable) Explain(domain string) *DomainTrace {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return &DomainTrace{
		Domain: strings.ToLower(domain),
		Winner: t.lookupUnlocked(domain),
	}
}
