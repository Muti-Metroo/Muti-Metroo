package routing

import (
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

func TestAgentRoute_String(t *testing.T) {
	agentID, _ := identity.NewAgentID()
	route := &AgentRoute{
		AgentID:     agentID,
		NextHop:     agentID,
		OriginAgent: agentID,
		Metric:      10,
	}

	str := route.String()
	if str == "" {
		t.Error("String() should not be empty")
	}
}

func TestAgentRoute_Clone(t *testing.T) {
	agentID, _ := identity.NewAgentID()
	original := &AgentRoute{
		AgentID:     agentID,
		NextHop:     agentID,
		OriginAgent: agentID,
		Metric:      10,
		Path:        []identity.AgentID{agentID},
		Sequence:    42,
		EncPath: &protocol.EncryptedData{
			Encrypted: false,
			Data:      []byte("test"),
		},
	}

	clone := original.Clone()

	if clone.AgentID != original.AgentID {
		t.Error("AgentID should be equal")
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

	// Verify Path is deep copied
	clone.Path[0] = identity.AgentID{}
	if original.Path[0] == (identity.AgentID{}) {
		t.Error("Clone path should not affect original path")
	}

	// Verify EncPath is deep copied
	clone.EncPath.Data[0] = 0xFF
	if original.EncPath.Data[0] == 0xFF {
		t.Error("Clone EncPath should not affect original EncPath")
	}
}

func TestAgentTable_AddAndLookup(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	route := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	}

	if !table.AddRoute(route) {
		t.Error("AddRoute should return true for new route")
	}

	found := table.Lookup(agentA)
	if found == nil {
		t.Fatal("Lookup should find the route")
	}
	if found.AgentID != agentA {
		t.Error("AgentID should match")
	}
	if found.Metric != 1 {
		t.Errorf("Metric = %d, want 1", found.Metric)
	}
}

func TestAgentTable_LoopDetection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	// Route with localID in path should be rejected
	route := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, localID, agentA},
		Sequence:    1,
	}

	if table.AddRoute(route) {
		t.Error("AddRoute should reject route with localID in path")
	}

	if table.Size() != 0 {
		t.Error("Table should be empty after rejected route")
	}
}

func TestAgentTable_MetricSelection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()

	// Add route with metric 5
	route1 := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      5,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Add route with lower metric 2 via different next hop
	route2 := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerC,
		OriginAgent: agentA,
		Metric:      2,
		Path:        []identity.AgentID{peerC, agentA},
		Sequence:    1,
	}
	table.AddRoute(route2)

	found := table.Lookup(agentA)
	if found == nil {
		t.Fatal("Lookup should find the route")
	}
	if found.Metric != 2 {
		t.Errorf("Metric = %d, want 2 (best route)", found.Metric)
	}
	if found.NextHop != peerC {
		t.Error("NextHop should be peerC (best route)")
	}
}

func TestAgentTable_SequenceUpdate(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	// Add route with sequence 1
	route1 := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      5,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Update with higher sequence but higher metric - should update
	route2 := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      10,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    2,
	}
	if !table.AddRoute(route2) {
		t.Error("Should accept newer sequence")
	}

	found := table.Lookup(agentA)
	if found.Metric != 10 {
		t.Errorf("Metric = %d, want 10 (updated)", found.Metric)
	}

	// Try with older sequence - should reject
	route3 := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	}
	if table.AddRoute(route3) {
		t.Error("Should reject older sequence")
	}
}

func TestAgentTable_RemoveRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	route := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	}
	table.AddRoute(route)

	if !table.RemoveRoute(agentA, agentA) {
		t.Error("RemoveRoute should return true")
	}
	if table.Lookup(agentA) != nil {
		t.Error("Lookup should return nil after removal")
	}
	if table.Size() != 0 {
		t.Errorf("Size = %d, want 0", table.Size())
	}
}

