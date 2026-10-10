package routing

import (
	"net"
	"testing"

	"github.com/postalsys/muti-metroo/internal/identity"
)

func addRoute(t *testing.T, table *Table, cidr string, origin identity.AgentID, metric uint16) {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("bad cidr %q: %v", cidr, err)
	}
	nextHop, _ := identity.NewAgentID()
	route := &Route{
		Network:     ipNet,
		NextHop:     nextHop,
		OriginAgent: origin,
		Metric:      metric,
		Sequence:    1,
	}
	if !table.AddRoute(route) {
		t.Fatalf("AddRoute(%s) returned false", cidr)
	}
}

func TestTable_Explain_NoMatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewTable(localID)

	origin, _ := identity.NewAgentID()
	addRoute(t, table, "10.0.0.0/8", origin, 1)

	tr := table.Explain(net.ParseIP("192.168.1.1"))
	if tr == nil {
		t.Fatal("Explain returned nil")
	}
	if len(tr.Candidates) != 0 {
		t.Errorf("Candidates = %d, want 0", len(tr.Candidates))
	}
}

func TestTable_Explain_LongestPrefixWins(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewTable(localID)

	o1, _ := identity.NewAgentID()
	o2, _ := identity.NewAgentID()

	addRoute(t, table, "0.0.0.0/0", o1, 1)
	addRoute(t, table, "10.0.0.0/8", o2, 5)

	tr := table.Explain(net.ParseIP("10.1.2.3"))
	if len(tr.Candidates) != 2 {
		t.Fatalf("Candidates = %d, want 2", len(tr.Candidates))
	}
	winner := tr.Candidates[0]
	if !winner.Winner {
		t.Error("first candidate should be winner")
	}
	if winner.Route.Network.String() != "10.0.0.0/8" {
		t.Errorf("winner network = %s, want 10.0.0.0/8", winner.Route.Network.String())
	}
	loser := tr.Candidates[1]
	if loser.Winner {
		t.Error("second candidate should not be winner")
	}
	if loser.Reason != "shorter prefix" {
		t.Errorf("loser reason = %q, want %q", loser.Reason, "shorter prefix")
	}
}

func TestTable_Explain_MetricTiebreak(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewTable(localID)

	o1, _ := identity.NewAgentID()
	o2, _ := identity.NewAgentID()

	// Same CIDR from two origins: the best-per-CIDR (routes[0]) gets picked
	// based on metric ordering already done inside AddRoute. Confirm Explain
	// reports that single winner for the CIDR.
	addRoute(t, table, "10.0.0.0/8", o1, 5)
	addRoute(t, table, "10.0.0.0/8", o2, 2)

	tr := table.Explain(net.ParseIP("10.1.2.3"))
	if len(tr.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1 (best-per-CIDR only)", len(tr.Candidates))
	}
	if tr.Candidates[0].Route.Metric != 2 {
		t.Errorf("winner metric = %d, want 2", tr.Candidates[0].Route.Metric)
	}
}

func TestTable_Explain_PrefixLosesOnMetric(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewTable(localID)

	o1, _ := identity.NewAgentID()
	o2, _ := identity.NewAgentID()

	// Two different CIDRs of equal prefix length — winner picked by metric.
	addRoute(t, table, "10.1.0.0/16", o1, 10)
	addRoute(t, table, "10.2.0.0/16", o2, 3) // doesn't contain 10.1.2.3

	tr := table.Explain(net.ParseIP("10.1.2.3"))
	if len(tr.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1", len(tr.Candidates))
	}
	if tr.Candidates[0].Route.Network.String() != "10.1.0.0/16" {
		t.Errorf("winner network = %s, want 10.1.0.0/16", tr.Candidates[0].Route.Network.String())
	}
}

func TestDomainTable_Explain(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewDomainTable(localID)

	origin, _ := identity.NewAgentID()
	peer, _ := identity.NewAgentID()
	route := &DomainRoute{
		Pattern:     "*.example.com",
		IsWildcard:  true,
		BaseDomain:  "example.com",
		NextHop:     peer,
		OriginAgent: origin,
		Metric:      1,
		Sequence:    1,
	}
	if !table.AddRoute(route) {
		t.Fatal("AddRoute returned false")
	}

	// Wildcard match
	tr := table.Explain("api.example.com")
	if tr.Winner == nil {
		t.Fatal("expected wildcard match for api.example.com")
	}
	if tr.Winner.Pattern != "*.example.com" {
		t.Errorf("winner pattern = %q, want *.example.com", tr.Winner.Pattern)
	}

	// No match for unrelated domain
	tr2 := table.Explain("unrelated.test")
	if tr2.Winner != nil {
		t.Errorf("unexpected domain match: %v", tr2.Winner)
	}
}
