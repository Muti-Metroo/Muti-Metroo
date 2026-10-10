package crypto

import (
	"bytes"
	"testing"
)

func TestSealedBox_SealOpen_Roundtrip(t *testing.T) {
	// Generate a keypair for testing
	privateKey, publicKey, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	// Create sealed box with both keys (operator mode)
	box := NewSealedBoxWithPrivate(publicKey, privateKey)

	testCases := []struct {
		name      string
		plaintext []byte
	}{
		{"empty", []byte{}},
		{"short", []byte("hello")},
		{"medium", []byte("The quick brown fox jumps over the lazy dog")},
		{"long", bytes.Repeat([]byte("A"), 10000)},
		{"binary", []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Seal
			ciphertext, err := box.Seal(tc.plaintext)
			if err != nil {
				t.Fatalf("Seal failed: %v", err)
			}

			// Verify overhead
			expectedLen := len(tc.plaintext) + SealedBoxOverhead
			if len(ciphertext) != expectedLen {
				t.Errorf("ciphertext length = %d, want %d", len(ciphertext), expectedLen)
			}

			// Open
			decrypted, err := box.Open(ciphertext)
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Verify plaintext matches
			if !bytes.Equal(decrypted, tc.plaintext) {
				t.Errorf("decrypted does not match plaintext")
			}
		})
	}
}

func TestSealedBox_EncryptOnlyMode(t *testing.T) {
	// Generate a keypair
	privateKey, publicKey, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	// Create encrypt-only box (field agent mode)
	encryptOnly := NewSealedBox(publicKey)

	// Should be able to encrypt
	plaintext := []byte("secret message")
	ciphertext, err := encryptOnly.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal failed: %v", err)
	}

	// Should NOT be able to decrypt
	if encryptOnly.CanDecrypt() {
		t.Error("CanDecrypt() = true, want false for encrypt-only mode")
	}

	_, err = encryptOnly.Open(ciphertext)
	if err != ErrNoPrivateKey {
		t.Errorf("Open() error = %v, want ErrNoPrivateKey", err)
	}

	// Create operator box with private key - should be able to decrypt
	operatorBox := NewSealedBoxWithPrivate(publicKey, privateKey)
	decrypted, err := operatorBox.Open(ciphertext)
	if err != nil {
		t.Fatalf("Operator Open failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Error("decrypted does not match plaintext")
	}
}

func TestSealedBox_DifferentCiphertextEachTime(t *testing.T) {
	privateKey, publicKey, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	box := NewSealedBoxWithPrivate(publicKey, privateKey)
	plaintext := []byte("same plaintext")

	// Seal the same plaintext multiple times
	ciphertext1, _ := box.Seal(plaintext)
	ciphertext2, _ := box.Seal(plaintext)
	ciphertext3, _ := box.Seal(plaintext)

	// Each ciphertext should be different (due to ephemeral keys and nonces)
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("ciphertext1 == ciphertext2, expected different")
	}
	if bytes.Equal(ciphertext2, ciphertext3) {
		t.Error("ciphertext2 == ciphertext3, expected different")
	}

	// But all should decrypt to the same plaintext
	for i, ct := range [][]byte{ciphertext1, ciphertext2, ciphertext3} {
		decrypted, err := box.Open(ct)
		if err != nil {
			t.Errorf("Open ciphertext%d failed: %v", i+1, err)
		}
		if !bytes.Equal(decrypted, plaintext) {
			t.Errorf("ciphertext%d decrypted incorrectly", i+1)
		}
	}
}

