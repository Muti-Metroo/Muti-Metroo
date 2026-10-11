// Package health provides health check HTTP endpoints for Muti Metroo.
package health

import (
	"net/http"
)

// SetupDefaultsResponse is the response body for GET /api/setup-defaults.
//
// It feeds a config-generation wizard (e.g. Muti Metroo Manager) with non-sensitive
// defaults derived from a running agent. Private key material, the HTTP
// bearer token, and the agent's identity certificate are intentionally
// excluded: the new agent being configured needs its own identity.
type SetupDefaultsResponse struct {
	AgentID       string                     `json:"agent_id"`
	DisplayName   string                     `json:"display_name"`
	Version       string                     `json:"version"`
	TLS           SetupDefaultsTLS           `json:"tls"`
	Management    SetupDefaultsManagement    `json:"management"`
	Listener      SetupDefaultsListener      `json:"listener"`
	SuggestedPeer SetupDefaultsSuggestedPeer `json:"suggested_peer"`
	HTTP          SetupDefaultsHTTP          `json:"http"`
	Shell         SetupDefaultsShell         `json:"shell"`
	FileTransfer  SetupDefaultsFileTransfer  `json:"file_transfer"`
}

// SetupDefaultsTLS exposes CA and TLS-mode defaults. The CA certificate is
// public; the agent's identity certificate and private key are never included.
type SetupDefaultsTLS struct {
	CAPEM      string `json:"ca_pem"`
	Strict     bool   `json:"strict"`
	MTLS       bool   `json:"mtls"`
	SelfSigned bool   `json:"self_signed"`
}

// SetupDefaultsManagement exposes management public keys and capability flags.
// Private keys are never included; the booleans communicate whether this agent
// happens to hold them locally.
type SetupDefaultsManagement struct {
	Enabled              bool   `json:"enabled"`
	PublicKey            string `json:"public_key"`
	HasPrivateKey        bool   `json:"has_private_key"`
	SigningPublicKey     string `json:"signing_public_key"`
	HasSigningPrivateKey bool   `json:"has_signing_private_key"`
}

// SetupDefaultsListener echoes the first configured listener so the wizard
// can seed transport/path defaults for the new agent's own listener.
type SetupDefaultsListener struct {
	Transport string `json:"transport"`
	Address   string `json:"address"`
	Path      string `json:"path"`
	Plaintext bool   `json:"plaintext"`
}

// SetupDefaultsSuggestedPeer describes how a new agent can reach this one.
// The address is best-effort: 0.0.0.0/:: listen addresses are replaced with
// the agent's first non-loopback IP so the generated config is usable as-is.
type SetupDefaultsSuggestedPeer struct {
	Address    string `json:"address,omitempty"`
	Transport  string `json:"transport,omitempty"`
	Path       string `json:"path,omitempty"`
	ExpectedID string `json:"expected_id,omitempty"`
}

// SetupDefaultsHTTP exposes HTTP-server defaults. TokenHash is a bcrypt digest
// and is safe to propagate: the plaintext token is never stored or returned.
type SetupDefaultsHTTP struct {
	Enabled   bool   `json:"enabled"`
	TokenHash string `json:"token_hash,omitempty"`
}

// SetupDefaultsShell exposes shell defaults. PasswordHash is a bcrypt digest
// intended to be copied verbatim to agents sharing the same credential.
type SetupDefaultsShell struct {
	Enabled      bool     `json:"enabled"`
	Whitelist    []string `json:"whitelist,omitempty"`
	PasswordHash string   `json:"password_hash,omitempty"`
}

// SetupDefaultsFileTransfer exposes file-transfer defaults. PasswordHash is a
// bcrypt digest intended to be copied verbatim to agents sharing credentials.
type SetupDefaultsFileTransfer struct {
	Enabled      bool     `json:"enabled"`
	AllowedPaths []string `json:"allowed_paths,omitempty"`
	PasswordHash string   `json:"password_hash,omitempty"`
}

// SetupDefaultsProvider supplies the payload for /api/setup-defaults.
// The agent implements this; tests can substitute their own provider.
type SetupDefaultsProvider interface {
	GetSetupDefaults() SetupDefaultsResponse
}

// SetSetupDefaultsProvider sets the setup defaults provider.
// This is called after the agent is initialized.
func (s *Server) SetSetupDefaultsProvider(provider SetupDefaultsProvider) {
	s.setupDefaultsProvider = provider
}

// handleSetupDefaults serves GET /api/setup-defaults.
func (s *Server) handleSetupDefaults(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	if s.setupDefaultsProvider == nil {
		http.Error(w, "provider not configured", http.StatusServiceUnavailable)
		return
	}
	// Payload includes the CA cert and management public keys; prevent
	// intermediaries from serving stale copies after an operator rotates them.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.setupDefaultsProvider.GetSetupDefaults())
}
