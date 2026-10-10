package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config contains shell configuration.
type Config struct {
	// Enabled controls whether shell is available
	Enabled bool `yaml:"enabled"`

	// Whitelist contains allowed commands. Empty list = no commands allowed.
	// Use ["*"] to allow all commands (for testing only!).
	// Commands should be base names only (e.g., "whoami", "ls", "bash").
	Whitelist []string `yaml:"whitelist"`

	// Timeout is the optional command timeout (0 = no timeout).
	Timeout time.Duration `yaml:"timeout"`

	// MaxSessions limits concurrent shell sessions (0 = unlimited)
	MaxSessions int `yaml:"max_sessions"`
}

// DefaultConfig returns default shell configuration (disabled).
func DefaultConfig() Config {
	return Config{
		Enabled:     false,
		Whitelist:   []string{},
		MaxSessions: 0, // 0 = unlimited
	}
}

// Executor handles shell command execution with security checks.
type Executor struct {
	config   Config
	mu       sync.Mutex
	sessions int // Active session count
}

// NewExecutor creates a new shell executor.
func NewExecutor(cfg Config) *Executor {
	return &Executor{
		config: cfg,
	}
}

// dangerousArgPattern matches shell metacharacters and injection attempts.
var dangerousArgPattern = regexp.MustCompile(`[;&|$` + "`" + `(){}[\]<>\\!*?~]`)

// hasWildcard returns true if the whitelist contains the wildcard entry "*".
func (e *Executor) hasWildcard() bool {
	for _, w := range e.config.Whitelist {
		if w == "*" {
			return true
		}
	}
	return false
}

// IsCommandAllowed checks if the command is in the whitelist.
func (e *Executor) IsCommandAllowed(command string) bool {
	if len(e.config.Whitelist) == 0 {
		return false
	}

	if e.hasWildcard() {
		return true
	}

	// Only allow base command names - no paths allowed
	if strings.ContainsAny(command, "/\\") {
		return false
	}

	// Command must match exactly (case-sensitive)
	for _, allowed := range e.config.Whitelist {
		if allowed == command {
			return true
		}
	}

	return false
}

// ValidateArgs checks command arguments for dangerous patterns.
func (e *Executor) ValidateArgs(args []string) error {
	// In wildcard mode, skip argument validation
	if e.hasWildcard() {
		return nil
	}

	for i, arg := range args {
		if dangerousArgPattern.MatchString(arg) {
			return fmt.Errorf("argument %d contains dangerous characters", i)
		}
		if filepath.IsAbs(arg) {
			return fmt.Errorf("argument %d: absolute paths not allowed", i)
		}
	}
	return nil
}

// AcquireSession tries to acquire a session slot.
// Returns an error if max sessions reached.
func (e *Executor) AcquireSession() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.config.MaxSessions > 0 && e.sessions >= e.config.MaxSessions {
		return fmt.Errorf("max sessions (%d) reached", e.config.MaxSessions)
	}

	e.sessions++
	return nil
}

// ReleaseSession releases a session slot.
func (e *Executor) ReleaseSession() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.sessions > 0 {
		e.sessions--
	}
}

// ActiveSessions returns the current number of active sessions.
func (e *Executor) ActiveSessions() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessions
}

// Session represents an active shell session.
type Session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	// drainSignal, when non-nil, makes the cmd.Wait goroutine block until
	// the caller closes it (via SignalDrainDone). This is needed when the
	// caller is reading from Stdout()/Stderr() concurrently: per the os/exec
	// docs, "it is incorrect to call Wait before all reads from the pipe
	// have completed", because Wait closes the pipe and any unread buffered
	// data is dropped. The default (nil channel) preserves the previous
	// auto-Wait behavior for callers that don't read the pipes.
	drainSignal chan struct{}
	exitCode    int32
	err         error
	mu          sync.Mutex
	started     bool
	startTime   time.Time
}

// validateAndAcquire performs common validation for all session types:
// checks if shell is enabled, validates the command against the whitelist,
// sanitizes arguments, and acquires a session slot. Authentication is
// verified upstream by the stream handler against the mesh management
// signing key -- this function assumes the caller is already authorized.
func (e *Executor) validateAndAcquire(meta *ShellMeta) error {
	if !e.config.Enabled {
		return fmt.Errorf("shell is disabled")
	}

	if !e.IsCommandAllowed(meta.Command) {
		return fmt.Errorf("command '%s' is not allowed", meta.Command)
	}

	if err := e.ValidateArgs(meta.Args); err != nil {
		return err
	}

	return e.AcquireSession()
}

