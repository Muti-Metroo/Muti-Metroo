// Package crypto provides sealed box encryption for management key protection.
// Sealed boxes use X25519 for key exchange and ChaCha20-Poly1305 for encryption.

package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	// SealedBoxOverhead is the total overhead added to each sealed message:
	// ephemeral public key (32) + nonce (12) + auth tag (16) = 60 bytes
	SealedBoxOverhead = KeySize + NonceSize + TagSize

	// sealedBoxInfo is the context string for HKDF key derivation in sealed boxes.
	sealedBoxInfo = "muti-metroo-sealed-v1"
)

var (
	// ErrNoPrivateKey is returned when attempting to open a sealed box
	// without a private key configured.
	ErrNoPrivateKey = errors.New("management private key not configured")

	// ErrInvalidCiphertext is returned when the ciphertext is malformed.
	ErrInvalidCiphertext = errors.New("invalid sealed box ciphertext")

	// ErrDecryptionFailed is returned when authentication fails.
	ErrDecryptionFailed = errors.New("sealed box decryption failed")
)

// SealedBox provides sealed box encryption using X25519 + ChaCha20-Poly1305.
// It supports encrypt-only mode (public key only) for field agents, and
// encrypt/decrypt mode (both keys) for operator nodes.
type SealedBox struct {
	publicKey  [KeySize]byte
	privateKey [KeySize]byte
	hasPrivate bool
}

// NewSealedBox creates a sealed box with public key only (encrypt-only mode).
// This is used by field agents that can encrypt data but cannot decrypt it.
func NewSealedBox(publicKey [KeySize]byte) *SealedBox {
	return &SealedBox{
		publicKey:  publicKey,
		hasPrivate: false,
	}
}

// NewSealedBoxWithPrivate creates a sealed box that can both encrypt and decrypt.
// This is used by operator nodes that need to view encrypted mesh data.
func NewSealedBoxWithPrivate(publicKey, privateKey [KeySize]byte) *SealedBox {
	return &SealedBox{
		publicKey:  publicKey,
		privateKey: privateKey,
		hasPrivate: true,
	}
}

// CanDecrypt returns true if this sealed box has a private key and can decrypt.
func (s *SealedBox) CanDecrypt() bool {
	return s.hasPrivate
}

// PublicKey returns the management public key.
func (s *SealedBox) PublicKey() [KeySize]byte {
	return s.publicKey
}

// Seal encrypts plaintext so that only the holder of the management private key
// can decrypt it. The output format is:
//
//	ephemeral_public_key (32 bytes) || nonce (12 bytes) || ciphertext || tag (16 bytes)
//
// The function generates a fresh ephemeral keypair for each call, ensuring
// that each sealed message has unique encryption keys.
func (s *SealedBox) Seal(plaintext []byte) ([]byte, error) {
	// Generate ephemeral keypair for this message
	ephemeralPrivate, ephemeralPublic, err := GenerateEphemeralKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	defer ZeroKey(&ephemeralPrivate)

	// Compute shared secret via ECDH
	sharedSecret, err := ComputeECDH(ephemeralPrivate, s.publicKey)
	if err != nil {
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}
	defer ZeroKey(&sharedSecret)

	// Derive symmetric key using HKDF
	// Salt includes both public keys to bind the key to this specific exchange
	salt := make([]byte, KeySize+KeySize)
	copy(salt[0:KeySize], ephemeralPublic[:])
	copy(salt[KeySize:], s.publicKey[:])

	symmetricKey := make([]byte, KeySize)
	reader := hkdf.New(sha256.New, sharedSecret[:], salt, []byte(sealedBoxInfo))
	if _, err := io.ReadFull(reader, symmetricKey); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	defer ZeroBytes(symmetricKey)

	// Generate random nonce
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Create AEAD cipher
	aead, err := chacha20poly1305.New(symmetricKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Build output: ephemeral_public || nonce || ciphertext || tag
	output := make([]byte, KeySize+NonceSize, KeySize+NonceSize+len(plaintext)+TagSize)
	copy(output[0:KeySize], ephemeralPublic[:])
	copy(output[KeySize:KeySize+NonceSize], nonce[:])

	output = aead.Seal(output, nonce[:], plaintext, nil)

	return output, nil
}

// Open decrypts a sealed box ciphertext. Returns ErrNoPrivateKey if this
// sealed box was created without a private key.
func (s *SealedBox) Open(ciphertext []byte) ([]byte, error) {
	if !s.hasPrivate {
		return nil, ErrNoPrivateKey
	}

	if len(ciphertext) < SealedBoxOverhead {
		return nil, ErrInvalidCiphertext
	}

	// Extract ephemeral public key
	var ephemeralPublic [KeySize]byte
	copy(ephemeralPublic[:], ciphertext[0:KeySize])

	// Extract nonce
	var nonce [NonceSize]byte
	copy(nonce[:], ciphertext[KeySize:KeySize+NonceSize])

	// Compute shared secret via ECDH
	sharedSecret, err := ComputeECDH(s.privateKey, ephemeralPublic)
	if err != nil {
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}
	defer ZeroKey(&sharedSecret)

	// Derive symmetric key using HKDF (same derivation as Seal)
	salt := make([]byte, KeySize+KeySize)
	copy(salt[0:KeySize], ephemeralPublic[:])
	copy(salt[KeySize:], s.publicKey[:])

	symmetricKey := make([]byte, KeySize)
	reader := hkdf.New(sha256.New, sharedSecret[:], salt, []byte(sealedBoxInfo))
	if _, err := io.ReadFull(reader, symmetricKey); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	defer ZeroBytes(symmetricKey)

	// Create AEAD cipher
	aead, err := chacha20poly1305.New(symmetricKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	// Decrypt
	plaintext, err := aead.Open(nil, nonce[:], ciphertext[KeySize+NonceSize:], nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// Zero clears the private key from memory. Call this when the sealed box
// is no longer needed.
func (s *SealedBox) Zero() {
	ZeroKey(&s.privateKey)
	s.hasPrivate = false
}
