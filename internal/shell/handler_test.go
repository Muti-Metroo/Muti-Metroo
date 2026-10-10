package shell

import (
	"log/slog"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// allowAllVerifier is an AuthVerifier that accepts every envelope.
// Tests that do not exercise the auth path use it so NewHandler has a
// valid verifier without constructing a real keypair.
func allowAllVerifier(_ *crypto.AuthEnvelope) error { return nil }

// testEphemeralKey generates a test ephemeral key pair for testing.
func testEphemeralKey(t *testing.T) [crypto.KeySize]byte {
	t.Helper()
	_, pub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate ephemeral key: %v", err)
	}
	return pub
}

// openStreamWithSessionKey opens a stream and returns the session key for encrypting test data.
func openStreamWithSessionKey(t *testing.T, handler *Handler, peerID identity.AgentID, streamID, requestID uint64, interactive bool) *crypto.SessionKey {
	t.Helper()
	// Generate client ephemeral keypair
	clientPriv, clientPub, err := crypto.GenerateEphemeralKeypair()
	if err != nil {
		t.Fatalf("failed to generate client ephemeral key: %v", err)
	}

	// Open stream and get handler's ephemeral public key
	errCode, handlerPub := handler.HandleStreamOpen(peerID, streamID, requestID, interactive, clientPub)
	if errCode != 0 {
		t.Fatalf("HandleStreamOpen() returned error code %d", errCode)
	}

	// Derive session key (as initiator)
	sharedSecret, err := crypto.ComputeECDH(clientPriv, handlerPub)
	if err != nil {
		t.Fatalf("failed to compute ECDH: %v", err)
	}
	crypto.ZeroKey(&clientPriv)

	sessionKey := crypto.DeriveSessionKey(sharedSecret, requestID, clientPub, handlerPub, true)
	crypto.ZeroKey(&sharedSecret)

	return sessionKey
}

// mockDataWriter is a mock implementation of DataWriter for testing.
type mockDataWriter struct {
	mu       sync.Mutex
	messages []mockMessage
	closed   map[uint64]bool
}

type mockMessage struct {
	peerID   identity.AgentID
	streamID uint64
	data     []byte
	flags    uint8
}

func newMockDataWriter() *mockDataWriter {
	return &mockDataWriter{
		messages: make([]mockMessage, 0),
		closed:   make(map[uint64]bool),
	}
}

func (m *mockDataWriter) WriteStreamData(peerID identity.AgentID, streamID uint64, data []byte, flags uint8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.messages = append(m.messages, mockMessage{peerID, streamID, dataCopy, flags})
	return nil
}

func (m *mockDataWriter) WriteStreamClose(peerID identity.AgentID, streamID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed[streamID] = true
	return nil
}

func (m *mockDataWriter) getMessages() []mockMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

func (m *mockDataWriter) isClosed(streamID uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed[streamID]
}

func mustNewAgentID(t *testing.T) identity.AgentID {
	t.Helper()
	id, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID() error = %v", err)
	}
	return id
}

func TestHandler_HandleStreamOpen_Disabled(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled: false, // Shell disabled
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open should fail with shell disabled error
	ephKey := testEphemeralKey(t)
	errCode, _ := handler.HandleStreamOpen(peerID, streamID, requestID, false, ephKey)
	if errCode == 0 {
		t.Error("HandleStreamOpen() should have returned error code for disabled shell")
	}
}

func TestHandler_HandleStreamOpen_Success_Streaming(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open should succeed (streaming mode)
	ephKey := testEphemeralKey(t)
	errCode, _ := handler.HandleStreamOpen(peerID, streamID, requestID, false, ephKey)
	if errCode != 0 {
		t.Errorf("HandleStreamOpen() returned error code %d, want 0", errCode)
	}

	// Verify stream is tracked
	if handler.ActiveStreams() != 1 {
		t.Errorf("ActiveStreams() = %d, want 1", handler.ActiveStreams())
	}

	// Close the stream
	handler.HandleStreamClose(peerID, streamID)

	// Verify stream is removed
	if handler.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() after close = %d, want 0", handler.ActiveStreams())
	}
}

