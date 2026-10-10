package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAgentID(t *testing.T) {
	id1, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	if id1.IsZero() {
		t.Error("NewAgentID() returned zero ID")
	}

	// Generate another ID and verify they're different
	id2, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	if id1.Equal(id2) {
		t.Error("NewAgentID() returned duplicate IDs")
	}
}

func TestAgentID_String(t *testing.T) {
	id, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	s := id.String()
	if len(s) != 32 { // 16 bytes * 2 hex chars
		t.Errorf("String() length = %d, want 32", len(s))
	}
}

func TestAgentID_ShortString(t *testing.T) {
	id, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	s := id.ShortString()
	if len(s) != 8 { // 4 bytes * 2 hex chars
		t.Errorf("ShortString() length = %d, want 8", len(s))
	}

	// Short string should be prefix of full string
	full := id.String()
	if s != full[:8] {
		t.Errorf("ShortString() = %s, want prefix of %s", s, full)
	}
}

func TestParseAgentID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid hex string",
			input:   "a3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e",
			wantErr: false,
		},
		{
			name:    "valid with 0x prefix",
			input:   "0xa3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e",
			wantErr: false,
		},
		{
			name:    "valid with whitespace",
			input:   "  a3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e  ",
			wantErr: false,
		},
		{
			name:    "too short",
			input:   "a3f8c2d1e5b94a7c",
			wantErr: true,
		},
		{
			name:    "too long",
			input:   "a3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e00",
			wantErr: true,
		},
		{
			name:    "invalid hex chars",
			input:   "g3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseAgentID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAgentID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && id.IsZero() {
				t.Error("ParseAgentID() returned zero ID for valid input")
			}
		})
	}
}

func TestFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{
			name:    "valid 16 bytes",
			input:   make([]byte, 16),
			wantErr: false,
		},
		{
			name:    "too short",
			input:   make([]byte, 15),
			wantErr: true,
		},
		{
			name:    "too long",
			input:   make([]byte, 17),
			wantErr: true,
		},
		{
			name:    "empty",
			input:   []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromBytes(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAgentID_Bytes(t *testing.T) {
	id, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	b := id.Bytes()
	if len(b) != IDSize {
		t.Errorf("Bytes() length = %d, want %d", len(b), IDSize)
	}

	// Verify round-trip
	id2, err := FromBytes(b)
	if err != nil {
		t.Fatalf("FromBytes() error = %v", err)
	}
	if !id.Equal(id2) {
		t.Error("Round-trip through Bytes() failed")
	}
}

func TestAgentID_IsZero(t *testing.T) {
	var zero AgentID
	if !zero.IsZero() {
		t.Error("IsZero() = false for zero ID")
	}

	id, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}
	if id.IsZero() {
		t.Error("IsZero() = true for non-zero ID")
	}
}

func TestAgentID_Equal(t *testing.T) {
	id1, _ := ParseAgentID("a3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e")
	id2, _ := ParseAgentID("a3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e")
	id3, _ := ParseAgentID("b3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e")

	if !id1.Equal(id2) {
		t.Error("Equal() = false for identical IDs")
	}
	if id1.Equal(id3) {
		t.Error("Equal() = true for different IDs")
	}
}

func TestAgentID_MarshalUnmarshalText(t *testing.T) {
	original, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	// Marshal
	text, err := original.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}

	// Unmarshal
	var restored AgentID
	if err := restored.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}

	if !original.Equal(restored) {
		t.Errorf("Round-trip failed: original=%s, restored=%s", original, restored)
	}
}

func TestAgentID_StoreAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate and store ID
	original, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	if err := original.Store(tmpDir); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Verify file exists
	filePath := filepath.Join(tmpDir, "agent_id")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Store() did not create file")
	}

	// Load and compare
	loaded, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !original.Equal(loaded) {
		t.Errorf("Load() = %s, want %s", loaded, original)
	}
}

func TestAgentID_Store_ZeroID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var zero AgentID
	if err := zero.Store(tmpDir); err == nil {
		t.Error("Store() should fail for zero ID")
	}
}

func TestLoad_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = Load(tmpDir)
	if err == nil {
		t.Error("Load() should fail when file doesn't exist")
	}
}

func TestLoadOrCreate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// First call should create
	id1, created1, err := LoadOrCreate(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if !created1 {
		t.Error("LoadOrCreate() created = false on first call")
	}
	if id1.IsZero() {
		t.Error("LoadOrCreate() returned zero ID")
	}

	// Second call should load
	id2, created2, err := LoadOrCreate(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if created2 {
		t.Error("LoadOrCreate() created = true on second call")
	}
	if !id1.Equal(id2) {
		t.Errorf("LoadOrCreate() returned different ID: %s vs %s", id1, id2)
	}
}

func TestExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "muti-metroo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Should not exist initially
	if Exists(tmpDir) {
		t.Error("Exists() = true before creating ID")
	}

	// Create ID
	id, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}
	if err := id.Store(tmpDir); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Should exist now
	if !Exists(tmpDir) {
		t.Error("Exists() = false after creating ID")
	}
}

func TestParseAgentID_RoundTrip(t *testing.T) {
	original, err := NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}

	// String -> Parse -> String should be identical
	s1 := original.String()
	parsed, err := ParseAgentID(s1)
	if err != nil {
		t.Fatalf("ParseAgentID() error = %v", err)
	}
	s2 := parsed.String()

	if s1 != s2 {
		t.Errorf("Round-trip failed: %s != %s", s1, s2)
	}
}