// NewSession creates a new streaming shell session (non-PTY).
func (e *Executor) NewSession(ctx context.Context, meta *ShellMeta) (*Session, error) {
	if err := e.validateAndAcquire(meta); err != nil {
		return nil, err
	}

	// Determine timeout (per-request timeout takes precedence, then config timeout)
	var maxDuration time.Duration
	if meta.Timeout > 0 {
		maxDuration = time.Duration(meta.Timeout) * time.Second
	} else if e.config.Timeout > 0 {
		maxDuration = e.config.Timeout
	}

	// Create context with optional timeout
	var sessionCtx context.Context
	var cancel context.CancelFunc
	if maxDuration > 0 {
		sessionCtx, cancel = context.WithTimeout(ctx, maxDuration)
	} else {
		sessionCtx, cancel = context.WithCancel(ctx)
	}

	// Create command
	cmd := exec.CommandContext(sessionCtx, meta.Command, meta.Args...)

	// Set up environment
	if len(meta.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range meta.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Set working directory
	if meta.WorkDir != "" {
		cmd.Dir = meta.WorkDir
	}

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		e.ReleaseSession()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		e.ReleaseSession()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		cancel()
		e.ReleaseSession()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	session := &Session{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		ctx:       sessionCtx,
		cancel:    cancel,
		done:      make(chan struct{}),
		exitCode:  -1,
		startTime: time.Now(),
	}

	return session, nil
}

// RequireDrain arms the Session to wait for an explicit drain signal before
// calling cmd.Wait. Callers that read from Stdout()/Stderr() must call this
// before Start, then call SignalDrainDone after the read goroutines have
// finished, otherwise cmd.Wait can race the readers and drop the final
// stdout/stderr chunks (per os/exec docs on StdoutPipe/StderrPipe).
func (s *Session) RequireDrain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drainSignal == nil {
		s.drainSignal = make(chan struct{})
	}
}

// SignalDrainDone tells the Wait goroutine that all reads from Stdout/Stderr
// have completed and it is now safe to call cmd.Wait. No-op if the session
// was not armed via RequireDrain. Idempotent.
func (s *Session) SignalDrainDone() {
	s.mu.Lock()
	ch := s.drainSignal
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// ForceCloseOutput closes the stdout and stderr ReadClosers held by this
// session. This unblocks any goroutine currently blocked on a Read of
// either pipe -- needed when the child has forked a grandchild that
// inherits the pipe write end, so the parent's death no longer signals
// EOF on the read end. Safe to call multiple times.
func (s *Session) ForceCloseOutput() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	if s.stderr != nil {
		_ = s.stderr.Close()
	}
}

// Start starts the session command.
func (s *Session) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("session already started")
	}

	if err := s.cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to start command: %w", err)
	}

	s.started = true
	drainSignal := s.drainSignal
	s.mu.Unlock()

	// Wait for command in background. If a drain signal is set, block until
	// the caller signals that all stdout/stderr reads are done -- otherwise
	// cmd.Wait would close the pipes and drop the final buffered chunks.
	go func() {
		if drainSignal != nil {
			<-drainSignal
		}
		err := s.cmd.Wait()
		s.mu.Lock()
		s.err = err
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				s.exitCode = int32(exitErr.ExitCode())
			}
		} else {
			s.exitCode = 0
		}
		s.mu.Unlock()
		close(s.done)
	}()

	return nil
}

// Stdin returns the stdin writer for the session.
func (s *Session) Stdin() io.WriteCloser {
	return s.stdin
}

// Stdout returns the stdout reader for the session.
func (s *Session) Stdout() io.ReadCloser {
	return s.stdout
}

// Stderr returns the stderr reader for the session.
func (s *Session) Stderr() io.ReadCloser {
	return s.stderr
}

// Done returns a channel that closes when the session exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Context returns the session context.
func (s *Session) Context() context.Context {
	return s.ctx
}

// ExitCode returns the exit code after the session ends.
func (s *Session) ExitCode() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// Error returns any error from the session.
func (s *Session) Error() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Signal sends a signal to the session process.
func (s *Session) Signal(sig syscall.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return fmt.Errorf("session not started")
	}

	if s.cmd.Process == nil {
		return fmt.Errorf("no process")
	}

	return s.cmd.Process.Signal(sig)
}

// Close terminates the session.
func (s *Session) Close() {
	s.cancel()

	// Close stdin to signal EOF to process
	if s.stdin != nil {
		s.stdin.Close()
	}

	// Kill the process if still running
	s.mu.Lock()
	if s.started && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	s.mu.Unlock()

	// Wait for done
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		// Force kill if not done
	}
}

// Duration returns how long the session has been running.
func (s *Session) Duration() time.Duration {
	return time.Since(s.startTime)
}
