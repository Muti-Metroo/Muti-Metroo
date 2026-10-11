package routing

import (
	"net"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
)

// ============================================================================
// Route Tests
// ============================================================================

func TestRoute_String(t *testing.T) {
	agentID, _ := identity.NewAgentID()
	route := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     agentID,
		OriginAgent: agentID,
		Metric:      10,
	}

	str := route.String()
	if str == "" {
		t.Error("String() should not be empty")
	}
}

func TestRoute_Clone(t *testing.T) {
	agentID, _ := identity.NewAgentID()
	original := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     agentID,
		OriginAgent: agentID,
		Metric:      10,
		Path:        []identity.AgentID{agentID},
		Sequence:    42,
	}

	clone := original.Clone()

	// Verify values are equal
	if clone.Network.String() != original.Network.String() {
		t.Error("Network should be equal")
	}
	if clone.NextHop != original.NextHop {
		t.Error("NextHop should be equal")
	}
	if clone.Metric != original.Metric {
		t.Errorf("Metric = %d, want %d", clone.Metric, original.Metric)
	}

	// Verify it's a deep copy
	clone.Metric = 999
	if original.Metric == 999 {
		t.Error("Clone should not affect original")
	}
}

// ============================================================================
// Table Tests
// ============================================================================

func TestNewTable(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewTable(localID)

	if table == nil {
		t.Fatal("NewTable returned nil")
	}
	if table.Size() != 0 {
		t.Errorf("Initial Size = %d, want 0", table.Size())
	}
}

func TestTable_AddRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	route := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
		Sequence:    1,
	}

	if !table.AddRoute(route) {
		t.Error("AddRoute should return true for new route")
	}
	if table.Size() != 1 {
		t.Errorf("Size = %d, want 1", table.Size())
	}
}

func TestTable_AddRoute_NilRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewTable(localID)

	if table.AddRoute(nil) {
		t.Error("AddRoute(nil) should return false")
	}
}

func TestTable_AddRoute_LoopDetection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Route with our ID in the path (loop)
	route := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
		Path:        []identity.AgentID{peerID, localID}, // Our ID in path!
		Sequence:    1,
	}

	if table.AddRoute(route) {
		t.Error("AddRoute should reject route with loop")
	}
}

func TestTable_AddRoute_UpdateBetterMetric(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Add initial route
	route1 := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Add better route from same origin
	route2 := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      5, // Better metric
		Sequence:    1,
	}

	if !table.AddRoute(route2) {
		t.Error("AddRoute should accept route with better metric")
	}

	// Verify the metric was updated
	result := table.GetRoute(MustParseCIDR("10.0.0.0/8"))
	if result.Metric != 5 {
		t.Errorf("Metric = %d, want 5", result.Metric)
	}
}

func TestTable_AddRoute_UpdateNewerSequence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Add initial route
	route1 := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Add route with newer sequence (even with worse metric)
	route2 := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      20, // Worse metric
		Sequence:    2,  // But newer sequence
	}

	if !table.AddRoute(route2) {
		t.Error("AddRoute should accept route with newer sequence")
	}

	result := table.GetRoute(MustParseCIDR("10.0.0.0/8"))
	if result.Sequence != 2 {
		t.Errorf("Sequence = %d, want 2", result.Sequence)
	}
}

func TestTable_AddRoute_MultipleOrigins(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Add route from peer1
	route1 := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peer1,
		OriginAgent: peer1,
		Metric:      10,
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Add same network from peer2
	route2 := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peer2,
		OriginAgent: peer2,
		Metric:      5, // Better metric
		Sequence:    1,
	}
	table.AddRoute(route2)

	// Should have 2 route entries for this prefix
	if table.TotalRoutes() != 2 {
		t.Errorf("TotalRoutes = %d, want 2", table.TotalRoutes())
	}

	// Best route should be from peer2 (lower metric)
	best := table.GetRoute(MustParseCIDR("10.0.0.0/8"))
	if best.NextHop != peer2 {
		t.Error("Best route should be from peer2")
	}
}

func TestTable_RemoveRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	network := MustParseCIDR("10.0.0.0/8")
	route := &Route{
		Network:     network,
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
		Sequence:    1,
	}
	table.AddRoute(route)

	if !table.RemoveRoute(network, peerID) {
		t.Error("RemoveRoute should return true")
	}
	if table.Size() != 0 {
		t.Errorf("Size after remove = %d, want 0", table.Size())
	}
}

