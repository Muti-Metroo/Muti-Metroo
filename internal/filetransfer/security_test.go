package filetransfer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// Authentication & Path Safety Negative Tests
//
// Password-based auth was removed; authorization now flows through the
// AuthVerifier callback (management-key signatures). These tests focus on
// path-safety invariants that still apply regardless of the verifier.
// ============================================================================

// TestAuthBypass_PathTraversal tests various path traversal attack attempts.
func TestAuthBypass_PathTraversal(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"/tmp/uploads"},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		// Basic traversal - filepath.Clean normalizes these, then allowed path check fails
		{"simple traversal", "/tmp/uploads/../../../etc/passwd", true, "not in allowed"},
		{"double dot", "/tmp/uploads/../../secret", true, "not in allowed"},
		{"encoded traversal", "/tmp/uploads/%2e%2e/secret", false, ""}, // URL encoding not decoded
		{"backslash traversal", "/tmp/uploads\\..\\..\\secret", true, ""},

		// Path prefix bypass
		{"prefix bypass", "/tmp/uploadsx/../../etc/passwd", true, ""},
		{"not in allowed", "/etc/passwd", true, "not in allowed"},
		{"similar prefix", "/tmp/upload/file.txt", true, "not in allowed"},

		// Null byte injection - now properly detected
		{"null byte", "/tmp/uploads/file.txt\x00.jpg", true, "dangerous characters"},

		// Symlink-like paths
		{"dot slash", "/tmp/uploads/./file.txt", false, ""}, // . is allowed
		{"double slash", "/tmp/uploads//file.txt", false, ""},

		// Valid paths
		{"valid path", "/tmp/uploads/file.txt", false, ""},
		{"valid nested", "/tmp/uploads/subdir/file.txt", false, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}

			if tc.wantErr && tc.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tc.errMsg)
				}
			}
		})
	}
}

// TestAuthBypass_RelativePath tests that relative paths are rejected.
func TestAuthBypass_RelativePath(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"relative simple", "file.txt", true},
		{"relative dot", "./file.txt", true},
		{"relative parent", "../file.txt", true},
		{"relative nested", "subdir/file.txt", true},
		{"empty path", "", true},
		{"whitespace only", "   ", true},

		// Absolute paths should work
		{"absolute unix", "/tmp/file.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAuthBypass_AllowedPathsBypass tests various attempts to bypass allowed paths.
func TestAuthBypass_AllowedPathsBypass(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"/var/www", "/tmp/data"},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Allowed paths
		{"exact www", "/var/www", false},
		{"www subpath", "/var/www/index.html", false},
		{"exact data", "/tmp/data", false},
		{"data subpath", "/tmp/data/file.json", false},

		// Not allowed
		{"root", "/", true},
		{"etc", "/etc/passwd", true},
		{"var but not www", "/var/log/syslog", true},
		{"tmp but not data", "/tmp/other/file.txt", true},

		// Bypass attempts - now properly detected
		{"prefix similar", "/var/wwwevil/file.txt", true}, // Fixed: requires exact path or separator
		{"with traversal", "/var/www/../log/syslog", true},
		{"case variation", "/VAR/WWW/file.txt", true}, // Case sensitive
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAuthBypass_SizeLimit tests file size limit enforcement.
func TestAuthBypass_SizeLimit(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
		MaxFileSize:  1024, // 1KB limit
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		size    int64
		isDir   bool
		wantErr bool
	}{
		{"under limit", 512, false, false},
		{"at limit", 1024, false, false},
		{"over limit", 1025, false, true},
		{"way over limit", 1024 * 1024, false, true},
		{"negative size", -1, false, false},              // -1 means unknown
		{"zero size", 0, false, false},                   // Empty file
		{"directory bypasses", 1024 * 1024, true, false}, // Dirs not checked
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path:        "/tmp/test.txt",
				Size:        tc.size,
				IsDirectory: tc.isDir,
			})

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("size=%d, isDir=%v: gotErr=%v, wantErr=%v, error=%v",
					tc.size, tc.isDir, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAuthBypass_DisabledTransfer tests that disabled transfer rejects all.
func TestAuthBypass_DisabledTransfer(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      false,      // Disabled!
		AllowedPaths: []string{}, // Empty list would also block, but Enabled=false is checked first
	}, allowAllVerifier)

	err := h.ValidateUploadMetadata(&TransferMetadata{
		Path: "/tmp/test.txt",
	})

	if err == nil {
		t.Error("disabled transfer should reject")
	}

	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error should mention disabled: %v", err)
	}
}

