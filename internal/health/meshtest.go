// Package health provides health check HTTP endpoints for Muti Metroo.
package health

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// MeshTestResult contains the result of testing a single agent's reachability.
type MeshTestResult struct {
	AgentID        string `json:"agent_id"`
	ShortID        string `json:"short_id"`
	DisplayName    string `json:"display_name"`
	IsLocal        bool   `json:"is_local"`
	Reachable      bool   `json:"reachable"`
	ResponseTimeMs int64  `json:"response_time_ms"`
	Error          string `json:"error,omitempty"`
}

// MeshTestResponse is the response for the /api/mesh-test endpoint.
type MeshTestResponse struct {
	LocalAgent     string           `json:"local_agent"`
	TestTime       time.Time        `json:"test_time"`
	DurationMs     int64            `json:"duration_ms"`
	TotalCount     int              `json:"total_count"`
	ReachableCount int              `json:"reachable_count"`
	Results        []MeshTestResult `json:"results"`
}

// MeshTestState holds cached mesh test results.
type MeshTestState struct {
	mu           sync.RWMutex
	lastResult   *MeshTestResponse
	lastTestTime time.Time
	cacheTTL     time.Duration
}

// NewMeshTestState creates a new mesh test state with default cache TTL.
func NewMeshTestState() *MeshTestState {
	return &MeshTestState{
		cacheTTL: 30 * time.Second,
	}
}

// GetCachedResult returns the cached result if still valid.
func (s *MeshTestState) GetCachedResult() *MeshTestResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastResult != nil && time.Since(s.lastTestTime) < s.cacheTTL {
		return s.lastResult
	}
	return nil
}

// SetResult stores a new test result.
func (s *MeshTestState) SetResult(result *MeshTestResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastResult = result
	s.lastTestTime = time.Now()
}

// handleMeshTest handles GET/POST /api/mesh-test for mesh connectivity testing.
// GET returns cached results (30s cache), POST forces a fresh test.
func (s *Server) handleMeshTest(w http.ResponseWriter, r *http.Request) {
	if s.remoteProvider == nil {
		http.Error(w, "remote provider not configured", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// When management key encryption is enabled but we lack the private key,
	// restrict to local-only to avoid leaking mesh topology.
	if s.shouldRestrictTopology() {
		localID := s.remoteProvider.ID()
		writeJSON(w, http.StatusOK, &MeshTestResponse{
			LocalAgent:     localID.ShortString(),
			TestTime:       time.Now(),
			TotalCount:     1,
			ReachableCount: 1,
			Results: []MeshTestResult{
				{
					AgentID:     localID.String(),
					ShortID:     localID.ShortString(),
					DisplayName: s.remoteProvider.DisplayName(),
					IsLocal:     true,
					Reachable:   true,
				},
			},
		})
		return
	}

	// Return cached results for GET if available
	if r.Method == http.MethodGet {
		if cached := s.meshTestState.GetCachedResult(); cached != nil {
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	// Run fresh test for POST or cache miss
	result := s.runMeshTest(r.Context())
	s.meshTestState.SetResult(result)

	writeJSON(w, http.StatusOK, result)
}

// agentTestInfo holds agent details for mesh connectivity testing.
type agentTestInfo struct {
	id          identity.AgentID
	displayName string
	isLocal     bool
}

// runMeshTest tests connectivity to all known agents with bounded concurrency.
func (s *Server) runMeshTest(ctx context.Context) *MeshTestResponse {
	startTime := time.Now()
	localID := s.remoteProvider.ID()
	displayNames := s.remoteProvider.GetAllDisplayNames()

	// Build list of agents to test, starting with local
	agents := []agentTestInfo{
		{id: localID, displayName: s.remoteProvider.DisplayName(), isLocal: true},
	}

	for _, id := range s.remoteProvider.GetKnownAgentIDs() {
		if id == localID {
			continue
		}
		name := displayNames[id]
		if name == "" {
			name = id.ShortString()
		}
		agents = append(agents, agentTestInfo{id: id, displayName: name, isLocal: false})
	}

	// Test all agents with bounded concurrency
	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)
	results := make([]MeshTestResult, len(agents))
	var wg sync.WaitGroup

	for i, agent := range agents {
		wg.Add(1)
		go func(idx int, a agentTestInfo) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = MeshTestResult{
					AgentID:        a.id.String(),
					ShortID:        a.id.ShortString(),
					DisplayName:    a.displayName,
					IsLocal:        a.isLocal,
					Reachable:      false,
					ResponseTimeMs: -1,
					Error:          "context cancelled",
				}
				return
			}

			results[idx] = s.testAgent(ctx, a.id, a.displayName, a.isLocal)
		}(i, agent)
	}

	wg.Wait()

	// Count reachable agents
	reachableCount := 0
	for _, r := range results {
		if r.Reachable {
			reachableCount++
		}
	}

	return &MeshTestResponse{
		LocalAgent:     localID.ShortString(),
		TestTime:       startTime,
		DurationMs:     time.Since(startTime).Milliseconds(),
		TotalCount:     len(results),
		ReachableCount: reachableCount,
		Results:        results,
	}
}

// testAgent tests connectivity to a single agent.
func (s *Server) testAgent(ctx context.Context, id identity.AgentID, displayName string, isLocal bool) MeshTestResult {
	result := MeshTestResult{
		AgentID:     id.String(),
		ShortID:     id.ShortString(),
		DisplayName: displayName,
		IsLocal:     isLocal,
	}

	// Local agent is always reachable
	if isLocal {
		result.Reachable = true
		return result
	}

	// Test remote agent with timeout
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := s.remoteProvider.SendControlRequest(testCtx, id, protocol.ControlTypeStatus)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		result.ResponseTimeMs = -1
		result.Error = err.Error()
		return result
	}

	if !resp.Success {
		result.ResponseTimeMs = elapsed
		result.Error = "remote agent returned error"
		return result
	}

	result.Reachable = true
	result.ResponseTimeMs = elapsed
	return result
}