func TestTable_RemoveRoute_NotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	unknownID, _ := identity.NewAgentID()
	table := NewTable(localID)

	if table.RemoveRoute(MustParseCIDR("10.0.0.0/8"), unknownID) {
		t.Error("RemoveRoute should return false for non-existent route")
	}
}

func TestTable_RemoveRoutesFromPeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Add routes from peer1
	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peer1,
		OriginAgent: peer1,
		Metric:      10,
	})
	table.AddRoute(&Route{
		Network:     MustParseCIDR("172.16.0.0/12"),
		NextHop:     peer1,
		OriginAgent: peer1,
		Metric:      10,
	})

	// Add route from peer2
	table.AddRoute(&Route{
		Network:     MustParseCIDR("192.168.0.0/16"),
		NextHop:     peer2,
		OriginAgent: peer2,
		Metric:      10,
	})

	// Remove all routes from peer1
	removed := table.RemoveRoutesFromPeer(peer1)
	if len(removed) != 2 {
		t.Errorf("RemoveRoutesFromPeer removed %d routes, want 2", len(removed))
	}
	if table.Size() != 1 {
		t.Errorf("Size = %d, want 1", table.Size())
	}
}

// ============================================================================
// Lookup Tests (LPM)
// ============================================================================

func TestTable_Lookup_ExactMatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.1.2.0/24"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})

	result := table.Lookup(net.ParseIP("10.1.2.100"))
	if result == nil {
		t.Fatal("Lookup should find route")
	}
	if result.Network.String() != "10.1.2.0/24" {
		t.Errorf("Network = %s, want 10.1.2.0/24", result.Network.String())
	}
}

func TestTable_Lookup_LongestPrefixMatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Add /8 route
	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peer1,
		OriginAgent: peer1,
		Metric:      10,
	})

	// Add more specific /24 route
	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.1.2.0/24"),
		NextHop:     peer2,
		OriginAgent: peer2,
		Metric:      10,
	})

	// Should match /24 (longest prefix)
	result := table.Lookup(net.ParseIP("10.1.2.100"))
	if result == nil {
		t.Fatal("Lookup should find route")
	}
	if result.NextHop != peer2 {
		t.Error("Should match more specific /24 route")
	}

	// Should match /8 for other addresses
	result2 := table.Lookup(net.ParseIP("10.5.5.5"))
	if result2 == nil {
		t.Fatal("Lookup should find route")
	}
	if result2.NextHop != peer1 {
		t.Error("Should match /8 route")
	}
}

func TestTable_Lookup_NoMatch(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})

	result := table.Lookup(net.ParseIP("192.168.1.1"))
	if result != nil {
		t.Error("Lookup should return nil for non-matching IP")
	}
}

func TestTable_Lookup_DefaultRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	// Add default route
	table.AddRoute(&Route{
		Network:     MustParseCIDR("0.0.0.0/0"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})

	// Any IP should match
	result := table.Lookup(net.ParseIP("8.8.8.8"))
	if result == nil {
		t.Fatal("Default route should match any IP")
	}
}

func TestTable_LookupAll(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	table := NewTable(localID)

	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peer1,
		OriginAgent: peer1,
		Metric:      10,
	})
	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.1.0.0/16"),
		NextHop:     peer2,
		OriginAgent: peer2,
		Metric:      5,
	})

	results := table.LookupAll(net.ParseIP("10.1.2.3"))
	if len(results) != 2 {
		t.Errorf("LookupAll returned %d results, want 2", len(results))
	}

	// First should be /16 (longest prefix)
	if results[0].Network.String() != "10.1.0.0/16" {
		t.Error("First result should be /16")
	}
}

// ============================================================================
// Manager Tests
// ============================================================================

func TestNewManager(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.Size() != 0 {
		t.Errorf("Initial Size = %d, want 0", mgr.Size())
	}
}

func TestManager_AddLocalRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	if !mgr.AddLocalRoute(network, 10, "") {
		t.Error("AddLocalRoute should return true")
	}

	routes := mgr.GetLocalRoutes()
	if len(routes) != 1 {
		t.Errorf("GetLocalRoutes = %d routes, want 1", len(routes))
	}
}

func TestManager_RemoveLocalRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	mgr.AddLocalRoute(network, 10, "")

	if !mgr.RemoveLocalRoute(network) {
		t.Error("RemoveLocalRoute should return true")
	}

	routes := mgr.GetLocalRoutes()
	if len(routes) != 0 {
		t.Errorf("GetLocalRoutes after remove = %d, want 0", len(routes))
	}
}

