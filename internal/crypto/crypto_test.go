package crypto

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"testing"
)

func TestGenerateEphemeralKeypair(t *testing.T) {
	priv1, pub1, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
	}

	// Check keys are not zero
	var zeroKey [KeySize]byte
	if priv1 == zeroKey {
		t.Error("private key is zero")
	}
	if pub1 == zeroKey {
		t.Error("public key is zero")
	}

	// Generate another keypair and verify they are different
	priv2, pub2, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() second call error = %v", err)
	}

	if priv1 == priv2 {
		t.Error("two generated private keys are identical")
	}
	if pub1 == pub2 {
		t.Error("two generated public keys are identical")
	}
}

func TestComputeECDH(t *testing.T) {
	// Generate two keypairs
	privA, pubA, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() A error = %v", err)
	}

	privB, pubB, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() B error = %v", err)
	}

	// Both sides should derive the same shared secret
	secretA, err := ComputeECDH(privA, pubB)
	if err != nil {
		t.Fatalf("ComputeECDH(A, pubB) error = %v", err)
	}

	secretB, err := ComputeECDH(privB, pubA)
	if err != nil {
		t.Fatalf("ComputeECDH(B, pubA) error = %v", err)
	}

	if secretA != secretB {
		t.Error("shared secrets do not match")
	}

	// Shared secret should not be zero
	var zeroKey [KeySize]byte
	if secretA == zeroKey {
		t.Error("shared secret is zero")
	}
}

func TestComputeECDH_ZeroKey(t *testing.T) {
	priv, _, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
	}

	// Should fail with zero public key
	var zeroKey [KeySize]byte
	_, err = ComputeECDH(priv, zeroKey)
	if err == nil {
		t.Error("ComputeECDH with zero public key should fail")
	}
}

func TestComputeECDH_LowOrderPoint(t *testing.T) {
	priv, _, err := GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("GenerateEphemeralKeypair() error = %v", err)
	}

	// 01 00 ... 00 is a known low-order point: X25519 with it yields an
	// all-zero shared secret, which must be rejected, not accepted.
	var lowOrder [KeySize]byte
	lowOrder[0] = 1
	if _, err = ComputeECDH(priv, lowOrder); err == nil {
		t.Error("ComputeECDH with low-order public key should fail")
	}
}

func TestDeriveSessionKey(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(42)

	// Derive session keys on both sides
	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)  // initiator
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false) // responder

	// Keys should be identical
	if skA.Key() != skB.Key() {
		t.Error("derived session keys do not match")
	}

	// Keys should not be zero
	var zeroKey [KeySize]byte
	if skA.Key() == zeroKey {
		t.Error("session key is zero")
	}
}

func TestDeriveSessionKey_UniquePerStream(t *testing.T) {
	priv, pub, _ := GenerateEphemeralKeypair()
	secret, _ := ComputeECDH(priv, pub) // Self-ECDH for testing

	sk1 := DeriveSessionKey(secret, 1, pub, pub, true)
	sk2 := DeriveSessionKey(secret, 2, pub, pub, true)

	if sk1.Key() == sk2.Key() {
		t.Error("session keys for different streams should be different")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(123)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)  // initiator
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false) // responder

	// Test initiator -> responder
	plaintext := []byte("Hello, World!")
	ciphertext, err := skA.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Ciphertext should be larger by EncryptionOverhead
	if len(ciphertext) != len(plaintext)+EncryptionOverhead {
		t.Errorf("ciphertext length = %d, want %d", len(ciphertext), len(plaintext)+EncryptionOverhead)
	}

	// Ciphertext should be different from plaintext
	if bytes.Equal(ciphertext[NonceSize:NonceSize+len(plaintext)], plaintext) {
		t.Error("ciphertext contains plaintext (encryption did nothing)")
	}

	// Responder should be able to decrypt
	decrypted, err := skB.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_BothDirections(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(456)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)  // initiator
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false) // responder

	// Initiator -> Responder
	msg1 := []byte("Message from initiator")
	enc1, _ := skA.Encrypt(msg1)
	dec1, err := skB.Decrypt(enc1)
	if err != nil {
		t.Fatalf("Decrypt initiator->responder error = %v", err)
	}
	if !bytes.Equal(dec1, msg1) {
		t.Errorf("initiator->responder: got %q, want %q", dec1, msg1)
	}

	// Responder -> Initiator
	msg2 := []byte("Reply from responder")
	enc2, _ := skB.Encrypt(msg2)
	dec2, err := skA.Decrypt(enc2)
	if err != nil {
		t.Fatalf("Decrypt responder->initiator error = %v", err)
	}
	if !bytes.Equal(dec2, msg2) {
		t.Errorf("responder->initiator: got %q, want %q", dec2, msg2)
	}
}