func TestHandler_HandleStreamOpen_Success_Interactive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping interactive test on Windows (no PTY support)")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open should succeed (interactive mode)
	ephKey := testEphemeralKey(t)
	errCode, _ := handler.HandleStreamOpen(peerID, streamID, requestID, true, ephKey)
	if errCode != 0 {
		t.Errorf("HandleStreamOpen() interactive returned error code %d, want 0", errCode)
	}

	// Verify stream is tracked
	if handler.ActiveStreams() != 1 {
		t.Errorf("ActiveStreams() = %d, want 1", handler.ActiveStreams())
	}

	// Close the stream
	handler.HandleStreamClose(peerID, streamID)
}

// TestHandler_StdinClose verifies that MsgStdinClose causes a streaming
// session to close the child's stdin, letting EOF-sensitive commands
// (cat without -) exit naturally rather than hanging forever.
func TestHandler_StdinClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping stdin-close test on Windows (no cat)")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     10 * time.Second,
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)
	defer handler.Close()

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Start `cat`. Without any stdin it would block forever.
	sendEncryptedMeta(t, handler, sessionKey, peerID, streamID, &ShellMeta{Command: "cat"})

	// Feed a few bytes and then close stdin.
	send := func(payload []byte) {
		enc, err := sessionKey.Encrypt(payload)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		handler.HandleStreamData(peerID, streamID, enc, 0)
	}
	// Give the session time to move past handleMetadata and start `cat`.
	time.Sleep(200 * time.Millisecond)
	send(EncodeStdin([]byte("hello\n")))
	send(EncodeStdinClose())

	// `cat` should now see EOF and exit within a couple of seconds.
	deadline := time.Now().Add(4 * time.Second)
	var gotStdout, gotExit bool
	for time.Now().Before(deadline) && !gotExit {
		for _, msg := range writer.getMessages() {
			if msg.streamID != streamID {
				continue
			}
			decrypted, err := sessionKey.Decrypt(msg.data)
			if err != nil {
				continue
			}
			mt, _, _ := DecodeMessage(decrypted)
			switch mt {
			case MsgStdout:
				gotStdout = true
			case MsgExit:
				gotExit = true
			}
		}
		if !gotExit {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !gotStdout {
		t.Error("expected MsgStdout echoing the piped input")
	}
	if !gotExit {
		t.Fatal("cat did not exit within 4s after MsgStdinClose; EOF signal not propagated")
	}
}

func TestHandler_StreamSessionFlow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping streaming session test on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     10 * time.Second,
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key for encryption
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send metadata - execute echo command (encrypted)
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello world"},
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt metadata: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK message with timeout
	deadline := time.Now().Add(5 * time.Second)
	var messages []mockMessage
	for time.Now().Before(deadline) {
		messages = writer.getMessages()
		if len(messages) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(messages) == 0 {
		t.Fatal("Expected messages from handler")
	}

	// First message should be ACK (decrypt first)
	firstMsg := messages[0]
	decrypted, err := sessionKey.Decrypt(firstMsg.data)
	if err != nil {
		t.Fatalf("Failed to decrypt first message: %v", err)
	}
	msgType, _, err := DecodeMessage(decrypted)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}
	if msgType != MsgAck {
		t.Errorf("First message type = %d, want MsgAck (%d)", msgType, MsgAck)
	}

	// Wait for stdout and exit messages with timeout
	hasStdout := false
	hasExit := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		messages = writer.getMessages()
		for _, msg := range messages {
			decrypted, err := sessionKey.Decrypt(msg.data)
			if err != nil {
				continue // Skip messages we can't decrypt
			}
			msgType, _, _ := DecodeMessage(decrypted)
			if msgType == MsgStdout {
				hasStdout = true
			}
			if msgType == MsgExit {
				hasExit = true
			}
		}
		if hasStdout && hasExit {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !hasStdout {
		t.Error("Expected STDOUT message")
	}
	if !hasExit {
		t.Error("Expected EXIT message")
	}

	// Close handler
	handler.Close()
}

// TestHandler_StreamSession_GrandchildInheritsStdout verifies that a child
// process which forks a grandchild inheriting stdout/stderr does NOT cause
// waitForExit to hang. The session timeout fires, ctx cancels,
// ForceCloseOutput unblocks the pumps, and the session ends within the
// test deadline rather than waiting for the grandchild to exit naturally.
//
// Regression test for the pre-fix hang where pumpsDone.Wait() blocked
// indefinitely because the grandchild kept the pipe write ends open.
func TestHandler_StreamSession_GrandchildInheritsStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemonization syntax is platform-specific; skip on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     1 * time.Second, // ctx will cancel after ~1s
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)
	defer handler.Close()

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// `sh -c 'sleep 5 &'` backgrounds a 5s sleep that inherits stdout/stderr,
	// then sh exits immediately. Without the fix, the pump goroutines would
	// block on the inherited pipes for the full 5 seconds. With the fix, the
	// session ctx cancels at ~1s, ForceCloseOutput unblocks the pumps, and
	// MsgExit ships within ~2s.
	meta := &ShellMeta{
		Command: "sh",
		Args:    []string{"-c", "sleep 5 &"},
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Use a 4s deadline. With the fix this fires at ~2s. Without the fix
	// the pumps hang for the full 5s grandchild lifetime, which is over
	// budget and the test fails. Pre- and post-fix outcomes are unambiguous.
	deadline := time.Now().Add(4 * time.Second)
	var exited bool
	for time.Now().Before(deadline) {
		for _, msg := range writer.getMessages() {
			decrypted, err := sessionKey.Decrypt(msg.data)
			if err != nil {
				continue
			}
			msgType, _, _ := DecodeMessage(decrypted)
			if msgType == MsgExit {
				exited = true
				break
			}
		}
		if exited {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !exited {
		t.Fatal("session did not produce MsgExit within 4s; pumps likely hung on inherited fds")
	}
}

func TestHandler_HandleMetadataError(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"ls", "cat"}, // echo not allowed
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key for encryption
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send metadata with non-whitelisted command (encrypted)
	meta := &ShellMeta{
		Command: "echo", // Not in whitelist
		Args:    []string{"hello"},
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt metadata: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for response
	time.Sleep(100 * time.Millisecond)

	// Check for error message (decrypt first)
	messages := writer.getMessages()
	hasError := false
	for _, msg := range messages {
		decrypted, err := sessionKey.Decrypt(msg.data)
		if err != nil {
			continue // Skip messages we can't decrypt
		}
		msgType, _, _ := DecodeMessage(decrypted)
		if msgType == MsgError {
			hasError = true
		}
	}

	if !hasError {
		t.Error("Expected ERROR message for non-whitelisted command")
	}

	// Stream should be closed
	if !writer.isClosed(streamID) {
		t.Error("Stream should be closed after error")
	}
}

// waitForShellError waits for a MsgError to appear in the mock writer and
// returns the decoded ShellError. Fails the test on timeout.
func waitForShellError(t *testing.T, writer *mockDataWriter, sessionKey *crypto.SessionKey, streamID uint64) *ShellError {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range writer.getMessages() {
			if msg.streamID != streamID {
				continue
			}
			decrypted, err := sessionKey.Decrypt(msg.data)
			if err != nil {
				continue
			}
			msgType, payload, err := DecodeMessage(decrypted)
			if err != nil || msgType != MsgError {
				continue
			}
			shellErr, err := DecodeError(payload)
			if err != nil {
				t.Fatalf("DecodeError() error = %v", err)
			}
			return shellErr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for MsgError")
	return nil
}

// sendEncryptedMeta encodes a ShellMeta, encrypts it with the session key, and
// hands it to the handler as a client frame.
func sendEncryptedMeta(t *testing.T, handler *Handler, sessionKey *crypto.SessionKey, peerID identity.AgentID, streamID uint64, meta *ShellMeta) {
	t.Helper()
	metaMsg, err := EncodeMeta(meta)
	if err != nil {
		t.Fatalf("EncodeMeta() error = %v", err)
	}
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("encrypt metadata: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)
}

func TestHandler_HandleMetadata_AuthMissing(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	rejectMissing := func(env *crypto.AuthEnvelope) error {
		if env == nil || len(env.Signature) == 0 {
			return crypto.ErrAuthMissing
		}
		return nil
	}
	handler := NewHandler(exec, writer, rejectMissing, logger)
	defer handler.Close()

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// META with no auth envelope
	sendEncryptedMeta(t, handler, sessionKey, peerID, streamID, &ShellMeta{
		Command: "echo",
		Args:    []string{"hi"},
	})

	shellErr := waitForShellError(t, writer, sessionKey, streamID)
	if shellErr.Code != ErrCodeAuthRequired {
		t.Errorf("ShellError.Code = %d, want %d (ErrCodeAuthRequired)", shellErr.Code, ErrCodeAuthRequired)
	}
	if !writer.isClosed(streamID) {
		t.Error("stream should be closed after auth-required error")
	}
}

func TestHandler_HandleMetadata_BadSignature(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	alwaysReject := func(_ *crypto.AuthEnvelope) error { return crypto.ErrAuthBadSignature }
	handler := NewHandler(exec, writer, alwaysReject, logger)
	defer handler.Close()

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	sendEncryptedMeta(t, handler, sessionKey, peerID, streamID, &ShellMeta{
		Command: "echo",
		Args:    []string{"hi"},
		Auth:    &crypto.AuthEnvelope{Signature: make([]byte, crypto.Ed25519SignatureSize)},
	})

	shellErr := waitForShellError(t, writer, sessionKey, streamID)
	if shellErr.Code != ErrCodeInvalidCredentials {
		t.Errorf("ShellError.Code = %d, want %d (ErrCodeInvalidCredentials)", shellErr.Code, ErrCodeInvalidCredentials)
	}
	if !writer.isClosed(streamID) {
		t.Error("stream should be closed after invalid-signature error")
	}
}

func TestHandler_HandleMetadata_AuthSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping streaming session test on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     10 * time.Second,
	})

	// Real sign/verify round-trip using a fresh keypair.
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	origin := mustNewAgentID(t)
	self := mustNewAgentID(t)
	verify := func(env *crypto.AuthEnvelope) error {
		if env == nil {
			return crypto.ErrAuthMissing
		}
		return crypto.VerifyManagement(kp.PublicKey, self, *env, crypto.OpShell, nil)
	}
	handler := NewHandler(exec, writer, verify, logger)
	defer handler.Close()

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	env := crypto.SignManagement(kp.PrivateKey, origin, self, crypto.OpShell, nil)
	sendEncryptedMeta(t, handler, sessionKey, peerID, streamID, &ShellMeta{
		Command: "echo",
		Args:    []string{"hi"},
		Auth:    &env,
	})

	// Expect ACK as first decrypted message
	deadline := time.Now().Add(2 * time.Second)
	var gotAck bool
	for time.Now().Before(deadline) && !gotAck {
		for _, msg := range writer.getMessages() {
			if msg.streamID != streamID {
				continue
			}
			decrypted, err := sessionKey.Decrypt(msg.data)
			if err != nil {
				continue
			}
			msgType, _, _ := DecodeMessage(decrypted)
			if msgType == MsgAck {
				gotAck = true
				break
			}
			if msgType == MsgError {
				t.Fatalf("received MsgError when expecting MsgAck for valid signature")
			}
		}
		if !gotAck {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !gotAck {
		t.Fatal("did not receive MsgAck for valid signature")
	}
}

func TestHandler_Close(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	// Open multiple streams
	peerID := mustNewAgentID(t)
	ephKey := testEphemeralKey(t)
	for i := uint64(1); i <= 3; i++ {
		handler.HandleStreamOpen(peerID, i, i, false, ephKey)
	}

	if handler.ActiveStreams() != 3 {
		t.Errorf("ActiveStreams() = %d, want 3", handler.ActiveStreams())
	}

	// Close handler - should close all streams
	handler.Close()

	if handler.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() after Close = %d, want 0", handler.ActiveStreams())
	}
}

func TestHandler_HandleStreamOpen_ZeroEphemeralKey(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Zero ephemeral key should be rejected
	var zeroKey [crypto.KeySize]byte
	errCode, _ := handler.HandleStreamOpen(peerID, streamID, requestID, false, zeroKey)
	if errCode == 0 {
		t.Error("HandleStreamOpen() should reject zero ephemeral key")
	}

	// Stream should not be tracked
	if handler.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() = %d, want 0", handler.ActiveStreams())
	}
}

func TestHandler_HandleStreamOpen_NilExecutor(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Handler with nil executor
	handler := NewHandler(nil, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)
	ephKey := testEphemeralKey(t)

	// Should return error because executor is nil
	errCode, _ := handler.HandleStreamOpen(peerID, streamID, requestID, false, ephKey)
	if errCode == 0 {
		t.Error("HandleStreamOpen() should fail with nil executor")
	}
}

func TestHandler_HandleStreamData_NoStream(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(999) // Non-existent stream

	// Should not panic or error, just return silently
	handler.HandleStreamData(peerID, streamID, []byte("test data"), 0)

	// No messages should be written
	if len(writer.getMessages()) != 0 {
		t.Errorf("Expected no messages for non-existent stream, got %d", len(writer.getMessages()))
	}
}

func TestHandler_HandleStreamClose_NonExistent(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)
	peerID, _ := identity.NewAgentID()

	// Close non-existent stream should not panic
	handler.HandleStreamClose(peerID, uint64(999))

	// ActiveStreams should be 0
	if handler.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() = %d, want 0", handler.ActiveStreams())
	}
}

