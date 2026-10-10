// Package health provides health check HTTP endpoints for Muti Metroo.
package health

import (
	"net/http"

	"github.com/postalsys/muti-metroo/internal/sysinfo"
)

// InterfacesResponse is the response body for GET /api/interfaces and
// GET /agents/{id}/interfaces. It enumerates every IP address assigned to an
// up interface on the agent, so an operator can pick a source address to bind
// a route's outbound dial to (see "muti-metroo route source-ip").
type InterfacesResponse struct {
	Addresses []sysinfo.InterfaceAddr `json:"addresses"`
}

// handleInterfaces serves GET /api/interfaces. It reports the local agent's
// assigned interface addresses. Read-only; no management-key signature
// required. The remote-agent variant is served by handleAgentInfo via the
// ControlTypeInterfaces control request.
func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, InterfacesResponse{Addresses: sysinfo.GetInterfaceAddresses()})
}