func TestEncryptDecrypt_MultipleMessages(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	skA := DeriveSessionKey(secretA, 789, pubA, pubB, true)
	skB := DeriveSessionKey(secretB, 789, pubA, pubB, false)

	// Send multiple messages
	messages := []string{
		"First message",
		"Second message",
		"Third message with more content",
		"",                          // Empty message
		string(make([]byte, 16000)), // Large message
	}

	for i, msg := range messages {
		enc, err := skA.Encrypt([]byte(msg))
		if err != nil {
			t.Fatalf("Encrypt message %d error = %v", i, err)
		}

		dec, err := skB.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt message %d error = %v", i, err)
		}

		if !bytes.Equal(dec, []byte(msg)) {
			t.Errorf("message %d: got len=%d, want len=%d", i, len(dec), len(msg))
		}
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	priv, pub, _ := GenerateEphemeralKeypair()
	secret, _ := ComputeECDH(priv, pub)
	sk := DeriveSessionKey(secret, 1, pub, pub, true)

	// Ciphertext shorter than overhead should fail
	shortCiphertext := make([]byte, EncryptionOverhead-1)
	_, err := sk.Decrypt(shortCiphertext)
	if err == nil {
		t.Error("Decrypt with short ciphertext should fail")
	}
}

func TestDecrypt_Tampered(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	skA := DeriveSessionKey(secretA, 1, pubA, pubB, true)
	skB := DeriveSessionKey(secretB, 1, pubA, pubB, false)

	plaintext := []byte("Secret message")
	ciphertext, _ := skA.Encrypt(plaintext)

	// Tamper with ciphertext
	ciphertext[NonceSize+5] ^= 0xFF

	_, err := skB.Decrypt(ciphertext)
	if err == nil {
		t.Error("Decrypt with tampered ciphertext should fail")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	_, pubB, _ := GenerateEphemeralKeypair()
	_, pubC, _ := GenerateEphemeralKeypair()

	secretAB, _ := ComputeECDH(privA, pubB)
	secretAC, _ := ComputeECDH(privA, pubC)

	skAB := DeriveSessionKey(secretAB, 1, pubA, pubB, true)
	skAC := DeriveSessionKey(secretAC, 1, pubA, pubC, true) // Different key

	plaintext := []byte("Secret message")
	ciphertext, _ := skAB.Encrypt(plaintext)

	// Try to decrypt with wrong key
	_, err := skAC.Decrypt(ciphertext)
	if err == nil {
		t.Error("Decrypt with wrong key should fail")
	}
}

func TestZeroBytes(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	ZeroBytes(data)

	for i, b := range data {
		if b != 0 {
			t.Errorf("byte %d = %d, want 0", i, b)
		}
	}
}

func TestZeroKey(t *testing.T) {
	key := [KeySize]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	ZeroKey(&key)

	var zeroKey [KeySize]byte
	if key != zeroKey {
		t.Error("key was not zeroed")
	}
}

func TestEncryptionOverhead(t *testing.T) {
	// Verify the constant matches reality
	if EncryptionOverhead != NonceSize+TagSize {
		t.Errorf("EncryptionOverhead = %d, want %d", EncryptionOverhead, NonceSize+TagSize)
	}
	if EncryptionOverhead != 28 {
		t.Errorf("EncryptionOverhead = %d, want 28", EncryptionOverhead)
	}
}

func BenchmarkEncrypt(b *testing.B) {
	priv, pub, _ := GenerateEphemeralKeypair()
	secret, _ := ComputeECDH(priv, pub)
	sk := DeriveSessionKey(secret, 1, pub, pub, true)

	plaintext := make([]byte, 1400) // Typical MTU-sized payload

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))

	for i := 0; i < b.N; i++ {
		_, _ = sk.Encrypt(plaintext)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	priv, pub, _ := GenerateEphemeralKeypair()
	secret, _ := ComputeECDH(priv, pub)
	sk := DeriveSessionKey(secret, 1, pub, pub, true)

	plaintext := make([]byte, 1400)
	ciphertext, _ := sk.Encrypt(plaintext)

	b.ResetTimer()
	b.SetBytes(int64(len(plaintext)))

	for i := 0; i < b.N; i++ {
		sk.recvNonce = 0 // Reset for benchmark
		_, _ = sk.Decrypt(ciphertext)
	}
}