func TestHandler_HandleStreamClose_Twice(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)
	ephKey := testEphemeralKey(t)

	// Open stream
	handler.HandleStreamOpen(peerID, streamID, requestID, false, ephKey)

	if handler.ActiveStreams() != 1 {
		t.Errorf("ActiveStreams() = %d, want 1", handler.ActiveStreams())
	}

	// Close twice should not panic
	handler.HandleStreamClose(peerID, streamID)
	handler.HandleStreamClose(peerID, streamID)

	if handler.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() after double close = %d, want 0", handler.ActiveStreams())
	}
}

func TestHandler_HandleStreamData_InvalidMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping streaming session test on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send invalid metadata (not proper JSON)
	invalidMeta := EncodeMessage(MsgMeta, []byte("not valid json"))
	encryptedInvalid, err := sessionKey.Encrypt(invalidMeta)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedInvalid, 0)

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	// Should have sent an error and closed the stream
	if !writer.isClosed(streamID) {
		t.Error("Stream should be closed after invalid metadata")
	}
}

func TestHandler_HandleStreamData_WrongMessageType(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping streaming session test on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// First message should be META, but send STDIN instead
	stdinMsg := EncodeStdin([]byte("wrong first message"))
	encryptedStdin, err := sessionKey.Encrypt(stdinMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedStdin, 0)

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	// Should have sent an error and closed the stream
	if !writer.isClosed(streamID) {
		t.Error("Stream should be closed when first message is not META")
	}
}

