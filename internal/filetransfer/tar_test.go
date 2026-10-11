package filetransfer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTarDirectory_Basic(t *testing.T) {
	// Create a temp directory with some files
	srcDir := t.TempDir()

	// Create files and subdirectories
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file2.txt"), []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(srcDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Tar the directory
	var buf bytes.Buffer
	if err := TarDirectory(context.Background(), srcDir, &buf); err != nil {
		t.Fatalf("TarDirectory failed: %v", err)
	}

	// Verify we got some output
	if buf.Len() == 0 {
		t.Fatal("TarDirectory produced no output")
	}

	// Untar to a new directory
	destDir := t.TempDir()
	if err := UntarDirectory(context.Background(), &buf, destDir); err != nil {
		t.Fatalf("UntarDirectory failed: %v", err)
	}

	// Verify files exist
	content1, err := os.ReadFile(filepath.Join(destDir, "file1.txt"))
	if err != nil {
		t.Fatalf("failed to read file1.txt: %v", err)
	}
	if string(content1) != "content1" {
		t.Errorf("file1.txt content mismatch: got %q, want %q", string(content1), "content1")
	}

	content2, err := os.ReadFile(filepath.Join(destDir, "file2.txt"))
	if err != nil {
		t.Fatalf("failed to read file2.txt: %v", err)
	}
	if string(content2) != "content2" {
		t.Errorf("file2.txt content mismatch: got %q, want %q", string(content2), "content2")
	}

	nested, err := os.ReadFile(filepath.Join(destDir, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("failed to read nested.txt: %v", err)
	}
	if string(nested) != "nested content" {
		t.Errorf("nested.txt content mismatch: got %q, want %q", string(nested), "nested content")
	}
}

func TestTarDirectory_EmptyDir(t *testing.T) {
	srcDir := t.TempDir()

	var buf bytes.Buffer
	if err := TarDirectory(context.Background(), srcDir, &buf); err != nil {
		t.Fatalf("TarDirectory failed on empty dir: %v", err)
	}

	// Should produce minimal output (just gzip header)
	if buf.Len() == 0 {
		t.Fatal("TarDirectory produced no output for empty dir")
	}

	destDir := t.TempDir()
	if err := UntarDirectory(context.Background(), &buf, destDir); err != nil {
		t.Fatalf("UntarDirectory failed: %v", err)
	}
}

func TestTarDirectory_PreservesPermissions(t *testing.T) {
	srcDir := t.TempDir()

	// Create file with specific permissions
	filePath := filepath.Join(srcDir, "executable.sh")
	if err := os.WriteFile(filePath, []byte("#!/bin/bash\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := TarDirectory(context.Background(), srcDir, &buf); err != nil {
		t.Fatalf("TarDirectory failed: %v", err)
	}

	destDir := t.TempDir()
	if err := UntarDirectory(context.Background(), &buf, destDir); err != nil {
		t.Fatalf("UntarDirectory failed: %v", err)
	}

	// Check permissions
	info, err := os.Stat(filepath.Join(destDir, "executable.sh"))
	if err != nil {
		t.Fatal(err)
	}

	// Check executable bit is set (at least user execute)
	if info.Mode().Perm()&0100 == 0 {
		t.Errorf("executable bit not preserved: got %o", info.Mode().Perm())
	}
}

func TestTarDirectory_NotADirectory(t *testing.T) {
	// Create a regular file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	var buf bytes.Buffer
	err = TarDirectory(context.Background(), tmpFile.Name(), &buf)
	if err == nil {
		t.Fatal("expected error when tarring a file, not a directory")
	}
}

func TestUntarDirectory_DirectoryTraversal(t *testing.T) {
	// This test verifies that we reject malicious tar entries with path traversal
	// We can't easily create such a tar in Go, so we test the sanitizeTarPath function directly

	destDir := "/tmp/safe"

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"normal file", "file.txt", false},
		{"nested file", "dir/file.txt", false},
		{"absolute path", "/etc/passwd", true},
		{"parent traversal", "../escape.txt", true},
		{"nested traversal", "dir/../../../escape.txt", true},
		{"dot-dot only", "..", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sanitizeTarPath(destDir, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeTarPath(%q, %q) error = %v, wantErr %v", destDir, tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSymlink(t *testing.T) {
	destDir := "/tmp/safe"

	tests := []struct {
		name        string
		symlinkPath string
		target      string
		wantErr     bool
	}{
		{"relative safe", "/tmp/safe/link", "file.txt", false},
		{"relative nested safe", "/tmp/safe/dir/link", "../file.txt", false},
		{"absolute target", "/tmp/safe/link", "/etc/passwd", true},
		{"escaping target", "/tmp/safe/link", "../../escape", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSymlink(destDir, tt.symlinkPath, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSymlink error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateDirectorySize(t *testing.T) {
	srcDir := t.TempDir()

	// Create files with known sizes
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "file2.txt"), []byte("1234567890"), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(srcDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}

	size, err := CalculateDirectorySize(srcDir)
	if err != nil {
		t.Fatalf("CalculateDirectorySize failed: %v", err)
	}

	// 5 + 10 + 3 = 18 bytes
	if size != 18 {
		t.Errorf("CalculateDirectorySize = %d, want 18", size)
	}
}

func TestTarDirectory_WithSymlinks(t *testing.T) {
	srcDir := t.TempDir()

	// Create a regular file
	if err := os.WriteFile(filepath.Join(srcDir, "original.txt"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink (skip on Windows)
	symlinkPath := filepath.Join(srcDir, "link.txt")
	if err := os.Symlink("original.txt", symlinkPath); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	var buf bytes.Buffer
	if err := TarDirectory(context.Background(), srcDir, &buf); err != nil {
		t.Fatalf("TarDirectory failed: %v", err)
	}

	destDir := t.TempDir()
	if err := UntarDirectory(context.Background(), &buf, destDir); err != nil {
		t.Fatalf("UntarDirectory failed: %v", err)
	}

	// Verify symlink exists and points to correct target
	linkTarget, err := os.Readlink(filepath.Join(destDir, "link.txt"))
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}
	if linkTarget != "original.txt" {
		t.Errorf("symlink target = %q, want %q", linkTarget, "original.txt")
	}

	// Verify symlink resolves to correct content
	content, err := os.ReadFile(filepath.Join(destDir, "link.txt"))
	if err != nil {
		t.Fatalf("failed to read through symlink: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("symlink content = %q, want %q", string(content), "original")
	}
}

func TestTarDirectory_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	srcDir := t.TempDir()

	// Create a 1MB file
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "large.bin"), largeData, 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := TarDirectory(context.Background(), srcDir, &buf); err != nil {
		t.Fatalf("TarDirectory failed: %v", err)
	}

	// Compressed size should be smaller due to pattern
	t.Logf("Original size: %d, Compressed size: %d", len(largeData), buf.Len())

	destDir := t.TempDir()
	if err := UntarDirectory(context.Background(), &buf, destDir); err != nil {
		t.Fatalf("UntarDirectory failed: %v", err)
	}

	// Verify content
	restored, err := os.ReadFile(filepath.Join(destDir, "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != len(largeData) {
		t.Errorf("restored size = %d, want %d", len(restored), len(largeData))
	}
	for i := range restored {
		if restored[i] != largeData[i] {
			t.Errorf("content mismatch at byte %d: got %d, want %d", i, restored[i], largeData[i])
			break
		}
	}
}
