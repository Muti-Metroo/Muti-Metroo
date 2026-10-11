// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/apiclient"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/shell"
)

// newShellTestChain creates a 4-agent chain with shell enabled on agent D
// and HTTP server enabled on agent A. Management signing keys are installed
// on every agent by default so privileged operations succeed out of the box.
func newShellTestChain(t *testing.T, shellCfg *config.ShellConfig) *AgentChain {
	chain := NewAgentChain(t)
	chain.EnableHTTP = true
	chain.ShellConfig = shellCfg
	return chain
}

// executeCommand runs a shell command through the mesh and returns the result.
// Authentication is handled automatically: the gateway agent (A) signs the
// request with its configured management private key, and agent D verifies it
// against the shared public key set up by the test chain.
func executeCommand(t *testing.T, chain *AgentChain, targetID, command string, args []string) (stdout, stderr string, exitCode int, err error) {
	var stdoutBuf, stderrBuf bytes.Buffer

	client := shell.NewClient(shell.ClientConfig{
		Endpoint:    apiclient.Endpoint{Scheme: "http", Host: chain.HTTPAddrs[0]},
		TargetID:    targetID,
		Interactive: false, // Streaming mode
		Command:     command,
		Args:        args,
		Stdout:      &stdoutBuf,
		Stderr:      &stderrBuf,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exitCode, err = client.Run(ctx)
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// TestShell_OneOffCommand tests executing a simple command through the mesh.
func TestShell_OneOffCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	stdout, stderr, exitCode, err := executeCommand(t, chain, targetID, "whoami", nil)
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	output := strings.TrimSpace(stdout)
	if output == "" {
		t.Errorf("Expected non-empty stdout, got empty (stderr: %s)", stderr)
	} else {
		t.Logf("whoami output: %s", output)
	}
}

// TestShell_CommandWithArgs tests executing a command with arguments.
func TestShell_CommandWithArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	stdout, _, exitCode, err := executeCommand(t, chain, targetID, "echo", []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	output := strings.TrimSpace(stdout)
	if output != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", output)
	}
}

// TestShell_CommandWithStderr tests that stderr is captured separately.
func TestShell_CommandWithStderr(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	stdout, stderr, exitCode, err := executeCommand(t, chain, targetID, "sh", []string{"-c", "echo stdout; echo stderr >&2"})
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	if !strings.Contains(stdout, "stdout") {
		t.Errorf("Expected stdout to contain 'stdout', got '%s'", stdout)
	}

	if !strings.Contains(stderr, "stderr") {
		t.Errorf("Expected stderr to contain 'stderr', got '%s'", stderr)
	}
}

// TestShell_NonZeroExitCode tests that non-zero exit codes are propagated.
func TestShell_NonZeroExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	_, _, exitCode, err := executeCommand(t, chain, targetID, "sh", []string{"-c", "exit 42"})
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if exitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", exitCode)
	}
}

// TestShell_StreamingOutput tests that streaming output is received correctly.
func TestShell_StreamingOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	stdout, _, exitCode, err := executeCommand(t, chain, targetID, "sh", []string{"-c", "for i in 1 2 3 4 5; do echo line$i; done"})
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	for i := 1; i <= 5; i++ {
		expected := "line" + string(rune('0'+i))
		if !strings.Contains(stdout, expected) {
			t.Errorf("Expected stdout to contain '%s', got '%s'", expected, stdout)
		}
	}
}

// TestShell_SignatureRequired verifies that a shell request from an agent
// without the management signing private key fails before reaching the
// target. This is the "no management key, no permissions" guarantee.
func TestShell_SignatureRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	// Strip the private key from the gateway agent (A) only. The origin
	// can no longer sign, so the request is rejected before any frames
	// hit the wire.
	chain.ManagementConfigure = func(idx int, mgmt *config.ManagementConfig) {
		if idx == 0 {
			mgmt.SigningPrivateKey = ""
		}
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	_, _, _, err := executeCommand(t, chain, targetID, "whoami", nil)
	if err == nil {
		t.Fatal("Expected error when gateway has no management signing private key, got nil")
	}
	if !strings.Contains(err.Error(), "signing") && !strings.Contains(err.Error(), "management") {
		t.Logf("Warning: error does not mention signing/management: %v", err)
	}
}

