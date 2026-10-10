package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewKeypair(t *testing.T) {
	kp1, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}

	// Check keys are not zero
	if IsZeroKey(kp1.PrivateKey) {
		t.Error("private key is zero")
	}
	if IsZeroKey(kp1.PublicKey) {
		t.Error("public key is zero")
	}

	// Generate another keypair and verify they are different
	kp2, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() second call error = %v", err)
	}

	if kp1.PrivateKey == kp2.PrivateKey {
		t.Error("two generated private keys are identical")
	}
	if kp1.PublicKey == kp2.PublicKey {
		t.Error("two generated public keys are identical")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid lowercase",
			input:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr: false,
		},
		{
			name:    "valid uppercase",
			input:   "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF",
			wantErr: false,
		},
		{
			name:    "with 0x prefix",
			input:   "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr: false,
		},
		{
			name:    "with whitespace",
			input:   "  0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  ",
			wantErr: false,
		},
		{
			name:    "too short",
			input:   "0123456789abcdef",
			wantErr: true,
		},
		{
			name:    "too long",
			input:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef00",
			wantErr: true,
		},
		{
			name:    "invalid hex",
			input:   "zzzz456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseKey(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKeyToString(t *testing.T) {
	key := [KeySize]byte{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}

	s := KeyToString(key)
	if len(s) != KeySize*2 {
		t.Errorf("KeyToString() length = %d, want %d", len(s), KeySize*2)
	}

	// Parse it back
	parsed, err := ParseKey(s)
	if err != nil {
		t.Fatalf("ParseKey(KeyToString()) error = %v", err)
	}
	if parsed != key {
		t.Error("round-trip failed")
	}
}

func TestIsZeroKey(t *testing.T) {
	var zeroKey [KeySize]byte
	if !IsZeroKey(zeroKey) {
		t.Error("IsZeroKey(zero) = false, want true")
	}

	nonZeroKey := [KeySize]byte{1}
	if IsZeroKey(nonZeroKey) {
		t.Error("IsZeroKey(nonzero) = true, want false")
	}
}

func TestKeypairStoreLoad(t *testing.T) {
	tmpDir := t.TempDir()

	// Create and store keypair
	kp1, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}

	if err := kp1.Store(tmpDir); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Verify files exist with correct permissions
	privPath := filepath.Join(tmpDir, keyFileName)
	pubPath := filepath.Join(tmpDir, pubKeyFileName)

	privInfo, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("private key file not found: %v", err)
	}
	if privInfo.Mode().Perm() != 0600 {
		t.Errorf("private key permissions = %o, want 0600", privInfo.Mode().Perm())
	}

	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("public key file not found: %v", err)
	}
	if pubInfo.Mode().Perm() != 0644 {
		t.Errorf("public key permissions = %o, want 0644", pubInfo.Mode().Perm())
	}

	// Load keypair and verify it matches
	kp2, err := LoadKeypair(tmpDir)
	if err != nil {
		t.Fatalf("LoadKeypair() error = %v", err)
	}

	if kp1.PrivateKey != kp2.PrivateKey {
		t.Error("loaded private key does not match")
	}
	if kp1.PublicKey != kp2.PublicKey {
		t.Error("loaded public key does not match")
	}
}

func TestLoadOrCreateKeypair_Create(t *testing.T) {
	tmpDir := t.TempDir()

	// First call should create
	kp1, created1, err := LoadOrCreateKeypair(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeypair() error = %v", err)
	}
	if !created1 {
		t.Error("expected created = true on first call")
	}
	if IsZeroKey(kp1.PublicKey) {
		t.Error("keypair public key is zero")
	}

	// Second call should load
	kp2, created2, err := LoadOrCreateKeypair(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeypair() second call error = %v", err)
	}
	if created2 {
		t.Error("expected created = false on second call")
	}
	if kp1.PublicKey != kp2.PublicKey {
		t.Error("loaded keypair does not match created one")
	}
}

func TestLoadKeypair_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadKeypair(tmpDir)
	if err == nil {
		t.Error("LoadKeypair() should fail when keypair does not exist")
	}
}

func TestLoadKeypair_CorruptedPublicKey(t *testing.T) {
	tmpDir := t.TempDir()

	// Create keypair
	kp, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}
	if err := kp.Store(tmpDir); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Corrupt public key file with a different valid key
	differentKey := [KeySize]byte{0xFF, 0xFF, 0xFF, 0xFF}
	pubPath := filepath.Join(tmpDir, pubKeyFileName)
	if err := os.WriteFile(pubPath, []byte(KeyToString(differentKey)+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Should fail because public key doesn't match private key
	_, err = LoadKeypair(tmpDir)
	if err == nil {
		t.Error("LoadKeypair() should fail with mismatched keys")
	}
}

func TestKeypairExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not exist initially
	if KeypairExists(tmpDir) {
		t.Error("KeypairExists() = true before creation")
	}

	// Create keypair
	kp, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}
	if err := kp.Store(tmpDir); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Should exist now
	if !KeypairExists(tmpDir) {
		t.Error("KeypairExists() = false after creation")
	}
}

