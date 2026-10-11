// Package crypto provides end-to-end encryption for stream data.
// It uses X25519 for key exchange and ChaCha20-Poly1305 for symmetric encryption.
package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	// KeySize is the size of X25519 and ChaCha20-Poly1305 keys in bytes.
	KeySize = 32

	// NonceSize is the size of ChaCha20-Poly1305 nonces in bytes.
	NonceSize = 12

	// TagSize is the size of Poly1305 authentication tags in bytes.
	TagSize = 16

	// EncryptionOverhead is the total overhead added to each encrypted message.
	// This includes the nonce (12 bytes) prepended and the auth tag (16 bytes) appended.
	EncryptionOverhead = NonceSize + TagSize

	// hkdfInfo is the context string for HKDF key derivation.
	hkdfInfo = "muti-metroo-e2e-v1"
)

// SessionKey holds the symmetric key and nonce state for encrypting/decrypting
// stream data. It is safe for concurrent use.
type SessionKey struct {
	key [KeySize]byte

	// aead is the ChaCha20-Poly1305 instance for key, constructed once at
	// derivation time: the key never changes over a session's life, so
	// re-running the key schedule per message (chacha20poly1305.New on every
	// Encrypt/Decrypt) is pure waste on the hot path. cipher.AEAD
	// implementations are safe for concurrent use.
	aead cipher.AEAD

	// Separate nonce counters for send and receive directions
	// to avoid nonce reuse in bidirectional streams.
	sendNonce uint64
	recvNonce uint64

	// isInitiator selects the direction indicator carried in the first four
	// nonce bytes: the initiator (ingress) sends with 0x00000000 and receives
	// 0x80000000, the responder (exit) does the reverse. One symmetric key
	// serves both directions, so Decrypt must reject any nonce whose direction
	// does not match the expected receive direction; otherwise a captured
	// frame replayed at its own sender authenticates.
	isInitiator bool

	mu sync.Mutex
}

// GenerateEphemeralKeypair generates a new ephemeral X25519 keypair for
// use in a single stream's key exchange. The private key should be zeroed
// after computing the shared secret.
func GenerateEphemeralKeypair() (privateKey, publicKey [KeySize]byte, err error) {
	if _, err = io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return privateKey, publicKey, fmt.Errorf("generate private key: %w", err)
	}

	// Clamp the private key per X25519 spec
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	// Compute public key from private key
	curve25519.ScalarBaseMult(&publicKey, &privateKey)

	return privateKey, publicKey, nil
}

// ComputeECDH performs X25519 Diffie-Hellman key exchange and returns
// the shared secret. The shared secret should be passed to DeriveSessionKey.
func ComputeECDH(privateKey, remotePublicKey [KeySize]byte) ([KeySize]byte, error) {
	var sharedSecret [KeySize]byte

	// Check for low-order points (all zeros public key is invalid)
	var zeroKey [KeySize]byte
	if remotePublicKey == zeroKey {
		return sharedSecret, fmt.Errorf("invalid remote public key: zero key")
	}

	// X25519 (unlike raw ScalarMult) rejects low-order points with an error
	// instead of silently returning an all-zero shared secret.
	secret, err := curve25519.X25519(privateKey[:], remotePublicKey[:])
	if err != nil {
		return sharedSecret, fmt.Errorf("invalid ECDH result: %w", err)
	}
	copy(sharedSecret[:], secret)

	return sharedSecret, nil
}