func TestManager_ProcessRouteAdvertise(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	entries := []RouteEntry{
		{Network: MustParseCIDR("10.0.0.0/8"), Metric: 10},
		{Network: MustParseCIDR("172.16.0.0/12"), Metric: 5},
	}

	accepted := mgr.ProcessRouteAdvertise(peerID, peerID, 1, entries, nil)
	if len(accepted) != 2 {
		t.Errorf("ProcessRouteAdvertise accepted %d routes, want 2", len(accepted))
	}

	// Verify routes were added with incremented metric
	route := mgr.Lookup(net.ParseIP("10.0.0.1"))
	if route == nil {
		t.Fatal("Route should exist")
	}
	if route.Metric != 11 { // Original 10 + 1
		t.Errorf("Metric = %d, want 11", route.Metric)
	}
}

func TestManager_ProcessRouteWithdraw(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	// Add routes
	entries := []RouteEntry{
		{Network: MustParseCIDR("10.0.0.0/8"), Metric: 10},
	}
	mgr.ProcessRouteAdvertise(peerID, peerID, 1, entries, nil)

	// Withdraw
	removed := mgr.ProcessRouteWithdraw(peerID, 1, entries)
	if !removed {
		t.Error("ProcessRouteWithdraw should return true")
	}

	if mgr.Size() != 0 {
		t.Errorf("Size after withdraw = %d, want 0", mgr.Size())
	}
}

func TestManager_ProcessForwardRouteWithdraw(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	originA, _ := identity.NewAgentID()
	originB, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	mgr.ProcessForwardRouteAdvertise(peerID, originA, 1,
		[]ForwardRouteEntry{{Key: "k", Target: "127.0.0.1:9000"}}, nil)
	if !mgr.ForwardTable().HasRoute("k", originA) {
		t.Fatal("setup: expected forward route to exist")
	}

	// Origin-mismatch: withdraw from a different origin must not drop originA's row.
	if mgr.ProcessForwardRouteWithdraw(originB, 1, []ForwardRouteEntry{{Key: "k"}}) {
		t.Error("withdraw from wrong origin should return false")
	}
	if !mgr.ForwardTable().HasRoute("k", originA) {
		t.Error("originA's row was dropped by a different origin's withdraw")
	}

	// Correct origin removes the row.
	if !mgr.ProcessForwardRouteWithdraw(originA, 1, []ForwardRouteEntry{{Key: "k"}}) {
		t.Error("withdraw should report removed=true")
	}
	if mgr.ForwardTable().HasRoute("k", originA) {
		t.Error("forward route still present after withdraw")
	}

	// Negative: withdrawing an unknown key returns false.
	if mgr.ProcessForwardRouteWithdraw(originA, 1, []ForwardRouteEntry{{Key: "missing"}}) {
		t.Error("withdraw of unknown key should return false")
	}
}

func TestManager_ProcessDomainRouteWithdraw(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	originA, _ := identity.NewAgentID()
	originB, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	mgr.ProcessDomainRouteAdvertise(peerID, originA, 1,
		[]DomainRouteEntry{{Pattern: "*.example.com", IsWildcard: true}}, nil)
	if !mgr.DomainTable().HasRoute("*.example.com", originA) {
		t.Fatal("setup: expected domain route to exist")
	}

	if mgr.ProcessDomainRouteWithdraw(originB, 1, []DomainRouteEntry{{Pattern: "*.example.com"}}) {
		t.Error("withdraw from wrong origin should return false")
	}
	if !mgr.DomainTable().HasRoute("*.example.com", originA) {
		t.Error("originA's row was dropped by a different origin's withdraw")
	}

	if !mgr.ProcessDomainRouteWithdraw(originA, 1, []DomainRouteEntry{{Pattern: "*.example.com"}}) {
		t.Error("withdraw should report removed=true")
	}
	if mgr.DomainTable().HasRoute("*.example.com", originA) {
		t.Error("domain route still present after withdraw")
	}

	if mgr.ProcessDomainRouteWithdraw(originA, 1, []DomainRouteEntry{{Pattern: "missing.com"}}) {
		t.Error("withdraw of unknown pattern should return false")
	}
}