func TestAgentTable_RemoveRoutesFromPeer(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	agentB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()
	peerD, _ := identity.NewAgentID()

	// Add routes via peerC
	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerC,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerC, agentA},
		Sequence:    1,
	})
	table.AddRoute(&AgentRoute{
		AgentID:     agentB,
		NextHop:     peerC,
		OriginAgent: agentB,
		Metric:      2,
		Path:        []identity.AgentID{peerC, agentB},
		Sequence:    1,
	})

	// Add route via peerD
	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerD,
		OriginAgent: agentA,
		Metric:      3,
		Path:        []identity.AgentID{peerD, agentA},
		Sequence:    1,
	})

	removed := table.RemoveRoutesFromPeer(peerC)
	if removed != 2 {
		t.Errorf("RemoveRoutesFromPeer = %d, want 2", removed)
	}

	// agentA should still be reachable via peerD
	found := table.Lookup(agentA)
	if found == nil {
		t.Error("agentA should still be reachable via peerD")
	}
	if found.NextHop != peerD {
		t.Error("agentA NextHop should be peerD")
	}

	// agentB should be gone
	if table.Lookup(agentB) != nil {
		t.Error("agentB should not be reachable")
	}
}

func TestAgentTable_CleanupStaleRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	route := &AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	}
	table.AddRoute(route)

	// Make the route stale by backdating
	table.mu.Lock()
	for _, routes := range table.routes {
		for _, r := range routes {
			r.LastUpdate = time.Now().Add(-10 * time.Minute)
		}
	}
	table.mu.Unlock()

	removed := table.CleanupStaleRoutes(5 * time.Minute)
	if removed != 1 {
		t.Errorf("CleanupStaleRoutes = %d, want 1", removed)
	}
	if table.Size() != 0 {
		t.Error("Table should be empty after cleanup")
	}
}

func TestAgentTable_CleanupPreservesLocalRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	// Add local agent's own route (OriginAgent == localID)
	route := &AgentRoute{
		AgentID:     localID,
		NextHop:     localID,
		OriginAgent: localID,
		Metric:      0,
		Sequence:    1,
	}
	table.AddRoute(route)

	// Make it stale
	table.mu.Lock()
	for _, routes := range table.routes {
		for _, r := range routes {
			r.LastUpdate = time.Now().Add(-10 * time.Minute)
		}
	}
	table.mu.Unlock()

	removed := table.CleanupStaleRoutes(5 * time.Minute)
	if removed != 0 {
		t.Errorf("CleanupStaleRoutes = %d, want 0 (local routes preserved)", removed)
	}
	if table.Size() != 1 {
		t.Error("Local route should be preserved")
	}
}

func TestAgentTable_GetAllRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	agentB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()

	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerC,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerC, agentA},
		Sequence:    1,
	})
	table.AddRoute(&AgentRoute{
		AgentID:     agentB,
		NextHop:     peerC,
		OriginAgent: agentB,
		Metric:      2,
		Path:        []identity.AgentID{peerC, agentB},
		Sequence:    1,
	})

	all := table.GetAllRoutes()
	if len(all) != 2 {
		t.Errorf("GetAllRoutes len = %d, want 2", len(all))
	}
}

func TestAgentTable_GetAllAgentIDs(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	agentB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()

	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerC,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerC, agentA},
		Sequence:    1,
	})
	table.AddRoute(&AgentRoute{
		AgentID:     agentB,
		NextHop:     peerC,
		OriginAgent: agentB,
		Metric:      2,
		Path:        []identity.AgentID{peerC, agentB},
		Sequence:    1,
	})

	ids := table.GetAllAgentIDs()
	if len(ids) != 2 {
		t.Errorf("GetAllAgentIDs len = %d, want 2", len(ids))
	}
}

func TestAgentTable_SizeAndTotalRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()

	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	})
	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerC,
		OriginAgent: agentA,
		Metric:      3,
		Path:        []identity.AgentID{peerC, agentA},
		Sequence:    1,
	})

	if table.Size() != 1 {
		t.Errorf("Size = %d, want 1 (one unique agent)", table.Size())
	}
	if table.TotalRoutes() != 2 {
		t.Errorf("TotalRoutes = %d, want 2", table.TotalRoutes())
	}
}

func TestAgentTable_NilRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	if table.AddRoute(nil) {
		t.Error("AddRoute(nil) should return false")
	}
}

func TestAgentTable_Clear(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerB,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerB, agentA},
		Sequence:    1,
	})

	table.Clear()
	if table.Size() != 0 {
		t.Errorf("Size = %d after Clear, want 0", table.Size())
	}
}