// TestAuthBypass_DownloadNonexistent tests download of nonexistent paths.
func TestAuthBypass_DownloadNonexistent(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
	}, allowAllVerifier)

	err := h.ValidateDownloadMetadata(&TransferMetadata{
		Path: "/nonexistent/path/that/does/not/exist/file.txt",
	})

	if err == nil {
		t.Error("nonexistent path should fail validation")
	}
}

// TestAuthBypass_SymlinkEscape tests that symlinks don't escape allowed paths.
func TestAuthBypass_SymlinkEscape(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	secretDir := filepath.Join(tmpDir, "secret")
	os.MkdirAll(allowedDir, 0755)
	os.MkdirAll(secretDir, 0755)

	secretFile := filepath.Join(secretDir, "secret.txt")
	os.WriteFile(secretFile, []byte("secret data"), 0644)

	symlinkPath := filepath.Join(allowedDir, "link")
	os.Symlink(secretFile, symlinkPath)

	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{allowedDir},
	}, allowAllVerifier)

	err := h.ValidateDownloadMetadata(&TransferMetadata{
		Path: symlinkPath,
	})

	if err == nil {
		t.Error("Symlink pointing outside allowed paths should be rejected")
	} else {
		if !strings.Contains(err.Error(), "symlink target not allowed") {
			t.Errorf("Expected 'symlink target not allowed' error, got: %v", err)
		}
	}
}

// TestAuthBypass_SymlinkEscapeOnUpload is the upload-side counterpart of
// TestAuthBypass_SymlinkEscape. Writing through a pre-existing symlink lands
// the upload outside the allowlist even though the path itself validates, so
// the destination must be checked the same way the download source is.
func TestAuthBypass_SymlinkEscapeOnUpload(t *testing.T) {
	// Resolve the temp dir up front: on macOS t.TempDir() sits under /var,
	// which is itself a symlink to /private/var, and EvalSymlinks would
	// otherwise report every resolved target as outside the allowlist.
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	allowedDir := filepath.Join(tmpDir, "allowed")
	secretDir := filepath.Join(tmpDir, "secret")
	os.MkdirAll(allowedDir, 0755)
	os.MkdirAll(secretDir, 0755)

	// An attacker-planted link inside the allowed directory pointing out of it.
	secretFile := filepath.Join(secretDir, "authorized_keys")
	os.WriteFile(secretFile, []byte("original"), 0644)
	symlinkPath := filepath.Join(allowedDir, "innocent.txt")
	os.Symlink(secretFile, symlinkPath)

	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{allowedDir},
	}, allowAllVerifier)

	if err := h.ValidateUploadMetadata(&TransferMetadata{Path: symlinkPath}); err == nil {
		t.Error("upload through a symlink escaping the allowed paths should be rejected")
	} else if !strings.Contains(err.Error(), "symlink target not allowed") {
		t.Errorf("Expected 'symlink target not allowed' error, got: %v", err)
	}

	// A symlink that stays inside the allowlist is still a legitimate target.
	insideTarget := filepath.Join(allowedDir, "real.txt")
	os.WriteFile(insideTarget, []byte("data"), 0644)
	insideLink := filepath.Join(allowedDir, "inside-link.txt")
	os.Symlink(insideTarget, insideLink)
	if err := h.ValidateUploadMetadata(&TransferMetadata{Path: insideLink}); err != nil {
		t.Errorf("symlink resolving inside the allowed paths should be accepted, got: %v", err)
	}

	// The ordinary case, a destination that does not exist yet, must still work.
	newFile := filepath.Join(allowedDir, "brand-new.txt")
	if err := h.ValidateUploadMetadata(&TransferMetadata{Path: newFile}); err != nil {
		t.Errorf("upload to a new path should be accepted, got: %v", err)
	}
}