func TestManager_HandlePeerDisconnect(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	// Add routes from peer
	entries := []RouteEntry{
		{Network: MustParseCIDR("10.0.0.0/8"), Metric: 10},
		{Network: MustParseCIDR("172.16.0.0/12"), Metric: 5},
	}
	mgr.ProcessRouteAdvertise(peerID, peerID, 1, entries, nil)

	removed := mgr.HandlePeerDisconnect(peerID)
	if len(removed) != 2 {
		t.Errorf("HandlePeerDisconnect removed %d routes, want 2", len(removed))
	}
}

func TestManager_LookupNextHop(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	entries := []RouteEntry{
		{Network: MustParseCIDR("10.0.0.0/8"), Metric: 10},
	}
	mgr.ProcessRouteAdvertise(peerID, peerID, 1, entries, nil)

	nextHop, found := mgr.LookupNextHop(net.ParseIP("10.1.2.3"))
	if !found {
		t.Error("LookupNextHop should find route")
	}
	if nextHop != peerID {
		t.Error("NextHop should be peerID")
	}
}

func TestManager_LookupNextHop_NotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	_, found := mgr.LookupNextHop(net.ParseIP("10.1.2.3"))
	if found {
		t.Error("LookupNextHop should not find route")
	}
}

func TestManager_Subscribe(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	ch := make(chan RouteChange, 10)
	mgr.Subscribe(ch)

	// Add a local route
	if !mgr.AddLocalRoute(MustParseCIDR("10.0.0.0/8"), 10, "") {
		t.Fatal("AddLocalRoute should return true")
	}

	// Should receive notification (allow a small delay)
	select {
	case change := <-ch:
		if change.Type != RouteAdded {
			t.Errorf("Change type = %v, want RouteAdded", change.Type)
		}
	default:
		t.Error("Should receive route change notification")
	}
}

func TestManager_GetRoutesToAdvertise(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	// Add local route
	if !mgr.AddLocalRoute(MustParseCIDR("10.0.0.0/8"), 10, "") {
		t.Fatal("AddLocalRoute should return true")
	}

	// Add route from peer1
	mgr.ProcessRouteAdvertise(peer1, peer1, 1, []RouteEntry{
		{Network: MustParseCIDR("172.16.0.0/12"), Metric: 5},
	}, nil)

	// Get routes to advertise to peer1 (should exclude route learned from peer1)
	entries := mgr.GetRoutesToAdvertise(peer1)
	if len(entries) != 1 {
		t.Errorf("GetRoutesToAdvertise returned %d entries, want 1", len(entries))
		return
	}
	if entries[0].Network.String() != "10.0.0.0/8" {
		t.Error("Should only include local route, not route from peer1")
	}

	// Get routes to advertise to peer2 (should include both)
	entries2 := mgr.GetRoutesToAdvertise(peer2)
	if len(entries2) != 2 {
		t.Errorf("GetRoutesToAdvertise for peer2 returned %d entries, want 2", len(entries2))
	}
}

// ============================================================================
// CIDR Parse Tests
// ============================================================================

func TestParseCIDR(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"10.0.0.0/8", false},
		{"192.168.1.0/24", false},
		{"0.0.0.0/0", false},
		{"2001:db8::/32", false},
		{"invalid", true},
		{"10.0.0.0", true},
		{"", true},
	}

	for _, tt := range tests {
		network, err := ParseCIDR(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseCIDR(%q) should return error", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseCIDR(%q) error: %v", tt.input, err)
			}
			if network == nil {
				t.Errorf("ParseCIDR(%q) returned nil network", tt.input)
			}
		}
	}
}

func TestMustParseCIDR_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseCIDR should panic on invalid CIDR")
		}
	}()
	MustParseCIDR("invalid")
}

// ============================================================================
// IPv6 Tests
// ============================================================================

func TestTable_Lookup_IPv6(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	table.AddRoute(&Route{
		Network:     MustParseCIDR("2001:db8::/32"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})

	result := table.Lookup(net.ParseIP("2001:db8::1"))
	if result == nil {
		t.Fatal("Lookup should find IPv6 route")
	}
}

// ============================================================================
// Table Utility Tests
// ============================================================================

func TestTable_Clear(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})

	table.Clear()
	if table.Size() != 0 {
		t.Errorf("Size after Clear = %d, want 0", table.Size())
	}
}

func TestTable_HasRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	network := MustParseCIDR("10.0.0.0/8")
	table.AddRoute(&Route{
		Network:     network,
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})

	if !table.HasRoute(network, peerID) {
		t.Error("HasRoute should return true")
	}

	unknownID, _ := identity.NewAgentID()
	if table.HasRoute(network, unknownID) {
		t.Error("HasRoute should return false for unknown origin")
	}
}

