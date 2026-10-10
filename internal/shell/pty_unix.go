//go:build !windows

package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// PTYSessionInterface defines the interface for PTY sessions.
type PTYSessionInterface interface {
	// Read reads from the PTY output.
	Read(p []byte) (n int, err error)
	// Write writes to the PTY input.
	Write(p []byte) (n int, err error)
	// Resize resizes the PTY.
	Resize(rows, cols uint16) error
	// Signal sends a signal to the process.
	Signal(sig syscall.Signal) error
	// Wait waits for the process to exit and returns the exit code.
	Wait() int32
	// Close closes the PTY session.
	Close()
}

// PTYSession represents an active PTY session.
type PTYSession struct {
	ptmx     *os.File
	cmd      *exec.Cmd
	done     chan struct{}
	exitCode int32
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
}

// NewPTYSession creates a new PTY session and adds it to the executor.
func (e *Executor) NewPTYSession(ctx context.Context, meta *ShellMeta) (PTYSessionInterface, error) {
	if err := e.validateAndAcquire(meta); err != nil {
		return nil, err
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	// Create command
	cmd := exec.CommandContext(sessionCtx, meta.Command, meta.Args...)

	// Set up environment
	cmd.Env = os.Environ()
	if meta.TTY != nil && meta.TTY.Term != "" {
		cmd.Env = append(cmd.Env, "TERM="+meta.TTY.Term)
	} else {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}
	for k, v := range meta.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Set working directory
	if meta.WorkDir != "" {
		cmd.Dir = meta.WorkDir
	}

	// Set up initial window size
	winsize := &pty.Winsize{
		Rows: 24,
		Cols: 80,
	}
	if meta.TTY != nil {
		if meta.TTY.Rows > 0 {
			winsize.Rows = meta.TTY.Rows
		}
		if meta.TTY.Cols > 0 {
			winsize.Cols = meta.TTY.Cols
		}
	}

	// Start with PTY
	ptmx, err := pty.StartWithSize(cmd, winsize)
	if err != nil {
		cancel()
		e.ReleaseSession()
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	session := &PTYSession{
		ptmx:     ptmx,
		cmd:      cmd,
		done:     make(chan struct{}),
		exitCode: -1,
		ctx:      sessionCtx,
		cancel:   cancel,
	}

	// Wait for command in background
	go func() {
		err := cmd.Wait()
		session.mu.Lock()
		session.err = err
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				session.exitCode = int32(exitErr.ExitCode())
			}
		} else {
			session.exitCode = 0
		}
		session.mu.Unlock()
		close(session.done)
	}()

	return session, nil
}

// Read reads from the PTY output.
func (s *PTYSession) Read(p []byte) (n int, err error) {
	return s.ptmx.Read(p)
}

// Write writes to the PTY input.
func (s *PTYSession) Write(p []byte) (n int, err error) {
	return s.ptmx.Write(p)
}

// Resize resizes the PTY.
func (s *PTYSession) Resize(rows, cols uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Signal sends a signal to the process.
func (s *PTYSession) Signal(sig syscall.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd.Process == nil {
		return fmt.Errorf("no process")
	}

	return s.cmd.Process.Signal(sig)
}

// Wait waits for the process to exit and returns the exit code.
func (s *PTYSession) Wait() int32 {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// Close closes the PTY session.
func (s *PTYSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()

	// Close PTY master
	if s.ptmx != nil {
		s.ptmx.Close()
	}

	// Kill process if still running
	s.mu.Lock()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	s.mu.Unlock()
}