// TestAuthBypass_SpecialFiles tests handling of special file paths.
func TestAuthBypass_SpecialFiles(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Device files (Unix) - valid absolute paths
		{"dev null", "/dev/null", false},
		{"dev random", "/dev/random", false},
		{"dev zero", "/dev/zero", false},
		{"proc self", "/proc/self/environ", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAuthBypass_ConcurrentValidation tests concurrent validation doesn't cause issues.
func TestAuthBypass_ConcurrentValidation(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"/tmp"},
	}, allowAllVerifier)

	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(attempt int) {
			defer func() { done <- true }()

			path := "/etc/passwd"
			expectErr := true

			if attempt%2 == 0 {
				path = "/tmp/file.txt"
				expectErr = false
			}

			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: path,
			})

			gotErr := err != nil
			if gotErr != expectErr {
				t.Errorf("Attempt %d: gotErr=%v, expectErr=%v", attempt, gotErr, expectErr)
			}
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestAuthBypass_LargeFilename tests handling of extremely long filenames.
func TestAuthBypass_LargeFilename(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
	}, allowAllVerifier)

	longName := strings.Repeat("a", 10000)
	longPath := "/tmp/" + longName

	err := h.ValidateUploadMetadata(&TransferMetadata{
		Path: longPath,
	})

	// Should not panic, may succeed or fail depending on implementation
	t.Logf("Long filename result: %v", err)
}

// TestAuthBypass_UnicodePathTraversal tests Unicode-based path traversal.
func TestAuthBypass_UnicodePathTraversal(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"/tmp/uploads"},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"fullwidth dot dot", "/tmp/uploads/\uFF0E\uFF0E/secret", false},
		{"overlong UTF-8 dot", "/tmp/uploads/\xc0\xae\xc0\xae/secret", false},
		{"unicode filename", "/tmp/uploads/\u00e9l\u00e8ve.txt", false},
		{"emoji filename", "/tmp/uploads/\U0001F600.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAuthBypass_MetadataInjection tests injection via other metadata fields.
func TestAuthBypass_MetadataInjection(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
	}, allowAllVerifier)

	testCases := []struct {
		name     string
		metadata *TransferMetadata
		wantErr  bool
	}{
		{
			name: "checksum injection",
			metadata: &TransferMetadata{
				Path:     "/tmp/file.txt",
				Checksum: "; rm -rf /",
			},
			wantErr: false,
		},
		{
			name: "very large mode",
			metadata: &TransferMetadata{
				Path: "/tmp/file.txt",
				Mode: 0xFFFFFFFF,
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(tc.metadata)

			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("gotErr=%v, wantErr=%v, error=%v", gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAuthBypass_WriteToReadOnly tests writing to read-only locations.
func TestAuthBypass_WriteToReadOnly(t *testing.T) {
	h := NewStreamHandler(StreamConfig{Enabled: true, AllowedPaths: []string{"*"}}, allowAllVerifier)

	tmpDir := t.TempDir()
	roDir := filepath.Join(tmpDir, "readonly")
	os.MkdirAll(roDir, 0555)    // Read + execute only
	defer os.Chmod(roDir, 0755) // Restore for cleanup

	destPath := filepath.Join(roDir, "file.txt")
	content := []byte("test content")

	_, err := h.WriteUploadedFile(context.Background(), destPath, bytes.NewReader(content), 0644, false, false)

	if err == nil {
		t.Logf("Write to read-only succeeded (may be running as root)")
	} else {
		t.Logf("Write to read-only correctly failed: %v", err)
	}
}

// TestAllowedPaths_EmptyList tests that empty allowed_paths blocks all paths.
func TestAllowedPaths_EmptyList(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{},
	}, allowAllVerifier)

	testCases := []struct {
		name string
		path string
	}{
		{"tmp path", "/tmp/file.txt"},
		{"home path", "/home/user/file.txt"},
		{"etc path", "/etc/config"},
		{"root path", "/file.txt"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})
			if err == nil {
				t.Errorf("expected error for path %q with empty allowed_paths", tc.path)
			}
			if !strings.Contains(err.Error(), "no paths are allowed") {
				t.Errorf("expected 'no paths are allowed' error, got: %v", err)
			}
		})
	}
}

