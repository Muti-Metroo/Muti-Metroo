package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/postalsys/muti-metroo/internal/apiclient"
	"golang.org/x/term"
	"nhooyr.io/websocket"
)

// Client handles shell session connections via WebSocket.
type Client struct {
	endpoint    apiclient.Endpoint
	targetID    string
	url         string
	interactive bool
	token       string
	command     string
	args        []string
	env         map[string]string
	workDir     string
	timeout     int

	// Output writers (defaults to os.Stdout/os.Stderr)
	stdout io.Writer
	stderr io.Writer

	// Agent info (fetched before connecting)
	agentName string

	conn      *websocket.Conn
	done      chan struct{}
	exitCode  int32
	exitError error
	mu        sync.Mutex
}

// ClientConfig contains configuration for the shell client.
type ClientConfig struct {
	// Endpoint is the parsed agent API endpoint (scheme + host + optional path prefix).
	Endpoint apiclient.Endpoint
	// TargetID is the target agent ID
	TargetID string
	// Interactive enables TTY mode (requires --tty flag, streaming is default)
	Interactive bool
	// Token is the API bearer token for HTTP authentication
	Token string
	// Command is the command to execute
	Command string
	// Args are command arguments
	Args []string
	// Env is additional environment variables
	Env map[string]string
	// WorkDir is the working directory
	WorkDir string
	// Timeout is the session timeout in seconds (0 = no timeout)
	Timeout int
	// Stdout is the writer for stdout (defaults to os.Stdout)
	Stdout io.Writer
	// Stderr is the writer for stderr (defaults to os.Stderr)
	Stderr io.Writer
}

// NewClient creates a new shell client.
func NewClient(cfg ClientConfig) *Client {
	mode := "tty"
	if !cfg.Interactive {
		mode = "stream"
	}

	url := cfg.Endpoint.WSURL(fmt.Sprintf("/agents/%s/shell?mode=%s", cfg.TargetID, mode))

	// Set default output writers
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	return &Client{
		endpoint:    cfg.Endpoint,
		targetID:    cfg.TargetID,
		url:         url,
		interactive: cfg.Interactive,
		token:       cfg.Token,
		command:     cfg.Command,
		args:        cfg.Args,
		env:         cfg.Env,
		workDir:     cfg.WorkDir,
		timeout:     cfg.Timeout,
		stdout:      stdout,
		stderr:      stderr,
		done:        make(chan struct{}),
	}
}

// Run executes the shell session and returns the exit code.
func (c *Client) Run(ctx context.Context) (int, error) {
	// Fetch agent info for greeting (only for interactive mode)
	if c.interactive {
		c.fetchAgentInfo()
	}

	// Connect to WebSocket
	dialOpts := &websocket.DialOptions{
		Subprotocols: []string{"muti-metroo-shell"},
	}
	if c.token != "" {
		dialOpts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + c.token},
		}
	}
	conn, _, err := websocket.Dial(ctx, c.url, dialOpts)
	if err != nil {
		return 1, fmt.Errorf("failed to connect: %w", err)
	}
	c.conn = conn
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Build metadata (before setting raw mode so we can get terminal size).
	// The Auth envelope is filled in by the gateway agent (OpenShellStream)
	// using its configured management signing private key.
	meta := &ShellMeta{
		Command: c.command,
		Args:    c.args,
		Env:     c.env,
		WorkDir: c.workDir,
		Timeout: c.timeout,
	}

	// Get terminal size if interactive
	if c.interactive && term.IsTerminal(int(os.Stdin.Fd())) {
		width, height, err := term.GetSize(int(os.Stdin.Fd()))
		if err == nil {
			meta.TTY = &TTYSettings{
				Rows: uint16(height),
				Cols: uint16(width),
				Term: os.Getenv("TERM"),
			}
			if meta.TTY.Term == "" {
				meta.TTY.Term = "xterm-256color"
			}
		}
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		return 1, fmt.Errorf("failed to encode metadata: %w", err)
	}

	// Send META message
	metaMsg := EncodeMessage(MsgMeta, metaData)
	if err := conn.Write(ctx, websocket.MessageBinary, metaMsg); err != nil {
		return 1, fmt.Errorf("failed to send metadata: %w", err)
	}

	// Wait for ACK with timeout (before entering raw mode so errors display properly)
	ackCtx, ackCancel := context.WithTimeout(ctx, 30*time.Second)
	defer ackCancel()

	_, ackData, err := conn.Read(ackCtx)
	if err != nil {
		// Check which context was cancelled
		if ctx.Err() == context.Canceled {
			return 1, fmt.Errorf("connection cancelled")
		}
		if ackCtx.Err() == context.DeadlineExceeded {
			return 1, fmt.Errorf("timeout waiting for shell session to start (remote agent may be unreachable)")
		}
		return 1, fmt.Errorf("failed to read ack: %w", err)
	}

	msgType, payload, err := DecodeMessage(ackData)
	if err != nil {
		return 1, fmt.Errorf("invalid ack message: %w", err)
	}

	if msgType == MsgError {
		var shellErr ShellError
		if err := json.Unmarshal(payload, &shellErr); err != nil {
			return 1, fmt.Errorf("remote error: %s", string(payload))
		}
		return 1, fmt.Errorf("remote error: %s", shellErr.Message)
	}

	if msgType != MsgAck {
		return 1, fmt.Errorf("unexpected message type: %d", msgType)
	}

	var ack ShellAck
	if err := json.Unmarshal(payload, &ack); err != nil {
		return 1, fmt.Errorf("invalid ack: %w", err)
	}

	if !ack.Success {
		return 1, fmt.Errorf("shell session failed: %s", ack.Error)
	}

	// Now that we have a successful ACK, set up terminal if interactive
	// This ensures errors are displayed properly before entering raw mode
	var oldState *term.State
	if c.interactive && term.IsTerminal(int(os.Stdin.Fd())) {
		// Print greeting before entering raw mode
		c.printGreeting()

		oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return 1, fmt.Errorf("failed to set raw mode: %w", err)
		}
		defer func() {
			term.Restore(int(os.Stdin.Fd()), oldState)
			// Print closing message after restoring terminal
			c.printClosing()
		}()
	}

	// Create cancellable context for goroutines
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// Handle window resize (SIGWINCH) in interactive mode
	if c.interactive {
		sigCh := make(chan os.Signal, 1)
		setupResizeSignal(sigCh)
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.handleResize(sessionCtx, sigCh)
		}()
	}

	// Read from stdin and send to WebSocket
	// Note: pumpStdin is NOT added to wg because os.Stdin.Read() is a blocking
	// syscall that doesn't respect context cancellation. The goroutine will exit
	// when the connection is closed or when the program exits.
	go func() {
		if c.interactive {
			defer cancel() // In interactive mode, stdin close ends the session
		}
		c.pumpStdin(sessionCtx)
	}()

	// Read from WebSocket and write to stdout/stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		c.pumpOutput(sessionCtx)
	}()

	// Wait for session to complete
	select {
	case <-c.done:
	case <-sessionCtx.Done():
	}

	// Cancel and wait for goroutines
	cancel()
	wg.Wait()

	return int(c.exitCode), c.exitError
}

