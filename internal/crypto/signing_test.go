package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateSigningKeypair(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	// Check that keys are not zero
	var zeroPublic [Ed25519PublicKeySize]byte
	var zeroPrivate [Ed25519PrivateKeySize]byte

	if kp.PublicKey == zeroPublic {
		t.Error("GenerateSigningKeypair() generated zero public key")
	}
	if kp.PrivateKey == zeroPrivate {
		t.Error("GenerateSigningKeypair() generated zero private key")
	}

	// Check that multiple calls generate different keys
	kp2, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() second call error = %v", err)
	}
	if kp.PublicKey == kp2.PublicKey {
		t.Error("GenerateSigningKeypair() generated same public key twice")
	}
}

func TestSigningKeypairFromSeed(t *testing.T) {
	// Generate a random seed
	var seed [Ed25519SeedSize]byte
	if err := RandomBytes(seed[:]); err != nil {
		t.Fatalf("RandomBytes() error = %v", err)
	}

	kp1 := SigningKeypairFromSeed(seed)
	kp2 := SigningKeypairFromSeed(seed)

	// Same seed should produce same keypair
	if kp1.PublicKey != kp2.PublicKey {
		t.Error("SigningKeypairFromSeed() different public keys from same seed")
	}
	if kp1.PrivateKey != kp2.PrivateKey {
		t.Error("SigningKeypairFromSeed() different private keys from same seed")
	}
}

func TestPublicKeyFromPrivate(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	derivedPub := PublicKeyFromPrivate(kp.PrivateKey)
	if derivedPub != kp.PublicKey {
		t.Error("PublicKeyFromPrivate() derived different public key")
	}
}

func TestSignAndVerify(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	message := []byte("test message for signing")
	signature := Sign(kp.PrivateKey, message)

	// Verify signature
	if !Verify(kp.PublicKey, message, signature) {
		t.Error("Verify() returned false for valid signature")
	}

	// Verify with wrong message
	wrongMessage := []byte("wrong message")
	if Verify(kp.PublicKey, wrongMessage, signature) {
		t.Error("Verify() returned true for wrong message")
	}

	// Verify with wrong public key
	kp2, _ := GenerateSigningKeypair()
	if Verify(kp2.PublicKey, message, signature) {
		t.Error("Verify() returned true for wrong public key")
	}

	// Verify with modified signature
	modifiedSig := signature
	modifiedSig[0] ^= 0xFF
	if Verify(kp.PublicKey, message, modifiedSig) {
		t.Error("Verify() returned true for modified signature")
	}
}

func TestIsZeroSignature(t *testing.T) {
	var zeroSig [Ed25519SignatureSize]byte
	if !IsZeroSignature(zeroSig) {
		t.Error("IsZeroSignature() returned false for zero signature")
	}

	// Non-zero signature
	zeroSig[0] = 1
	if IsZeroSignature(zeroSig) {
		t.Error("IsZeroSignature() returned true for non-zero signature")
	}

	// Actual signature
	kp, _ := GenerateSigningKeypair()
	sig := Sign(kp.PrivateKey, []byte("test"))
	if IsZeroSignature(sig) {
		t.Error("IsZeroSignature() returned true for actual signature")
	}
}

func TestZeroSigningKey(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	// Store original for comparison
	original := kp.PrivateKey

	// Zero the key
	ZeroSigningKey(&kp.PrivateKey)

	// Check it's zeroed
	var zero [Ed25519PrivateKeySize]byte
	if kp.PrivateKey != zero {
		t.Error("ZeroSigningKey() did not zero the key")
	}

	// Verify original was not zero (sanity check)
	if original == zero {
		t.Error("Original key was already zero (test is invalid)")
	}
}

func TestRandomBytes(t *testing.T) {
	buf1 := make([]byte, 32)
	buf2 := make([]byte, 32)

	if err := RandomBytes(buf1); err != nil {
		t.Fatalf("RandomBytes() error = %v", err)
	}
	if err := RandomBytes(buf2); err != nil {
		t.Fatalf("RandomBytes() second call error = %v", err)
	}

	// Should generate different random bytes
	if bytes.Equal(buf1, buf2) {
		t.Error("RandomBytes() generated same bytes twice")
	}

	// Should not be all zeros
	allZero := make([]byte, 32)
	if bytes.Equal(buf1, allZero) {
		t.Error("RandomBytes() generated all zeros")
	}
}

func TestSignatureSize(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	signature := Sign(kp.PrivateKey, []byte("test"))

	// Ed25519 signatures are always 64 bytes
	if len(signature) != Ed25519SignatureSize {
		t.Errorf("Sign() returned signature of length %d, want %d", len(signature), Ed25519SignatureSize)
	}
}

func TestDeterministicSignature(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	message := []byte("deterministic signing test")

	sig1 := Sign(kp.PrivateKey, message)
	sig2 := Sign(kp.PrivateKey, message)

	// Ed25519 signatures are deterministic
	if sig1 != sig2 {
		t.Error("Sign() is not deterministic - same key/message produced different signatures")
	}
}