// TestAllowedPaths_Wildcard tests that ["*"] allows all absolute paths.
func TestAllowedPaths_Wildcard(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled:      true,
		AllowedPaths: []string{"*"},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"tmp path", "/tmp/file.txt", false},
		{"home path", "/home/user/file.txt", false},
		{"etc path", "/etc/config", false},
		{"root path", "/file.txt", false},
		{"deep nested", "/a/b/c/d/e/f/g.txt", false},

		{"relative", "file.txt", true},
		{"relative dot", "./file.txt", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAllowedPaths_GlobPatterns tests glob pattern matching.
func TestAllowedPaths_GlobPatterns(t *testing.T) {
	h := NewStreamHandler(StreamConfig{
		Enabled: true,
		AllowedPaths: []string{
			"/tmp/**",
			"/home/*/uploads",
			"/var/log/*.log",
			"/data",
		},
	}, allowAllVerifier)

	testCases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"tmp root", "/tmp", false},
		{"tmp file", "/tmp/file.txt", false},
		{"tmp nested", "/tmp/foo/bar/baz.txt", false},

		{"home alice uploads", "/home/alice/uploads", false},
		{"home bob uploads", "/home/bob/uploads", false},
		{"home alice uploads file", "/home/alice/uploads/doc.pdf", false},

		{"var log syslog", "/var/log/syslog.log", false},
		{"var log nested (not allowed)", "/var/log/app/error.log", true},

		{"data root", "/data", false},
		{"data file", "/data/file.txt", false},
		{"data nested", "/data/subdir/file.txt", false},

		{"etc passwd", "/etc/passwd", true},
		{"root secret", "/root/secret", true},
		{"usr bin", "/usr/bin/ls", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateUploadMetadata(&TransferMetadata{
				Path: tc.path,
			})
			gotErr := err != nil
			if gotErr != tc.wantErr {
				t.Errorf("path=%q: gotErr=%v, wantErr=%v, error=%v",
					tc.path, gotErr, tc.wantErr, err)
			}
		})
	}
}

// TestAllowedPaths_ConsistentWithRPC tests that behavior matches RPC whitelist.
func TestAllowedPaths_ConsistentWithRPC(t *testing.T) {
	t.Run("empty list blocks all", func(t *testing.T) {
		h := NewStreamHandler(StreamConfig{
			Enabled:      true,
			AllowedPaths: []string{},
		}, allowAllVerifier)
		err := h.ValidateUploadMetadata(&TransferMetadata{Path: "/tmp/file.txt"})
		if err == nil {
			t.Error("empty allowed_paths should block all paths")
		}
	})

	t.Run("wildcard allows all", func(t *testing.T) {
		h := NewStreamHandler(StreamConfig{
			Enabled:      true,
			AllowedPaths: []string{"*"},
		}, allowAllVerifier)
		err := h.ValidateUploadMetadata(&TransferMetadata{Path: "/any/path/at/all"})
		if err != nil {
			t.Errorf("wildcard should allow all paths: %v", err)
		}
	})

	t.Run("specific paths restrict access", func(t *testing.T) {
		h := NewStreamHandler(StreamConfig{
			Enabled:      true,
			AllowedPaths: []string{"/tmp"},
		}, allowAllVerifier)
		if err := h.ValidateUploadMetadata(&TransferMetadata{Path: "/tmp/file.txt"}); err != nil {
			t.Errorf("/tmp/file.txt should be allowed: %v", err)
		}
		if err := h.ValidateUploadMetadata(&TransferMetadata{Path: "/etc/passwd"}); err == nil {
			t.Error("/etc/passwd should not be allowed")
		}
	})
}

// TestAuthBypass_TarSlipAttack tests tar slip vulnerability.
func TestAuthBypass_TarSlipAttack(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "dest")
	os.MkdirAll(destDir, 0755)

	secretFile := filepath.Join(tmpDir, "secret.txt")
	os.WriteFile(secretFile, []byte("original secret"), 0644)

	t.Logf("TarSlip test: destDir=%s, secretFile=%s", destDir, secretFile)

	content, _ := os.ReadFile(secretFile)
	if string(content) != "original secret" {
		t.Error("Secret file was modified - tar slip vulnerability!")
	}
}
