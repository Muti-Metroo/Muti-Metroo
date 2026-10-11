package agent

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/filetransfer"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// newFileBrowseAgent builds an agent with file transfer enabled and a
// management signing keypair configured, without starting listeners.
func newFileBrowseAgent(t *testing.T) (*Agent, *crypto.SigningKeypair) {
	t.Helper()

	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair: %v", err)
	}

	cfg := config.Default()
	cfg.Agent.DataDir = t.TempDir()
	cfg.FileTransfer.Enabled = true
	cfg.FileTransfer.AllowedPaths = []string{"*"}
	cfg.Management.SigningPublicKey = hex.EncodeToString(kp.PublicKey[:])
	cfg.Management.SigningPrivateKey = hex.EncodeToString(kp.PrivateKey[:])

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a, kp
}

// TestAgent_handleFileBrowse_RejectsUnsignedRemote: a browse request arriving
// over the control channel without a management signature must be rejected
// with auth_required, not stamped with this agent's own signing key.
func TestAgent_handleFileBrowse_RejectsUnsignedRemote(t *testing.T) {
	a, _ := newFileBrowseAgent(t)

	data, err := json.Marshal(&filetransfer.BrowseRequest{Action: "list", Path: "/"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	respData, success := a.handleFileBrowse(data)
	if success {
		t.Fatal("unsigned control-channel browse should not succeed")
	}

	var resp filetransfer.BrowseResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ErrorCode != filetransfer.ErrCodeAuthRequired {
		t.Errorf("ErrorCode = %q, want %q", resp.ErrorCode, filetransfer.ErrCodeAuthRequired)
	}
}

// TestAgent_BrowseFiles_StampsLocalGateway: the local HTTP gateway path still
// stamps an unsigned request with the agent's own key.
func TestAgent_BrowseFiles_StampsLocalGateway(t *testing.T) {
	a, _ := newFileBrowseAgent(t)

	req := &filetransfer.BrowseRequest{Action: "roots"}
	resp := a.BrowseFiles(req)
	if resp.Error != "" {
		t.Fatalf("local gateway browse failed: %q (%q)", resp.Error, resp.ErrorCode)
	}
	if req.Auth == nil {
		t.Error("expected BrowseFiles to stamp req.Auth with a local signature")
	}
}

// TestAgent_handleFileBrowse_AcceptsSignedRemote: a control-channel browse
// carrying a valid signature from another agent is verified and served.
func TestAgent_handleFileBrowse_AcceptsSignedRemote(t *testing.T) {
	a, kp := newFileBrowseAgent(t)

	origin, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID: %v", err)
	}
	env := crypto.SignManagement(kp.PrivateKey, origin, a.ID(), crypto.OpFile, nil)

	data, err := json.Marshal(&filetransfer.BrowseRequest{Action: "roots", Auth: &env})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	respData, success := a.handleFileBrowse(data)
	if !success {
		t.Fatalf("signed control-channel browse failed: %s", respData)
	}

	var resp filetransfer.BrowseResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %q (%q)", resp.Error, resp.ErrorCode)
	}
}

// TestAgent_handleFileBrowse_RejectsBadSignature: a control-channel browse
// signed by the wrong key is rejected as invalid_credentials.
func TestAgent_handleFileBrowse_RejectsBadSignature(t *testing.T) {
	a, _ := newFileBrowseAgent(t)

	other, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair: %v", err)
	}
	origin, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID: %v", err)
	}
	env := crypto.SignManagement(other.PrivateKey, origin, a.ID(), crypto.OpFile, nil)

	data, err := json.Marshal(&filetransfer.BrowseRequest{Action: "roots", Auth: &env})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	respData, success := a.handleFileBrowse(data)
	if success {
		t.Fatal("browse signed by an unknown key should not succeed")
	}

	var resp filetransfer.BrowseResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ErrorCode != filetransfer.ErrCodeInvalidCredentials {
		t.Errorf("ErrorCode = %q, want %q", resp.ErrorCode, filetransfer.ErrCodeInvalidCredentials)
	}
}