// DeriveSessionKey derives a symmetric encryption key from an ECDH shared secret.
// The streamID and both public keys are mixed into the derivation to ensure
// unique keys per stream and prevent key confusion attacks.
//
// Parameters:
//   - sharedSecret: The raw ECDH shared secret
//   - streamID: The stream ID (ensures unique keys per stream)
//   - initiatorPub: The initiator's (ingress) ephemeral public key
//   - responderPub: The responder's (exit) ephemeral public key
//   - isInitiator: True if this is the initiator (ingress) side
func DeriveSessionKey(sharedSecret [KeySize]byte, streamID uint64,
	initiatorPub, responderPub [KeySize]byte, isInitiator bool) *SessionKey {

	// Build salt: streamID || initiatorPub || responderPub
	// This ensures different keys for different streams and prevents
	// key confusion if public keys are reused.
	salt := make([]byte, 8+KeySize+KeySize)
	binary.BigEndian.PutUint64(salt[0:8], streamID)
	copy(salt[8:8+KeySize], initiatorPub[:])
	copy(salt[8+KeySize:], responderPub[:])

	// Use HKDF-SHA256 to derive the session key
	reader := hkdf.New(sha256.New, sharedSecret[:], salt, []byte(hkdfInfo))

	sk := &SessionKey{
		isInitiator: isInitiator,
	}
	if _, err := io.ReadFull(reader, sk.key[:]); err != nil {
		// This should never happen with valid inputs
		panic(fmt.Sprintf("HKDF failed: %v", err))
	}

	// KeySize matches chacha20poly1305's required key length, so this cannot
	// fail.
	aead, err := chacha20poly1305.New(sk.key[:])
	if err != nil {
		panic(fmt.Sprintf("create cipher: %v", err))
	}
	sk.aead = aead

	return sk
}