func TestTable_GetAllRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	table.AddRoute(&Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      10,
	})
	table.AddRoute(&Route{
		Network:     MustParseCIDR("172.16.0.0/12"),
		NextHop:     peerID,
		OriginAgent: peerID,
		Metric:      5,
	})

	all := table.GetAllRoutes()
	if len(all) != 2 {
		t.Errorf("GetAllRoutes returned %d routes, want 2", len(all))
	}
}

// ============================================================================
// Dynamic Route Tests
// ============================================================================

func TestManager_AddDynamicRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	if err := mgr.AddDynamicRoute(network, 0, ""); err != nil {
		t.Fatalf("AddDynamicRoute() error = %v", err)
	}

	// Verify in dynamic routes
	dynamic := mgr.GetDynamicRoutes()
	if len(dynamic) != 1 {
		t.Fatalf("GetDynamicRoutes() len = %d, want 1", len(dynamic))
	}
	if dynamic[0].Network.String() != "10.0.0.0/8" {
		t.Errorf("Network = %s, want 10.0.0.0/8", dynamic[0].Network.String())
	}

	// Verify in local routes (for advertisement)
	local := mgr.GetLocalRoutes()
	if len(local) != 1 {
		t.Fatalf("GetLocalRoutes() len = %d, want 1", len(local))
	}

	// Verify in routing table
	route := mgr.Lookup(net.ParseIP("10.1.2.3"))
	if route == nil {
		t.Fatal("Lookup should find route for 10.1.2.3")
	}
}

func TestManager_AddDynamicRoute_ConfigRouteConflict(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	mgr.AddLocalRoute(network, 0, "") // Config route

	err := mgr.AddDynamicRoute(network, 0, "")
	if err == nil {
		t.Fatal("AddDynamicRoute() should error for config route conflict")
	}
}

func TestManager_AddDynamicRoute_UpdateMetric(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	if err := mgr.AddDynamicRoute(network, 5, ""); err != nil {
		t.Fatalf("AddDynamicRoute() error = %v", err)
	}

	// Re-add with different metric
	if err := mgr.AddDynamicRoute(network, 10, ""); err != nil {
		t.Fatalf("AddDynamicRoute() update error = %v", err)
	}

	dynamic := mgr.GetDynamicRoutes()
	if len(dynamic) != 1 {
		t.Fatalf("GetDynamicRoutes() len = %d, want 1", len(dynamic))
	}
	if dynamic[0].Metric != 10 {
		t.Errorf("Metric = %d, want 10", dynamic[0].Metric)
	}
}

func TestManager_RemoveDynamicRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	mgr.AddDynamicRoute(network, 0, "")

	if err := mgr.RemoveDynamicRoute(network); err != nil {
		t.Fatalf("RemoveDynamicRoute() error = %v", err)
	}

	if len(mgr.GetDynamicRoutes()) != 0 {
		t.Error("GetDynamicRoutes() should be empty after remove")
	}
	if len(mgr.GetLocalRoutes()) != 0 {
		t.Error("GetLocalRoutes() should be empty after remove")
	}
	if mgr.Lookup(net.ParseIP("10.1.2.3")) != nil {
		t.Error("Lookup should return nil after remove")
	}
}

func TestManager_RemoveDynamicRoute_ConfigRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	mgr.AddLocalRoute(network, 0, "") // Config route

	err := mgr.RemoveDynamicRoute(network)
	if err == nil {
		t.Fatal("RemoveDynamicRoute() should error for config route")
	}
}

func TestManager_RemoveDynamicRoute_NotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	network := MustParseCIDR("10.0.0.0/8")
	err := mgr.RemoveDynamicRoute(network)
	if err == nil {
		t.Fatal("RemoveDynamicRoute() should error for non-existent route")
	}
}

func TestManager_GetDynamicRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	mgr.AddDynamicRoute(MustParseCIDR("10.0.0.0/8"), 0, "")
	mgr.AddDynamicRoute(MustParseCIDR("172.16.0.0/12"), 5, "")

	routes := mgr.GetDynamicRoutes()
	if len(routes) != 2 {
		t.Fatalf("GetDynamicRoutes() len = %d, want 2", len(routes))
	}
}

func TestManager_HasDynamicRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	if mgr.HasDynamicRoutes() {
		t.Error("HasDynamicRoutes() should be false initially")
	}

	network := MustParseCIDR("10.0.0.0/8")
	mgr.AddDynamicRoute(network, 0, "")

	if !mgr.HasDynamicRoutes() {
		t.Error("HasDynamicRoutes() should be true after add")
	}

	mgr.RemoveDynamicRoute(network)

	if mgr.HasDynamicRoutes() {
		t.Error("HasDynamicRoutes() should be false after remove")
	}
}

func TestManager_DynamicRoutesInLocalRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	// Add a config route
	mgr.AddLocalRoute(MustParseCIDR("192.168.0.0/16"), 0, "")

	// Add a dynamic route
	mgr.AddDynamicRoute(MustParseCIDR("10.0.0.0/8"), 0, "")

	// Both should appear in local routes
	local := mgr.GetLocalRoutes()
	if len(local) != 2 {
		t.Fatalf("GetLocalRoutes() len = %d, want 2", len(local))
	}

	// Only dynamic should appear in dynamic routes
	dynamic := mgr.GetDynamicRoutes()
	if len(dynamic) != 1 {
		t.Fatalf("GetDynamicRoutes() len = %d, want 1", len(dynamic))
	}
	if dynamic[0].Network.String() != "10.0.0.0/8" {
		t.Errorf("Dynamic route = %s, want 10.0.0.0/8", dynamic[0].Network.String())
	}
}

func TestManager_IsDynamicRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	configNet := MustParseCIDR("192.168.0.0/16")
	dynamicNet := MustParseCIDR("10.0.0.0/8")

	mgr.AddLocalRoute(configNet, 0, "")
	mgr.AddDynamicRoute(dynamicNet, 0, "")

	if mgr.IsDynamicRoute(configNet) {
		t.Error("IsDynamicRoute() should be false for config route")
	}
	if !mgr.IsDynamicRoute(dynamicNet) {
		t.Error("IsDynamicRoute() should be true for dynamic route")
	}
	if mgr.IsDynamicRoute(nil) {
		t.Error("IsDynamicRoute(nil) should be false")
	}
}

// TestManager_RouteComments verifies comments flow through AddLocalRoute /
// AddDynamicRoute and that SetRouteComment edits both kinds. Comments live
// only on LocalRoute; they are never advertised over the wire.
func TestManager_RouteComments(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	cfgNet := MustParseCIDR("10.0.0.0/8")
	dynNet := MustParseCIDR("192.168.0.0/16")

	// Config-seeded route carries a comment from YAML.
	if !mgr.AddLocalRoute(cfgNet, 0, "office LAN") {
		t.Fatal("AddLocalRoute should return true")
	}

	// Dynamic route carries a comment from the management API.
	if err := mgr.AddDynamicRoute(dynNet, 0, "temp customer"); err != nil {
		t.Fatalf("AddDynamicRoute error = %v", err)
	}

	// GetLocalRoutes covers both; check both comments survive.
	commentByCIDR := map[string]string{}
	for _, r := range mgr.GetLocalRoutes() {
		commentByCIDR[r.Network.String()] = r.Comment
	}
	if commentByCIDR[cfgNet.String()] != "office LAN" {
		t.Errorf("config route Comment = %q, want %q", commentByCIDR[cfgNet.String()], "office LAN")
	}
	if commentByCIDR[dynNet.String()] != "temp customer" {
		t.Errorf("dynamic route Comment = %q, want %q", commentByCIDR[dynNet.String()], "temp customer")
	}

	// GetDynamicRoutes returns only the dynamic one and includes its comment.
	dyn := mgr.GetDynamicRoutes()
	if len(dyn) != 1 || dyn[0].Comment != "temp customer" {
		t.Errorf("GetDynamicRoutes = %+v, want one entry with Comment=temp customer", dyn)
	}

	if err := mgr.SetRouteComment(cfgNet, "renamed"); err != nil {
		t.Fatalf("SetRouteComment(config) error = %v", err)
	}
	if err := mgr.SetRouteComment(dynNet, "renamed dyn"); err != nil {
		t.Fatalf("SetRouteComment(dynamic) error = %v", err)
	}
	for _, r := range mgr.GetLocalRoutes() {
		commentByCIDR[r.Network.String()] = r.Comment
	}
	if commentByCIDR[cfgNet.String()] != "renamed" {
		t.Errorf("after override, config comment = %q, want %q", commentByCIDR[cfgNet.String()], "renamed")
	}
	if commentByCIDR[dynNet.String()] != "renamed dyn" {
		t.Errorf("after override, dynamic comment = %q, want %q", commentByCIDR[dynNet.String()], "renamed dyn")
	}

	// Empty string clears the comment.
	if err := mgr.SetRouteComment(cfgNet, ""); err != nil {
		t.Fatalf("SetRouteComment(clear) error = %v", err)
	}
	for _, r := range mgr.GetLocalRoutes() {
		if r.Network.String() == cfgNet.String() && r.Comment != "" {
			t.Errorf("after clear, comment = %q, want empty", r.Comment)
		}
	}

	missing := MustParseCIDR("172.16.0.0/12")
	if err := mgr.SetRouteComment(missing, "nope"); err == nil {
		t.Error("SetRouteComment(missing) error = nil, want error")
	}
}

