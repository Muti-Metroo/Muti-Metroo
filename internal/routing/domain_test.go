package routing

import (
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// mustNewAgentID creates a new AgentID or panics.
func mustNewAgentID() identity.AgentID {
	id, err := identity.NewAgentID()
	if err != nil {
		panic(err)
	}
	return id
}

func TestDomainTable_ExactMatch(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	route := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Sequence:    1,
	}

	if !table.AddRoute(route) {
		t.Fatal("AddRoute should return true for new route")
	}

	// Exact match
	result := table.Lookup("api.example.com")
	if result == nil {
		t.Fatal("Lookup should find exact match")
	}
	if result.Pattern != "api.example.com" {
		t.Errorf("Expected pattern 'api.example.com', got '%s'", result.Pattern)
	}

	// Case insensitive
	result = table.Lookup("API.EXAMPLE.COM")
	if result == nil {
		t.Fatal("Lookup should be case insensitive")
	}

	// No match for different domain
	result = table.Lookup("other.example.com")
	if result != nil {
		t.Error("Lookup should return nil for non-matching domain")
	}
}

func TestDomainTable_WildcardMatch(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	route := &DomainRoute{
		Pattern:     "*.example.com",
		IsWildcard:  true,
		BaseDomain:  "example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Sequence:    1,
	}

	if !table.AddRoute(route) {
		t.Fatal("AddRoute should return true for new route")
	}

	// Single-level wildcard match
	result := table.Lookup("foo.example.com")
	if result == nil {
		t.Fatal("Lookup should match single-level subdomain")
	}
	if result.Pattern != "*.example.com" {
		t.Errorf("Expected pattern '*.example.com', got '%s'", result.Pattern)
	}

	// Another single-level match
	result = table.Lookup("bar.example.com")
	if result == nil {
		t.Fatal("Lookup should match another single-level subdomain")
	}

	// Should NOT match the base domain itself
	result = table.Lookup("example.com")
	if result != nil {
		t.Error("Wildcard should not match base domain")
	}
}

func TestDomainTable_WildcardNoNestedMatch(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	route := &DomainRoute{
		Pattern:     "*.example.com",
		IsWildcard:  true,
		BaseDomain:  "example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Sequence:    1,
	}

	table.AddRoute(route)

	// Multi-level subdomain should NOT match (single-level wildcard only)
	result := table.Lookup("a.b.example.com")
	if result != nil {
		t.Error("Wildcard should NOT match multi-level subdomains")
	}

	result = table.Lookup("foo.bar.example.com")
	if result != nil {
		t.Error("Wildcard should NOT match multi-level subdomains")
	}
}

func TestDomainTable_ExactBeforeWildcard(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	// Add wildcard route
	wildcardRoute := &DomainRoute{
		Pattern:     "*.example.com",
		IsWildcard:  true,
		BaseDomain:  "example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Sequence:    1,
	}
	table.AddRoute(wildcardRoute)

	// Add exact route for specific subdomain
	exactRoute := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      2, // Higher metric
		Sequence:    2,
	}
	table.AddRoute(exactRoute)

	// Exact match should take precedence even with higher metric
	result := table.Lookup("api.example.com")
	if result == nil {
		t.Fatal("Lookup should find route")
	}
	if result.Pattern != "api.example.com" {
		t.Error("Exact match should take precedence over wildcard")
	}

	// Other subdomains should still match wildcard
	result = table.Lookup("other.example.com")
	if result == nil {
		t.Fatal("Lookup should find wildcard route")
	}
	if result.Pattern != "*.example.com" {
		t.Error("Should match wildcard for non-exact domain")
	}
}

func TestDomainTable_MultipleOrigins(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	origin1 := mustNewAgentID()
	origin2 := mustNewAgentID()
	nextHop1 := mustNewAgentID()
	nextHop2 := mustNewAgentID()

	// Add route from origin1 with higher metric
	route1 := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHop1,
		OriginAgent: origin1,
		Metric:      5,
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Add route from origin2 with lower metric
	route2 := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHop2,
		OriginAgent: origin2,
		Metric:      2,
		Sequence:    1,
	}
	table.AddRoute(route2)

	// Should return the lower metric route
	result := table.Lookup("api.example.com")
	if result == nil {
		t.Fatal("Lookup should find route")
	}
	if result.Metric != 2 {
		t.Errorf("Should return lowest metric route (2), got %d", result.Metric)
	}
	if result.OriginAgent != origin2 {
		t.Error("Should return route from origin2")
	}

	// Total routes should be 2
	if table.TotalRoutes() != 2 {
		t.Errorf("Expected 2 total routes, got %d", table.TotalRoutes())
	}
}

func TestDomainTable_MetricPriority(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	origin1 := mustNewAgentID()
	origin2 := mustNewAgentID()
	nextHop := mustNewAgentID()

	// Add high metric route first
	route1 := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHop,
		OriginAgent: origin1,
		Metric:      10,
		Sequence:    1,
	}
	table.AddRoute(route1)

	// Add low metric route second
	route2 := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHop,
		OriginAgent: origin2,
		Metric:      1,
		Sequence:    1,
	}
	table.AddRoute(route2)

	result := table.Lookup("api.example.com")
	if result == nil {
		t.Fatal("Lookup should find route")
	}
	if result.Metric != 1 {
		t.Errorf("Should return lowest metric (1), got %d", result.Metric)
	}
}

