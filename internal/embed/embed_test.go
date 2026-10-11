package embed

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestXOR(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"single byte", []byte{0x00}},
		{"short string", []byte("hello")},
		{"longer than key", []byte("this is a string that is longer than the 32-byte XOR key used for obfuscation")},
		{"binary data", []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}},
		{"yaml config", []byte("agent:\n  data_dir: /tmp/test\nsocks5:\n  enabled: true\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// XOR should be reversible
			encoded := XOR(tt.input)
			decoded := XOR(encoded)

			if !bytes.Equal(decoded, tt.input) {
				t.Errorf("XOR round-trip failed: got %v, want %v", decoded, tt.input)
			}

			// Encoded should be different from input (unless empty)
			if len(tt.input) > 0 && bytes.Equal(encoded, tt.input) {
				t.Error("XOR encoding produced identical output")
			}
		})
	}
}

func TestXORDeterministic(t *testing.T) {
	input := []byte("test config data")

	// XOR should produce the same output every time
	result1 := XOR(input)
	result2 := XOR(input)

	if !bytes.Equal(result1, result2) {
		t.Error("XOR is not deterministic")
	}
}

func TestAppendAndReadConfig(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a fake "binary" file
	srcBinary := filepath.Join(tmpDir, "test-binary")
	binaryContent := []byte("fake executable content here - this simulates a real binary")
	if err := os.WriteFile(srcBinary, binaryContent, 0755); err != nil {
		t.Fatalf("failed to create source binary: %v", err)
	}

	// Config to embed
	configContent := []byte(`agent:
  data_dir: /var/lib/muti-metroo
  display_name: "Test Agent"
socks5:
  enabled: true
  address: ":1080"
exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
`)

	// Create output binary with embedded config
	dstBinary := filepath.Join(tmpDir, "test-binary-embedded")
	if err := AppendConfig(srcBinary, dstBinary, configContent); err != nil {
		t.Fatalf("AppendConfig failed: %v", err)
	}

	// Verify the embedded binary is larger
	srcStat, _ := os.Stat(srcBinary)
	dstStat, _ := os.Stat(dstBinary)
	expectedSize := srcStat.Size() + int64(len(configContent)) + FooterSize
	if dstStat.Size() != expectedSize {
		t.Errorf("embedded binary size wrong: got %d, want %d", dstStat.Size(), expectedSize)
	}

	// Check HasEmbeddedConfig
	has, err := HasEmbeddedConfig(dstBinary)
	if err != nil {
		t.Fatalf("HasEmbeddedConfig failed: %v", err)
	}
	if !has {
		t.Error("HasEmbeddedConfig returned false for embedded binary")
	}

	// Check source doesn't have embedded config
	has, err = HasEmbeddedConfig(srcBinary)
	if err != nil {
		t.Fatalf("HasEmbeddedConfig failed on source: %v", err)
	}
	if has {
		t.Error("HasEmbeddedConfig returned true for source binary")
	}

	// Read back the config
	readConfig, err := ReadEmbeddedConfig(dstBinary)
	if err != nil {
		t.Fatalf("ReadEmbeddedConfig failed: %v", err)
	}

	if !bytes.Equal(readConfig, configContent) {
		t.Errorf("config content mismatch:\ngot: %s\nwant: %s", readConfig, configContent)
	}
}

func TestAppendConfigPreservesExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcBinary := filepath.Join(tmpDir, "executable")
	binaryContent := []byte("#!/bin/sh\necho hello\n")
	if err := os.WriteFile(srcBinary, binaryContent, 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Embed config
	config := []byte("test: config")
	dstBinary := filepath.Join(tmpDir, "executable-embedded")
	if err := AppendConfig(srcBinary, dstBinary, config); err != nil {
		t.Fatalf("AppendConfig failed: %v", err)
	}

	// Verify the original binary content is preserved at the start
	dstContent, err := os.ReadFile(dstBinary)
	if err != nil {
		t.Fatalf("failed to read dst: %v", err)
	}

	if !bytes.HasPrefix(dstContent, binaryContent) {
		t.Error("original binary content not preserved at start of embedded binary")
	}

	// Verify permissions are preserved
	srcStat, _ := os.Stat(srcBinary)
	dstStat, _ := os.Stat(dstBinary)
	if srcStat.Mode() != dstStat.Mode() {
		t.Errorf("permissions not preserved: got %v, want %v", dstStat.Mode(), srcStat.Mode())
	}
}