func TestSealedBox_InvalidCiphertext(t *testing.T) {
	privateKey, publicKey, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	box := NewSealedBoxWithPrivate(publicKey, privateKey)

	testCases := []struct {
		name       string
		ciphertext []byte
		wantErr    error
		anyErr     bool // if true, any error is acceptable
	}{
		{"empty", []byte{}, ErrInvalidCiphertext, false},
		{"too_short", make([]byte, SealedBoxOverhead-1), ErrInvalidCiphertext, false},
		{"minimum_length_garbage", make([]byte, SealedBoxOverhead), nil, true}, // garbage may fail at ECDH or decrypt
		{"corrupted", func() []byte {
			ct, _ := box.Seal([]byte("test"))
			ct[len(ct)-1] ^= 0xff // Flip bits in tag
			return ct
		}(), ErrDecryptionFailed, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := box.Open(tc.ciphertext)
			if tc.anyErr {
				if err == nil {
					t.Error("Open() expected error, got nil")
				}
			} else if err != tc.wantErr {
				t.Errorf("Open() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSealedBox_WrongPrivateKey(t *testing.T) {
	// Generate two different keypairs
	privateKey1, publicKey1, _ := GenerateEphemeralKeypair()
	privateKey2, publicKey2, _ := GenerateEphemeralKeypair()

	// Seal with publicKey1
	senderBox := NewSealedBox(publicKey1)
	plaintext := []byte("secret message")
	ciphertext, _ := senderBox.Seal(plaintext)

	// Try to open with wrong private key (privateKey2)
	wrongBox := NewSealedBoxWithPrivate(publicKey2, privateKey2)
	_, err := wrongBox.Open(ciphertext)
	if err != ErrDecryptionFailed {
		t.Errorf("Open with wrong key: error = %v, want ErrDecryptionFailed", err)
	}

	// Should work with correct private key
	correctBox := NewSealedBoxWithPrivate(publicKey1, privateKey1)
	decrypted, err := correctBox.Open(ciphertext)
	if err != nil {
		t.Fatalf("Open with correct key failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Error("decrypted does not match plaintext")
	}
}

func TestSealedBox_Zero(t *testing.T) {
	privateKey, publicKey, _ := GenerateEphemeralKeypair()
	box := NewSealedBoxWithPrivate(publicKey, privateKey)

	// Should be able to decrypt initially
	if !box.CanDecrypt() {
		t.Error("CanDecrypt() = false before Zero(), want true")
	}

	// Zero the box
	box.Zero()

	// Should not be able to decrypt after zeroing
	if box.CanDecrypt() {
		t.Error("CanDecrypt() = true after Zero(), want false")
	}

	// Verify private key is zeroed
	var zeroKey [KeySize]byte
	if box.privateKey != zeroKey {
		t.Error("privateKey not zeroed after Zero()")
	}
}

func TestSealedBox_PublicKey(t *testing.T) {
	_, publicKey, _ := GenerateEphemeralKeypair()
	box := NewSealedBox(publicKey)

	got := box.PublicKey()
	if got != publicKey {
		t.Error("PublicKey() does not match input")
	}
}

func TestSealedBoxOverhead(t *testing.T) {
	// Verify the overhead constant is correct
	expected := KeySize + NonceSize + TagSize // 32 + 12 + 16 = 60
	if SealedBoxOverhead != expected {
		t.Errorf("SealedBoxOverhead = %d, want %d", SealedBoxOverhead, expected)
	}
	if SealedBoxOverhead != 60 {
		t.Errorf("SealedBoxOverhead = %d, want 60", SealedBoxOverhead)
	}
}

func BenchmarkSealedBox_Seal(b *testing.B) {
	_, publicKey, _ := GenerateEphemeralKeypair()
	box := NewSealedBox(publicKey)
	plaintext := make([]byte, 1024) // 1KB payload (typical NodeInfo size)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = box.Seal(plaintext)
	}
}

func BenchmarkSealedBox_Open(b *testing.B) {
	privateKey, publicKey, _ := GenerateEphemeralKeypair()
	box := NewSealedBoxWithPrivate(publicKey, privateKey)
	plaintext := make([]byte, 1024)
	ciphertext, _ := box.Seal(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = box.Open(ciphertext)
	}
}