// Encrypt encrypts plaintext using ChaCha20-Poly1305 with a unique nonce.
// The nonce is prepended to the ciphertext, resulting in a message that is
// EncryptionOverhead bytes larger than the plaintext.
//
// The nonce format uses the upper bit to indicate direction (send vs receive)
// and the remaining bits as a counter, ensuring nonce uniqueness.
func (s *SessionKey) Encrypt(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	nonce := s.buildSendNonce()
	s.sendNonce++
	s.mu.Unlock()

	// Output: nonce || ciphertext || tag
	// Capacity: NonceSize + len(plaintext) + TagSize
	ciphertext := make([]byte, NonceSize, NonceSize+len(plaintext)+TagSize)
	copy(ciphertext, nonce[:])

	ciphertext = s.aead.Seal(ciphertext, nonce[:], plaintext, nil)

	return ciphertext, nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
// The ciphertext must include the prepended nonce.
// Returns an error if the ciphertext is too short or authentication fails.
//
// The anti-replay check is gap-tolerant: any nonce at or above the high-water
// mark advances it. That suits the unordered fast lanes (UDP datagrams, ICMP
// echoes), where reorder and loss are expected. Ordered-lane (stream) traffic
// must use DecryptStrict instead.
func (s *SessionKey) Decrypt(ciphertext []byte) ([]byte, error) {
	return s.decrypt(ciphertext, false)
}

// DecryptStrict is Decrypt for ordered-lane (stream) traffic: the nonce
// counter must be exactly the next expected value. A gap on an ordered lane
// means a relay dropped a frame, and accepting it would deliver a byte stream
// with an undetectable hole. A rejected frame does not advance the counter,
// so a single failure wedges the stream -- callers treat a strict decrypt
// error as fatal for the stream, which is the intent: after a gap the stream
// is unrecoverable anyway.
func (s *SessionKey) DecryptStrict(ciphertext []byte) ([]byte, error) {
	return s.decrypt(ciphertext, true)
}

func (s *SessionKey) decrypt(ciphertext []byte, strict bool) ([]byte, error) {
	if len(ciphertext) < EncryptionOverhead {
		return nil, fmt.Errorf("ciphertext too short: %d bytes", len(ciphertext))
	}

	// Extract nonce from the beginning
	var nonce [NonceSize]byte
	copy(nonce[:], ciphertext[:NonceSize])

	s.mu.Lock()
	expectedNonce := s.buildRecvNonce()

	// The direction indicator must match the direction we expect to receive
	// from. Both directions share one symmetric key, so without this check a
	// frame we sent ourselves authenticates when replayed back at us. Checked
	// before the counter is advanced so a rejected frame cannot also push the
	// high-water mark forward and starve legitimate traffic.
	//
	// Compared and formatted as integers rather than byte slices: escape
	// analysis is path-insensitive, so passing nonce[:4] to fmt.Errorf on this
	// error path would move both nonce arrays to the heap on every call,
	// including the overwhelmingly common success path.
	gotDirection := binary.BigEndian.Uint32(nonce[:4])
	wantDirection := binary.BigEndian.Uint32(expectedNonce[:4])
	if gotDirection != wantDirection {
		s.mu.Unlock()
		return nil, fmt.Errorf("nonce direction mismatch: got %08x, expected %08x", gotDirection, wantDirection)
	}

	// Anti-replay high-water mark. In gap-tolerant mode anything at or above
	// the mark is accepted and moves the mark (anything below is dropped, so
	// reordered datagrams on the UDP and ICMP lanes are discarded rather than
	// buffered). In strict mode (ordered stream lane) the counter must be
	// exactly the next expected value: a gap is stream corruption, not
	// reorder.
	// expectedNonce carries s.recvNonce in its counter half, so compare
	// against s.recvNonce directly rather than decoding it back out.
	nonceValue := binary.BigEndian.Uint64(nonce[4:])
	if strict {
		if nonceValue != s.recvNonce {
			s.mu.Unlock()
			return nil, fmt.Errorf("nonce gap on ordered lane: received %d, expected %d", nonceValue, s.recvNonce)
		}
	} else if nonceValue < s.recvNonce {
		s.mu.Unlock()
		return nil, fmt.Errorf("nonce too old: received %d, expected >= %d", nonceValue, s.recvNonce)
	}
	s.recvNonce = nonceValue + 1
	s.mu.Unlock()

	plaintext, err := s.aead.Open(nil, nonce[:], ciphertext[NonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// buildSendNonce creates a nonce for sending based on counter and direction.
// Format: [4 bytes: direction indicator] [8 bytes: counter]
// Direction: 0x00000000 for initiator->responder, 0x80000000 for responder->initiator
func (s *SessionKey) buildSendNonce() [NonceSize]byte {
	var nonce [NonceSize]byte

	// Set direction bit in first 4 bytes
	if !s.isInitiator {
		// Responder sends with high bit set
		nonce[0] = 0x80
	}

	// Counter in last 8 bytes
	binary.BigEndian.PutUint64(nonce[4:], s.sendNonce)

	return nonce
}

// buildRecvNonce creates an expected nonce for receiving based on counter and direction.
func (s *SessionKey) buildRecvNonce() [NonceSize]byte {
	var nonce [NonceSize]byte

	// Receive direction is opposite of send
	if s.isInitiator {
		// Initiator receives from responder (high bit set)
		nonce[0] = 0x80
	}

	// Counter in last 8 bytes
	binary.BigEndian.PutUint64(nonce[4:], s.recvNonce)

	return nonce
}

// Key returns a copy of the session key bytes.
// This should only be used for debugging or testing.
func (s *SessionKey) Key() [KeySize]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key
}

// Zero securely zeros the session key material.
// Call this when the stream using this key is closed.
//
// Note: the cached AEAD instance retains its own copy of the key schedule
// internally; Zero clears the SessionKey's key bytes but cannot reach into
// the cipher's state. The SessionKey (and its AEAD) should simply be dropped
// after Zero so both are reclaimed together.
func (s *SessionKey) Zero() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ZeroKey(&s.key)
}

// ZeroBytes zeroes out a byte slice to prevent sensitive data from lingering
// in memory. Use this to clear ephemeral private keys after computing
// the shared secret.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ZeroKey zeroes out a key array.
func ZeroKey(k *[KeySize]byte) {
	for i := range k {
		k[i] = 0
	}
}
