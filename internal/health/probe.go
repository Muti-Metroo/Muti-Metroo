// Package health provides health check HTTP endpoints for Muti Metroo.
package health

import (
	"io"
	"net/http"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/probe"
	"github.com/postalsys/muti-metroo/internal/protocol"
)

// ProbeFunc is the dialer used by the local /api/probe handler. Defaulted to
// probe.Probe in NewServer; tests override it to inject deterministic results
// without needing a real listener.
type ProbeFunc = probe.Func

// probeMaxRequestBytes caps the request body so a misbehaving caller cannot
// flood the agent with arbitrarily large inline PEM uploads. 1 MiB easily
// fits any realistic CA bundle plus client cert/key.
const probeMaxRequestBytes = 1 << 20

// handleProbe handles POST /api/probe.
//
// Runs a connectivity probe (dial + PEER_HELLO handshake) from this agent and
// returns the result as JSON. A failed probe (handshake error, refused, etc.)
// is reported as HTTP 200 with success=false in the body, matching the
// /api/mesh-test convention. HTTP 4xx is reserved for invalid requests.
func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, probeMaxRequestBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body: " + err.Error()})
		return
	}

	out, ok := probe.HandleRequest(r.Context(), body, s.probeFunc)
	status := http.StatusOK
	if !ok {
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(out)
}

// handleRemoteProbe forwards a probe request to a remote agent over the mesh
// using the existing ControlTypeProbe control channel.
func (s *Server) handleRemoteProbe(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	s.forwardRemoteControl(w, r, targetID, protocol.ControlTypeProbe, "probe")
}