func BenchmarkKeyExchange(b *testing.B) {
	for i := 0; i < b.N; i++ {
		privA, pubA, _ := GenerateEphemeralKeypair()
		_, pubB, _ := GenerateEphemeralKeypair()

		secretA, _ := ComputeECDH(privA, pubB)
		_ = DeriveSessionKey(secretA, uint64(i), pubA, pubB, true)
	}
}

// ============================================================================
// Concurrency Tests
// ============================================================================

func TestEncryptDecrypt_Concurrent(t *testing.T) {
	// Test concurrent encryption from multiple goroutines
	// Note: Decryption must happen in-order due to anti-replay nonce protection
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(999)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false)

	const numGoroutines = 10
	const messagesPerGoroutine = 100

	// Test that concurrent encryption is thread-safe
	type encResult struct {
		nonce uint64
		data  []byte
	}
	encryptedCh := make(chan encResult, numGoroutines*messagesPerGoroutine)
	errCh := make(chan error, numGoroutines*messagesPerGoroutine)

	var wgEncrypt sync.WaitGroup

	// Concurrent encryption
	for g := 0; g < numGoroutines; g++ {
		wgEncrypt.Add(1)
		go func(goroutineID int) {
			defer wgEncrypt.Done()
			for i := 0; i < messagesPerGoroutine; i++ {
				msg := []byte(fmt.Sprintf("message-%d-%d", goroutineID, i))
				enc, err := skA.Encrypt(msg)
				if err != nil {
					errCh <- fmt.Errorf("encrypt error: %w", err)
					return
				}
				// Extract nonce for ordering
				nonceVal := binary.BigEndian.Uint64(enc[4:12])
				encryptedCh <- encResult{nonce: nonceVal, data: enc}
			}
		}(g)
	}

	wgEncrypt.Wait()
	close(encryptedCh)

	// Collect and sort by nonce for in-order decryption
	var results []encResult
	for enc := range encryptedCh {
		results = append(results, enc)
	}

	// Sort by nonce to ensure in-order decryption
	sort.Slice(results, func(i, j int) bool {
		return results[i].nonce < results[j].nonce
	})

	// Decrypt in order (anti-replay requires this)
	decryptErrs := 0
	for _, enc := range results {
		_, err := skB.Decrypt(enc.data)
		if err != nil {
			decryptErrs++
		}
	}

	close(errCh)

	// Check for encryption errors
	for err := range errCh {
		t.Errorf("Concurrent encryption failed: %v", err)
	}

	// Decryption should succeed for most messages when done in order
	if decryptErrs > len(results)/10 {
		t.Errorf("Too many decryption errors: %d out of %d", decryptErrs, len(results))
	}
}