// pumpStdin reads from stdin and sends to WebSocket.
func (c *Client) pumpStdin(ctx context.Context) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if n > 0 {
			msg := EncodeStdin(buf[:n])
			if err := c.conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
				return
			}
		}
		if err != nil {
			if err == io.EOF && !c.interactive {
				// Tell the remote that no more stdin will arrive so
				// EOF-sensitive commands (cat, wc, sort, ...) exit
				// cleanly. Interactive mode closes the whole session
				// instead (see defer cancel in Run).
				_ = c.conn.Write(ctx, websocket.MessageBinary, EncodeStdinClose())
			} else if err != io.EOF {
				c.setError(err)
			}
			return
		}
	}
}

// pumpOutput reads from WebSocket and writes to stdout/stderr.
func (c *Client) pumpOutput(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := c.conn.Read(ctx)
		if err != nil {
			// Normal closure or EOF after MsgExit: clean shutdown.
			// Otherwise: record the error for the caller.
			select {
			case <-c.done:
				// MsgExit already received
			default:
				if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					c.setError(err)
				}
				close(c.done)
			}
			return
		}

		msgType, payload, err := DecodeMessage(data)
		if err != nil {
			c.setError(fmt.Errorf("invalid message: %w", err))
			return
		}

		switch msgType {
		case MsgStdout:
			c.stdout.Write(payload)
		case MsgStderr:
			c.stderr.Write(payload)
		case MsgExit:
			exitCode, err := DecodeExit(payload)
			if err != nil {
				c.setError(err)
			} else {
				c.mu.Lock()
				c.exitCode = exitCode
				c.mu.Unlock()
			}
			close(c.done)
			return
		case MsgError:
			var shellErr ShellError
			if err := json.Unmarshal(payload, &shellErr); err != nil {
				c.setError(fmt.Errorf("remote error: %s", string(payload)))
			} else {
				c.setError(fmt.Errorf("remote error: %s", shellErr.Message))
			}
			close(c.done)
			return
		}
	}
}

// handleResize handles SIGWINCH signals and sends resize messages.
func (c *Client) handleResize(ctx context.Context, sigCh <-chan os.Signal) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				continue
			}
			width, height, err := term.GetSize(int(os.Stdin.Fd()))
			if err != nil {
				continue
			}
			msg := EncodeResize(uint16(height), uint16(width))
			if err := c.conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
				return
			}
		}
	}
}

// setError sets the exit error (thread-safe).
func (c *Client) setError(err error) {
	c.mu.Lock()
	if c.exitError == nil {
		c.exitError = err
	}
	c.mu.Unlock()
}

// SendSignal sends a signal to the remote process.
func (c *Client) SendSignal(ctx context.Context, sig syscall.Signal) error {
	msg := EncodeSignal(uint8(sig))
	return c.conn.Write(ctx, websocket.MessageBinary, msg)
}

// fetchAgentInfo fetches agent information from the health API.
func (c *Client) fetchAgentInfo() {
	// Try to fetch agent info from /agents endpoint
	url := c.endpoint.HTTPURL("/agents")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var agents []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Hostname    string `json:"hostname"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return
	}

	// Find the target agent
	for _, agent := range agents {
		if agent.ID == c.targetID {
			if agent.DisplayName != "" {
				c.agentName = agent.DisplayName
			} else if agent.Hostname != "" {
				c.agentName = agent.Hostname
			}
			return
		}
	}
}

// displayName returns the agent name if available, otherwise the short target ID.
func (c *Client) displayName() string {
	if c.agentName != "" {
		return c.agentName
	}
	return c.targetID[:8]
}

// printGreeting prints the connection greeting for interactive sessions.
func (c *Client) printGreeting() {
	if c.agentName != "" {
		fmt.Fprintf(os.Stderr, "Connected to %s (%s)\r\n", c.agentName, c.targetID[:8])
	} else {
		fmt.Fprintf(os.Stderr, "Connected to %s\r\n", c.targetID[:8])
	}
}

// printClosing prints the connection closing message for interactive sessions.
func (c *Client) printClosing() {
	fmt.Fprintf(os.Stderr, "Connection to %s closed.\n", c.displayName())
}