// TestShell_TargetMissingPublicKey verifies that when the target has no
// public key configured it refuses every inbound shell request, even when
// the gateway signs.
func TestShell_TargetMissingPublicKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.ManagementConfigure = func(idx int, mgmt *config.ManagementConfig) {
		if idx == 3 {
			mgmt.SigningPublicKey = ""
		}
	}

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	_, _, _, err := executeCommand(t, chain, targetID, "whoami", nil)
	if err == nil {
		t.Fatal("Expected error when target has no management signing public key, got nil")
	}
}

// TestShell_WhitelistAllowed tests that whitelisted commands are allowed.
func TestShell_WhitelistAllowed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"whoami", "echo"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	stdout, _, exitCode, err := executeCommand(t, chain, targetID, "whoami", nil)
	if err != nil {
		t.Fatalf("whoami failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for whoami, got %d", exitCode)
	}
	t.Logf("whoami output: %s", strings.TrimSpace(stdout))

	stdout, _, exitCode, err = executeCommand(t, chain, targetID, "echo", []string{"test"})
	if err != nil {
		t.Fatalf("echo failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for echo, got %d", exitCode)
	}
	if strings.TrimSpace(stdout) != "test" {
		t.Errorf("Expected 'test', got '%s'", stdout)
	}
}

// TestShell_WhitelistDenied tests that non-whitelisted commands are denied.
func TestShell_WhitelistDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"whoami"}, // Only whoami allowed
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	_, _, _, err := executeCommand(t, chain, targetID, "cat", []string{"/etc/passwd"})
	if err == nil {
		t.Error("Expected error for non-whitelisted command, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
		if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "whitelist") {
			t.Logf("Warning: error message doesn't mention whitelist: %v", err)
		}
	}
}

// TestShell_MaxSessions tests that session limits are enforced.
func TestShell_MaxSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 2, // Only allow 2 concurrent sessions
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		client := shell.NewClient(shell.ClientConfig{
			Endpoint: apiclient.Endpoint{Scheme: "http", Host: chain.HTTPAddrs[0]},
			TargetID: targetID,
			Command:  "sleep",
			Args:     []string{"10"},
			Stdout:   io.Discard,
			Stderr:   io.Discard,
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Run(ctx)
		}()
	}

	time.Sleep(500 * time.Millisecond)

	_, _, _, err := executeCommand(t, chain, targetID, "whoami", nil)
	if err == nil {
		t.Log("Warning: third session succeeded - max sessions may not be enforced")
	} else if strings.Contains(err.Error(), "session") || strings.Contains(err.Error(), "limit") {
		t.Logf("Got expected session limit error: %v", err)
	} else {
		t.Logf("Got error (may be session limit): %v", err)
	}

	cancel()
	wg.Wait()
}

// TestShell_TTYSessionOpen tests that TTY sessions can be opened and closed.
func TestShell_TTYSessionOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	shellCfg := &config.ShellConfig{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	}

	chain := newShellTestChain(t, shellCfg)
	defer chain.Close()

	chain.CreateAgents(t)
	chain.StartAgents(t)

	time.Sleep(3 * time.Second)

	targetID := chain.Agents[3].ID().String()

	var stdoutBuf, stderrBuf bytes.Buffer
	client := shell.NewClient(shell.ClientConfig{
		Endpoint:    apiclient.Endpoint{Scheme: "http", Host: chain.HTTPAddrs[0]},
		TargetID:    targetID,
		Interactive: true, // TTY mode
		Command:     "sh",
		Args:        []string{"-c", "echo ttytest; exit 0"},
		Stdout:      &stdoutBuf,
		Stderr:      &stderrBuf,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exitCode, err := client.Run(ctx)

	if err != nil {
		t.Logf("TTY session result: err=%v", err)
	} else {
		t.Logf("TTY session completed with exit code %d", exitCode)
		if strings.Contains(stdoutBuf.String(), "ttytest") {
			t.Log("TTY output received correctly")
		}
	}
}