func TestEncryptDecrypt_BidirectionalConcurrent(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(888)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false)

	const numMessages = 100
	var wg sync.WaitGroup
	errCh := make(chan error, numMessages*4)

	// A -> B concurrent
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			msg := []byte(fmt.Sprintf("A-to-B-%d", i))
			enc, err := skA.Encrypt(msg)
			if err != nil {
				errCh <- err
				continue
			}
			dec, err := skB.Decrypt(enc)
			if err != nil {
				errCh <- err
				continue
			}
			if !bytes.Equal(dec, msg) {
				errCh <- fmt.Errorf("A->B mismatch at %d", i)
			}
		}
	}()

	// B -> A concurrent
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numMessages; i++ {
			msg := []byte(fmt.Sprintf("B-to-A-%d", i))
			enc, err := skB.Encrypt(msg)
			if err != nil {
				errCh <- err
				continue
			}
			dec, err := skA.Decrypt(enc)
			if err != nil {
				errCh <- err
				continue
			}
			if !bytes.Equal(dec, msg) {
				errCh <- fmt.Errorf("B->A mismatch at %d", i)
			}
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Bidirectional concurrent error: %v", err)
	}
}

// ============================================================================
// Nonce and Replay Protection Tests
// ============================================================================

func TestDecrypt_NonceReplay(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(777)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false)

	// Send multiple messages
	msg1 := []byte("message 1")
	msg2 := []byte("message 2")
	msg3 := []byte("message 3")

	enc1, _ := skA.Encrypt(msg1)
	enc2, _ := skA.Encrypt(msg2)
	enc3, _ := skA.Encrypt(msg3)

	// Decrypt in order - all should succeed
	_, err := skB.Decrypt(enc1)
	if err != nil {
		t.Fatalf("Decrypt enc1 failed: %v", err)
	}
	_, err = skB.Decrypt(enc2)
	if err != nil {
		t.Fatalf("Decrypt enc2 failed: %v", err)
	}
	_, err = skB.Decrypt(enc3)
	if err != nil {
		t.Fatalf("Decrypt enc3 failed: %v", err)
	}

	// Replaying enc1 (old nonce) should fail
	_, err = skB.Decrypt(enc1)
	if err == nil {
		t.Error("Replaying old message should fail")
	}

	// Replaying enc2 should also fail
	_, err = skB.Decrypt(enc2)
	if err == nil {
		t.Error("Replaying enc2 should fail")
	}
}

func TestDecrypt_OutOfOrder(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(666)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false)

	// Encrypt 5 messages
	messages := make([][]byte, 5)
	encrypted := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		messages[i] = []byte(fmt.Sprintf("message-%d", i))
		encrypted[i], _ = skA.Encrypt(messages[i])
	}

	// Decrypt out of order: 2, 0, 4, 1, 3
	order := []int{2, 0, 4, 1, 3}
	for _, idx := range order {
		dec, err := skB.Decrypt(encrypted[idx])
		if err != nil {
			// Out-of-order messages after nonce advancement may fail
			// The implementation rejects old nonces for replay protection
			t.Logf("Decrypt message %d out-of-order: %v (may be expected)", idx, err)
			continue
		}
		if !bytes.Equal(dec, messages[idx]) {
			t.Errorf("Message %d mismatch", idx)
		}
	}
}

func TestNonce_DirectionIsolation(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(555)

	skA := DeriveSessionKey(secretA, streamID, pubA, pubB, true)  // initiator
	skB := DeriveSessionKey(secretB, streamID, pubA, pubB, false) // responder

	// Both sides encrypt the same plaintext
	plaintext := []byte("same message")

	encA, _ := skA.Encrypt(plaintext)
	encB, _ := skB.Encrypt(plaintext)

	// Ciphertexts should be different due to different direction bits in nonce
	if bytes.Equal(encA, encB) {
		t.Error("Ciphertexts from different directions should be different")
	}

	// A's message should only be decryptable by B
	decByB, err := skB.Decrypt(encA)
	if err != nil {
		t.Fatalf("B decrypting A's message failed: %v", err)
	}
	if !bytes.Equal(decByB, plaintext) {
		t.Error("B decrypted wrong plaintext")
	}

	// B's message should only be decryptable by A
	decByA, err := skA.Decrypt(encB)
	if err != nil {
		t.Fatalf("A decrypting B's message failed: %v", err)
	}
	if !bytes.Equal(decByA, plaintext) {
		t.Error("A decrypted wrong plaintext")
	}

	// A should not be able to decrypt its own message (wrong direction)
	_, err = skA.Decrypt(encA)
	if err == nil {
		t.Error("A should not decrypt its own message")
	}

	// B should not be able to decrypt its own message (wrong direction)
	_, err = skB.Decrypt(encB)
	if err == nil {
		t.Error("B should not decrypt its own message")
	}
}

