// Package health: route trace handlers.
package health

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

// RouteTraceRequest is the JSON body for POST /route/trace.
type RouteTraceRequest struct {
	// Dest is an IP address, CIDR, or hostname. Hostnames are resolved on
	// the receiving agent using its own resolver.
	Dest string `json:"dest"`
}

// RouteTraceCandidate describes one CIDR route considered for an IP.
type RouteTraceCandidate struct {
	Network     string `json:"network"`
	PrefixLen   int    `json:"prefix_len"`
	Metric      uint16 `json:"metric"`
	Origin      string `json:"origin"`
	OriginName  string `json:"origin_name,omitempty"`
	NextHop     string `json:"next_hop"`
	NextHopName string `json:"next_hop_name,omitempty"`
	HopCount    int    `json:"hop_count"`
	LastUpdate  string `json:"last_update,omitempty"`
	Winner      bool   `json:"winner"`
	Reason      string `json:"reason,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// RouteTraceDomain describes a matched domain route (or absence thereof).
type RouteTraceDomain struct {
	Matched     bool   `json:"matched"`
	Pattern     string `json:"pattern,omitempty"`
	IsWildcard  bool   `json:"is_wildcard,omitempty"`
	Metric      uint16 `json:"metric,omitempty"`
	Origin      string `json:"origin,omitempty"`
	OriginName  string `json:"origin_name,omitempty"`
	NextHop     string `json:"next_hop,omitempty"`
	NextHopName string `json:"next_hop_name,omitempty"`
	HopCount    int    `json:"hop_count,omitempty"`
}

// RouteTraceIP holds the CIDR trace for a single resolved IP.
type RouteTraceIP struct {
	IP         string                 `json:"ip"`
	Candidates []*RouteTraceCandidate `json:"candidates,omitempty"`
	NoMatch    bool                   `json:"no_match,omitempty"`
}

// Resolution* are the values used for RouteTraceResult.Resolution. The
// strings match Mutiauk's CLI vocabulary so future code that
// forwards an agent trace through the entry display layer keeps a
// single set of identifiers.
const (
	ResolutionIP     = "ip"
	ResolutionMesh   = "mesh"
	ResolutionSystem = "system"
)

// RouteTraceResult is the JSON response shape for a trace.
type RouteTraceResult struct {
	Status     string            `json:"status"`
	Dest       string            `json:"dest"`
	Hostname   string            `json:"hostname,omitempty"`
	Resolution string            `json:"resolution,omitempty"`
	ResolverOK bool              `json:"resolver_ok"`
	IPs        []*RouteTraceIP   `json:"ips,omitempty"`
	Domain     *RouteTraceDomain `json:"domain,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// RouteTraceProvider answers "what would this agent do for dest?". Implemented
// by the live agent so the trace uses the same routing tables as the stream
// open path.
type RouteTraceProvider interface {
	TraceRoute(dest string) (*RouteTraceResult, error)
}

// SetRouteTraceProvider wires the trace implementation.
func (s *Server) SetRouteTraceProvider(provider RouteTraceProvider) {
	s.routeTraceProvider = provider
}

// routeTraceMaxRequestBytes caps the request body. A destination string does
// not need more than a few KB.
const routeTraceMaxRequestBytes = 16 << 10

// handleRouteTrace handles POST /route/trace. Read-only diagnostic that runs
// local selection logic against a destination and returns the match set.
func (s *Server) handleRouteTrace(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if s.routeTraceProvider == nil {
		http.Error(w, "route trace not configured", http.StatusServiceUnavailable)
		return
	}
	if s.shouldRestrictTopology() {
		http.Error(w, "route trace restricted: management key decryption unavailable", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, routeTraceMaxRequestBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body: " + err.Error()})
		return
	}

	var req RouteTraceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}
	if req.Dest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dest is required"})
		return
	}

	result, err := s.routeTraceProvider.TraceRoute(req.Dest)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRemoteRouteTrace forwards a trace request to a remote agent over the
// mesh using ControlTypeRouteTrace. Read-only; no management-key signature.
func (s *Server) handleRemoteRouteTrace(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControl(w, r, targetID, protocol.ControlTypeRouteTrace, "route trace")
}