func TestKeypairZero(t *testing.T) {
	kp, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}

	// Verify key is not zero
	if IsZeroKey(kp.PrivateKey) {
		t.Error("private key is already zero")
	}

	// Zero it
	kp.Zero()

	// Verify it's now zero
	if !IsZeroKey(kp.PrivateKey) {
		t.Error("private key was not zeroed")
	}

	// Public key should not be affected
	if IsZeroKey(kp.PublicKey) {
		t.Error("public key was unexpectedly zeroed")
	}
}

func TestKeypairPublicKeyString(t *testing.T) {
	kp, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}

	s := kp.PublicKeyString()
	if len(s) != KeySize*2 {
		t.Errorf("PublicKeyString() length = %d, want %d", len(s), KeySize*2)
	}

	short := kp.PublicKeyShortString()
	if len(short) != 16 {
		t.Errorf("PublicKeyShortString() length = %d, want 16", len(short))
	}
}

func TestStoreZeroKey(t *testing.T) {
	tmpDir := t.TempDir()

	// Should fail with zero private key
	kp := &Keypair{}
	err := kp.Store(tmpDir)
	if err == nil {
		t.Error("Store() should fail with zero private key")
	}
}

// Tests for KeypairFromConfig

func TestKeypairFromConfig_ValidPrivateKey(t *testing.T) {
	// Valid 64-character hex string
	privateKeyHex := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	kp, err := KeypairFromConfig(privateKeyHex, "")
	if err != nil {
		t.Fatalf("KeypairFromConfig() error = %v", err)
	}

	// Verify private key was parsed correctly
	if kp.PrivateKey[0] != 0x48 || kp.PrivateKey[1] != 0xbb {
		t.Errorf("private key first bytes = %x %x, want 48 bb", kp.PrivateKey[0], kp.PrivateKey[1])
	}

	// Verify public key was derived (not zero)
	if IsZeroKey(kp.PublicKey) {
		t.Error("public key is zero, should be derived from private key")
	}

	// Verify public key is correctly derived
	derivedPub := DerivePublicKey(kp.PrivateKey)
	if kp.PublicKey != derivedPub {
		t.Error("public key does not match derivation from private key")
	}
}

func TestKeypairFromConfig_WithMatchingPublicKey(t *testing.T) {
	privateKeyHex := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	// First get the correct public key by deriving it
	privKey, _ := ParseKey(privateKeyHex)
	correctPublicKey := DerivePublicKey(privKey)
	publicKeyHex := KeyToString(correctPublicKey)

	kp, err := KeypairFromConfig(privateKeyHex, publicKeyHex)
	if err != nil {
		t.Fatalf("KeypairFromConfig() error = %v", err)
	}

	if kp.PublicKey != correctPublicKey {
		t.Error("public key does not match expected derived key")
	}
}

func TestKeypairFromConfig_MismatchedPublicKey(t *testing.T) {
	privateKeyHex := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"
	// A different public key that doesn't match the private key
	wrongPublicKeyHex := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	_, err := KeypairFromConfig(privateKeyHex, wrongPublicKeyHex)
	if err == nil {
		t.Error("KeypairFromConfig() should fail with mismatched public key")
	}
	if err.Error() != "public key does not match derivation from private key" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestKeypairFromConfig_EmptyPrivateKey(t *testing.T) {
	_, err := KeypairFromConfig("", "")
	if err == nil {
		t.Error("KeypairFromConfig() should fail with empty private key")
	}
	if err.Error() != "private key is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestKeypairFromConfig_InvalidPrivateKeyHex(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
	}{
		{"too short", "a1b2c3d4"},
		{"invalid hex", "not_valid_hex_string_here_needs_to_be_64_chars_long_for_testing!"},
		{"odd length", "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := KeypairFromConfig(tt.privateKey, "")
			if err == nil {
				t.Error("KeypairFromConfig() should fail for invalid private key")
			}
		})
	}
}

func TestKeypairFromConfig_InvalidPublicKeyHex(t *testing.T) {
	validPrivateKey := "48bbea6c0c9be254bde983c92c8a53db759f27e51a6ae77fd9cca81895a5d57c"

	tests := []struct {
		name      string
		publicKey string
	}{
		{"too short", "a1b2c3d4"},
		{"invalid hex", "not_valid_hex_string_here_needs_to_be_64_chars_long_for_testing!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := KeypairFromConfig(validPrivateKey, tt.publicKey)
			if err == nil {
				t.Error("KeypairFromConfig() should fail for invalid public key")
			}
		})
	}
}

func TestKeypairFromConfig_RoundTrip(t *testing.T) {
	// Generate a keypair the normal way
	origKP, err := NewKeypair()
	if err != nil {
		t.Fatalf("NewKeypair() error = %v", err)
	}

	// Export to hex strings
	privateKeyHex := KeyToString(origKP.PrivateKey)
	publicKeyHex := KeyToString(origKP.PublicKey)

	// Load it via KeypairFromConfig
	loadedKP, err := KeypairFromConfig(privateKeyHex, publicKeyHex)
	if err != nil {
		t.Fatalf("KeypairFromConfig() error = %v", err)
	}

	// Verify the keys match
	if loadedKP.PrivateKey != origKP.PrivateKey {
		t.Error("private key does not match after round-trip")
	}
	if loadedKP.PublicKey != origKP.PublicKey {
		t.Error("public key does not match after round-trip")
	}
}