// A transit agent that reflects a frame straight back at its sender must not
// have it authenticate. Both directions share one symmetric key, so only the
// direction indicator in the nonce separates them.
//
// The reflection happens before either side receives anything legitimate: with
// the counter high-water mark still at zero, the direction check is the only
// thing standing between the sender and its own frame. TestNonce_DirectionIsolation
// reflects after a legitimate receive has advanced the counter, so it passes on
// the counter check alone and cannot catch a missing direction check.
func TestDecrypt_RejectsReflectedFrame(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()
	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)

	streamID := uint64(4242)
	initiator := DeriveSessionKey(secretA, streamID, pubA, pubB, true)
	responder := DeriveSessionKey(secretB, streamID, pubA, pubB, false)

	plaintext := []byte("GET /internal/admin HTTP/1.1")

	// Initiator's first frame, reflected back before anything else happens.
	fromInitiator, err := initiator.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if _, err := initiator.Decrypt(fromInitiator); err == nil {
		t.Error("initiator authenticated its own frame replayed back at it")
	}

	// Same in the other direction.
	fromResponder, err := responder.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if _, err := responder.Decrypt(fromResponder); err == nil {
		t.Error("responder authenticated its own frame replayed back at it")
	}

	// Rejecting a reflected frame must not disturb the receive counters, or
	// the reflection becomes a denial of service against real traffic.
	if got, err := responder.Decrypt(fromInitiator); err != nil {
		t.Errorf("legitimate frame rejected after a reflection attempt: %v", err)
	} else if !bytes.Equal(got, plaintext) {
		t.Errorf("plaintext mismatch: got %q", got)
	}
	if got, err := initiator.Decrypt(fromResponder); err != nil {
		t.Errorf("legitimate frame rejected after a reflection attempt: %v", err)
	} else if !bytes.Equal(got, plaintext) {
		t.Errorf("plaintext mismatch: got %q", got)
	}
}

func TestEncrypt_LargePayload(t *testing.T) {
	priv, pub, _ := GenerateEphemeralKeypair()
	secret, _ := ComputeECDH(priv, pub)
	skA := DeriveSessionKey(secret, 1, pub, pub, true)
	skB := DeriveSessionKey(secret, 1, pub, pub, false)

	// Test with large payloads (up to max frame size)
	sizes := []int{1024, 4096, 16384, 32768, 65536}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			plaintext := make([]byte, size)
			for i := range plaintext {
				plaintext[i] = byte(i % 256)
			}

			enc, err := skA.Encrypt(plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}

			// Reset recv nonce for clean test
			skB.mu.Lock()
			skB.recvNonce = 0
			skB.mu.Unlock()

			dec, err := skB.Decrypt(enc)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}

			if !bytes.Equal(dec, plaintext) {
				t.Errorf("Large payload mismatch at size %d", size)
			}
		})
	}
}

func TestDeriveSessionKey_DifferentPublicKeyOrder(t *testing.T) {
	privA, pubA, _ := GenerateEphemeralKeypair()
	_, pubB, _ := GenerateEphemeralKeypair()

	secretA, _ := ComputeECDH(privA, pubB)

	streamID := uint64(123)

	// Correct order: initiator pub first
	sk1 := DeriveSessionKey(secretA, streamID, pubA, pubB, true)

	// Wrong order: responder pub first - should produce different key
	sk2 := DeriveSessionKey(secretA, streamID, pubB, pubA, true)

	if sk1.Key() == sk2.Key() {
		t.Error("Different public key order should produce different session keys")
	}
}

// ============================================================================
// Strict (ordered-lane) decrypt tests
// ============================================================================