func TestDomainTable_LoopDetection(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	// Route with local ID in path (loop)
	route := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Path:        []identity.AgentID{localID, nextHopID}, // Contains local ID
		Sequence:    1,
	}

	if table.AddRoute(route) {
		t.Error("AddRoute should reject route with local ID in path (loop)")
	}

	if table.TotalRoutes() != 0 {
		t.Error("Table should be empty after rejecting loop")
	}
}

func TestDomainTable_RemoveRoute(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	route := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Sequence:    1,
	}
	table.AddRoute(route)

	// Remove the route
	if !table.RemoveRoute("api.example.com", originID) {
		t.Error("RemoveRoute should return true for existing route")
	}

	// Should not find route anymore
	result := table.Lookup("api.example.com")
	if result != nil {
		t.Error("Lookup should return nil after route removal")
	}
}

func TestDomainTable_RemoveRoutesFromPeer(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	peer1 := mustNewAgentID()
	peer2 := mustNewAgentID()
	origin1 := mustNewAgentID()
	origin2 := mustNewAgentID()

	// Add routes from different next hops
	route1 := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     peer1,
		OriginAgent: origin1,
		Metric:      1,
		Sequence:    1,
	}
	table.AddRoute(route1)

	route2 := &DomainRoute{
		Pattern:     "other.example.com",
		IsWildcard:  false,
		BaseDomain:  "other.example.com",
		NextHop:     peer2,
		OriginAgent: origin2,
		Metric:      1,
		Sequence:    1,
	}
	table.AddRoute(route2)

	// Remove routes from peer1
	removed := table.RemoveRoutesFromPeer(peer1)
	if removed != 1 {
		t.Errorf("Expected 1 route removed, got %d", removed)
	}

	// peer2's route should still exist
	result := table.Lookup("other.example.com")
	if result == nil {
		t.Error("Route from peer2 should still exist")
	}
}

func TestDomainTable_CleanupStaleRoutes(t *testing.T) {
	localID := mustNewAgentID()
	table := NewDomainTable(localID)

	originID := mustNewAgentID()
	nextHopID := mustNewAgentID()

	route := &DomainRoute{
		Pattern:     "api.example.com",
		IsWildcard:  false,
		BaseDomain:  "api.example.com",
		NextHop:     nextHopID,
		OriginAgent: originID,
		Metric:      1,
		Sequence:    1,
		LastUpdate:  time.Now().Add(-10 * time.Minute), // 10 minutes ago
	}

	// Manually set last update to simulate stale route
	table.mu.Lock()
	table.exactRoutes["api.example.com"] = []*DomainRoute{route}
	table.mu.Unlock()

	// Cleanup with 5 minute max age
	removed := table.CleanupStaleRoutes(5 * time.Minute)
	if removed != 1 {
		t.Errorf("Expected 1 stale route removed, got %d", removed)
	}

	result := table.Lookup("api.example.com")
	if result != nil {
		t.Error("Stale route should have been removed")
	}
}

func TestParseDomainPattern(t *testing.T) {
	tests := []struct {
		pattern    string
		isWildcard bool
		baseDomain string
	}{
		{"example.com", false, "example.com"},
		{"api.example.com", false, "api.example.com"},
		{"*.example.com", true, "example.com"},
		{"*.api.example.com", true, "api.example.com"},
	}

	for _, tt := range tests {
		isWildcard, baseDomain := ParseDomainPattern(tt.pattern)
		if isWildcard != tt.isWildcard {
			t.Errorf("ParseDomainPattern(%q): isWildcard = %v, want %v", tt.pattern, isWildcard, tt.isWildcard)
		}
		if baseDomain != tt.baseDomain {
			t.Errorf("ParseDomainPattern(%q): baseDomain = %q, want %q", tt.pattern, baseDomain, tt.baseDomain)
		}
	}
}

func TestValidateDomainPattern(t *testing.T) {
	validPatterns := []string{
		"example.com",
		"api.example.com",
		"sub.api.example.com",
		"*.example.com",
		"*.api.example.com",
		"my-domain.com",
		"my-domain-123.example.com",
	}

	for _, pattern := range validPatterns {
		if err := ValidateDomainPattern(pattern); err != nil {
			t.Errorf("ValidateDomainPattern(%q) should be valid, got error: %v", pattern, err)
		}
	}

	invalidPatterns := []struct {
		pattern string
		reason  string
	}{
		{"", "empty"},
		{"*.", "empty after wildcard"},
		{".example.com", "starts with dot"},
		{"example.com.", "ends with dot"},
		{"example..com", "consecutive dots"},
		{"example", "no TLD"},
		{"exam ple.com", "space in domain"},
	}

	for _, tt := range invalidPatterns {
		err := ValidateDomainPattern(tt.pattern)
		if err == nil {
			t.Errorf("ValidateDomainPattern(%q) should be invalid (%s)", tt.pattern, tt.reason)
		}
	}
}