func TestAgentTable_LookupNotFound(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	unknown, _ := identity.NewAgentID()
	if table.Lookup(unknown) != nil {
		t.Error("Lookup for unknown agent should return nil")
	}
}

func TestAgentTable_RemoveNonexistent(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	unknown, _ := identity.NewAgentID()
	if table.RemoveRoute(unknown, unknown) {
		t.Error("RemoveRoute for nonexistent route should return false")
	}
}

func TestAgentTable_GetRoutesForAgent(t *testing.T) {
	localID, _ := identity.NewAgentID()
	table := NewAgentTable(localID)

	agentA, _ := identity.NewAgentID()
	agentB, _ := identity.NewAgentID()
	peerC, _ := identity.NewAgentID()
	peerD, _ := identity.NewAgentID()

	// Add two routes to agentA via different hops
	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerC,
		OriginAgent: agentA,
		Metric:      1,
		Path:        []identity.AgentID{peerC, agentA},
		Sequence:    1,
	})
	table.AddRoute(&AgentRoute{
		AgentID:     agentA,
		NextHop:     peerD,
		OriginAgent: agentA,
		Metric:      3,
		Path:        []identity.AgentID{peerD, agentA},
		Sequence:    1,
	})

	// Add route to agentB
	table.AddRoute(&AgentRoute{
		AgentID:     agentB,
		NextHop:     peerC,
		OriginAgent: agentB,
		Metric:      2,
		Path:        []identity.AgentID{peerC, agentB},
		Sequence:    1,
	})

	// GetRoutesForAgent should return only routes for agentA
	routes := table.GetRoutesForAgent(agentA)
	if len(routes) != 2 {
		t.Fatalf("GetRoutesForAgent(agentA) len = %d, want 2", len(routes))
	}
	// Best route (metric 1) should be first
	if routes[0].Metric != 1 {
		t.Errorf("First route metric = %d, want 1", routes[0].Metric)
	}

	// GetRoutesForAgent for unknown should return nil
	unknown, _ := identity.NewAgentID()
	routes = table.GetRoutesForAgent(unknown)
	if len(routes) != 0 {
		t.Errorf("GetRoutesForAgent(unknown) len = %d, want 0", len(routes))
	}
}

func TestManager_ProcessAgentRouteAdvertise(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	path := []identity.AgentID{peerB, agentA}
	added := mgr.ProcessAgentRouteAdvertise(peerB, agentA, 1, agentA, path, nil, 1)
	if !added {
		t.Error("ProcessAgentRouteAdvertise should return true for new route")
	}

	// Verify lookup works
	route := mgr.LookupAgent(agentA)
	if route == nil {
		t.Fatal("LookupAgent should find the route")
	}
	if route.AgentID != agentA {
		t.Errorf("AgentID = %s, want %s", route.AgentID.ShortString(), agentA.ShortString())
	}
	if route.Metric != 1 {
		t.Errorf("Metric = %d, want 1", route.Metric)
	}

	// Verify HandlePeerDisconnectAgent
	removed := mgr.HandlePeerDisconnectAgent(peerB)
	if removed != 1 {
		t.Errorf("HandlePeerDisconnectAgent = %d, want 1", removed)
	}
	if mgr.LookupAgent(agentA) != nil {
		t.Error("LookupAgent should return nil after peer disconnect")
	}
}

func TestManager_CleanupStaleAgentRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	mgr := NewManager(localID)

	agentA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	mgr.ProcessAgentRouteAdvertise(peerB, agentA, 1, agentA, []identity.AgentID{peerB, agentA}, nil, 1)

	// Backdate the route
	agentTable := mgr.AgentTable()
	agentTable.mu.Lock()
	for _, routes := range agentTable.routes {
		for _, r := range routes {
			r.LastUpdate = r.LastUpdate.Add(-10 * time.Minute)
		}
	}
	agentTable.mu.Unlock()

	removed := mgr.CleanupStaleAgentRoutes(5 * time.Minute)
	if removed != 1 {
		t.Errorf("CleanupStaleAgentRoutes = %d, want 1", removed)
	}
}