// strictTestKeys returns an initiator/responder SessionKey pair for one
// stream, mirroring the derivation the other tests use.
func strictTestKeys(t *testing.T, streamID uint64) (skA, skB *SessionKey) {
	t.Helper()
	privA, pubA, _ := GenerateEphemeralKeypair()
	privB, pubB, _ := GenerateEphemeralKeypair()
	secretA, _ := ComputeECDH(privA, pubB)
	secretB, _ := ComputeECDH(privB, pubA)
	return DeriveSessionKey(secretA, streamID, pubA, pubB, true),
		DeriveSessionKey(secretB, streamID, pubA, pubB, false)
}

func TestDecryptStrict_InOrder(t *testing.T) {
	skA, skB := strictTestKeys(t, 901)

	for i := 0; i < 5; i++ {
		msg := []byte(fmt.Sprintf("chunk-%d", i))
		enc, err := skA.Encrypt(msg)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		dec, err := skB.DecryptStrict(enc)
		if err != nil {
			t.Fatalf("DecryptStrict(chunk %d) error = %v", i, err)
		}
		if !bytes.Equal(dec, msg) {
			t.Errorf("chunk %d: got %q, want %q", i, dec, msg)
		}
	}
}

func TestDecryptStrict_RejectsGap(t *testing.T) {
	skA, skB := strictTestKeys(t, 902)

	enc0, _ := skA.Encrypt([]byte("chunk-0"))
	enc1, _ := skA.Encrypt([]byte("chunk-1"))
	enc2, _ := skA.Encrypt([]byte("chunk-2"))

	if _, err := skB.DecryptStrict(enc0); err != nil {
		t.Fatalf("DecryptStrict(enc0) error = %v", err)
	}

	// A dropped frame (enc1) must not be skippable: enc2 carries nonce 2 while
	// the next expected counter is 1.
	if _, err := skB.DecryptStrict(enc2); err == nil {
		t.Fatal("DecryptStrict(enc2) should reject the nonce gap left by enc1")
	}

	// The failed decrypt must not have advanced the counter: the missing
	// frame itself still decrypts.
	dec, err := skB.DecryptStrict(enc1)
	if err != nil {
		t.Fatalf("DecryptStrict(enc1) after gap rejection error = %v", err)
	}
	if !bytes.Equal(dec, []byte("chunk-1")) {
		t.Errorf("got %q, want %q", dec, "chunk-1")
	}
	if _, err := skB.DecryptStrict(enc2); err != nil {
		t.Fatalf("DecryptStrict(enc2) after catching up error = %v", err)
	}
}

func TestDecryptStrict_RejectsReplay(t *testing.T) {
	skA, skB := strictTestKeys(t, 903)

	enc0, _ := skA.Encrypt([]byte("chunk-0"))
	if _, err := skB.DecryptStrict(enc0); err != nil {
		t.Fatalf("DecryptStrict(enc0) error = %v", err)
	}
	if _, err := skB.DecryptStrict(enc0); err == nil {
		t.Error("DecryptStrict should reject a replayed frame")
	}
}

// TestDecrypt_GapStillTolerant pins the unordered-lane contract: Decrypt (the
// UDP/ICMP fast lane) keeps accepting nonces above the high-water mark.
func TestDecrypt_GapStillTolerant(t *testing.T) {
	skA, skB := strictTestKeys(t, 904)

	enc0, _ := skA.Encrypt([]byte("dgram-0"))
	_, _ = skA.Encrypt([]byte("dgram-1")) // "lost"
	enc2, _ := skA.Encrypt([]byte("dgram-2"))

	if _, err := skB.Decrypt(enc0); err != nil {
		t.Fatalf("Decrypt(enc0) error = %v", err)
	}
	// The gap left by the lost datagram is tolerated on the unordered lane.
	if _, err := skB.Decrypt(enc2); err != nil {
		t.Fatalf("Decrypt(enc2) should tolerate the gap, got %v", err)
	}
	// But old nonces are still rejected.
	if _, err := skB.Decrypt(enc0); err == nil {
		t.Error("Decrypt should reject a replay below the high-water mark")
	}
}