// TestManager_GetCommentOverrides verifies the helper that journal compaction
// uses to capture runtime comment overrides on config-seeded routes. Dynamic
// routes are excluded because their full state is captured separately by
// GetDynamicRoutes.
func TestManager_GetCommentOverrides(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	cfgNet := MustParseCIDR("10.0.0.0/8")
	dynNet := MustParseCIDR("192.168.0.0/16")

	if !mgr.AddLocalRoute(cfgNet, 0, "office LAN") {
		t.Fatal("AddLocalRoute should return true")
	}
	if err := mgr.AddDynamicRoute(dynNet, 0, "temp customer"); err != nil {
		t.Fatalf("AddDynamicRoute error = %v", err)
	}

	if got := mgr.GetCommentOverrides(); len(got) != 0 {
		t.Errorf("GetCommentOverrides at baseline = %v, want empty", got)
	}

	// Override the config-seeded route.
	if err := mgr.SetRouteComment(cfgNet, "renamed"); err != nil {
		t.Fatalf("SetRouteComment(cfg) error = %v", err)
	}
	got := mgr.GetCommentOverrides()
	if len(got) != 1 || got[cfgNet.String()] != "renamed" {
		t.Errorf("GetCommentOverrides after override = %v, want {%s: \"renamed\"}", got, cfgNet)
	}

	// Override the dynamic route too. It must NOT appear in overrides
	// because GetDynamicRoutes already covers it.
	if err := mgr.SetRouteComment(dynNet, "renamed dyn"); err != nil {
		t.Fatalf("SetRouteComment(dyn) error = %v", err)
	}
	got = mgr.GetCommentOverrides()
	if _, ok := got[dynNet.String()]; ok {
		t.Errorf("GetCommentOverrides should not include dynamic route, got %v", got)
	}
	if got[cfgNet.String()] != "renamed" {
		t.Errorf("GetCommentOverrides[%s] = %q, want %q", cfgNet, got[cfgNet.String()], "renamed")
	}

	// Reset config-seeded comment back to baseline -- override disappears.
	if err := mgr.SetRouteComment(cfgNet, "office LAN"); err != nil {
		t.Fatalf("SetRouteComment(restore) error = %v", err)
	}
	if got := mgr.GetCommentOverrides(); len(got) != 0 {
		t.Errorf("GetCommentOverrides after restore = %v, want empty", got)
	}

	// Clearing a config-seeded comment is itself an override worth
	// preserving (current "" differs from baseline "office LAN").
	if err := mgr.SetRouteComment(cfgNet, ""); err != nil {
		t.Fatalf("SetRouteComment(clear) error = %v", err)
	}
	got = mgr.GetCommentOverrides()
	if v, ok := got[cfgNet.String()]; !ok || v != "" {
		t.Errorf("GetCommentOverrides after clear = %v, want %s -> \"\"", got, cfgNet)
	}
}

// TestRoute_Clone_CopiesComment ensures the Comment field survives Clone
// — the cloned route is what the table stores so its comment must equal
// the input.
func TestRoute_Clone_CopiesComment(t *testing.T) {
	id, _ := identity.NewAgentID()
	r := &Route{
		Network:     MustParseCIDR("10.0.0.0/8"),
		NextHop:     id,
		OriginAgent: id,
		Metric:      5,
		Sequence:    1,
		Comment:     "operator note",
	}
	clone := r.Clone()
	if clone.Comment != "operator note" {
		t.Errorf("Clone.Comment = %q, want %q", clone.Comment, "operator note")
	}
}

