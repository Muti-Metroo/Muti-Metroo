package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/filetransfer"
)

// postJSON sends body as JSON to url and returns the status and response body.
func postJSON(t *testing.T, url string, body any) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// browse runs one browse action against targetID through the agent's HTTP API.
func browse(t *testing.T, agentAddr, targetID string, req filetransfer.BrowseRequest) (int, filetransfer.BrowseResponse) {
	t.Helper()
	status, body := postJSON(t, "http://"+agentAddr+"/agents/"+targetID+"/file/browse", req)
	var resp filetransfer.BrowseResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("browse %s: status %d, body %q", req.Action, status, body)
	}
	return status, resp
}

// TestFileTransfer_ShareOperations covers the operations a mounted file
// share needs, end to end through the mesh: mkdir, rename, a zero-byte
// upload, ranged downloads, transfers addressed to the serving agent's own
// ID, and refusal of a path that escapes the allowlist through a symlinked
// parent directory.
func TestFileTransfer_ShareOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	root := t.TempDir()
	share := filepath.Join(root, "share")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{share, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	chain := newFileTransferTestChain(t, &config.FileTransferConfig{Enabled: true, AllowedPaths: []string{share}})
	chain.FileTransferOnA = true
	defer chain.Close()
	chain.CreateAgents(t)
	chain.StartAgents(t)
	chain.WaitForAgentPath(t, 0, 3)

	api := chain.HTTPAddrs[0]
	remote := chain.Agents[3].ID().String()
	local := chain.Agents[0].ID().String()

	// mkdir, then a duplicate mkdir.
	dir := filepath.Join(share, "docs")
	if status, resp := browse(t, api, remote, filetransfer.BrowseRequest{Action: "mkdir", Path: dir}); status != http.StatusOK {
		t.Fatalf("mkdir: %d %+v", status, resp)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("mkdir did not create %s: %v", dir, err)
	}
	if _, resp := browse(t, api, remote, filetransfer.BrowseRequest{Action: "mkdir", Path: dir}); resp.ErrorCode != filetransfer.ErrCodeAlreadyExists {
		t.Errorf("duplicate mkdir: code %q (%s)", resp.ErrorCode, resp.Error)
	}

	// Zero-byte upload, then a real file renamed into place.
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := uploadFile(t, api, remote, empty, filepath.Join(dir, "empty.txt")); err != nil || !res.Success {
		t.Fatalf("zero-byte upload: %v %+v", err, res)
	}
	if info, err := os.Stat(filepath.Join(dir, "empty.txt")); err != nil || info.Size() != 0 {
		t.Fatalf("zero-byte file: %v", err)
	}

	content := filepath.Join(t.TempDir(), "content")
	if err := os.WriteFile(content, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := uploadFile(t, api, remote, content, filepath.Join(dir, "draft.txt")); err != nil || !res.Success {
		t.Fatalf("upload: %v %+v", err, res)
	}
	final := filepath.Join(dir, "final.txt")
	if status, resp := browse(t, api, remote, filetransfer.BrowseRequest{Action: "rename", Path: filepath.Join(dir, "draft.txt"), NewPath: final}); status != http.StatusOK {
		t.Fatalf("rename: %d %+v", status, resp)
	}

	// Ranged downloads from the remote agent and from the serving agent itself.
	for name, target := range map[string]string{"remote": remote, "local": local} {
		status, body := postJSON(t, "http://"+api+"/agents/"+target+"/file/download", map[string]any{"path": final, "offset": 2, "length": 3})
		if status != http.StatusOK || string(body) != "234" {
			t.Errorf("%s ranged download: %d %q, want 200 %q", name, status, body, "234")
		}
	}

	// Upload addressed to the serving agent's own ID.
	if res, err := uploadFile(t, api, local, content, filepath.Join(share, "local.txt")); err != nil || !res.Success {
		t.Fatalf("local upload: %v %+v", err, res)
	}
	if got, err := os.ReadFile(filepath.Join(share, "local.txt")); err != nil || string(got) != "0123456789" {
		t.Errorf("local upload content: %q %v", got, err)
	}

	// Refused transfers carry stable codes through the mesh and the HTTP API.
	for _, tc := range []struct{ path, code string }{
		{filepath.Join(share, "missing.txt"), filetransfer.ErrCodeNotFound},
		{filepath.Join(outside, "x.txt"), filetransfer.ErrCodeNotAllowed},
	} {
		status, body := postJSON(t, "http://"+api+"/agents/"+remote+"/file/download", map[string]any{"path": tc.path})
		var resp struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(body, &resp)
		if status != http.StatusBadGateway || resp.Code != tc.code {
			t.Errorf("download %s: %d %s, want 502 with code %q", tc.path, status, body, tc.code)
		}
	}

	// A symlinked parent directory must not lead outside the allowlist.
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(share, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, resp := browse(t, api, remote, filetransfer.BrowseRequest{Action: "list", Path: link}); resp.ErrorCode != filetransfer.ErrCodeNotAllowed {
		t.Errorf("list through symlink: code %q (%s)", resp.ErrorCode, resp.Error)
	}
	if status, body := postJSON(t, "http://"+api+"/agents/"+remote+"/file/download", map[string]any{"path": filepath.Join(link, "secret.txt")}); status == http.StatusOK {
		t.Errorf("download through symlinked parent returned %q", body)
	}
}
