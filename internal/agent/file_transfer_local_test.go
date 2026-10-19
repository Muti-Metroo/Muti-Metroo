package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/crypto"
	"github.com/Muti-Metroo/Muti-Metroo/internal/filetransfer"
	"github.com/Muti-Metroo/Muti-Metroo/internal/health"
)

// newSharedDirAgent is newFileBrowseAgent restricted to one allowed directory.
func newSharedDirAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	a, _ := newFileBrowseAgent(t)
	share := t.TempDir()
	a.fileStreamHandler = filetransfer.NewStreamHandler(filetransfer.StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{share},
	}, a.verifyManagementAuthFor(crypto.OpFile))
	return a, share
}

// TestTransferMetadata_TamperedPathRejected: metadata signed for one path
// must not authorize a transfer of another. This is what the target agent
// sees when a captured envelope is attached to an edited request.
func TestTransferMetadata_TamperedPathRejected(t *testing.T) {
	a, share := newSharedDirAgent(t)

	signed := &filetransfer.TransferMetadata{Path: filepath.Join(share, "public.txt")}
	env, _, err := a.signBody(a.ID(), crypto.OpFile, signed)
	if err != nil {
		t.Fatalf("signBody: %v", err)
	}

	for _, upload := range []bool{false, true} {
		tampered := &filetransfer.TransferMetadata{Path: filepath.Join(share, "secret.txt"), Auth: env}
		wire, err := json.Marshal(tampered)
		if err != nil {
			t.Fatal(err)
		}
		if tampered.BodyHash, err = crypto.CanonicalBodyHash(wire); err != nil {
			t.Fatal(err)
		}
		if upload {
			err = a.fileStreamHandler.ValidateUploadMetadata(tampered)
		} else {
			err = a.fileStreamHandler.ValidateDownloadMetadata(tampered)
		}
		if filetransfer.ErrorCode(err) != filetransfer.ErrCodeInvalidCredentials {
			t.Errorf("upload=%v: err = %v, want invalid credentials", upload, err)
		}
	}

	// The untouched request still verifies.
	ok := &filetransfer.TransferMetadata{Path: filepath.Join(share, "public.txt"), Auth: env}
	wire, _ := json.Marshal(ok)
	ok.BodyHash, _ = crypto.CanonicalBodyHash(wire)
	if err := a.fileStreamHandler.ValidateUploadMetadata(ok); err != nil {
		t.Errorf("signed request rejected: %v", err)
	}
}

// TestLocalTransfers covers uploads and downloads addressed to the agent's
// own ID: content, ranges, modes, and the error codes the HTTP layer and
// Muti Metroo Manager rely on.
func TestLocalTransfers(t *testing.T) {
	a, share := newSharedDirAgent(t)
	ctx := context.Background()

	src := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(src, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(share, "dst.txt")
	if err := a.UploadFile(ctx, a.ID(), src, dst, health.TransferOptions{Mode: 0o640}, nil); err != nil {
		t.Fatalf("local upload: %v", err)
	}
	if info, err := os.Stat(dst); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("uploaded file: %v %v", info, err)
	}

	read := func(opts health.TransferOptions) (string, *health.DownloadStreamResult) {
		t.Helper()
		res, err := a.DownloadFileStream(ctx, a.ID(), dst, opts)
		if err != nil {
			t.Fatalf("local download %+v: %v", opts, err)
		}
		defer res.Reader.Close()
		b, err := io.ReadAll(res.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(b), res
	}
	if got, res := read(health.TransferOptions{}); got != "0123456789" || res.OriginalSize != 10 || res.Mode != 0o640 {
		t.Errorf("full download = %q (size %d, mode %o)", got, res.OriginalSize, res.Mode)
	}
	if got, res := read(health.TransferOptions{Offset: 3, Length: 4}); got != "3456" || res.OriginalSize != 10 {
		t.Errorf("ranged download = %q (original size %d)", got, res.OriginalSize)
	}

	// Directory upload through the tar path.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.UploadFile(ctx, a.ID(), dir, filepath.Join(share, "tree"), health.TransferOptions{}, nil); err != nil {
		t.Fatalf("local directory upload: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(share, "tree", "a.txt")); err != nil || string(got) != "a" {
		t.Errorf("directory upload content %q %v", got, err)
	}

	code := func(err error) string {
		var re *filetransfer.RemoteError
		if !errors.As(err, &re) {
			t.Fatalf("error %v is not a RemoteError", err)
		}
		return re.Code
	}
	_, err := a.DownloadFileStream(ctx, a.ID(), filepath.Join(share, "missing.txt"), health.TransferOptions{})
	if c := code(err); c != filetransfer.ErrCodeNotFound {
		t.Errorf("missing file: code %q (%v)", c, err)
	}
	_, err = a.DownloadFileStream(ctx, a.ID(), "/etc/hosts", health.TransferOptions{})
	if c := code(err); c != filetransfer.ErrCodeNotAllowed {
		t.Errorf("outside allowlist: code %q (%v)", c, err)
	}
	err = a.UploadFile(ctx, a.ID(), src, "/etc/muti-metroo-test.txt", health.TransferOptions{}, nil)
	if c := code(err); c != filetransfer.ErrCodeNotAllowed {
		t.Errorf("upload outside allowlist: code %q (%v)", c, err)
	}
}
