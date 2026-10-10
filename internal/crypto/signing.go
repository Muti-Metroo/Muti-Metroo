// Package crypto provides Ed25519 signing for command authentication.
// Ed25519 is used for signing sleep/wake commands to prevent unauthorized
// mesh hibernation.

package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
)

const (
	// Ed25519PublicKeySize is the size of Ed25519 public keys in bytes.
	Ed25519PublicKeySize = 32

	// Ed25519PrivateKeySize is the size of Ed25519 private keys in bytes.
	// Note: ed25519.PrivateKey is 64 bytes (seed + public key), but we store
	// only the 32-byte seed and derive the full key when needed.
	Ed25519PrivateKeySize = 64

	// Ed25519SeedSize is the size of Ed25519 seed (private key seed) in bytes.
	Ed25519SeedSize = 32

	// Ed25519SignatureSize is the size of Ed25519 signatures in bytes.
	Ed25519SignatureSize = 64
)

// SigningKeypair holds an Ed25519 keypair for command signing.
type SigningKeypair struct {
	PublicKey  [Ed25519PublicKeySize]byte
	PrivateKey [Ed25519PrivateKeySize]byte
}

// GenerateSigningKeypair generates a new Ed25519 keypair for command signing.
// The private key should be kept secret and only distributed to operators
// who need to issue signed commands.
func GenerateSigningKeypair() (*SigningKeypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	kp := &SigningKeypair{}
	copy(kp.PublicKey[:], pub)
	copy(kp.PrivateKey[:], priv)

	return kp, nil
}

// SigningKeypairFromSeed creates an Ed25519 keypair from a 32-byte seed.
// This is useful for deriving the keypair from stored seed bytes.
func SigningKeypairFromSeed(seed [Ed25519SeedSize]byte) *SigningKeypair {
	priv := ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)

	kp := &SigningKeypair{}
	copy(kp.PublicKey[:], pub)
	copy(kp.PrivateKey[:], priv)

	return kp
}

// PublicKeyFromPrivate derives the Ed25519 public key from a private key.
func PublicKeyFromPrivate(privateKey [Ed25519PrivateKeySize]byte) [Ed25519PublicKeySize]byte {
	priv := ed25519.PrivateKey(privateKey[:])
	pub := priv.Public().(ed25519.PublicKey)

	var pubKey [Ed25519PublicKeySize]byte
	copy(pubKey[:], pub)
	return pubKey
}

// Sign creates an Ed25519 signature of the message using the private key.
func Sign(privateKey [Ed25519PrivateKeySize]byte, message []byte) [Ed25519SignatureSize]byte {
	priv := ed25519.PrivateKey(privateKey[:])
	sig := ed25519.Sign(priv, message)

	var signature [Ed25519SignatureSize]byte
	copy(signature[:], sig)
	return signature
}

// Verify checks if the signature is valid for the message using the public key.
// Returns true if the signature is valid, false otherwise.
func Verify(publicKey [Ed25519PublicKeySize]byte, message []byte, signature [Ed25519SignatureSize]byte) bool {
	pub := ed25519.PublicKey(publicKey[:])
	return ed25519.Verify(pub, message, signature[:])
}

// IsZeroSignature checks if a signature is all zeros (unsigned).
func IsZeroSignature(signature [Ed25519SignatureSize]byte) bool {
	for _, b := range signature {
		if b != 0 {
			return false
		}
	}
	return true
}

// ZeroSigningKey zeroes out a signing private key array.
func ZeroSigningKey(k *[Ed25519PrivateKeySize]byte) {
	for i := range k {
		k[i] = 0
	}
}

// RandomBytes fills a byte slice with cryptographically secure random bytes.
// This is a helper function for generating random data.
func RandomBytes(b []byte) error {
	_, err := io.ReadFull(rand.Reader, b)
	return err
}