func TestAppendConfigAlreadyEmbedded(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcBinary := filepath.Join(tmpDir, "binary")
	if err := os.WriteFile(srcBinary, []byte("binary content"), 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Embed config first time
	config := []byte("config content")
	embeddedBinary := filepath.Join(tmpDir, "embedded")
	if err := AppendConfig(srcBinary, embeddedBinary, config); err != nil {
		t.Fatalf("first AppendConfig failed: %v", err)
	}

	// Try to embed again - should fail
	doubleEmbedded := filepath.Join(tmpDir, "double-embedded")
	err := AppendConfig(embeddedBinary, doubleEmbedded, []byte("more config"))
	if err != ErrAlreadyEmbedded {
		t.Errorf("expected ErrAlreadyEmbedded, got: %v", err)
	}
}

func TestReadEmbeddedConfigNoConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a plain file without embedded config
	plainFile := filepath.Join(tmpDir, "plain")
	if err := os.WriteFile(plainFile, []byte("just a regular file"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	_, err := ReadEmbeddedConfig(plainFile)
	if err != ErrNoEmbeddedConfig {
		t.Errorf("expected ErrNoEmbeddedConfig, got: %v", err)
	}
}

func TestReadEmbeddedConfigFileTooSmall(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file smaller than footer size
	smallFile := filepath.Join(tmpDir, "small")
	if err := os.WriteFile(smallFile, []byte("tiny"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	_, err := ReadEmbeddedConfig(smallFile)
	if err != ErrNoEmbeddedConfig {
		t.Errorf("expected ErrNoEmbeddedConfig for small file, got: %v", err)
	}
}

func TestGetOriginalBinarySize(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcBinary := filepath.Join(tmpDir, "binary")
	binaryContent := []byte("original binary content - exactly 38 chars")
	if err := os.WriteFile(srcBinary, binaryContent, 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Test without embedded config
	size, err := GetOriginalBinarySize(srcBinary)
	if err != nil {
		t.Fatalf("GetOriginalBinarySize failed: %v", err)
	}
	if size != int64(len(binaryContent)) {
		t.Errorf("size without embed: got %d, want %d", size, len(binaryContent))
	}

	// Embed config
	config := []byte("embedded config data")
	embeddedBinary := filepath.Join(tmpDir, "embedded")
	if err := AppendConfig(srcBinary, embeddedBinary, config); err != nil {
		t.Fatalf("AppendConfig failed: %v", err)
	}

	// Test with embedded config
	size, err = GetOriginalBinarySize(embeddedBinary)
	if err != nil {
		t.Fatalf("GetOriginalBinarySize failed on embedded: %v", err)
	}
	if size != int64(len(binaryContent)) {
		t.Errorf("size with embed: got %d, want %d", size, len(binaryContent))
	}
}

func TestCopyBinaryWithoutConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcBinary := filepath.Join(tmpDir, "binary")
	binaryContent := []byte("original binary content")
	if err := os.WriteFile(srcBinary, binaryContent, 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Embed config
	config := []byte("config to be stripped")
	embeddedBinary := filepath.Join(tmpDir, "embedded")
	if err := AppendConfig(srcBinary, embeddedBinary, config); err != nil {
		t.Fatalf("AppendConfig failed: %v", err)
	}

	// Copy without config
	strippedBinary := filepath.Join(tmpDir, "stripped")
	if err := CopyBinaryWithoutConfig(embeddedBinary, strippedBinary); err != nil {
		t.Fatalf("CopyBinaryWithoutConfig failed: %v", err)
	}

	// Verify content matches original
	strippedContent, err := os.ReadFile(strippedBinary)
	if err != nil {
		t.Fatalf("failed to read stripped: %v", err)
	}

	if !bytes.Equal(strippedContent, binaryContent) {
		t.Errorf("stripped content doesn't match original:\ngot: %s\nwant: %s", strippedContent, binaryContent)
	}

	// Verify no embedded config
	has, _ := HasEmbeddedConfig(strippedBinary)
	if has {
		t.Error("stripped binary still has embedded config")
	}
}

func TestEmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()

	srcBinary := filepath.Join(tmpDir, "binary")
	if err := os.WriteFile(srcBinary, []byte("binary"), 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Empty config should work but read back as empty
	dstBinary := filepath.Join(tmpDir, "embedded")
	if err := AppendConfig(srcBinary, dstBinary, []byte{}); err != nil {
		t.Fatalf("AppendConfig with empty config failed: %v", err)
	}

	// Reading empty config should return ErrNoEmbeddedConfig (length is 0)
	_, err := ReadEmbeddedConfig(dstBinary)
	if err != ErrNoEmbeddedConfig {
		t.Errorf("expected ErrNoEmbeddedConfig for empty config, got: %v", err)
	}
}

func TestLargeConfig(t *testing.T) {
	tmpDir := t.TempDir()

	srcBinary := filepath.Join(tmpDir, "binary")
	if err := os.WriteFile(srcBinary, []byte("binary content"), 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Create a large config (1MB)
	largeConfig := make([]byte, 1024*1024)
	for i := range largeConfig {
		largeConfig[i] = byte(i % 256)
	}

	dstBinary := filepath.Join(tmpDir, "embedded")
	if err := AppendConfig(srcBinary, dstBinary, largeConfig); err != nil {
		t.Fatalf("AppendConfig with large config failed: %v", err)
	}

	// Read back
	readConfig, err := ReadEmbeddedConfig(dstBinary)
	if err != nil {
		t.Fatalf("ReadEmbeddedConfig failed: %v", err)
	}

	if !bytes.Equal(readConfig, largeConfig) {
		t.Error("large config round-trip failed")
	}
}

func TestHasEmbeddedConfigNonexistent(t *testing.T) {
	has, err := HasEmbeddedConfig("/nonexistent/path/to/binary")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if has {
		t.Error("expected false for nonexistent file")
	}
}

func TestInPlaceEmbed(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	binaryPath := filepath.Join(tmpDir, "binary")
	binaryContent := []byte("original binary content")
	if err := os.WriteFile(binaryPath, binaryContent, 0755); err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Embed config in place (src == dst)
	config := []byte("config content")
	if err := AppendConfig(binaryPath, binaryPath, config); err != nil {
		t.Fatalf("in-place AppendConfig failed: %v", err)
	}

	// Verify
	has, _ := HasEmbeddedConfig(binaryPath)
	if !has {
		t.Error("in-place embed: no embedded config found")
	}

	readConfig, err := ReadEmbeddedConfig(binaryPath)
	if err != nil {
		t.Fatalf("ReadEmbeddedConfig failed: %v", err)
	}

	if !bytes.Equal(readConfig, config) {
		t.Error("in-place embed: config mismatch")
	}
}