// TestTable_AddRoute_SameSequenceCommentRefresh checks the in-place
// comment refresh for same-sequence/same-metric re-advertisements.
// AddRoute returns false (route selection unchanged) but the stored
// Comment is updated so dashboards reflect the latest operator
// metadata.
func TestTable_AddRoute_SameSequenceCommentRefresh(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	table := NewTable(localID)

	first := &Route{
		Network: MustParseCIDR("10.0.0.0/8"), NextHop: peerID, OriginAgent: peerID,
		Metric: 5, Sequence: 1, Comment: "initial",
	}
	if !table.AddRoute(first) {
		t.Fatal("AddRoute(first) should return true")
	}

	// Same origin, same sequence, same metric, but new comment.
	refresh := &Route{
		Network: MustParseCIDR("10.0.0.0/8"), NextHop: peerID, OriginAgent: peerID,
		Metric: 5, Sequence: 1, Comment: "updated",
	}
	if table.AddRoute(refresh) {
		t.Error("AddRoute should return false on same-sequence comment-only refresh (no route change)")
	}

	got := table.GetRoute(MustParseCIDR("10.0.0.0/8"))
	if got == nil {
		t.Fatal("route disappeared")
	}
	if got.Comment != "updated" {
		t.Errorf("Comment = %q, want %q (in-place refresh)", got.Comment, "updated")
	}
	// Sequence and metric unchanged.
	if got.Sequence != 1 || got.Metric != 5 {
		t.Errorf("metric/sequence drift: got Sequence=%d Metric=%d", got.Sequence, got.Metric)
	}
}

// A withdraw carrying a sequence older than the installed route's sequence
// must be rejected per route; one at or above the installed sequence applies.
func TestManager_ProcessRouteWithdraw_SequenceGuard(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	entries := []RouteEntry{{Network: MustParseCIDR("10.0.0.0/8"), Metric: 10}}
	mgr.ProcessRouteAdvertise(peerID, peerID, 5, entries, nil)

	if mgr.ProcessRouteWithdraw(peerID, 4, entries) {
		t.Error("stale withdraw (seq 4 < installed 5) should not remove the route")
	}
	if mgr.Size() != 1 {
		t.Fatal("route was removed by a stale withdraw")
	}

	if !mgr.ProcessRouteWithdraw(peerID, 5, entries) {
		t.Error("withdraw at the installed sequence should remove the route")
	}
	if mgr.Size() != 0 {
		t.Error("route still present after in-sequence withdraw")
	}
}

func TestManager_ProcessDomainRouteWithdraw_SequenceGuard(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	entries := []DomainRouteEntry{{Pattern: "example.com"}}
	mgr.ProcessDomainRouteAdvertise(peerID, peerID, 5, entries, nil)

	if mgr.ProcessDomainRouteWithdraw(peerID, 4, entries) {
		t.Error("stale withdraw should not remove the domain route")
	}
	if !mgr.DomainTable().HasRoute("example.com", peerID) {
		t.Fatal("domain route was removed by a stale withdraw")
	}

	if !mgr.ProcessDomainRouteWithdraw(peerID, 6, entries) {
		t.Error("newer withdraw should remove the domain route")
	}
}

func TestManager_ProcessForwardRouteWithdraw_SequenceGuard(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	entries := []ForwardRouteEntry{{Key: "ssh", Target: "10.0.0.1:22"}}
	mgr.ProcessForwardRouteAdvertise(peerID, peerID, 5, entries, nil)

	if mgr.ProcessForwardRouteWithdraw(peerID, 4, entries) {
		t.Error("stale withdraw should not remove the forward route")
	}
	if !mgr.ForwardTable().HasRoute("ssh", peerID) {
		t.Fatal("forward route was removed by a stale withdraw")
	}

	if !mgr.ProcessForwardRouteWithdraw(peerID, 5, entries) {
		t.Error("in-sequence withdraw should remove the forward route")
	}
}

func TestManager_ProcessAgentRouteWithdraw_SequenceGuard(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	mgr.ProcessAgentRouteAdvertise(peerID, originID, 5, originID, []identity.AgentID{peerID, originID}, 1)

	if mgr.ProcessAgentRouteWithdraw(originID, 4, originID) {
		t.Error("stale withdraw should not remove the agent route")
	}
	if mgr.LookupAgent(originID) == nil {
		t.Fatal("agent route was removed by a stale withdraw")
	}

	if !mgr.ProcessAgentRouteWithdraw(originID, 5, originID) {
		t.Error("in-sequence withdraw should remove the agent route")
	}
	if mgr.LookupAgent(originID) != nil {
		t.Error("agent route still present after in-sequence withdraw")
	}

	// Withdrawing an unknown agent returns false.
	if mgr.ProcessAgentRouteWithdraw(originID, 9, originID) {
		t.Error("withdraw of unknown agent route should return false")
	}
}
