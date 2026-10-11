package routing

import (
	"net"
	"testing"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// fixedAgentID returns a deterministic AgentID with b as its first byte, so
// tests control the origin-ID ordering used by the equal-metric tiebreak.
func fixedAgentID(b byte) identity.AgentID {
	var id identity.AgentID
	id[0] = b
	return id
}

// TestTable_EqualMetricTiebreak_LowestOriginWins: two exits advertising the
// same CIDR with the same metric always resolve to the lower origin agent ID,
// regardless of insertion order.
func TestTable_EqualMetricTiebreak_LowestOriginWins(t *testing.T) {
	localID, _ := identity.NewAgentID()
	lower := fixedAgentID(0x10)
	higher := fixedAgentID(0x20)

	for _, name := range []string{"lower first", "higher first"} {
		t.Run(name, func(t *testing.T) {
			table := NewTable(localID)
			if name == "lower first" {
				addRoute(t, table, "10.0.0.0/8", lower, 5)
				addRoute(t, table, "10.0.0.0/8", higher, 5)
			} else {
				addRoute(t, table, "10.0.0.0/8", higher, 5)
				addRoute(t, table, "10.0.0.0/8", lower, 5)
			}

			_, ipNet, _ := net.ParseCIDR("10.0.0.0/8")
			if r := table.GetRoute(ipNet); r == nil || r.OriginAgent != lower {
				t.Errorf("GetRoute origin = %v, want %v", r, lower)
			}
			if r := table.Lookup(net.ParseIP("10.1.2.3")); r == nil || r.OriginAgent != lower {
				t.Errorf("Lookup origin = %v, want %v", r, lower)
			}
		})
	}
}

// TestTable_EqualMetricTiebreak_LookupMatchesTrace: the route trace winner for
// equal-metric same-CIDR routes is the same route a real lookup selects.
func TestTable_EqualMetricTiebreak_LookupMatchesTrace(t *testing.T) {
	localID, _ := identity.NewAgentID()
	lower := fixedAgentID(0x10)
	higher := fixedAgentID(0x20)

	table := NewTable(localID)
	addRoute(t, table, "10.0.0.0/8", higher, 5)
	addRoute(t, table, "10.0.0.0/8", lower, 5)

	lookup := table.Lookup(net.ParseIP("10.1.2.3"))
	if lookup == nil {
		t.Fatal("Lookup returned nil")
	}

	tr := table.Explain(net.ParseIP("10.1.2.3"))
	if len(tr.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1", len(tr.Candidates))
	}
	winner := tr.Candidates[0]
	if !winner.Winner {
		t.Error("trace candidate not marked winner")
	}
	if winner.Route.OriginAgent != lookup.OriginAgent {
		t.Errorf("trace winner origin %v != lookup origin %v", winner.Route.OriginAgent, lookup.OriginAgent)
	}
	if winner.Route.OriginAgent != lower {
		t.Errorf("winner origin = %v, want lower origin %v", winner.Route.OriginAgent, lower)
	}
}

// TestAgentTable_EqualMetricTiebreak: equal-metric presence routes for the
// same agent resolve to the lowest origin, then the lowest next hop.
func TestAgentTable_EqualMetricTiebreak(t *testing.T) {
	localID, _ := identity.NewAgentID()
	target := fixedAgentID(0x30)
	origin := fixedAgentID(0x10)
	lowerHop := fixedAgentID(0x01)
	higherHop := fixedAgentID(0x02)

	table := NewAgentTable(localID)
	if !table.AddRoute(&AgentRoute{AgentID: target, NextHop: higherHop, OriginAgent: origin, Metric: 5, Sequence: 1}) {
		t.Fatal("AddRoute returned false")
	}
	if !table.AddRoute(&AgentRoute{AgentID: target, NextHop: lowerHop, OriginAgent: origin, Metric: 5, Sequence: 1}) {
		t.Fatal("AddRoute returned false")
	}

	if r := table.Lookup(target); r == nil || r.NextHop != lowerHop {
		t.Errorf("Lookup next hop = %v, want %v", r, lowerHop)
	}
}

// TestDomainTable_EqualMetricTiebreak: equal-metric domain routes from two
// origins resolve to the lower origin agent ID.
func TestDomainTable_EqualMetricTiebreak(t *testing.T) {
	localID, _ := identity.NewAgentID()
	lower := fixedAgentID(0x10)
	higher := fixedAgentID(0x20)

	table := NewDomainTable(localID)
	if !table.AddRoute(&DomainRoute{Pattern: "example.com", BaseDomain: "example.com", NextHop: higher, OriginAgent: higher, Metric: 5, Sequence: 1}) {
		t.Fatal("AddRoute returned false")
	}
	if !table.AddRoute(&DomainRoute{Pattern: "example.com", BaseDomain: "example.com", NextHop: lower, OriginAgent: lower, Metric: 5, Sequence: 1}) {
		t.Fatal("AddRoute returned false")
	}

	if r := table.Lookup("example.com"); r == nil || r.OriginAgent != lower {
		t.Errorf("Lookup origin = %v, want %v", r, lower)
	}
}

// TestForwardTable_EqualMetricTiebreak: equal-metric forward routes from two
// origins resolve to the lower origin agent ID.
func TestForwardTable_EqualMetricTiebreak(t *testing.T) {
	localID, _ := identity.NewAgentID()
	lower := fixedAgentID(0x10)
	higher := fixedAgentID(0x20)

	table := NewForwardTable(localID)
	if !table.AddRoute(&ForwardRoute{Key: "svc", NextHop: higher, OriginAgent: higher, Metric: 5, Sequence: 1}) {
		t.Fatal("AddRoute returned false")
	}
	if !table.AddRoute(&ForwardRoute{Key: "svc", NextHop: lower, OriginAgent: lower, Metric: 5, Sequence: 1}) {
		t.Fatal("AddRoute returned false")
	}

	if r := table.Lookup("svc"); r == nil || r.OriginAgent != lower {
		t.Errorf("Lookup origin = %v, want %v", r, lower)
	}
}
