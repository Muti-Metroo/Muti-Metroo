package filetransfer

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// newMutateHandler returns a handler whose allowlist is a fresh "share"
// directory, plus that directory and a sibling "outside" directory that is
// not allowed.
func newMutateHandler(t *testing.T) (h *StreamHandler, share, outside string) {
	t.Helper()
	root := t.TempDir()
	share = filepath.Join(root, "share")
	outside = filepath.Join(root, "outside")
	for _, d := range []string{share, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return NewStreamHandler(StreamConfig{Enabled: true, AllowedPaths: []string{share}}, allowAllVerifier), share, outside
}

func TestBrowse_Mkdir(t *testing.T) {
	h, share, _ := newMutateHandler(t)

	dir := filepath.Join(share, "new")
	resp := h.Browse(&BrowseRequest{Action: "mkdir", Path: dir})
	if resp.Error != "" || resp.Entry == nil || !resp.Entry.IsDir {
		t.Fatalf("mkdir: %+v", resp)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("created dir: %v, mode %v", err, info)
	}

	if resp := h.Browse(&BrowseRequest{Action: "mkdir", Path: dir}); resp.ErrorCode != ErrCodeAlreadyExists {
		t.Errorf("mkdir existing: code %q (%s)", resp.ErrorCode, resp.Error)
	}
	if resp := h.Browse(&BrowseRequest{Action: "mkdir", Path: filepath.Join(share, "missing", "child")}); resp.ErrorCode != ErrCodeNotFound {
		t.Errorf("mkdir without parent: code %q (%s)", resp.ErrorCode, resp.Error)
	}
}

func TestBrowse_Rename(t *testing.T) {
	h, share, outside := newMutateHandler(t)

	src := filepath.Join(share, "a.txt")
	if err := os.WriteFile(src, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(share, "b.txt")
	resp := h.Browse(&BrowseRequest{Action: "rename", Path: src, NewPath: dst})
	if resp.Error != "" || resp.Path != dst {
		t.Fatalf("rename: %+v", resp)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("destination missing: %v", err)
	}

	if resp := h.Browse(&BrowseRequest{Action: "rename", Path: src, NewPath: dst}); resp.ErrorCode != ErrCodeNotFound {
		t.Errorf("rename missing source: code %q (%s)", resp.ErrorCode, resp.Error)
	}

	other := filepath.Join(share, "c.txt")
	if err := os.WriteFile(other, []byte("c"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := h.Browse(&BrowseRequest{Action: "rename", Path: other, NewPath: dst}); resp.ErrorCode != ErrCodeAlreadyExists {
		t.Errorf("rename onto existing: code %q (%s)", resp.ErrorCode, resp.Error)
	}
	if resp := h.Browse(&BrowseRequest{Action: "rename", Path: other, NewPath: filepath.Join(outside, "c.txt")}); resp.ErrorCode != ErrCodeNotAllowed {
		t.Errorf("rename out of the allowlist: code %q (%s)", resp.ErrorCode, resp.Error)
	}
	if resp := h.Browse(&BrowseRequest{Action: "rename", Path: other}); resp.Error == "" {
		t.Error("rename without new_path should fail")
	}
}

func TestBrowse_ErrorCodes(t *testing.T) {
	h, share, outside := newMutateHandler(t)

	if resp := h.Browse(&BrowseRequest{Action: "stat", Path: filepath.Join(share, "nope")}); resp.ErrorCode != ErrCodeNotFound {
		t.Errorf("stat missing: code %q", resp.ErrorCode)
	}
	if resp := h.Browse(&BrowseRequest{Action: "list", Path: outside}); resp.ErrorCode != ErrCodeNotAllowed {
		t.Errorf("list outside: code %q", resp.ErrorCode)
	}
	dir := filepath.Join(share, "full")
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if resp := h.Browse(&BrowseRequest{Action: "delete", Path: dir}); resp.ErrorCode != ErrCodeNotEmpty {
		t.Errorf("delete non-empty: code %q", resp.ErrorCode)
	}
}

// TestSymlinkedParentEscape: a symlink inside the allowed directory that
// points outside it must not make the outside reachable, neither as the
// final component nor as a parent directory, for any operation.
func TestSymlinkedParentEscape(t *testing.T) {
	h, share, outside := newMutateHandler(t)

	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(share, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	through := filepath.Join(link, "secret.txt")

	for _, req := range []*BrowseRequest{
		{Action: "list", Path: link},
		{Action: "stat", Path: through},
		{Action: "chmod", Path: through, Mode: "0644"},
		{Action: "delete", Path: through},
		{Action: "mkdir", Path: filepath.Join(link, "d")},
		{Action: "rename", Path: through, NewPath: filepath.Join(share, "stolen.txt")},
	} {
		if resp := h.Browse(req); resp.ErrorCode != ErrCodeNotAllowed {
			t.Errorf("%s %s: code %q (%s)", req.Action, req.Path, resp.ErrorCode, resp.Error)
		}
	}
	if err := h.ValidateDownloadMetadata(&TransferMetadata{Path: through}); err == nil {
		t.Error("download through a symlinked parent should be refused")
	}
	if err := h.ValidateUploadMetadata(&TransferMetadata{Path: filepath.Join(link, "new.txt")}); err == nil {
		t.Error("upload through a symlinked parent should be refused")
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("secret was touched: %v", err)
	}

	// Listing the share describes the link itself, not the outside target.
	resp := h.Browse(&BrowseRequest{Action: "list", Path: share})
	for _, e := range resp.Entries {
		if e.Name == "link" && (e.IsDir || !e.IsSymlink) {
			t.Errorf("outside link listed as %+v; want the link itself", e)
		}
	}

	// The link itself can still be deleted; that does not touch its target.
	if resp := h.Browse(&BrowseRequest{Action: "delete", Path: link}); resp.Error != "" {
		t.Errorf("delete link itself: %s", resp.Error)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("deleting the link removed its target: %v", err)
	}
}

func TestReadFileRange(t *testing.T) {
	h, share, _ := newMutateHandler(t)
	path := filepath.Join(share, "f.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, size, _, _, err := h.ReadFileRange(context.Background(), path, 2, 3, false)
	if err != nil {
		t.Fatalf("ReadFileRange: %v", err)
	}
	if got, _ := io.ReadAll(r); string(got) != "234" || size != 3 {
		t.Errorf("range = %q (size %d), want %q (3)", got, size, "234")
	}

	r, _, _, _, err = h.ReadFileRange(context.Background(), path, 8, 100, true)
	if err != nil {
		t.Fatalf("ReadFileRange compressed: %v", err)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(gz); string(got) != "89" {
		t.Errorf("compressed range past the end = %q, want %q", got, "89")
	}

	if _, _, _, _, err := h.ReadFileRange(context.Background(), path, 0, -1, false); err == nil {
		t.Error("negative length should be rejected")
	}
}
