//go:build windows

package shell

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/conpty"
	"golang.org/x/sys/windows"
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

// ConPTYSession represents an active ConPTY session on Windows.
type ConPTYSession struct {
	cpty           *conpty.ConPty
	process        windows.Handle
	done           chan struct{}
	exitCode       int32
	err            error
	ctx            context.Context
	cancel         context.CancelFunc
	releaseSession func() // Callback to release session slot
	mu             sync.Mutex
	closed         bool
	handleClosed   bool // Track if process handle has been closed
	cptyClosed     bool // Track if ConPTY has been closed
}

// NewPTYSession creates a new ConPTY session on Windows.
func (e *Executor) NewPTYSession(ctx context.Context, meta *ShellMeta) (PTYSessionInterface, error) {
	if err := e.validateAndAcquire(meta); err != nil {
		return nil, err
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	// Set up initial window size
	width := 80
	height := 24
	if meta.TTY != nil {
		if meta.TTY.Cols > 0 {
			width = int(meta.TTY.Cols)
		}
		if meta.TTY.Rows > 0 {
			height = int(meta.TTY.Rows)
		}
	}

	// Create ConPTY
	cpty, err := conpty.New(width, height, 0)
	if err != nil {
		cancel()
		e.ReleaseSession()
		return nil, fmt.Errorf("failed to create ConPTY: %w", err)
	}

	// Build environment
	env := os.Environ()
	if meta.TTY != nil && meta.TTY.Term != "" {
		env = append(env, "TERM="+meta.TTY.Term)
	} else {
		env = append(env, "TERM=xterm-256color")
	}
	for k, v := range meta.Env {
		env = append(env, k+"="+v)
	}

	// Build process attributes
	procAttr := &syscall.ProcAttr{
		Env: env,
	}
	if meta.WorkDir != "" {
		procAttr.Dir = meta.WorkDir
	}

	// Spawn the process
	_, handle, err := cpty.Spawn(meta.Command, meta.Args, procAttr)
	if err != nil {
		cpty.Close()
		cancel()
		e.ReleaseSession()
		return nil, fmt.Errorf("failed to spawn process: %w", err)
	}

	session := &ConPTYSession{
		cpty:           cpty,
		process:        windows.Handle(handle),
		done:           make(chan struct{}),
		exitCode:       -1,
		ctx:            sessionCtx,
		cancel:         cancel,
		releaseSession: e.ReleaseSession,
	}

	// Wait for process exit in background
	go func() {
		defer close(session.done)
		defer session.releaseSession() // Release session slot when process exits

		// Wait for process to exit
		windows.WaitForSingleObject(session.process, windows.INFINITE)

		// Get exit code
		session.mu.Lock()
		var exitCode uint32
		if err := windows.GetExitCodeProcess(session.process, &exitCode); err == nil {
			session.exitCode = int32(exitCode)
		}
		session.mu.Unlock()

		// Note: We don't close the process handle here to avoid race conditions.
		// The handle will be closed in Close() after waiting for the done channel.
	}()

	// Monitor context cancellation and process exit
	go func() {
		select {
		case <-sessionCtx.Done():
			session.Close()
		case <-session.done:
			// Process exited normally - close ConPTY to unblock Read()
			session.mu.Lock()
			if !session.closed && !session.cptyClosed && session.cpty != nil {
				session.cpty.Close()
				session.cptyClosed = true
			}
			session.mu.Unlock()
		}
	}()

	return session, nil
}

// Read reads from the ConPTY output.
func (s *ConPTYSession) Read(p []byte) (n int, err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, fmt.Errorf("session is closed")
	}
	s.mu.Unlock()
	return s.cpty.Read(p)
}

// Write writes to the ConPTY input.
func (s *ConPTYSession) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, fmt.Errorf("session is closed")
	}
	s.mu.Unlock()
	return s.cpty.Write(p)
}

// Resize resizes the ConPTY.
func (s *ConPTYSession) Resize(rows, cols uint16) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session is closed")
	}
	s.mu.Unlock()
	return s.cpty.Resize(int(cols), int(rows))
}

// Signal sends a signal to the process.
// On Windows, only SIGINT and SIGTERM/SIGKILL are supported.
func (s *ConPTYSession) Signal(sig syscall.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.handleClosed {
		return fmt.Errorf("session is closed")
	}

	// Check if process has already exited
	select {
	case <-s.done:
		return fmt.Errorf("process has exited")
	default:
	}

	switch sig {
	case syscall.SIGINT:
		// Send Ctrl+C event
		// Note: GenerateConsoleCtrlEvent sends to all processes in the console group.
		// This is a Windows limitation. Consider sending Ctrl+C via PTY write for
		// more targeted signaling if this becomes problematic.
		return windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0)
	case syscall.SIGTERM, syscall.SIGKILL:
		// Terminate the process
		return windows.TerminateProcess(s.process, 1)
	default:
		// Ignore unsupported signals silently
		return nil
	}
}

// Wait waits for the process to exit and returns the exit code.
func (s *ConPTYSession) Wait() int32 {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// Close closes the ConPTY session.
func (s *ConPTYSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()

	// Close ConPTY first (this will also close the pipes and may cause the process to exit)
	s.mu.Lock()
	if !s.cptyClosed && s.cpty != nil {
		s.cpty.Close()
		s.cptyClosed = true
	}
	s.mu.Unlock()

	// Check if process is still running
	select {
	case <-s.done:
		// Process already exited
	default:
		// Process still running, terminate it
		s.mu.Lock()
		if !s.handleClosed {
			windows.TerminateProcess(s.process, 1)
		}
		s.mu.Unlock()

		// Wait for process to exit with timeout
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			// Timeout waiting for process, continue anyway
		}
	}

	// Now close the process handle
	s.mu.Lock()
	if !s.handleClosed {
		windows.CloseHandle(s.process)
		s.handleClosed = true
	}
	s.mu.Unlock()
}