func TestNewHandler(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	if handler == nil {
		t.Fatal("NewHandler() returned nil")
	}

	if handler.executor != exec {
		t.Error("executor not set correctly")
	}

	if handler.writer != writer {
		t.Error("writer not set correctly")
	}

	if handler.logger != logger {
		t.Error("logger not set correctly")
	}

	if handler.ActiveStreams() != 0 {
		t.Errorf("initial ActiveStreams() = %d, want 0", handler.ActiveStreams())
	}
}

func TestHandler_HandleMessage_Stdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first
	meta := &ShellMeta{
		Command: "cat",
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK
	time.Sleep(100 * time.Millisecond)

	// Now send stdin
	stdinData := []byte("test input data")
	stdinMsg := EncodeStdin(stdinData)
	encryptedStdin, err := sessionKey.Encrypt(stdinMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedStdin, 0)

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

func TestHandler_HandleMessage_Resize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first
	meta := &ShellMeta{
		Command: "cat",
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK
	time.Sleep(100 * time.Millisecond)

	// Now send resize (won't do anything for non-PTY, but tests the handler path)
	resizeMsg := EncodeResize(50, 120)
	encryptedResize, err := sessionKey.Encrypt(resizeMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedResize, 0)

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

func TestHandler_HandleMessage_Signal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first - use sleep so we can signal it
	meta := &ShellMeta{
		Command: "sleep",
		Args:    []string{"10"},
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for session to start
	time.Sleep(200 * time.Millisecond)

	// Now send signal (SIGTERM = 15)
	signalMsg := EncodeSignal(15)
	encryptedSignal, err := sessionKey.Encrypt(signalMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedSignal, 0)

	// Wait for signal to be processed
	time.Sleep(100 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

func TestHandler_HandleMessage_UnknownType(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK
	time.Sleep(200 * time.Millisecond)

	// Send an unknown message type (0xFF)
	unknownMsg := EncodeMessage(0xFF, []byte("unknown data"))
	encryptedUnknown, err := sessionKey.Encrypt(unknownMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedUnknown, 0)

	// Should not crash, just log and continue
	time.Sleep(50 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

func TestHandler_HandleStreamData_ClosedStream(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Close the stream first
	handler.HandleStreamClose(peerID, streamID)

	// Now try to send data to the closed stream
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	// This should silently return without error
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// No crash means success
}

func TestHandler_HandleMessage_InvalidResize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first
	meta := &ShellMeta{
		Command: "cat",
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK
	time.Sleep(100 * time.Millisecond)

	// Send invalid resize (too short payload)
	invalidResize := EncodeMessage(MsgResize, []byte{0x00, 0x18}) // Only 2 bytes, need 4
	encryptedInvalid, err := sessionKey.Encrypt(invalidResize)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedInvalid, 0)

	// Should not crash
	time.Sleep(50 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

func TestHandler_HandleMessage_InvalidSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first
	meta := &ShellMeta{
		Command: "cat",
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK
	time.Sleep(100 * time.Millisecond)

	// Send invalid signal (empty payload)
	invalidSignal := EncodeMessage(MsgSignal, []byte{})
	encryptedInvalid, err := sessionKey.Encrypt(invalidSignal)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedInvalid, 0)

	// Should not crash
	time.Sleep(50 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

func TestHandler_HandleStreamData_EmptyData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	handler := NewHandler(exec, writer, allowAllVerifier, logger)

	peerID := mustNewAgentID(t)
	streamID := uint64(1)
	requestID := uint64(1)

	// Open stream and get session key
	sessionKey := openStreamWithSessionKey(t, handler, peerID, streamID, requestID, false)

	// Send valid metadata first
	meta := &ShellMeta{
		Command: "cat",
	}
	metaMsg, _ := EncodeMeta(meta)
	encryptedMeta, err := sessionKey.Encrypt(metaMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedMeta, 0)

	// Wait for ACK
	time.Sleep(100 * time.Millisecond)

	// Send empty data (after metadata is received)
	emptyMsg := []byte{}
	encryptedEmpty, err := sessionKey.Encrypt(emptyMsg)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	handler.HandleStreamData(peerID, streamID, encryptedEmpty, 0)

	// Should not crash
	time.Sleep(50 * time.Millisecond)

	// Clean up
	handler.HandleStreamClose(peerID, streamID)
}

// Two peers each open their first shell stream, both numbered 1 because stream
// IDs are per peer connection. A shell stream owns a live process, so a peer
// being able to close another peer's stream by quoting a guessable ID would
// kill that process.
func TestHandler_StreamIDCollisionAcrossPeers(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exec := NewExecutor(Config{Enabled: true, MaxSessions: 10, Whitelist: []string{"*"}})
	handler := NewHandler(exec, writer, allowAllVerifier, logger)
	defer handler.Close()

	peerA := mustNewAgentID(t)
	peerB := mustNewAgentID(t)
	ephKey := testEphemeralKey(t)

	if code, _ := handler.HandleStreamOpen(peerA, 1, 1, false, ephKey); code != 0 {
		t.Fatalf("peer A open returned error code %d", code)
	}
	if code, _ := handler.HandleStreamOpen(peerB, 1, 2, false, ephKey); code != 0 {
		t.Fatalf("peer B open returned error code %d", code)
	}

	// Neither open displaced the other.
	if got := handler.ActiveStreams(); got != 2 {
		t.Fatalf("ActiveStreams() = %d, want 2", got)
	}

	// Peer B closing its own stream 1 must leave peer A's running.
	handler.HandleStreamClose(peerB, 1)
	if got := handler.ActiveStreams(); got != 1 {
		t.Errorf("ActiveStreams() = %d, want 1 after peer B closed its own stream", got)
	}

	// A peer that owns nothing here must not be able to close anything.
	stranger := mustNewAgentID(t)
	handler.HandleStreamClose(stranger, 1)
	if got := handler.ActiveStreams(); got != 1 {
		t.Errorf("ActiveStreams() = %d, want 1 after a stranger's close", got)
	}

	// Peer A can still close its own.
	handler.HandleStreamClose(peerA, 1)
	if got := handler.ActiveStreams(); got != 0 {
		t.Errorf("ActiveStreams() = %d, want 0", got)
	}
}

// A disconnected peer's shell streams must be torn down rather than left
// holding a process for a peer that can no longer read its output.
func TestHandler_CloseStreamsForPeer(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exec := NewExecutor(Config{Enabled: true, MaxSessions: 10, Whitelist: []string{"*"}})
	handler := NewHandler(exec, writer, allowAllVerifier, logger)
	defer handler.Close()

	peerA := mustNewAgentID(t)
	peerB := mustNewAgentID(t)
	ephKey := testEphemeralKey(t)

	handler.HandleStreamOpen(peerA, 1, 1, false, ephKey)
	handler.HandleStreamOpen(peerA, 3, 2, false, ephKey)
	handler.HandleStreamOpen(peerB, 1, 3, false, ephKey)

	if n := handler.CloseStreamsForPeer(peerA); n != 2 {
		t.Errorf("CloseStreamsForPeer(A) = %d, want 2", n)
	}
	if got := handler.ActiveStreams(); got != 1 {
		t.Errorf("ActiveStreams() = %d, want 1 (peer B's stream survives)", got)
	}
	if n := handler.CloseStreamsForPeer(peerA); n != 0 {
		t.Errorf("second CloseStreamsForPeer(A) = %d, want 0", n)
	}
}

// ShellStream.closed is set while holding h.mu (alongside the map delete) but
// read on the data path, which holds only ss.mu. Guarding one field with two
// different locks is a data race; this exercises both paths concurrently so
// -race catches a regression to a plain bool.
func TestHandler_ConcurrentCloseAndData(t *testing.T) {
	writer := newMockDataWriter()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exec := NewExecutor(Config{Enabled: true, MaxSessions: 64, Whitelist: []string{"*"}})
	handler := NewHandler(exec, writer, allowAllVerifier, logger)
	defer handler.Close()

	ephKey := testEphemeralKey(t)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		peerID := mustNewAgentID(t)
		streamID := uint64(1)
		if code, _ := handler.HandleStreamOpen(peerID, streamID, uint64(i), false, ephKey); code != 0 {
			t.Fatalf("open %d returned error code %d", i, code)
		}

		wg.Add(2)
		go func() {
			defer wg.Done()
			handler.HandleStreamData(peerID, streamID, []byte("payload"), 0)
		}()
		go func() {
			defer wg.Done()
			handler.HandleStreamClose(peerID, streamID)
		}()
	}
	wg.Wait()

	if got := handler.ActiveStreams(); got != 0 {
		t.Errorf("ActiveStreams() = %d, want 0 after every stream was closed", got)
	}
}
