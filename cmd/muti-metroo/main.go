// Package main provides the CLI entry point for Muti Metroo mesh agent.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/postalsys/muti-metroo/internal/agent"
	"github.com/postalsys/muti-metroo/internal/apiclient"
	"github.com/postalsys/muti-metroo/internal/certutil"
	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/embed"
	"github.com/postalsys/muti-metroo/internal/filetransfer"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/probe"
	"github.com/postalsys/muti-metroo/internal/service"
	"github.com/postalsys/muti-metroo/internal/shell"
	"github.com/postalsys/muti-metroo/internal/sysinfo"
	"github.com/postalsys/muti-metroo/internal/wizard"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
	"nhooyr.io/websocket"
)

const (
	agentAddrDefault = "localhost:8080"

	// detachedEnvVar is set by detachSelf() on the re-spawned child so that
	// `muti-metroo run -D` carrying `--detach` in argv does not loop back into
	// detachSelf() inside the child.
	detachedEnvVar = "MUTI_METROO_DETACHED"
)

// Reused flag help strings.
func agentAddrHelp() string { return "Agent API address - host:port or http(s)://host[:port][/prefix]" }
func gatewayAddrHelp() string {
	return "Gateway agent API address - host:port or http(s)://host[:port][/prefix]"
}

// CLI exit codes. Documented in docs/docs/cli/overview.md so scripts and
// runbooks can branch on the failure mode.
const (
	exitOK                   = 0
	exitGenericError         = 1
	exitUsageError           = 2
	exitAgentUnreachable     = 3
	exitAuthFailed           = 4
	exitOperationRejected    = 5
	exitConfirmationRequired = 6
)

// Sentinel errors used to communicate failure category from RunE up to
// main(), where they are mapped to specific exit codes. Wrap with %w to
// preserve message detail.
var (
	errAgentUnreachable     = errors.New("agent unreachable")
	errAuthFailed           = errors.New("authentication failed")
	errOperationRejected    = errors.New("operation rejected")
	errConfirmationRequired = errors.New("confirmation required")
	errUsageError           = errors.New("usage error")
)

var (
	// Version is set at build time via ldflags.
	// When "dev", we use sysinfo.Version which has enhanced dev version info.
	Version = "dev"

	// apiToken is the bearer token for HTTP API authentication.
	// Set via --token flag or MUTI_METROO_TOKEN environment variable.
	apiToken string

	// assumeYes skips interactive confirmation prompts on destructive
	// operations. Set via --yes / -y persistent flag. When true and stdin
	// is not a TTY, scripts can run destructive commands non-interactively.
	assumeYes bool

	// noColorFlag forces color output off when set via --no-color. The
	// effective decision is made by colorEnabled(), which also honors the
	// NO_COLOR env var (no-color.org) and auto-disables when stdout is not
	// a TTY (matches gh/git/kubectl).
	noColorFlag bool
)

// ANSI escape codes used by tabular CLI output. Centralized so colorize()
// can drop them when --no-color, NO_COLOR, or non-TTY stdout is in effect.
const (
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiReset = "\033[0m"
)

// (bcrypt cost lives in internal/config; both muti-metroo hash and the wizard
// import config.DefaultBcryptCost so the OWASP-recommended value is in
// one place.)

// colorEnabled reports whether colorized output should be emitted to
// stdout. False when the operator passed --no-color, when the NO_COLOR
// env var is set (any value), or when stdout is not a terminal -- the
// last case keeps log scrapers, ticketing systems, and audit pipelines
// free of escape-sequence garbage.
func colorEnabled() bool {
	if noColorFlag {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// colorize wraps s in ANSI code when useColor is true, otherwise returns
// s unchanged. The caller is expected to compute useColor once per render
// (via colorEnabled()) and pass the cached value into the loop -- the
// per-call probe of NO_COLOR / IsTerminal would otherwise add a syscall
// per row of tabular output.
func colorize(useColor bool, code, s string) string {
	if !useColor {
		return s
	}
	return code + s + ansiReset
}

func init() {
	// If Version wasn't set via ldflags, use the enhanced version from sysinfo
	// which includes git commit or build timestamp
	if Version == "dev" {
		Version = sysinfo.Version
	} else {
		// Sync sysinfo.Version with ldflags-set version for consistency
		sysinfo.Version = Version
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:     "muti-metroo",
		Short:   "Muti Metroo - Userspace mesh networking agent",
		Long:    "Muti Metroo is a userspace mesh networking agent that creates\nvirtual TCP tunnels across heterogeneous transport layers.\n\nIt enables multi-hop routing with SOCKS5 ingress and CIDR-based\nexit routing, operating entirely in userspace without requiring\nroot privileges.",
		Version: Version,
		// Errors returned from RunE (network failures, confirmation refusals,
		// validation failures) are not usage problems -- only flag/argument
		// parsing is. Suppress the usage block in those cases so operators
		// see one clean error line instead of a wall of help text.
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if apiToken == "" {
				apiToken = os.Getenv("MUTI_METROO_TOKEN")
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&apiToken, "token", "", "API bearer token for authentication (or set MUTI_METROO_TOKEN)")
	rootCmd.PersistentFlags().BoolVarP(&assumeYes, "yes", "y", false, "Skip interactive confirmation prompts on destructive operations")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "Disable ANSI colors in output (also honors NO_COLOR env var; auto-off when stdout is not a TTY)")

	// Define command groups for organized help output.
	rootCmd.AddGroup(&cobra.Group{ID: "start", Title: "Getting Started:"})
	rootCmd.AddGroup(&cobra.Group{ID: "status", Title: "Agent Status:"})
	rootCmd.AddGroup(&cobra.Group{ID: "remote", Title: "Remote Operations:"})
	rootCmd.AddGroup(&cobra.Group{ID: "admin", Title: "Administration:"})

	// Getting Started commands
	setup := setupCmd()
	setup.GroupID = "start"
	rootCmd.AddCommand(setup)

	embedConfig := embedConfigCmd()
	embedConfig.GroupID = "start"
	rootCmd.AddCommand(embedConfig)

	initC := initCmd()
	initC.GroupID = "start"
	rootCmd.AddCommand(initC)

	run := runCmd()
	run.GroupID = "start"
	rootCmd.AddCommand(run)

	// Agent Status commands
	status := statusCmd()
	status.GroupID = "status"
	rootCmd.AddCommand(status)

	peers := peersCmd()
	peers.GroupID = "status"
	rootCmd.AddCommand(peers)

	routes := routesCmd()
	routes.GroupID = "status"
	rootCmd.AddCommand(routes)

	probeC := probeCmd()
	probeC.GroupID = "status"
	rootCmd.AddCommand(probeC)

	meshTest := meshTestCmd()
	meshTest.GroupID = "status"
	rootCmd.AddCommand(meshTest)

	// Remote Operations commands
	shellC := shellCmd()
	shellC.GroupID = "remote"
	rootCmd.AddCommand(shellC)

	upload := uploadCmd()
	upload.GroupID = "remote"
	rootCmd.AddCommand(upload)

	download := downloadCmd()
	download.GroupID = "remote"
	rootCmd.AddCommand(download)

	pingC := pingCmd()
	pingC.GroupID = "remote"
	rootCmd.AddCommand(pingC)

	sleepC := sleepCmd()
	sleepC.GroupID = "remote"
	rootCmd.AddCommand(sleepC)

	wakeC := wakeCmd()
	wakeC.GroupID = "remote"
	rootCmd.AddCommand(wakeC)

	sleepStatusC := sleepStatusCmd()
	sleepStatusC.GroupID = "remote"
	rootCmd.AddCommand(sleepStatusC)

	stopC := stopCmd()
	stopC.GroupID = "remote"
	rootCmd.AddCommand(stopC)

	routeC := routeCmd()
	routeC.GroupID = "remote"
	rootCmd.AddCommand(routeC)

	forwardC := forwardCmd()
	forwardC.GroupID = "remote"
	rootCmd.AddCommand(forwardC)

	peerC := peerCmd()
	peerC.GroupID = "remote"
	rootCmd.AddCommand(peerC)

	listenerC := listenerCmd()
	listenerC.GroupID = "remote"
	rootCmd.AddCommand(listenerC)

	displayNameC := displayNameCmd()
	displayNameC.GroupID = "remote"
	rootCmd.AddCommand(displayNameC)

	hostC := hostCmd()
	hostC.GroupID = "remote"
	rootCmd.AddCommand(hostC)

	// Administration commands
	svc := serviceCmd()
	svc.GroupID = "admin"
	rootCmd.AddCommand(svc)

	cert := certCmd()
	cert.GroupID = "admin"
	rootCmd.AddCommand(cert)

	hash := hashCmd()
	hash.GroupID = "admin"
	rootCmd.AddCommand(hash)

	mgmtKey := managementKeyCmd()
	mgmtKey.GroupID = "admin"
	rootCmd.AddCommand(mgmtKey)

	signingKey := signingKeyCmd()
	signingKey.GroupID = "admin"
	rootCmd.AddCommand(signingKey)

	configC := configCmd()
	configC.GroupID = "admin"
	rootCmd.AddCommand(configC)

	// Auto-start when invoked without arguments and an embedded config is
	// present. The implicit default is "run" (the whole point of embedding a
	// config is to ship a single-file agent); set `default_action: help` in
	// the embedded YAML to opt out and show usage instead.
	if len(os.Args) == 1 && embed.HasEmbeddedConfigSelf() {
		if getEmbeddedDefaultAction() != "help" {
			rootCmd.SetArgs([]string{"run"})
		}
	}

	if err := rootCmd.Execute(); err != nil {
		// Cobra has already printed "Error: <err>" via SilenceErrors=false;
		// skip a second print and just translate to the right exit code.
		os.Exit(classifyExitCode(err))
	}
}

// classifyExitCode maps a returned error to a documented CLI exit code.
// Sentinel errors take precedence; otherwise we fall back to generic error.
// Cobra's own argument-parsing errors are treated as usage errors.
func classifyExitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errConfirmationRequired):
		return exitConfirmationRequired
	case errors.Is(err, errAgentUnreachable):
		return exitAgentUnreachable
	case errors.Is(err, errAuthFailed):
		return exitAuthFailed
	case errors.Is(err, errOperationRejected):
		return exitOperationRejected
	case errors.Is(err, errUsageError):
		return exitUsageError
	}
	// Cobra surfaces flag/argument errors with these well-known prefixes.
	msg := err.Error()
	if strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag") ||
		strings.HasPrefix(msg, "invalid argument") ||
		strings.HasPrefix(msg, "required flag(s)") ||
		strings.HasPrefix(msg, "accepts ") {
		return exitUsageError
	}
	return exitGenericError
}

// setAuthToken sets the Authorization: Bearer header on the request if apiToken is configured.
func setAuthToken(req *http.Request) {
	if apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}
}

// getEmbeddedDefaultAction reads the default_action from embedded config.
// Returns empty string if no embedded config or default_action is not set.
func getEmbeddedDefaultAction() string {
	data, err := embed.ReadFromSelf()
	if err != nil {
		return ""
	}
	var partial struct {
		DefaultAction string `yaml:"default_action"`
	}
	if yaml.Unmarshal(data, &partial) != nil {
		return ""
	}
	return partial.DefaultAction
}

func setupCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard",
		Long: `Run an interactive setup wizard to configure the mesh agent.

The wizard will guide you through:
  - Basic configuration (data directory, config file path)
  - Agent role selection (ingress, transit, exit)
  - Network configuration (transport, listen address)
  - TLS setup (generate, paste, or use existing certificates)
  - Peer connections
  - SOCKS5 proxy settings (for ingress nodes)
  - Exit node configuration (for exit nodes)
  - Advanced options (logging, health checks)

You can also use this command to edit the configuration of an existing binary
that has embedded configuration. Use -c to specify the binary path:

  muti-metroo setup -c /usr/local/bin/my-agent

If the binary has embedded config, it will be loaded as defaults.
After the wizard completes, the new config will be embedded into that binary.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := wizard.New()

			// Check if the config path is a binary with embedded config
			if configPath != "" {
				// Check if it looks like a binary (not .yaml/.yml)
				if !strings.HasSuffix(configPath, ".yaml") && !strings.HasSuffix(configPath, ".yml") {
					// Reject DLL files - embedding is not supported for DLLs
					// (UPX compression is incompatible with config embedding)
					if strings.HasSuffix(strings.ToLower(configPath), ".dll") {
						return fmt.Errorf("config embedding is not supported for DLL files; use a config file instead: rundll32.exe %s,Run config.yaml", configPath)
					}
					// Check if it's an executable
					info, err := os.Stat(configPath)
					if err != nil {
						return fmt.Errorf("failed to stat %s: %w", configPath, err)
					}
					// Check if it's a regular file (not a directory)
					if info.Mode().IsRegular() {
						// Try to set it as target binary
						if err := w.SetTargetBinary(configPath); err != nil {
							return fmt.Errorf("failed to set target binary: %w", err)
						}
					}
				}
			}

			result, err := w.Run()
			if err != nil {
				return fmt.Errorf("setup wizard failed: %w", err)
			}

			_ = result // Result contains the generated config
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or binary with embedded config")

	return cmd
}

func embedConfigCmd() *cobra.Command {
	var (
		binaryPath string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "embed-config <config.yaml>",
		Short: "Embed a YAML config into a Muti Metroo binary",
		Long:  "Embed a YAML config file into a Muti Metroo binary, producing a self-contained\nexecutable that loads its configuration on startup without needing a\nconfig file on disk.\n\nUnlike `muti-metroo setup`, this command is non-interactive and is intended\nfor CI pipelines and scripted deployments where the config is already\nprepared.\n\nBy default, the running binary is updated in place. Use --binary to\nspecify a different source binary, and --output to write the result to\na new path. If the source binary already has an embedded config, it is\nreplaced.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := args[0]

			lower := strings.ToLower(configPath)
			if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
				return fmt.Errorf("config file must have .yaml or .yml extension: %s", configPath)
			}

			if binaryPath == "" {
				exe, err := os.Executable()
				if err != nil {
					return fmt.Errorf("failed to resolve running executable: %w", err)
				}
				binaryPath = exe
			}
			if strings.HasSuffix(strings.ToLower(binaryPath), ".dll") {
				return fmt.Errorf("config embedding is not supported for DLL files; use a config file instead: rundll32.exe %s,Run config.yaml", binaryPath)
			}
			srcInfo, err := os.Stat(binaryPath)
			if err != nil {
				return fmt.Errorf("failed to stat source binary %s: %w", binaryPath, err)
			}
			if !srcInfo.Mode().IsRegular() {
				return fmt.Errorf("source binary is not a regular file: %s", binaryPath)
			}
			srcMode := srcInfo.Mode().Perm()

			if outputPath == "" {
				outputPath = binaryPath
			}

			yamlBytes, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("failed to read config file: %w", err)
			}
			if _, err := config.Parse(yamlBytes); err != nil {
				return fmt.Errorf("invalid config file %s: %w", configPath, err)
			}

			outDir := filepath.Dir(outputPath)
			hasConfig, _ := embed.HasEmbeddedConfig(binaryPath)
			cleanSource := binaryPath
			if hasConfig {
				f, err := os.CreateTemp(outDir, ".muti-metroo-embed-strip-*")
				if err != nil {
					return fmt.Errorf("failed to create temp file: %w", err)
				}
				stripTmp := f.Name()
				f.Close()
				defer os.Remove(stripTmp)
				if err := embed.CopyBinaryWithoutConfig(binaryPath, stripTmp); err != nil {
					return fmt.Errorf("failed to strip existing embedded config: %w", err)
				}
				cleanSource = stripTmp
			}

			f, err := os.CreateTemp(outDir, ".muti-metroo-embed-final-*")
			if err != nil {
				return fmt.Errorf("failed to create temp file: %w", err)
			}
			finalTmp := f.Name()
			f.Close()
			renamed := false
			defer func() {
				if !renamed {
					os.Remove(finalTmp)
				}
			}()

			if err := embed.AppendConfig(cleanSource, finalTmp, yamlBytes); err != nil {
				return fmt.Errorf("failed to embed config: %w", err)
			}
			// AppendConfig opens the existing temp with O_TRUNC, which keeps
			// the 0600 perms set by os.CreateTemp instead of applying the
			// source binary's mode. Restore it explicitly before rename.
			if err := os.Chmod(finalTmp, srcMode); err != nil {
				return fmt.Errorf("failed to set output permissions: %w", err)
			}
			if err := os.Rename(finalTmp, outputPath); err != nil {
				return fmt.Errorf("failed to write output binary: %w", err)
			}
			renamed = true

			fmt.Printf("Embedded config into %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&binaryPath, "binary", "b", "", "Source binary to embed config into (default: running executable)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Destination binary path (default: overwrite source in place)")

	return cmd
}

func initCmd() *cobra.Command {
	var dataDir string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new agent",
		Long:  "Initialize a new agent by creating data directory and generating identity.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if already initialized
			if identity.Exists(dataDir) {
				id, err := identity.Load(dataDir)
				if err != nil {
					return fmt.Errorf("failed to load existing identity: %w", err)
				}
				fmt.Printf("Agent already initialized in %s\n", dataDir)
				fmt.Printf("Agent ID: %s\n", id.String())
				return nil
			}

			// Create new identity
			id, created, err := identity.LoadOrCreate(dataDir)
			if err != nil {
				return fmt.Errorf("failed to initialize agent: %w", err)
			}

			if created {
				fmt.Printf("Agent initialized in %s\n", dataDir)
				fmt.Printf("Agent ID: %s\n", id.String())
			} else {
				fmt.Printf("Agent already exists in %s\n", dataDir)
				fmt.Printf("Agent ID: %s\n", id.String())
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&dataDir, "data-dir", "d", "./data", "Directory for persistent state")

	return cmd
}

func runCmd() *cobra.Command {
	var configPath string
	var startupDelay time.Duration
	var detach bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the mesh agent",
		Long:  "Start the mesh agent with the specified configuration.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if detach && os.Getenv(detachedEnvVar) != "1" {
				return detachSelf()
			}

			// Load configuration (embedded config takes precedence over -c flag)
			cfg, isEmbedded, err := config.LoadOrEmbedded(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if isEmbedded {
				fmt.Println("Loaded configuration: embedded in binary")
			} else {
				abs, _ := filepath.Abs(configPath)
				if info, statErr := os.Stat(abs); statErr == nil {
					fmt.Printf("Loaded configuration from: %s (mtime %s)\n",
						abs, info.ModTime().Format("2006-01-02 15:04:05 MST"))
				} else {
					fmt.Printf("Loaded configuration from: %s\n", abs)
				}
			}

			// Warn loudly if any configured TLS cert is expired or about to
			// expire (<60 days). Auto-generated certs (no path configured)
			// are skipped -- they're regenerated each startup so expiry is
			// not an operator concern. Inline cert_pem material is also
			// skipped here for simplicity; operators using inline PEMs are
			// expected to manage rotation explicitly.
			warnExpiringCerts(cfg)
			warnIgnoredConfigFields(cfg)

			// Override startup delay from CLI flag if explicitly set
			if cmd.Flags().Changed("startup-delay") {
				cfg.Agent.StartupDelay = startupDelay
			}

			// Create agent
			a, err := agent.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to create agent: %w", err)
			}

			// Check if running as Windows service
			if !service.IsInteractive() {
				// Running as Windows service - use service handler
				return service.RunAsService("muti-metroo", a)
			}

			// Running interactively (console mode)
			fmt.Printf("Starting Muti Metroo agent...\n")
			if cfg.Agent.DisplayName != "" {
				fmt.Printf("Display Name: %s\n", cfg.Agent.DisplayName)
			}
			fmt.Printf("Agent ID: %s\n", a.ID().String())
			if cfg.Agent.StartupDelay > 0 {
				fmt.Printf("Startup delay: %s\n", cfg.Agent.StartupDelay)
			}

			// Register signal handler before Start() so signals during
			// the startup delay are caught instead of killing the process.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			// Forward signals to Stop() during startup
			startDone := make(chan struct{})
			go func() {
				select {
				case <-sigCh:
					a.Stop()
				case <-startDone:
				}
			}()

			// Start agent (blocks during startup delay)
			startErr := a.Start()
			close(startDone)

			if startErr != nil {
				if errors.Is(startErr, agent.ErrInterrupted) {
					fmt.Println("\nStartup interrupted, exiting.")
					return nil
				}
				return fmt.Errorf("failed to start agent: %w", startErr)
			}

			// Race guard: the signal goroutine may have called Stop()
			// between Start() returning normally and close(startDone).
			if !a.IsRunning() {
				fmt.Println("\nAgent stopped during startup.")
				return nil
			}

			// Replay persisted dynamic config (routes, forwards, peers,
			// display-name) through the same Manage* methods the CLI hits.
			// Failures here never block agent startup -- any per-event
			// failure is logged by the method itself.
			if err := a.ReplayDynamicConfig(); err != nil {
				fmt.Printf("Warning: replay dynamic config failed: %v\n", err)
			}

			// Print status
			stats := a.Stats()
			if cfg.SOCKS5.Enabled {
				fmt.Printf("SOCKS5 server: %s\n", cfg.SOCKS5.Address)
			}
			if cfg.Exit.Enabled {
				fmt.Printf("Exit routes: %v\n", cfg.Exit.Routes)
			}
			fmt.Printf("Status: running (peers: %d, routes: %d)\n", stats.PeerCount, stats.RouteCount)

			// Wait for graceful shutdown: OS signal or POST /shutdown.
			select {
			case sig := <-sigCh:
				fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
			case <-a.ShutdownCh():
				fmt.Println("\nShutdown requested via API, shutting down...")
			}

			// Graceful shutdown with timeout
			shutdownTimeout := 10 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()

			if err := a.StopWithContext(ctx); err != nil {
				// Stop is still running in the background but didn't finish in
				// time -- usually a goroutine stuck on cancellation-unaware
				// I/O (a wedged file transfer is the canonical example).
				// Force exit instead of returning normally, otherwise the
				// process can hang past this point waiting on goroutines that
				// never unblock.
				fmt.Fprintf(os.Stderr, "Shutdown did not finish within %s: %v -- forcing exit\n", shutdownTimeout, err)
				os.Exit(exitGenericError)
			}

			fmt.Println("Agent stopped.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "./config.yaml", "Path to configuration file (ignored if embedded config present)")
	cmd.Flags().DurationVar(&startupDelay, "startup-delay", 0, "Delay before starting network activity (e.g., 90s, 2m)")
	cmd.Flags().BoolVarP(&detach, "detach", "D", false, "Re-spawn outside SSH Job Object and exit (Windows only)")

	return cmd
}

func statusCmd() *cobra.Command {
	var agentAddr string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		Long: `Display the current status of the running agent via HTTP API.

Shows running agent information including:
  - Agent status (OK/error)
  - Connected peer count
  - Active stream count
  - Route table size
  - SOCKS5 proxy status
  - Exit handler status

Example output:
  Agent Status
  ============
  Status:       OK
  Running:      true
  Peer Count:   3
  Stream Count: 12
  Route Count:  5
  SOCKS5:       true
  Exit Handler: false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			url := ep.HTTPURL("/healthz")
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var status struct {
				Status             string `json:"status"`
				Running            bool   `json:"running"`
				PeerCount          int    `json:"peer_count"`
				StreamCount        int    `json:"stream_count"`
				RouteCount         int    `json:"route_count"`
				SOCKS5Running      bool   `json:"socks5_running"`
				ExitHandlerRunning bool   `json:"exit_handler_running"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}

			fmt.Printf("Agent Status\n")
			fmt.Printf("============\n")
			fmt.Printf("Status:       %s\n", status.Status)
			fmt.Printf("Running:      %v\n", status.Running)
			fmt.Printf("Peer Count:   %d\n", status.PeerCount)
			fmt.Printf("Stream Count: %d\n", status.StreamCount)
			fmt.Printf("Route Count:  %d\n", status.RouteCount)
			fmt.Printf("SOCKS5:       %v\n", status.SOCKS5Running)
			fmt.Printf("Exit Handler: %v\n", status.ExitHandlerRunning)

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// runPeersStatus fetches /api/dashboard and renders the live peer table.
// Shared by the deprecated top-level `peers` command and the canonical
// `peer status` subcommand so both stay in lockstep.
func runPeersStatus(agentAddr string, jsonOutput bool) error {
	ep, err := apiclient.Parse(agentAddr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := ep.HTTPURL("/api/dashboard")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	setAuthToken(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errAgentUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var dashboard struct {
		Peers []struct {
			ID           string `json:"id"`
			ShortID      string `json:"short_id"`
			DisplayName  string `json:"display_name"`
			State        string `json:"state"`
			RTTMs        int64  `json:"rtt_ms"`
			Unresponsive bool   `json:"unresponsive"`
			IsDialer     bool   `json:"is_dialer"`
		} `json:"peers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dashboard); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(dashboard.Peers)
	}

	fmt.Printf("Connected Peers\n")
	fmt.Printf("===============\n")
	if len(dashboard.Peers) == 0 {
		fmt.Println("No peers connected.")
	} else {
		useColor := colorEnabled()
		fmt.Printf("%-12s %-20s %-14s %-10s %-8s\n", "ID", "NAME", "STATE", "ROLE", "RTT")
		fmt.Printf("%-12s %-20s %-14s %-10s %-8s\n", "--", "----", "-----", "----", "---")
		for _, peer := range dashboard.Peers {
			role := "listener"
			if peer.IsDialer {
				role = "dialer"
			}
			rtt := fmt.Sprintf("%dms", peer.RTTMs)
			if peer.RTTMs == 0 {
				rtt = "-"
			}
			state := peer.State
			if peer.Unresponsive {
				state = colorize(useColor, ansiRed, "UNRESPONSIVE")
			}
			fmt.Printf("%-12s %-20s %-14s %-10s %-8s\n",
				peer.ShortID,
				peer.DisplayName,
				state,
				role,
				rtt,
			)
		}
		fmt.Printf("\nTotal: %d peer(s)\n", len(dashboard.Peers))
	}

	return nil
}

func peersCmd() *cobra.Command {
	var agentAddr string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:        "peers",
		Short:      "List connected peers (deprecated: use 'muti-metroo peer status')",
		Long:       "Display all peers currently connected to this agent via HTTP API.\n\nDeprecated: use 'muti-metroo peer status' instead. The top-level 'peers' command will be removed in a future release.",
		Deprecated: "use 'muti-metroo peer status' (same data; cleaner naming alongside 'muti-metroo peer list')",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPeersStatus(agentAddr, jsonOutput)
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// runRoutesStatus fetches /api/dashboard and renders the live route
// table. Shared by the deprecated top-level `routes` command and the
// canonical `route status` subcommand.
func runRoutesStatus(agentAddr string, jsonOutput bool) error {
	ep, err := apiclient.Parse(agentAddr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := ep.HTTPURL("/api/dashboard")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	setAuthToken(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errAgentUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var dashboard struct {
		Routes []struct {
			Network     string   `json:"network"`
			NextHopID   string   `json:"next_hop_id"`
			NextHopName string   `json:"next_hop_name"`
			OriginID    string   `json:"origin_id"`
			OriginName  string   `json:"origin_name"`
			Metric      int      `json:"metric"`
			HopCount    int      `json:"hop_count"`
			PathDisplay []string `json:"path_display"`
			Comment     string   `json:"comment,omitempty"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dashboard); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(dashboard.Routes)
	}

	fmt.Printf("Route Table\n")
	fmt.Printf("===========\n")
	if len(dashboard.Routes) == 0 {
		fmt.Println("No routes in table.")
	} else {
		fmt.Printf("%-20s %-15s %-15s %-8s %-6s %s\n", "NETWORK", "NEXT HOP", "ORIGIN", "METRIC", "HOPS", "COMMENT")
		fmt.Printf("%-20s %-15s %-15s %-8s %-6s %s\n", "-------", "--------", "------", "------", "----", "-------")
		for _, route := range dashboard.Routes {
			nextHop := route.NextHopName
			if nextHop == "" {
				nextHop = route.NextHopID
			}
			origin := route.OriginName
			if origin == "" {
				origin = route.OriginID
			}
			fmt.Printf("%-20s %-15s %-15s %-8d %-6d %s\n",
				route.Network,
				nextHop,
				origin,
				route.Metric,
				route.HopCount,
				route.Comment,
			)
		}
		fmt.Printf("\nTotal: %d route(s)\n", len(dashboard.Routes))
	}

	return nil
}

func routesCmd() *cobra.Command {
	var agentAddr string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:        "routes",
		Short:      "List route table (deprecated: use 'muti-metroo route status')",
		Long:       "Display the current routing table via HTTP API.\n\nDeprecated: use 'muti-metroo route status' instead. The top-level 'routes' command will be removed in a future release.",
		Deprecated: "use 'muti-metroo route status' (same data; cleaner naming alongside 'muti-metroo route list')",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutesStatus(agentAddr, jsonOutput)
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func meshTestCmd() *cobra.Command {
	var agentAddr string
	var timeout string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "mesh-test",
		Short: "Test connectivity to all mesh agents",
		Long: `Test connectivity to all known agents in the mesh network.

This command sends a status request to each known agent and reports
their reachability status along with response times.

Example output:
  Testing mesh connectivity...

  AGENT           STATUS      RESPONSE TIME
  gateway-1       OK          local
  exit-us-west    OK          45ms
  exit-eu-east    OK          123ms
  transit-1       OK          67ms
  exit-offline    FAILED      timeout after 10s

  Summary: 4/5 agents reachable (tested in 1.2s)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			// Parse timeout
			timeoutDuration, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("invalid timeout: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
			defer cancel()

			// POST to force fresh test
			url := ep.HTTPURL("/api/mesh-test")
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			setAuthToken(req)

			if !jsonOutput {
				fmt.Println("Testing mesh connectivity...")
				fmt.Println()
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}

			var result struct {
				LocalAgent     string `json:"local_agent"`
				TestTime       string `json:"test_time"`
				DurationMs     int64  `json:"duration_ms"`
				TotalCount     int    `json:"total_count"`
				ReachableCount int    `json:"reachable_count"`
				Results        []struct {
					AgentID        string `json:"agent_id"`
					ShortID        string `json:"short_id"`
					DisplayName    string `json:"display_name"`
					IsLocal        bool   `json:"is_local"`
					Reachable      bool   `json:"reachable"`
					ResponseTimeMs int64  `json:"response_time_ms"`
					Error          string `json:"error,omitempty"`
				} `json:"results"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			useColor := colorEnabled()
			fmt.Printf("%-20s %-12s %s\n", "AGENT", "STATUS", "RESPONSE TIME")
			fmt.Printf("%-20s %-12s %s\n", "-----", "------", "-------------")
			for _, agent := range result.Results {
				name := agent.DisplayName
				if name == "" {
					name = agent.ShortID
				}

				var status, responseTime string
				if agent.Reachable {
					status = colorize(useColor, ansiGreen, "OK")
					if agent.IsLocal {
						responseTime = "local"
					} else {
						responseTime = fmt.Sprintf("%dms", agent.ResponseTimeMs)
					}
				} else {
					status = colorize(useColor, ansiRed, "FAILED")
					if agent.Error != "" {
						responseTime = agent.Error
					} else {
						responseTime = "unknown error"
					}
				}

				fmt.Printf("%-20s %-12s %s\n", name, status, responseTime)
			}

			fmt.Println()
			durationSec := float64(result.DurationMs) / 1000.0
			fmt.Printf("Summary: %d/%d agents reachable (tested in %.1fs)\n",
				result.ReachableCount, result.TotalCount, durationSec)

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	registerTimeoutFlag(cmd, &timeout, "30s", "Overall timeout for the test")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func probeCmd() *cobra.Command {
	var (
		transport  string
		path       string
		timeout    string
		caCert     string
		clientCert string
		clientKey  string
		insecure   bool
		plaintext  bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "probe <address>",
		Short: "Test connectivity to a Muti Metroo listener",
		Long: `Probe tests if a Muti Metroo listener is reachable and responding.

This command performs:
  1. Transport-level connection (TCP/TLS)
  2. Protocol handshake verification (PEER_HELLO exchange)

Use this to verify connectivity before deploying an agent, or to diagnose
connection issues with existing listeners.

The probe does not require a running agent - it operates standalone.`,
		Example: `  # Test QUIC listener (default transport)
  muti-metroo probe server.example.com:4433

  # Test HTTP/2 listener
  muti-metroo probe --transport h2 server.example.com:443 --path /mesh

  # Test WebSocket listener
  muti-metroo probe --transport ws server.example.com:443 --path /mesh

  # Test plaintext WebSocket (behind reverse proxy)
  muti-metroo probe --transport ws --plaintext localhost:8080 --path /mesh

  # Test with self-signed certificate
  muti-metroo probe --insecure server.example.com:4433

  # Test with specific CA certificate
  muti-metroo probe --ca ./certs/ca.crt server.example.com:4433

  # Test with mTLS (client certificate)
  muti-metroo probe --ca ./certs/ca.crt --cert ./certs/client.crt --key ./certs/client.key server.example.com:4433

  # Output as JSON (for scripting)
  muti-metroo probe --json server.example.com:4433`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]

			// Parse timeout
			timeoutDuration, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("invalid timeout: %w", err)
			}

			// Validate plaintext option
			if plaintext && transport != "ws" {
				return fmt.Errorf("--plaintext is only supported for WebSocket transport (-T ws)")
			}

			// Build probe options
			opts := probe.Options{
				Transport:    transport,
				Address:      address,
				Path:         path,
				Timeout:      timeoutDuration,
				StrictVerify: !insecure,
				CACert:       caCert,
				ClientCert:   clientCert,
				ClientKey:    clientKey,
				PlainText:    plaintext,
			}

			// Run probe
			if !jsonOutput {
				scheme := transport
				if transport == "ws" && plaintext {
					scheme = "ws (plaintext)"
				}
				fmt.Printf("Probing %s://%s...\n", scheme, address)
			}

			ctx := context.Background()
			result := probe.Probe(ctx, opts)

			// Output results
			if jsonOutput {
				type probeJSON struct {
					Success           bool    `json:"success"`
					Transport         string  `json:"transport"`
					Address           string  `json:"address"`
					RemoteID          string  `json:"remote_id,omitempty"`
					RemoteDisplayName string  `json:"remote_display_name,omitempty"`
					RTTMs             float64 `json:"rtt_ms,omitempty"`
					Error             string  `json:"error,omitempty"`
				}
				out := probeJSON{
					Success:           result.Success,
					Transport:         result.Transport,
					Address:           result.Address,
					RemoteID:          result.RemoteID,
					RemoteDisplayName: result.RemoteDisplayName,
				}
				if result.RTT > 0 {
					out.RTTMs = float64(result.RTT.Milliseconds())
				}
				if result.Error != nil {
					out.Error = result.ErrorDetail
				}

				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Println()
			if result.Success {
				fmt.Printf("[OK] Connection successful!\n")
				fmt.Printf("  Transport:    %s\n", result.Transport)
				fmt.Printf("  Address:      %s\n", result.Address)
				fmt.Printf("  Remote ID:    %s\n", result.RemoteID)
				if result.RemoteDisplayName != "" {
					fmt.Printf("  Display Name: %s\n", result.RemoteDisplayName)
				}
				fmt.Printf("  RTT:          %dms\n", result.RTT.Milliseconds())
			} else {
				fmt.Printf("[FAILED] Connection failed\n")
				fmt.Printf("  Transport:  %s\n", result.Transport)
				fmt.Printf("  Address:    %s\n", result.Address)
				fmt.Printf("  Error:      %s\n", result.ErrorDetail)
				return fmt.Errorf("probe failed")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&transport, "transport", "T", "quic", "Transport type: quic, h2, ws")
	cmd.Flags().StringVar(&path, "path", "/mesh", "HTTP path for h2/ws transports")
	registerTimeoutFlag(cmd, &timeout, "10s", "Connection timeout")
	cmd.Flags().StringVar(&caCert, "ca", "", "CA certificate file for TLS verification")
	cmd.Flags().StringVar(&clientCert, "cert", "", "Client certificate file for mTLS")
	cmd.Flags().StringVar(&clientKey, "key", "", "Client key file for mTLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification")
	cmd.Flags().BoolVar(&plaintext, "plaintext", false, "Plaintext mode (no TLS) for WebSocket behind reverse proxy")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	// Add listen subcommand
	cmd.AddCommand(probeListenCmd())

	return cmd
}

func probeListenCmd() *cobra.Command {
	var (
		transport     string
		address       string
		path          string
		tlsCert       string
		tlsKey        string
		tlsCA         string
		plaintext     bool
		displayName   string
		jsonOutput    bool
		alpnProtocol  string
		httpHeader    string
		wsSubprotocol string
	)

	cmd := &cobra.Command{
		Use:   "listen",
		Short: "Start a test listener for connectivity probing",
		Long: `Start a minimal test listener to validate transport configurations.

This command creates a temporary listener that accepts probe connections
and responds with PEER_HELLO_ACK. Use this to verify that your TLS
certificates and transport configuration work correctly before deploying
a full agent.

The listener runs until interrupted (Ctrl+C).`,
		Example: `  # Start QUIC listener with ephemeral TLS certificate (default)
  muti-metroo probe listen -T quic -a 0.0.0.0:4433

  # Start HTTP/2 listener
  muti-metroo probe listen -T h2 -a 0.0.0.0:443 --path /mesh

  # Start WebSocket listener
  muti-metroo probe listen -T ws -a 0.0.0.0:443 --path /mesh

  # Plaintext WebSocket (behind reverse proxy that handles TLS)
  muti-metroo probe listen -T ws -a 127.0.0.1:8080 --path /mesh --plaintext

  # With static TLS certificates
  muti-metroo probe listen -T quic -a 0.0.0.0:4433 --cert ./certs/agent.crt --key ./certs/agent.key

  # With mTLS (require client certificates)
  muti-metroo probe listen -T quic -a 0.0.0.0:4433 --cert ./certs/agent.crt --key ./certs/agent.key --ca ./certs/ca.crt

  # Output connection events as JSON
  muti-metroo probe listen --json -T quic -a 0.0.0.0:4433

  # Custom display name
  muti-metroo probe listen --name "test-server" -T quic -a 0.0.0.0:4433`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate plaintext option
			if plaintext && transport != "ws" {
				return fmt.Errorf("--plaintext is only supported for WebSocket transport (-T ws)")
			}

			// Validate cert/key are provided together
			if (tlsCert != "") != (tlsKey != "") {
				return fmt.Errorf("both --cert and --key must be provided together")
			}

			// Verify certificate files exist if provided
			for _, file := range []struct{ path, desc string }{
				{tlsCert, "certificate"},
				{tlsKey, "key"},
				{tlsCA, "CA certificate"},
			} {
				if file.path != "" {
					if _, err := os.Stat(file.path); os.IsNotExist(err) {
						return fmt.Errorf("%s file not found: %s", file.desc, file.path)
					}
				}
			}

			// Build listen options
			opts := probe.ListenOptions{
				Transport:     transport,
				Address:       address,
				Path:          path,
				TLSCert:       tlsCert,
				TLSKey:        tlsKey,
				TLSCA:         tlsCA,
				PlainText:     plaintext,
				DisplayName:   displayName,
				ALPNProtocol:  alpnProtocol,
				HTTPHeader:    httpHeader,
				WSSubprotocol: wsSubprotocol,
			}

			// Print header
			if !jsonOutput {
				fmt.Printf("Probe Listener\n")
				fmt.Printf("==============\n")
				fmt.Printf("Transport:  %s\n", transport)
				fmt.Printf("Address:    %s\n", address)
				if transport != "quic" {
					fmt.Printf("Path:       %s\n", path)
				}
				fmt.Printf("Name:       %s\n", displayName)
				if plaintext {
					fmt.Printf("TLS:        disabled (plaintext)\n")
				} else if tlsCert != "" {
					fmt.Printf("TLS:        enabled (static certificates)\n")
					if tlsCA != "" {
						fmt.Printf("mTLS:       enabled (client certs required)\n")
					}
				} else {
					fmt.Printf("TLS:        enabled (ephemeral certificates)\n")
				}
				fmt.Printf("\nListening for connections... (Ctrl+C to stop)\n\n")
			}

			// Setup signal handling
			ctx, cancel := context.WithCancel(context.Background())
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			go func() {
				<-sigChan
				if !jsonOutput {
					fmt.Printf("\nShutting down...\n")
				}
				cancel()
			}()

			// Create event channel
			eventChan := make(chan probe.ConnectionEvent, 100)

			// Track statistics
			var totalConnections, successfulConnections int

			// Start event printer
			go func() {
				for event := range eventChan {
					totalConnections++
					if event.Success {
						successfulConnections++
					}

					if jsonOutput {
						enc := json.NewEncoder(os.Stdout)
						enc.Encode(event)
					} else {
						timestamp := event.Timestamp.Format("2006-01-02 15:04:05")
						if event.Success {
							fmt.Printf("[%s] [OK] %s\n", timestamp, event.RemoteAddr)
							if event.RemoteID != "" {
								fmt.Printf("  Remote ID:   %s\n", event.RemoteID)
							}
							if event.RemoteName != "" {
								fmt.Printf("  Remote Name: %s\n", event.RemoteName)
							}
							fmt.Printf("  RTT:         %.0fms\n", event.RTTMs)
						} else {
							fmt.Printf("[%s] [FAIL] %s\n", timestamp, event.RemoteAddr)
							fmt.Printf("  Error: %s\n", event.Error)
						}
						fmt.Println()
					}
				}
			}()

			// Run listener
			err := probe.Listen(ctx, opts, eventChan)
			close(eventChan)

			// Print summary
			if !jsonOutput && totalConnections > 0 {
				fmt.Printf("Total connections: %d (%d successful, %d failed)\n",
					totalConnections, successfulConnections, totalConnections-successfulConnections)
			}

			// Don't return context.Canceled as an error
			if err == context.Canceled {
				return nil
			}
			return err
		},
	}

	cmd.Flags().StringVarP(&transport, "transport", "T", "quic", "Transport type: quic, h2, ws")
	cmd.Flags().StringVarP(&address, "address", "a", "0.0.0.0:4433", "Listen address")
	cmd.Flags().StringVar(&path, "path", "/mesh", "HTTP path for h2/ws transports")
	cmd.Flags().StringVar(&tlsCert, "cert", "", "TLS certificate file (optional, ephemeral cert used if not provided)")
	cmd.Flags().StringVar(&tlsKey, "key", "", "TLS private key file (optional, ephemeral key used if not provided)")
	cmd.Flags().StringVar(&tlsCA, "ca", "", "CA certificate for client verification (mTLS)")
	cmd.Flags().BoolVar(&plaintext, "plaintext", false, "Plaintext mode (no TLS) for WebSocket behind reverse proxy")
	cmd.Flags().StringVarP(&displayName, "name", "n", "probe-listener", "Display name for this test listener")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output connection events as JSON")
	cmd.Flags().StringVar(&alpnProtocol, "alpn", "", "Custom ALPN protocol (empty for transport default)")
	cmd.Flags().StringVar(&httpHeader, "http-header", "", "Custom HTTP header for h2 transport")
	cmd.Flags().StringVar(&wsSubprotocol, "ws-subprotocol", "", "Custom WebSocket subprotocol")

	return cmd
}

// serviceNameFlag returns " -n <name>" when serviceName differs from the
// default, or an empty string otherwise. Used to build hint commands.
func serviceNameFlag(serviceName string) string {
	if serviceName != "muti-metroo" {
		return " -n " + serviceName
	}
	return ""
}

func serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "System service management",
		Long: `Manage Muti Metroo as a system service.

Supported platforms:
  - Linux: systemd (root) or cron+nohup (user)
  - macOS: launchd (root)
  - Windows: Windows Service (administrator)`,
	}

	cmd.AddCommand(serviceInstallCmd())
	cmd.AddCommand(serviceUninstallCmd())
	cmd.AddCommand(serviceStatusCmd())

	return cmd
}

func serviceInstallCmd() *cobra.Command {
	var configPath string
	var serviceName string
	var userMode bool
	var userMethod string
	var dllPath string
	var deployBinary bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install as a system service",
		Long: `Install Muti Metroo as a system service.

On Linux:
  - Without --user: Uses systemd (requires root)
  - With --user: Uses systemd --user when available, otherwise falls back to
    cron @reboot + nohup (no root required). Override the backend with
    --user-method=auto|systemd|cron.

On macOS: Uses launchd (requires root)

On Windows:
  - Without --user: Uses Windows Service (requires Administrator)
  - With --user: Uses Registry Run key + rundll32 (no admin required)
    Requires --dll flag to specify the DLL path

The --user flag allows non-admin users to install the agent as a
user-level service that starts at logon.

By default, the binary is copied to a standard system location
(e.g., /usr/local/bin on Linux/macOS). Use --deploy=false to skip
this and use the binary in its current location.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check platform support
			if !service.IsSupported() {
				return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
			}

			// Handle user mode
			if userMode {
				switch runtime.GOOS {
				case "linux":
					// Linux user mode: systemd --user (preferred) or cron+nohup (fallback)
					if configPath == "" {
						return fmt.Errorf("config file is required: use -c flag")
					}

					method := strings.ToLower(strings.TrimSpace(userMethod))
					if method == "" {
						method = service.UserMethodAuto
					}
					switch method {
					case service.UserMethodAuto, service.UserMethodSystemd, service.UserMethodCron:
					default:
						return fmt.Errorf("invalid --user-method %q (expected %s, %s, or %s)",
							userMethod, service.UserMethodAuto, service.UserMethodSystemd, service.UserMethodCron)
					}

					absPath, err := filepath.Abs(configPath)
					if err != nil {
						return fmt.Errorf("failed to resolve config path: %w", err)
					}

					if _, err := os.Stat(absPath); os.IsNotExist(err) {
						return fmt.Errorf("config file not found: %s", absPath)
					}

					// Check if user service already installed
					if service.IsUserInstalled(serviceName) {
						return fmt.Errorf("user service is already installed")
					}

					cfg := service.DefaultConfig(absPath)
					cfg.Name = serviceName

					// Install user service
					fmt.Printf("Installing user service...\n")
					fmt.Printf("  Config: %s\n", absPath)
					fmt.Printf("  Requested method: %s\n", method)

					if err := service.InstallUserWithMethod(cfg, method); err != nil {
						return fmt.Errorf("failed to install user service: %w", err)
					}

					nameFlag := serviceNameFlag(serviceName)
					installed := service.GetUserServiceInfo(serviceName)
					fmt.Println("\nUser service installed successfully.")
					fmt.Println("\nManage the service with:")
					fmt.Printf("  muti-metroo service status%s\n", nameFlag)
					fmt.Printf("  muti-metroo service uninstall%s\n", nameFlag)
					fmt.Println("\nView logs:")
					if installed != nil && installed.LogPath != "" {
						// cron+nohup backend writes to a file.
						fmt.Printf("  tail -f %s\n", installed.LogPath)
					} else {
						// systemd-user backend logs to journald.
						fmt.Printf("  journalctl --user -u %s -f\n", serviceName)
					}

				case "windows":
					// Windows user mode: Registry Run key + rundll32
					if dllPath == "" {
						return fmt.Errorf("--dll flag is required for Windows user service installation")
					}
					if configPath == "" {
						return fmt.Errorf("config file is required: use -c flag")
					}

					absConfigPath, err := filepath.Abs(configPath)
					if err != nil {
						return fmt.Errorf("failed to resolve config path: %w", err)
					}

					if _, err := os.Stat(absConfigPath); os.IsNotExist(err) {
						return fmt.Errorf("config file not found: %s", absConfigPath)
					}

					absDLLPath, err := filepath.Abs(dllPath)
					if err != nil {
						return fmt.Errorf("failed to resolve DLL path: %w", err)
					}

					if _, err := os.Stat(absDLLPath); os.IsNotExist(err) {
						return fmt.Errorf("DLL file not found: %s", absDLLPath)
					}

					// Check if user service already installed
					if service.IsUserInstalled(serviceName) {
						return fmt.Errorf("user service is already installed")
					}

					fmt.Printf("Installing user service '%s'...\n", serviceName)
					fmt.Printf("  DLL: %s\n", absDLLPath)
					fmt.Printf("  Config: %s\n", absConfigPath)
					fmt.Printf("  Method: Registry Run key + rundll32\n")

					if err := service.InstallUserWindows(serviceName, absDLLPath, absConfigPath); err != nil {
						return fmt.Errorf("failed to install user service: %w", err)
					}

					nameFlag := serviceNameFlag(serviceName)
					fmt.Println("\nUser service installed and started.")
					fmt.Println("The service will also start automatically at user logon.")
					fmt.Println("\nManage the service with:")
					fmt.Printf("  muti-metroo service status%s\n", nameFlag)
					fmt.Printf("  muti-metroo service uninstall%s\n", nameFlag)

				default:
					return fmt.Errorf("--user flag is only supported on Linux and Windows")
				}

				return nil
			}

			// System service installation
			if configPath == "" {
				return fmt.Errorf("config file is required: use -c flag")
			}

			absPath, err := filepath.Abs(configPath)
			if err != nil {
				return fmt.Errorf("failed to resolve config path: %w", err)
			}

			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				return fmt.Errorf("config file not found: %s", absPath)
			}

			// Create service config
			svcCfg := service.DefaultConfig(absPath)
			svcCfg.Name = serviceName

			// Load config file to extract data_dir for systemd ReadWritePaths
			if runtime.GOOS == "linux" {
				appCfg, _, err := config.LoadOrEmbedded(absPath)
				if err == nil && appCfg.Agent.DataDir != "" {
					dataDir, _ := filepath.Abs(appCfg.Agent.DataDir)
					svcCfg.DataDir = dataDir
				}
			}

			// System service installation requires root
			if !service.IsRoot() {
				switch runtime.GOOS {
				case "linux":
					return fmt.Errorf("root privileges required for system-level systemd. Use --user for a user-level service (systemd-user or cron+nohup, auto-detected)")
				case "darwin":
					return fmt.Errorf("must run as root to install the service (try: sudo muti-metroo service install ...)")
				case "windows":
					return fmt.Errorf("must run as Administrator to install the service. Use --user --dll for Registry Run key installation")
				}
			}

			// Check if already installed
			if service.IsInstalled(serviceName) {
				return fmt.Errorf("service '%s' is already installed", serviceName)
			}

			// Install system service
			fmt.Printf("Installing service '%s'...\n", serviceName)
			fmt.Printf("  Config: %s\n", absPath)
			fmt.Printf("  Platform: %s\n", service.Platform())

			var installErr error
			if deployBinary {
				installPath := service.GetInstallPath(serviceName)
				fmt.Printf("  Binary will be installed to: %s\n", installPath)
				installErr = service.InstallWithDeployment(svcCfg)
			} else {
				installErr = service.Install(svcCfg)
			}

			if installErr != nil {
				return fmt.Errorf("failed to install service: %w", installErr)
			}

			fmt.Println("\nService installed successfully.")

			switch runtime.GOOS {
			case "linux":
				fmt.Println("\nManage the service with:")
				fmt.Printf("  sudo systemctl status %s\n", serviceName)
				fmt.Printf("  sudo systemctl restart %s\n", serviceName)
				fmt.Printf("  sudo journalctl -u %s -f\n", serviceName)
			case "darwin":
				fmt.Println("\nManage the service with:")
				fmt.Printf("  sudo launchctl list com.%s\n", serviceName)
				fmt.Printf("  tail -f %s/%s.log\n", svcCfg.WorkingDir, serviceName)
			case "windows":
				fmt.Println("\nManage the service with:")
				fmt.Printf("  sc query %s\n", serviceName)
				fmt.Printf("  sc stop %s\n", serviceName)
				fmt.Printf("  sc start %s\n", serviceName)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file (required)")
	cmd.Flags().StringVarP(&serviceName, "name", "n", "muti-metroo", "Service name")
	cmd.Flags().BoolVar(&userMode, "user", false, "Install as user service (Linux: systemd-user or cron+nohup, auto-detected; Windows: Registry Run)")
	cmd.Flags().StringVar(&userMethod, "user-method", "auto", "User-service backend on Linux: auto|systemd|cron (default: auto)")
	cmd.Flags().StringVar(&dllPath, "dll", "", "Path to muti-metroo.dll (Windows --user mode only)")
	cmd.Flags().BoolVar(&deployBinary, "deploy", true, "Copy binary to system location (e.g., /usr/local/bin)")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func serviceUninstallCmd() *cobra.Command {
	var serviceName string
	var force bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the system service",
		Long: `Remove the Muti Metroo system service.

On Linux:
  - Removes systemd service (if installed as root)
  - Removes cron+nohup user service (if installed with --user)

On macOS: Unloads and removes the launchd service.

On Windows:
  - Removes Windows service (if installed as Administrator)
  - Removes Registry Run key user service (if installed with --user)

Root/administrator privileges required for system service removal.
User service can be removed without root/admin.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check platform support
			if !service.IsSupported() {
				return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
			}

			// Check for user service first (Linux and Windows)
			if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && service.IsUserInstalled(serviceName) {
				// Confirm unless force flag is set
				if !force {
					fmt.Println("This will stop and remove the user service.")
					fmt.Print("Continue? [y/N]: ")
					var response string
					fmt.Scanln(&response)
					if response != "y" && response != "Y" && response != "yes" {
						fmt.Println("Aborted.")
						return nil
					}
				}

				// Uninstall user service
				if err := service.UninstallUser(serviceName); err != nil {
					return fmt.Errorf("failed to uninstall user service: %w", err)
				}

				fmt.Println("\nUser service uninstalled successfully.")
				return nil
			}

			// System service requires root
			if !service.IsRoot() {
				switch runtime.GOOS {
				case "linux", "darwin":
					return fmt.Errorf("must run as root to uninstall the service (try: sudo muti-metroo service uninstall)")
				case "windows":
					return fmt.Errorf("must run as Administrator to uninstall the service")
				}
			}

			// Check if installed
			if !service.IsInstalled(serviceName) {
				fmt.Printf("Service '%s' is not installed.\n", serviceName)
				return nil
			}

			// Confirm unless force flag is set
			if !force {
				fmt.Printf("This will stop and remove the '%s' service.\n", serviceName)
				fmt.Print("Continue? [y/N]: ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" && response != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			// Uninstall
			if err := service.Uninstall(serviceName); err != nil {
				return fmt.Errorf("failed to uninstall service: %w", err)
			}

			fmt.Println("\nService uninstalled successfully.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&serviceName, "name", "n", "muti-metroo", "Service name")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}

// serviceStatusInfo is the structured payload emitted by
// `muti-metroo service status --json`. Field names are part of the CLI's JSON
// contract for fleet tooling and are intentionally NOT localized. The shape is
// cross-platform-native: it carries the per-OS status string Muti Metroo already
// computes rather than systemd-specific booleans (which are not available on
// launchd / SCM / cron backends).
type serviceStatusInfo struct {
	Installed   bool   `json:"installed"`
	Scope       string `json:"scope"` // "system" | "user" | "none"
	Platform    string `json:"platform"`
	ServiceName string `json:"service_name"`
	Status      string `json:"status,omitempty"` // per-OS status string
	Type        string `json:"type,omitempty"`   // user-service backend, e.g. "cron+nohup"
	ConfigPath  string `json:"config_path,omitempty"`
	LogPath     string `json:"log_path,omitempty"`
	DLLPath     string `json:"dll_path,omitempty"`
}

// newServiceStatusInfo assembles a serviceStatusInfo from the values the status
// command already gathers. info may be nil (no recorded paths). Pure and
// side-effect free so it can be unit-tested without an installed service.
func newServiceStatusInfo(installed bool, scope, name, status, typ string, info *service.UserServiceInfo) serviceStatusInfo {
	out := serviceStatusInfo{
		Installed:   installed,
		Scope:       scope,
		Platform:    service.Platform(),
		ServiceName: name,
		Status:      status,
		Type:        typ,
	}
	if info != nil {
		out.ConfigPath = info.ConfigPath
		out.LogPath = info.LogPath
		out.DLLPath = info.DLLPath
	}
	return out
}

func emitServiceStatusJSON(info serviceStatusInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

func serviceStatusCmd() *cobra.Command {
	var serviceName string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show service status",
		Long:  `Show the current status of the Muti Metroo system service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check platform support
			if !service.IsSupported() {
				return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
			}

			// Check for user service first (Linux and Windows)
			if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && service.IsUserInstalled(serviceName) {
				status, err := service.StatusUser(serviceName)
				if err != nil {
					return fmt.Errorf("failed to get user service status: %w", err)
				}

				info := service.GetUserServiceInfo(serviceName)
				typ := ""
				switch runtime.GOOS {
				case "linux":
					typ = "cron+nohup"
				case "windows":
					typ = "Registry Run key"
				}

				if jsonOutput {
					return emitServiceStatusJSON(newServiceStatusInfo(true, "user", serviceName, status, typ, info))
				}

				fmt.Printf("Service: user service\n")
				fmt.Printf("Status: %s\n", status)

				names := service.DeriveNames(serviceName)
				switch runtime.GOOS {
				case "linux":
					fmt.Printf("Type: %s\n", typ)
					if info != nil {
						if info.ConfigPath != "" {
							fmt.Printf("Config: %s\n", info.ConfigPath)
						}
						if info.LogPath != "" {
							fmt.Printf("Log: %s\n", info.LogPath)
						}
					} else {
						fmt.Printf("Log: ~/%s/%s\n", names.DirName, names.LogFileName)
					}
				case "windows":
					fmt.Printf("Type: %s\n", typ)
					if info != nil {
						if info.DLLPath != "" {
							fmt.Printf("DLL: %s\n", info.DLLPath)
						}
						if info.ConfigPath != "" {
							fmt.Printf("Config: %s\n", info.ConfigPath)
						}
					}
				}

				return nil
			}

			// Check if system service installed
			if !service.IsInstalled(serviceName) {
				if jsonOutput {
					return emitServiceStatusJSON(newServiceStatusInfo(false, "none", serviceName, "", "", nil))
				}
				fmt.Printf("Service '%s' is not installed.\n", serviceName)
				return nil
			}

			// Get status
			status, err := service.Status(serviceName)
			if err != nil {
				return fmt.Errorf("failed to get service status: %w", err)
			}

			if jsonOutput {
				return emitServiceStatusJSON(newServiceStatusInfo(true, "system", serviceName, status, "", nil))
			}

			fmt.Printf("Service: %s\n", serviceName)
			fmt.Printf("Status: %s\n", status)
			fmt.Printf("Platform: %s\n", service.Platform())

			return nil
		},
	}

	cmd.Flags().StringVarP(&serviceName, "name", "n", "muti-metroo", "Service name")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func certCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Certificate management commands",
		Long:  "Generate and manage TLS certificates for the mesh network.",
	}

	cmd.AddCommand(certCACmd())
	cmd.AddCommand(certAgentCmd())
	cmd.AddCommand(certClientCmd())
	cmd.AddCommand(certInfoCmd())

	return cmd
}

func certCACmd() *cobra.Command {
	var (
		commonName string
		outDir     string
		validDays  int
	)

	cmd := &cobra.Command{
		Use:   "ca",
		Short: "Generate a CA certificate",
		Long:  "Generate a new Certificate Authority certificate and private key.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if commonName == "" {
				commonName = "Muti Metroo CA"
			}

			validFor := time.Duration(validDays) * 24 * time.Hour

			fmt.Printf("Generating CA certificate...\n")
			fmt.Printf("  Common Name: %s\n", commonName)
			fmt.Printf("  Valid for: %d days\n", validDays)

			ca, err := certutil.GenerateCA(commonName, validFor)
			if err != nil {
				return fmt.Errorf("failed to generate CA: %w", err)
			}

			certPath := outDir + "/ca.crt"
			keyPath := outDir + "/ca.key"

			if err := ca.SaveToFiles(certPath, keyPath); err != nil {
				return fmt.Errorf("failed to save CA: %w", err)
			}

			fmt.Printf("\nCA certificate generated:\n")
			fmt.Printf("  Certificate: %s\n", certPath)
			fmt.Printf("  Private key: %s\n", keyPath)
			fmt.Printf("  Fingerprint: %s\n", ca.Fingerprint())
			fmt.Printf("  Expires: %s\n", ca.Certificate.NotAfter.Format(time.RFC3339))

			return nil
		},
	}

	cmd.Flags().StringVar(&commonName, "cn", "Muti Metroo CA", "Common name for the CA")
	cmd.Flags().StringVarP(&outDir, "out", "o", "./certs", "Output directory for certificate files")
	cmd.Flags().IntVar(&validDays, "days", 365, "Validity period in days")

	return cmd
}

func certAgentCmd() *cobra.Command {
	var (
		commonName string
		outDir     string
		validDays  int
		caPath     string
		caKeyPath  string
		dnsNames   string
		ipAddrs    string
	)

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Generate an agent/peer certificate",
		Long:  "Generate a new agent certificate signed by a CA. The certificate can be used for both server and client authentication.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if commonName == "" {
				return fmt.Errorf("common name is required")
			}

			// Load CA
			ca, err := certutil.LoadCert(caPath, caKeyPath)
			if err != nil {
				return fmt.Errorf("failed to load CA: %w", err)
			}

			validFor := time.Duration(validDays) * 24 * time.Hour

			fmt.Printf("Generating agent certificate...\n")
			fmt.Printf("  Common Name: %s\n", commonName)
			fmt.Printf("  Valid for: %d days\n", validDays)
			fmt.Printf("  CA: %s\n", ca.Certificate.Subject.CommonName)

			// Build options
			opts := certutil.DefaultPeerOptions(commonName)
			opts.ValidFor = validFor
			opts.ParentCert = ca.Certificate
			opts.ParentKey = ca.PrivateKey

			// Add DNS names
			if dnsNames != "" {
				opts.DNSNames = append(opts.DNSNames, strings.Split(dnsNames, ",")...)
			}

			// Add IP addresses
			if ipAddrs != "" {
				for _, ip := range strings.Split(ipAddrs, ",") {
					parsed := net.ParseIP(strings.TrimSpace(ip))
					if parsed == nil {
						return fmt.Errorf("invalid IP address: %s", ip)
					}
					opts.IPAddresses = append(opts.IPAddresses, parsed)
				}
			}

			cert, err := certutil.GenerateCert(opts)
			if err != nil {
				return fmt.Errorf("failed to generate certificate: %w", err)
			}

			certPath := outDir + "/" + commonName + ".crt"
			keyPath := outDir + "/" + commonName + ".key"

			if err := cert.SaveToFiles(certPath, keyPath); err != nil {
				return fmt.Errorf("failed to save certificate: %w", err)
			}

			fmt.Printf("\nAgent certificate generated:\n")
			fmt.Printf("  Certificate: %s\n", certPath)
			fmt.Printf("  Private key: %s\n", keyPath)
			fmt.Printf("  Fingerprint: %s\n", cert.Fingerprint())
			fmt.Printf("  Expires: %s\n", cert.Certificate.NotAfter.Format(time.RFC3339))
			if len(opts.DNSNames) > 0 {
				fmt.Printf("  DNS Names: %s\n", strings.Join(opts.DNSNames, ", "))
			}
			if len(opts.IPAddresses) > 0 {
				ips := make([]string, len(opts.IPAddresses))
				for i, ip := range opts.IPAddresses {
					ips[i] = ip.String()
				}
				fmt.Printf("  IP Addresses: %s\n", strings.Join(ips, ", "))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&commonName, "cn", "", "Common name for the certificate (required)")
	cmd.Flags().StringVarP(&outDir, "out", "o", "./certs", "Output directory for certificate files")
	cmd.Flags().IntVar(&validDays, "days", 365, "Validity period in days (default 365 to match the CA; pick a shorter value for high-rotation environments)")
	cmd.Flags().StringVar(&caPath, "ca", "./certs/ca.crt", "Path to CA certificate")
	cmd.Flags().StringVar(&caKeyPath, "ca-key", "./certs/ca.key", "Path to CA private key")
	cmd.Flags().StringVar(&dnsNames, "dns", "", "Additional DNS names (comma-separated)")
	cmd.Flags().StringVar(&ipAddrs, "ip", "", "Additional IP addresses (comma-separated)")

	_ = cmd.MarkFlagRequired("cn")

	return cmd
}

func certClientCmd() *cobra.Command {
	var (
		commonName string
		outDir     string
		validDays  int
		caPath     string
		caKeyPath  string
	)

	cmd := &cobra.Command{
		Use:   "client",
		Short: "Generate a client certificate",
		Long:  "Generate a new client certificate signed by a CA. The certificate is for client authentication only.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if commonName == "" {
				return fmt.Errorf("common name is required")
			}

			// Load CA
			ca, err := certutil.LoadCert(caPath, caKeyPath)
			if err != nil {
				return fmt.Errorf("failed to load CA: %w", err)
			}

			validFor := time.Duration(validDays) * 24 * time.Hour

			fmt.Printf("Generating client certificate...\n")
			fmt.Printf("  Common Name: %s\n", commonName)
			fmt.Printf("  Valid for: %d days\n", validDays)
			fmt.Printf("  CA: %s\n", ca.Certificate.Subject.CommonName)

			cert, err := certutil.GenerateClientCert(commonName, validFor, ca)
			if err != nil {
				return fmt.Errorf("failed to generate certificate: %w", err)
			}

			certPath := outDir + "/" + commonName + ".crt"
			keyPath := outDir + "/" + commonName + ".key"

			if err := cert.SaveToFiles(certPath, keyPath); err != nil {
				return fmt.Errorf("failed to save certificate: %w", err)
			}

			fmt.Printf("\nClient certificate generated:\n")
			fmt.Printf("  Certificate: %s\n", certPath)
			fmt.Printf("  Private key: %s\n", keyPath)
			fmt.Printf("  Fingerprint: %s\n", cert.Fingerprint())
			fmt.Printf("  Expires: %s\n", cert.Certificate.NotAfter.Format(time.RFC3339))

			return nil
		},
	}

	cmd.Flags().StringVar(&commonName, "cn", "", "Common name for the certificate (required)")
	cmd.Flags().StringVarP(&outDir, "out", "o", "./certs", "Output directory for certificate files")
	cmd.Flags().IntVar(&validDays, "days", 365, "Validity period in days (default 365 to match the CA; pick a shorter value for high-rotation environments)")
	cmd.Flags().StringVar(&caPath, "ca", "./certs/ca.crt", "Path to CA certificate")
	cmd.Flags().StringVar(&caKeyPath, "ca-key", "./certs/ca.key", "Path to CA private key")

	_ = cmd.MarkFlagRequired("cn")

	return cmd
}

func certInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <certificate>",
		Short: "Display certificate information",
		Long:  "Display detailed information about a certificate file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			certPath := args[0]

			info, err := certutil.GetCertInfoFromFile(certPath)
			if err != nil {
				return fmt.Errorf("failed to read certificate: %w", err)
			}

			fmt.Printf("Certificate: %s\n\n", certPath)
			fmt.Printf("Subject:      %s\n", info.Subject)
			fmt.Printf("Issuer:       %s\n", info.Issuer)
			fmt.Printf("Serial:       %s\n", info.SerialNumber)
			fmt.Printf("Fingerprint:  %s\n", info.Fingerprint)
			fmt.Printf("Is CA:        %v\n", info.IsCA)
			fmt.Printf("Not Before:   %s\n", info.NotBefore.Format(time.RFC3339))
			fmt.Printf("Not After:    %s\n", info.NotAfter.Format(time.RFC3339))

			band, daysLeft := certutil.ClassifyExpiry(info.NotAfter, time.Now())
			switch band {
			case certutil.CertExpired:
				fmt.Printf("Status:       EXPIRED\n")
			case certutil.CertExpiringSoon:
				fmt.Printf("Status:       EXPIRING SOON (%d days left)\n", daysLeft)
			case certutil.CertExpiringPlan:
				fmt.Printf("Status:       expiring within 60 days (%d days left); plan rotation\n", daysLeft)
			default:
				fmt.Printf("Status:       Valid (%d days left)\n", daysLeft)
			}

			if len(info.DNSNames) > 0 {
				fmt.Printf("DNS Names:    %s\n", strings.Join(info.DNSNames, ", "))
			}
			if len(info.IPAddresses) > 0 {
				fmt.Printf("IP Addresses: %s\n", strings.Join(info.IPAddresses, ", "))
			}
			if len(info.KeyUsage) > 0 {
				fmt.Printf("Key Usage:    %s\n", strings.Join(info.KeyUsage, ", "))
			}
			if len(info.ExtKeyUsage) > 0 {
				fmt.Printf("Ext Key Usage: %s\n", strings.Join(info.ExtKeyUsage, ", "))
			}

			return nil
		},
	}

	return cmd
}

func shellCmd() *cobra.Command {
	var (
		agentAddr  string
		timeoutStr string
		ttyMode    bool
	)

	cmd := &cobra.Command{
		Use:   "shell [flags] <target-agent-id> [command] [args...]",
		Short: "Run management commands on a remote agent",
		Long: `Run management commands on a remote agent.

The <target-agent-id> is the final destination agent where the command executes.
The --agent flag specifies which gateway agent to connect through (defaults to localhost).

By default, commands run in streaming mode without a PTY, suitable for simple
commands like 'whoami', 'ls', or long-running output like 'tail -f'.
Use --tty for interactive mode when you need a full terminal (vim, htop, bash).

Streaming mode (default):
  - No PTY allocation
  - Separate stdout/stderr streams
  - Suitable for simple commands and continuous output

Interactive mode (--tty):
  - Allocates a PTY on the remote agent
  - Supports terminal resize (SIGWINCH)
  - Required for interactive programs (vim, less, htop, etc.)

Examples:
  # Simple command (streaming mode)
  muti-metroo shell abc123def456 whoami

  # Long-running output (streaming mode)
  muti-metroo shell abc123def456 tail -f /var/log/syslog

  # Follow logs (streaming mode)
  muti-metroo shell abc123def456 journalctl -u muti-metroo -f

  # Interactive bash shell (requires --tty)
  muti-metroo shell --tty abc123def456 bash

  # Interactive vim (requires --tty)
  muti-metroo shell --tty abc123def456 vim /etc/config.yaml

  # Via a different agent
  muti-metroo shell -a 192.168.1.10:8080 abc123def456 top`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			targetID := args[0]
			var command string
			var cmdArgs []string

			if len(args) > 1 {
				command = args[1]
				cmdArgs = args[2:]
			} else {
				// Default to shell if no command specified
				command = "bash"
			}

			// Parse timeout (supports duration strings like "5m" or plain seconds)
			timeoutSec, err := parseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid timeout: %w", err)
			}

			// Resolve short agent ID prefix to full ID
			resolvedID, err := resolveAgentID(targetID, ep)
			if err != nil {
				return err
			}

			// Validate target agent ID
			if _, err := identity.ParseAgentID(resolvedID); err != nil {
				return fmt.Errorf("invalid agent ID '%s': %w", resolvedID, err)
			}

			// Create shell client
			client := shell.NewClient(shell.ClientConfig{
				Endpoint:    ep,
				TargetID:    resolvedID,
				Interactive: ttyMode,
				Token:       apiToken,
				Command:     command,
				Args:        cmdArgs,
				Timeout:     timeoutSec,
			})

			// Run the shell session
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle interrupt signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			exitCode, err := client.Run(ctx)
			if err != nil {
				return err
			}

			if exitCode != 0 {
				os.Exit(exitCode)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, gatewayAddrHelp())
	registerTimeoutFlag(cmd, &timeoutStr, "0", "Session timeout (e.g., 30s, 5m, or 0 for no timeout)")
	cmd.Flags().BoolVar(&ttyMode, "tty", false, "Interactive mode with PTY (for vim, bash, htop, etc.)")

	return cmd
}

func uploadCmd() *cobra.Command {
	var (
		agentAddr  string
		timeoutStr string
		rateLimit  string
		resume     bool
		quiet      bool
	)

	cmd := &cobra.Command{
		Use:   "upload [flags] <target-agent-id> <local-path> <remote-path>",
		Short: "Upload a file or directory to a remote agent",
		Long: `Upload a local file or directory to a remote agent via the file transfer interface.

The <target-agent-id> is the final destination agent where the file is stored.
The --agent flag specifies which gateway agent to connect through (defaults to localhost).

File permissions (mode) are preserved. The remote path must be absolute.
Directories are automatically detected and uploaded as tar archives.

Examples:
  # Upload a file to a remote agent
  muti-metroo upload abc123def456 ./local/file.txt /tmp/remote-file.txt

  # Upload a large file (streaming, supports any size)
  muti-metroo upload abc123def456 ./large-iso.iso /tmp/large-iso.iso

  # Upload a directory (auto-detected)
  muti-metroo upload abc123def456 ./my-folder /tmp/my-folder

  # Via a different agent
  muti-metroo upload -a 192.168.1.10:8080 abc123def456 config.yaml /etc/app/config.yaml

  # Rate-limited upload (100 KB/s)
  muti-metroo upload --rate-limit 100KB abc123def456 ./large.iso /tmp/large.iso

  # Resume an interrupted upload
  muti-metroo upload --resume abc123def456 ./huge.iso /tmp/huge.iso`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			targetID := args[0]
			localPath := args[1]
			remotePath := args[2]

			// Parse timeout (supports duration strings like "5m" or plain seconds)
			timeoutSec, err := parseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid timeout: %w", err)
			}

			// Resolve short agent ID prefix to full ID
			resolvedID, err := resolveAgentID(targetID, ep)
			if err != nil {
				return err
			}

			// Validate target agent ID
			if _, err := identity.ParseAgentID(resolvedID); err != nil {
				return fmt.Errorf("invalid agent ID '%s': %w", resolvedID, err)
			}

			// Validate remote path is absolute (supports both Unix and Windows paths)
			if !isRemotePathAbsolute(remotePath) {
				return fmt.Errorf("remote path must be absolute: %s", remotePath)
			}

			// Resolve local path
			absLocalPath, err := filepath.Abs(localPath)
			if err != nil {
				return fmt.Errorf("failed to resolve local path: %w", err)
			}

			// Check if local file/directory exists
			info, err := os.Stat(absLocalPath)
			if err != nil {
				return fmt.Errorf("cannot access local path: %w", err)
			}

			// Parse rate limit
			var rateLimitBytes int64
			if rateLimit != "" {
				rateLimitBytes, err = filetransfer.ParseSize(rateLimit)
				if err != nil {
					return fmt.Errorf("invalid rate limit: %w", err)
				}
			}

			// Resume not supported for directories
			if resume && info.IsDir() {
				fmt.Println("Warning: Resume not supported for directory uploads, starting fresh")
				resume = false
			}

			isDirectory := info.IsDir()
			return uploadFile(ep, resolvedID, absLocalPath, remotePath, timeoutSec, isDirectory, rateLimitBytes, resume, quiet)
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, gatewayAddrHelp())
	registerTimeoutFlag(cmd, &timeoutStr, "5m", "Transfer timeout (e.g., 30s, 5m, 1h)")
	cmd.Flags().StringVar(&rateLimit, "rate-limit", "", "Maximum transfer speed (e.g., 100KB, 1MB, 10MiB)")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume interrupted transfer if possible")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress progress output")

	return cmd
}

// uploadFile uploads a file or directory via multipart form streaming.
func uploadFile(ep apiclient.Endpoint, targetID, localPath, remotePath string, timeout int, isDirectory bool, rateLimit int64, resume bool, quiet bool) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot access local path: %w", err)
	}

	// Create a pipe for the multipart form
	pr, pw := io.Pipe()

	// Create multipart writer
	writer := multipart.NewWriter(pw)

	// Progress tracking
	var totalSize int64
	if !isDirectory {
		totalSize = info.Size()
	}
	startTime := time.Now()
	var bytesWritten int64

	// Start goroutine to write form data
	errCh := make(chan error, 1)
	go func() {
		defer pw.Close()
		defer writer.Close()

		// Add form fields
		writer.WriteField("path", remotePath)
		if isDirectory {
			writer.WriteField("directory", "true")
		}
		if rateLimit > 0 {
			writer.WriteField("rate_limit", fmt.Sprintf("%d", rateLimit))
		}
		if resume {
			writer.WriteField("resume", "true")
			// Also include original file size for validation
			writer.WriteField("original_size", fmt.Sprintf("%d", info.Size()))
		}

		// Create file part
		part, err := writer.CreateFormFile("file", filepath.Base(localPath))
		if err != nil {
			errCh <- fmt.Errorf("failed to create form file: %w", err)
			return
		}

		if isDirectory {
			// Tar and stream directory (no progress for directories)
			if !quiet {
				fmt.Printf("Uploading directory %s to %s:%s\n", localPath, targetID[:12], remotePath)
			}
			// CLI side: rely on the multipart pipe to surface cancellation --
			// a Ctrl-C breaks the HTTP request, the pipe writer fails, the
			// next io.Copy round aborts.
			if err := filetransfer.TarDirectory(context.Background(), localPath, part); err != nil {
				errCh <- fmt.Errorf("failed to tar directory: %w", err)
				return
			}
		} else {
			// Stream file with progress tracking
			if !quiet {
				fmt.Printf("Uploading %s (%s) to %s:%s\n",
					filepath.Base(localPath), humanize.Bytes(uint64(info.Size())), targetID[:12], remotePath)
			}
			f, err := os.Open(localPath)
			if err != nil {
				errCh <- fmt.Errorf("failed to open file: %w", err)
				return
			}
			defer f.Close()

			// Create progress-tracking reader
			progressReader := &progressTrackingReader{
				reader:    f,
				total:     totalSize,
				written:   &bytesWritten,
				startTime: startTime,
				quiet:     quiet,
			}

			if _, err := io.Copy(part, progressReader); err != nil {
				errCh <- fmt.Errorf("failed to stream file: %w", err)
				return
			}
		}
		errCh <- nil
	}()

	// Build URL
	url := ep.HTTPURL(fmt.Sprintf("/agents/%s/file/upload", targetID))

	// Create HTTP request
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	setAuthToken(req)

	if quiet {
		// No progress output in quiet mode
	} else if isDirectory {
		fmt.Print("Uploading... ")
	}

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)

	// Clear progress line if we were showing progress
	if !quiet && !isDirectory && totalSize > 0 {
		fmt.Print("\r" + strings.Repeat(" ", 70) + "\r") // Clear line
	}

	if err != nil {
		if !quiet {
			fmt.Println("FAILED")
		}
		// Check if there was an error in the goroutine
		if writeErr := <-errCh; writeErr != nil {
			return fmt.Errorf("upload error: %w (form write: %v)", err, writeErr)
		}
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Wait for goroutine
	if writeErr := <-errCh; writeErr != nil {
		if !quiet {
			fmt.Println("FAILED")
		}
		return writeErr
	}

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if !quiet {
			fmt.Println("FAILED")
		}
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var uploadResp struct {
		Success      bool   `json:"success"`
		Error        string `json:"error,omitempty"`
		BytesWritten int64  `json:"bytes_written"`
		RemotePath   string `json:"remote_path"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		if !quiet {
			fmt.Println("FAILED")
		}
		return fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if !uploadResp.Success {
		if !quiet {
			fmt.Println("FAILED")
		}
		return fmt.Errorf("upload failed: %s", uploadResp.Error)
	}

	if !quiet {
		elapsed := time.Since(startTime)
		speed := float64(uploadResp.BytesWritten) / elapsed.Seconds()
		fmt.Printf("Uploaded %s to %s in %s (%s/s)\n",
			humanize.Bytes(uint64(uploadResp.BytesWritten)),
			remotePath,
			elapsed.Round(time.Millisecond),
			humanize.Bytes(uint64(speed)))
	}

	return nil
}

func downloadCmd() *cobra.Command {
	var (
		agentAddr  string
		timeoutStr string
		rateLimit  string
		resume     bool
		quiet      bool
	)

	cmd := &cobra.Command{
		Use:   "download [flags] <target-agent-id> <remote-path> <local-path>",
		Short: "Download a file or directory from a remote agent",
		Long: `Download a file or directory from a remote agent via the file transfer interface.

The <target-agent-id> is the final destination agent where the file is stored.
The --agent flag specifies which gateway agent to connect through (defaults to localhost).

File permissions (mode) are preserved. The remote path must be absolute.
Directories are automatically detected and downloaded as tar archives.

Examples:
  # Download a file from a remote agent
  muti-metroo download abc123def456 /tmp/remote-file.txt ./local/file.txt

  # Download a large file (streaming, supports any size)
  muti-metroo download abc123def456 /var/backup/large.iso ./large.iso

  # Download a directory (auto-detected)
  muti-metroo download abc123def456 /etc/myapp ./myapp-config

  # Via a different agent
  muti-metroo download -a 192.168.1.10:8080 abc123def456 /etc/app/config.yaml config.yaml

  # Rate-limited download (1 MB/s)
  muti-metroo download --rate-limit 1MB abc123def456 /data/backup.tar.gz ./backup.tar.gz

  # Resume an interrupted download
  muti-metroo download --resume abc123def456 /data/large.iso ./large.iso`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			targetID := args[0]
			remotePath := args[1]
			localPath := args[2]

			// Parse timeout (supports duration strings like "5m" or plain seconds)
			timeoutSec, err := parseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid timeout: %w", err)
			}

			// Resolve short agent ID prefix to full ID
			resolvedID, err := resolveAgentID(targetID, ep)
			if err != nil {
				return err
			}

			// Validate target agent ID
			if _, err := identity.ParseAgentID(resolvedID); err != nil {
				return fmt.Errorf("invalid agent ID '%s': %w", resolvedID, err)
			}

			// Validate remote path is absolute (supports both Unix and Windows paths)
			if !isRemotePathAbsolute(remotePath) {
				return fmt.Errorf("remote path must be absolute: %s", remotePath)
			}

			// Resolve local path
			absLocalPath, err := filepath.Abs(localPath)
			if err != nil {
				return fmt.Errorf("failed to resolve local path: %w", err)
			}

			// Parse rate limit
			var rateLimitBytes int64
			if rateLimit != "" {
				rateLimitBytes, err = filetransfer.ParseSize(rateLimit)
				if err != nil {
					return fmt.Errorf("invalid rate limit: %w", err)
				}
			}

			return downloadFile(ep, resolvedID, remotePath, absLocalPath, timeoutSec, rateLimitBytes, resume, quiet)
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, gatewayAddrHelp())
	registerTimeoutFlag(cmd, &timeoutStr, "5m", "Transfer timeout (e.g., 30s, 5m, 1h)")
	cmd.Flags().StringVar(&rateLimit, "rate-limit", "", "Maximum transfer speed (e.g., 100KB, 1MB, 10MiB)")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume interrupted transfer if possible")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress progress output")

	return cmd
}

// downloadFile downloads a file or directory via streaming.
func downloadFile(ep apiclient.Endpoint, targetID, remotePath, localPath string, timeout int, rateLimit int64, resume bool, quiet bool) error {
	if !quiet {
		fmt.Printf("Downloading %s:%s to %s\n", targetID[:12], remotePath, localPath)
	}

	// Check for existing partial file if resume is requested
	var offset int64
	var originalSize int64
	if resume {
		partialInfo, err := filetransfer.HasPartialFile(localPath)
		if err != nil {
			if !quiet {
				fmt.Printf("Warning: failed to check partial file: %v\n", err)
			}
		} else if partialInfo != nil {
			offset = partialInfo.BytesWritten
			originalSize = partialInfo.OriginalSize
			if !quiet {
				fmt.Printf("Resuming from offset %s (of %s)\n",
					humanize.Bytes(uint64(offset)), humanize.Bytes(uint64(originalSize)))
			}
		}
	}

	// Build request
	reqBody := map[string]interface{}{
		"path": remotePath,
	}
	if rateLimit > 0 {
		reqBody["rate_limit"] = rateLimit
	}
	if offset > 0 {
		reqBody["offset"] = offset
		reqBody["original_size"] = originalSize
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	// Build URL
	url := ep.HTTPURL(fmt.Sprintf("/agents/%s/file/download", targetID))

	// Create HTTP request
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthToken(req)

	if !quiet {
		if offset > 0 {
			fmt.Print("Resuming... ")
		} else {
			fmt.Print("Downloading... ")
		}
	}

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if !quiet {
			fmt.Println("FAILED")
		}
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for error response (JSON)
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			if !quiet {
				fmt.Println("FAILED")
			}
			return fmt.Errorf("failed to read response: %w", err)
		}
		var errResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(respBody, &errResp); err == nil && !errResp.Success {
			if !quiet {
				fmt.Println("FAILED")
			}
			return fmt.Errorf("download failed: %s", errResp.Error)
		}
		if !quiet {
			fmt.Println("FAILED")
		}
		return fmt.Errorf("unexpected JSON response: %s", string(respBody))
	}

	// Check if it's a tar.gz (directory download)
	isTarGz := strings.HasPrefix(contentType, "application/gzip") ||
		strings.HasSuffix(resp.Header.Get("Content-Disposition"), ".tar.gz\"")

	if isTarGz {
		// Directories don't support resume
		if offset > 0 {
			if !quiet {
				fmt.Println("FAILED")
			}
			return fmt.Errorf("resume not supported for directory downloads")
		}

		// Extract tar.gz to directory
		if err := os.MkdirAll(localPath, 0755); err != nil {
			if !quiet {
				fmt.Println("FAILED")
			}
			return fmt.Errorf("failed to create directory: %w", err)
		}

		startTime := time.Now()
		// CLI side: rely on resp.Body close to surface cancellation.
		if err := filetransfer.UntarDirectory(context.Background(), resp.Body, localPath); err != nil {
			if !quiet {
				fmt.Println("FAILED")
			}
			return fmt.Errorf("failed to extract directory: %w", err)
		}

		elapsed := time.Since(startTime)
		if !quiet {
			fmt.Println("OK")
			fmt.Printf("Extracted directory to %s in %.1fs\n", localPath, elapsed.Seconds())
		}
	} else {
		// Write file directly
		dir := filepath.Dir(localPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			if !quiet {
				fmt.Println("FAILED")
			}
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Get file mode from header
		var mode os.FileMode = 0644 // default
		if modeStr := resp.Header.Get("X-File-Mode"); modeStr != "" {
			var modeVal uint32
			if _, err := fmt.Sscanf(modeStr, "%o", &modeVal); err == nil {
				mode = os.FileMode(modeVal)
			}
		}

		// Get original file size from header (for progress tracking)
		var totalSize int64
		if sizeStr := resp.Header.Get("X-Original-Size"); sizeStr != "" {
			fmt.Sscanf(sizeStr, "%d", &totalSize)
		}

		var f *os.File
		var written int64

		if offset > 0 {
			// Resume: open partial file for appending
			f, err = filetransfer.OpenPartialFileForAppend(localPath)
			if err != nil {
				if !quiet {
					fmt.Println("FAILED")
				}
				return fmt.Errorf("failed to open partial file: %w", err)
			}
			written = offset // Start counting from offset
		} else {
			// New download: create partial file
			if totalSize > 0 {
				f, err = filetransfer.CreatePartialFile(localPath, totalSize, remotePath, mode)
			} else {
				// No size info, create directly
				f, err = os.Create(filetransfer.GetPartialPath(localPath))
			}
			if err != nil {
				if !quiet {
					fmt.Println("FAILED")
				}
				return fmt.Errorf("failed to create file: %w", err)
			}
			if totalSize > 0 {
				// Partial info already created by CreatePartialFile
			}
		}

		// Copy data to file with progress tracking
		startTime := time.Now()
		if !quiet {
			fmt.Println() // Move to new line for progress bar
		}

		pw := &progressTrackingWriter{
			writer:    f,
			total:     totalSize,
			written:   &written,
			startTime: startTime,
			quiet:     quiet,
		}

		newBytes, err := io.Copy(pw, resp.Body)
		f.Close()

		if err != nil {
			// Update partial info with progress so far
			if totalSize > 0 {
				filetransfer.UpdatePartialProgress(localPath, written)
			}
			if !quiet {
				fmt.Print("\r") // Clear progress bar
			}
			fmt.Println("FAILED")
			return fmt.Errorf("failed to write file: %w", err)
		}

		// Finalize: rename partial to final
		if err := filetransfer.FinalizePartial(localPath, mode); err != nil {
			if !quiet {
				fmt.Print("\r") // Clear progress bar
			}
			fmt.Println("FAILED")
			return fmt.Errorf("failed to finalize file: %w", err)
		}

		// Clear progress bar and print final summary
		elapsed := time.Since(startTime)
		if !quiet && totalSize > 0 {
			fmt.Print("\r\033[K") // Clear line
		}

		speed := float64(newBytes) / elapsed.Seconds()
		if offset > 0 {
			fmt.Printf("Downloaded %s (resumed +%s) to %s in %.1fs (%s/s)\n",
				humanize.Bytes(uint64(written)), humanize.Bytes(uint64(newBytes)),
				localPath, elapsed.Seconds(), humanize.Bytes(uint64(speed)))
		} else {
			fmt.Printf("Downloaded %s to %s in %.1fs (%s/s)\n",
				humanize.Bytes(uint64(written)), localPath,
				elapsed.Seconds(), humanize.Bytes(uint64(speed)))
		}
	}

	return nil
}

func pingCmd() *cobra.Command {
	var (
		agentAddr   string
		count       int
		intervalStr string
		timeoutStr  string
	)

	cmd := &cobra.Command{
		Use:   "ping [flags] <target-agent-id> <destination>",
		Short: "Send ICMP echo requests through a remote agent",
		Long: `Send ICMP echo (ping) requests through a remote agent.

The <target-agent-id> is the exit agent that sends the actual ICMP packets.
The --agent flag specifies which gateway agent to connect through.

The destination must be an IP address (domain names are not supported for ICMP).

Note: The exit agent must have ICMP enabled in its configuration:
  icmp:
    enabled: true
    max_sessions: 100
    idle_timeout: 60s
    echo_timeout: 5s

Examples:
  # Ping through a remote agent
  muti-metroo ping abc123def456 8.8.8.8

  # Via a different gateway
  muti-metroo ping -a 192.168.1.10:8080 abc123def456 1.1.1.1

  # Custom count and interval
  muti-metroo ping -c 10 -i 500ms abc123def456 8.8.8.8

  # Continuous ping
  muti-metroo ping -c 0 abc123def456 8.8.8.8`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			targetID := args[0]
			destination := args[1]

			// Validate destination is an IP address
			destIP := net.ParseIP(destination)
			if destIP == nil {
				return fmt.Errorf("destination must be a valid IP address: %s", destination)
			}

			// Parse interval
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid interval: %w", err)
			}

			// Parse timeout
			timeout, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid timeout: %w", err)
			}

			// Resolve short agent ID prefix to full ID
			resolvedID, err := resolveAgentID(targetID, ep)
			if err != nil {
				return err
			}

			// Validate target agent ID
			if _, err := identity.ParseAgentID(resolvedID); err != nil {
				return fmt.Errorf("invalid agent ID '%s': %w", resolvedID, err)
			}

			// Run ping
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle interrupt signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return runPing(ctx, ep, resolvedID, destIP, count, interval, timeout)
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, gatewayAddrHelp())
	cmd.Flags().IntVarP(&count, "count", "c", 4, "Number of echo requests to send (0 = infinite)")
	cmd.Flags().StringVarP(&intervalStr, "interval", "i", "1s", "Interval between requests")
	registerTimeoutFlag(cmd, &timeoutStr, "5s", "Per-echo timeout")

	return cmd
}

func runPing(ctx context.Context, ep apiclient.Endpoint, targetID string, destIP net.IP, count int, interval, timeout time.Duration) error {
	// Build WebSocket URL
	wsURL := ep.WSURL(fmt.Sprintf("/agents/%s/icmp", targetID))

	// Connect to WebSocket
	dialOpts := &websocket.DialOptions{}
	if apiToken != "" {
		dialOpts.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + apiToken}}
	}
	conn, _, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to ICMP endpoint: %w", err)
	}
	defer conn.CloseNow()

	fmt.Printf("PING %s via %s\n", destIP, targetID[:12])

	// Statistics
	var sent, received int
	var rttMin, rttMax, rttSum time.Duration

	// Helper to write JSON
	writeJSON := func(v interface{}) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)
	}

	// Helper to read JSON
	readJSON := func(readCtx context.Context, v interface{}) error {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, v)
	}

	// Send initial request with destination
	initMsg := map[string]interface{}{
		"type":    "init",
		"dest_ip": destIP.String(),
	}
	if err := writeJSON(initMsg); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}

	// Read init response
	var initResp struct {
		Type    string `json:"type"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := readJSON(ctx, &initResp); err != nil {
		return fmt.Errorf("failed to read init response: %w", err)
	}
	if !initResp.Success {
		return fmt.Errorf("ICMP session failed: %s", initResp.Error)
	}

	// Ping loop
	seq := 1
	for count == 0 || sent < count {
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		// Send echo request
		startTime := time.Now()
		echoMsg := map[string]interface{}{
			"type":     "echo",
			"sequence": seq,
			"payload":  fmt.Sprintf("muti-metroo ping seq=%d", seq),
		}
		if err := writeJSON(echoMsg); err != nil {
			fmt.Printf("seq=%d: send failed: %v\n", seq, err)
			sent++
			seq++
			time.Sleep(interval)
			continue
		}
		sent++

		// Read with timeout via context
		readCtx, readCancel := context.WithDeadline(ctx, time.Now().Add(timeout))

		// Wait for reply
		var reply struct {
			Type     string  `json:"type"`
			Sequence int     `json:"sequence"`
			RTTMS    float64 `json:"rtt_ms,omitempty"`
			Error    string  `json:"error,omitempty"`
			SrcIP    string  `json:"src_ip,omitempty"`
		}
		if err := readJSON(readCtx, &reply); err != nil {
			readCancel()
			if ctx.Err() != nil {
				goto done
			}
			fmt.Printf("seq=%d: timeout\n", seq)
			seq++
			time.Sleep(interval)
			continue
		}
		readCancel()

		if reply.Type == "error" {
			fmt.Printf("seq=%d: error: %s\n", seq, reply.Error)
		} else if reply.Type == "reply" {
			rtt := time.Since(startTime)
			received++
			rttSum += rtt
			if rttMin == 0 || rtt < rttMin {
				rttMin = rtt
			}
			if rtt > rttMax {
				rttMax = rtt
			}
			replyFrom := destIP.String()
			if reply.SrcIP != "" {
				replyFrom = reply.SrcIP
			}
			fmt.Printf("Reply from %s: seq=%d time=%.1fms\n", replyFrom, reply.Sequence, float64(rtt.Microseconds())/1000)
		}

		seq++

		// Wait for interval before next ping
		if count == 0 || sent < count {
			select {
			case <-ctx.Done():
				goto done
			case <-time.After(interval):
			}
		}
	}

done:
	// Print statistics
	fmt.Printf("\n--- %s ping statistics ---\n", destIP)
	lossPercent := 0.0
	if sent > 0 {
		lossPercent = float64(sent-received) / float64(sent) * 100
	}
	fmt.Printf("%d packets transmitted, %d received, %.0f%% packet loss\n", sent, received, lossPercent)
	if received > 0 {
		avgRTT := rttSum / time.Duration(received)
		fmt.Printf("rtt min/avg/max = %.1f/%.1f/%.1f ms\n",
			float64(rttMin.Microseconds())/1000,
			float64(avgRTT.Microseconds())/1000,
			float64(rttMax.Microseconds())/1000)
	}

	return nil
}

func hashCmd() *cobra.Command {
	var cost int

	cmd := &cobra.Command{
		Use:   "hash [password]",
		Short: "Generate a bcrypt hash for use in configuration",
		Long: `Generate a bcrypt password hash for use in configuration files.

The generated hash can be used in:
  - http.token_hash                     (HTTP API bearer token authentication)
  - socks5.auth.users[].password_hash  (SOCKS5 proxy authentication)

Management commands, file transfer, sleep/wake, and dynamic config mutations are
authorized by the mesh management Ed25519 signing key, not bcrypt passwords.
See the management.* config block and 'muti-metroo signing-key generate'.

If no password is provided as an argument, you will be prompted to enter
it interactively (recommended for security).

Examples:
  # Interactive prompt (recommended - password hidden)
  muti-metroo hash

  # From argument (less secure - visible in shell history)
  muti-metroo hash "mysecretpassword"

  # With custom cost (default: 10, range: 4-31)
  muti-metroo hash --cost 12

  # Use in config file:
  # http:
  #   token_hash: "<paste hash here>"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var password string

			if len(args) > 0 {
				password = args[0]
				fmt.Fprintln(os.Stderr, "WARNING: password supplied on command line; this is recorded in shell history. Re-run without an argument for a hidden prompt.")
			} else {
				// Interactive prompt
				fmt.Print("Enter password: ")
				pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println() // newline after hidden input
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}

				fmt.Print("Confirm password: ")
				confirmBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}

				if string(pwBytes) != string(confirmBytes) {
					return fmt.Errorf("passwords do not match")
				}

				password = string(pwBytes)
			}

			if password == "" {
				return fmt.Errorf("password cannot be empty")
			}

			// Validate cost
			if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
				return fmt.Errorf("cost must be between %d and %d", bcrypt.MinCost, bcrypt.MaxCost)
			}

			// Generate hash
			hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
			if err != nil {
				return fmt.Errorf("failed to generate hash: %w", err)
			}

			fmt.Println(string(hash))
			return nil
		},
	}

	cmd.Flags().IntVar(&cost, "cost", config.DefaultBcryptCost, fmt.Sprintf("bcrypt cost factor (4-31, default %d per OWASP 2024)", config.DefaultBcryptCost))

	return cmd
}

func managementKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "management-key",
		Short: "Manage mesh topology encryption keys",
		Long: `Manage X25519 keypairs for encrypting mesh topology data.

When management key encryption is enabled, sensitive data like NodeInfo
(hostnames, IPs, OS info) and route paths are encrypted before flooding
through the mesh. Only operators with the private key can decrypt and
view topology details.

This provides cryptographic compartmentalization: if a field agent is
compromised, the attacker only sees encrypted blobs, not the mesh topology.`,
	}

	// Add subcommands
	cmd.AddCommand(managementKeyGenerateCmd())
	cmd.AddCommand(managementKeyPublicCmd())

	return cmd
}

// writeKeyPair writes a hex-encoded public/private key pair to outDir using
// safe file permissions (public 0644, private 0600). Refuses to overwrite
// existing files unless force is true. The basename argument controls the
// filenames: <basename>-public.key and <basename>-private.key.
func writeKeyPair(outDir, basename, pubHex, privHex string, force bool) (pubPath, privPath string, err error) {
	if err = os.MkdirAll(outDir, 0700); err != nil {
		return "", "", fmt.Errorf("failed to create output directory: %w", err)
	}
	pubPath = filepath.Join(outDir, basename+"-public.key")
	privPath = filepath.Join(outDir, basename+"-private.key")
	for _, p := range []string{pubPath, privPath} {
		if _, statErr := os.Stat(p); statErr == nil && !force {
			return "", "", fmt.Errorf("%s already exists; use --force to overwrite", p)
		}
	}
	if err = os.WriteFile(pubPath, []byte(pubHex+"\n"), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write public key: %w", err)
	}
	if err = os.WriteFile(privPath, []byte(privHex+"\n"), 0600); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %w", err)
	}
	return pubPath, privPath, nil
}

func managementKeyGenerateCmd() *cobra.Command {
	var (
		outDir string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new management keypair",
		Long: `Generate a new X25519 keypair for management key encryption.

The generated keys should be distributed as follows:
  - Public key: Add to ALL agent configs (field agents and operators)
  - Private key: Add ONLY to operator/management node configs

By default the private key is printed to stdout (and so survives in your
terminal scrollback). Pass --out <dir> to write the keys to disk with safe
permissions (public 0644, private 0600) and skip the stdout print:

  muti-metroo management-key generate --out ./keys

Example output (without --out) can be copied directly into your config.yaml:

  management:
    public_key: "<public key hex>"
    private_key: "<private key hex>"  # Only on operator nodes!`,
		RunE: func(cmd *cobra.Command, args []string) error {
			keypair, err := identity.NewKeypair()
			if err != nil {
				return fmt.Errorf("failed to generate keypair: %w", err)
			}

			pubKeyHex := hex.EncodeToString(keypair.PublicKey[:])
			privKeyHex := hex.EncodeToString(keypair.PrivateKey[:])

			if outDir != "" {
				pubPath, privPath, err := writeKeyPair(outDir, "management", pubKeyHex, privKeyHex, force)
				if err != nil {
					return err
				}
				fmt.Println("=== Management Keypair Generated ===")
				fmt.Printf("Public Key:   %s (mode 0644)\n", pubPath)
				fmt.Printf("Private Key:  %s (mode 0600)\n", privPath)
				fmt.Println()
				fmt.Println("Public Key (also shown for convenience):")
				fmt.Printf("  %s\n", pubKeyHex)
				fmt.Println()
				fmt.Println("The private key is not echoed to stdout. Distribute it carefully.")
				return nil
			}

			fmt.Println("Tip: --out <dir> writes keys to disk with safe permissions and avoids")
			fmt.Println("     leaking the private key into terminal scrollback.")
			fmt.Println()
			fmt.Println("=== Management Keypair Generated ===")
			fmt.Println()
			fmt.Println("Public Key (add to ALL agent configs):")
			fmt.Printf("  %s\n", pubKeyHex)
			fmt.Println()
			fmt.Println("Private Key (add ONLY to operator configs - KEEP SECRET!):")
			fmt.Printf("  %s\n", privKeyHex)
			fmt.Println()
			fmt.Println("Config snippet for field agents:")
			fmt.Println("  management:")
			fmt.Printf("    public_key: \"%s\"\n", pubKeyHex)
			fmt.Println()
			fmt.Println("Config snippet for operator nodes:")
			fmt.Println("  management:")
			fmt.Printf("    public_key: \"%s\"\n", pubKeyHex)
			fmt.Printf("    private_key: \"%s\"\n", privKeyHex)

			return nil
		},
	}

	cmd.Flags().StringVar(&outDir, "out", "", "Write keys to <dir>/management-public.key (0644) and <dir>/management-private.key (0600); private key is not printed")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files in --out directory")

	return cmd
}

func managementKeyPublicCmd() *cobra.Command {
	var (
		privateKey string
		fromStdin  bool
	)

	cmd := &cobra.Command{
		Use:   "public",
		Short: "Derive public key from private key",
		Long: `Derive the public key from an existing management private key.

This is useful if you've lost the public key but still have the private key,
or to verify that your keypair is consistent.

The recommended way to provide the private key is interactively (no args)
or from a file via stdin:

  muti-metroo management-key public                 # hidden interactive prompt
  muti-metroo management-key public --stdin < key   # read hex from stdin

The --private flag is hidden and triggers a stderr warning because the
key value is recorded in shell history and 'ps' output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			privateKey = strings.TrimSpace(privateKey)
			if privateKey != "" {
				fmt.Fprintln(os.Stderr, "WARNING: --private secret on command line; this is recorded in shell history. Use --stdin or omit the flag for a hidden prompt.")
			}
			if privateKey == "" && fromStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("failed to read private key from stdin: %w", err)
				}
				privateKey = strings.TrimSpace(string(data))
			}
			if privateKey == "" {
				// Interactive prompt
				fmt.Print("Enter private key (hex): ")
				pkBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return fmt.Errorf("failed to read private key: %w", err)
				}
				privateKey = strings.TrimSpace(string(pkBytes))
			}

			// Decode private key
			privBytes, err := hex.DecodeString(privateKey)
			if err != nil {
				return fmt.Errorf("invalid private key hex: %w", err)
			}

			if len(privBytes) != 32 {
				return fmt.Errorf("private key must be 32 bytes (64 hex chars), got %d bytes", len(privBytes))
			}

			// Use identity package to derive public key
			var privKey [32]byte
			copy(privKey[:], privBytes)

			pubKey := identity.DerivePublicKey(privKey)
			pubKeyHex := hex.EncodeToString(pubKey[:])

			fmt.Println("Public Key:")
			fmt.Printf("  %s\n", pubKeyHex)

			return nil
		},
	}

	cmd.Flags().StringVar(&privateKey, "private", "", "Private key in hex format (HIDDEN: discouraged; leaks via shell history)")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read the private key from standard input")
	_ = cmd.Flags().MarkHidden("private")

	return cmd
}

func signingKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signing-key",
		Short: "Manage command signing keys",
		Long: `Manage Ed25519 keypairs for signing sleep/wake commands.

When command signing is enabled, sleep/wake commands must be signed with
a valid Ed25519 private key. Agents with the corresponding public key
will verify signatures before accepting commands.

This prevents unauthorized mesh hibernation - even if an attacker gains
access to an agent, they cannot issue sleep/wake commands without the
signing private key.`,
	}

	// Add subcommands
	cmd.AddCommand(signingKeyGenerateCmd())
	cmd.AddCommand(signingKeyPublicCmd())

	return cmd
}

func signingKeyGenerateCmd() *cobra.Command {
	var (
		outDir string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new signing keypair",
		Long: `Generate a new Ed25519 keypair for command signing.

The generated keys should be distributed as follows:
  - Public key: Add to ALL agent configs (field agents and operators)
  - Private key: Add ONLY to operator/management node configs

By default the private key is printed to stdout (and so survives in your
terminal scrollback). Pass --out <dir> to write the keys to disk with safe
permissions (public 0644, private 0600) and skip the stdout print:

  muti-metroo signing-key generate --out ./keys

Example output (without --out) can be copied directly into your config.yaml:

  management:
    signing_public_key: "<public key hex>"
    signing_private_key: "<private key hex>"  # Only on operator nodes!`,
		RunE: func(cmd *cobra.Command, args []string) error {
			kp, err := crypto.GenerateSigningKeypair()
			if err != nil {
				return fmt.Errorf("failed to generate keypair: %w", err)
			}

			pubKeyHex := hex.EncodeToString(kp.PublicKey[:])
			privKeyHex := hex.EncodeToString(kp.PrivateKey[:])

			if outDir != "" {
				pubPath, privPath, err := writeKeyPair(outDir, "signing", pubKeyHex, privKeyHex, force)
				if err != nil {
					return err
				}
				fmt.Println("=== Signing Keypair Generated ===")
				fmt.Printf("Public Key:   %s (mode 0644)\n", pubPath)
				fmt.Printf("Private Key:  %s (mode 0600)\n", privPath)
				fmt.Println()
				fmt.Println("Public Key (also shown for convenience):")
				fmt.Printf("  %s\n", pubKeyHex)
				fmt.Println()
				fmt.Println("The private key is not echoed to stdout. Distribute it carefully.")
				return nil
			}

			fmt.Println("Tip: --out <dir> writes keys to disk with safe permissions and avoids")
			fmt.Println("     leaking the private key into terminal scrollback.")
			fmt.Println()
			fmt.Println("=== Signing Keypair Generated ===")
			fmt.Println()
			fmt.Println("Public Key (add to ALL agent configs):")
			fmt.Printf("  %s\n", pubKeyHex)
			fmt.Println()
			fmt.Println("Private Key (add ONLY to operator configs - KEEP SECRET!):")
			fmt.Printf("  %s\n", privKeyHex)
			fmt.Println()
			fmt.Println("Config snippet for field agents:")
			fmt.Println("  management:")
			fmt.Printf("    signing_public_key: \"%s\"\n", pubKeyHex)
			fmt.Println()
			fmt.Println("Config snippet for operator nodes:")
			fmt.Println("  management:")
			fmt.Printf("    signing_public_key: \"%s\"\n", pubKeyHex)
			fmt.Printf("    signing_private_key: \"%s\"\n", privKeyHex)

			return nil
		},
	}

	cmd.Flags().StringVar(&outDir, "out", "", "Write keys to <dir>/signing-public.key (0644) and <dir>/signing-private.key (0600); private key is not printed")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing files in --out directory")

	return cmd
}

func signingKeyPublicCmd() *cobra.Command {
	var (
		privateKey string
		fromStdin  bool
	)

	cmd := &cobra.Command{
		Use:   "public",
		Short: "Derive public key from private key",
		Long: `Derive the Ed25519 public key from an existing signing private key.

This is useful if you've lost the public key but still have the private key,
or to verify that your keypair is consistent.

The recommended way to provide the private key is interactively (no args)
or from a file via stdin:

  muti-metroo signing-key public                 # hidden interactive prompt
  muti-metroo signing-key public --stdin < key   # read hex from stdin

The --private flag is hidden and triggers a stderr warning because the
key value is recorded in shell history and 'ps' output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			privateKey = strings.TrimSpace(privateKey)
			if privateKey != "" {
				fmt.Fprintln(os.Stderr, "WARNING: --private secret on command line; this is recorded in shell history. Use --stdin or omit the flag for a hidden prompt.")
			}
			if privateKey == "" && fromStdin {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("failed to read private key from stdin: %w", err)
				}
				privateKey = strings.TrimSpace(string(data))
			}
			if privateKey == "" {
				// Interactive prompt
				fmt.Print("Enter private key (hex): ")
				pkBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return fmt.Errorf("failed to read private key: %w", err)
				}
				privateKey = strings.TrimSpace(string(pkBytes))
			}

			// Decode private key
			privBytes, err := hex.DecodeString(privateKey)
			if err != nil {
				return fmt.Errorf("invalid private key hex: %w", err)
			}

			if len(privBytes) != 64 {
				return fmt.Errorf("private key must be 64 bytes (128 hex chars), got %d bytes", len(privBytes))
			}

			// Derive public key
			var privKey [64]byte
			copy(privKey[:], privBytes)

			pubKey := crypto.PublicKeyFromPrivate(privKey)
			pubKeyHex := hex.EncodeToString(pubKey[:])

			fmt.Println("Public Key:")
			fmt.Printf("  %s\n", pubKeyHex)

			return nil
		},
	}

	cmd.Flags().StringVar(&privateKey, "private", "", "Private key in hex format (HIDDEN: discouraged; leaks via shell history)")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read the private key from standard input")
	_ = cmd.Flags().MarkHidden("private")

	return cmd
}

// registerTimeoutFlag wires --timeout / -w (canonical) plus a hidden
// --timeout-legacy / -t alias on cmd, both backed by *target. When -t is
// supplied (cobra detects via Changed("timeout-legacy")), the value is
// copied into *target and a deprecation warning is printed to stderr in
// PreRunE -- so the user gets one notice per invocation, not one per use.
//
// Migration plan: -t historically meant timeout on action commands and
// target on management commands. Pin the larger surface (-t = --target,
// 16 commands) and migrate the smaller surface (-t = --timeout, 6
// commands). Old scripts keep working for a release; the warning steers
// operators toward -w before -t flips meaning in a future major release.
func registerTimeoutFlag(cmd *cobra.Command, target *string, defaultValue, help string) {
	const legacyFlag = "timeout-legacy"
	const deprecationMsg = "Warning: -t for --timeout is deprecated; use -w instead. " +
		"-t will mean --target (matching management commands) in a future release."

	var legacy string
	cmd.Flags().StringVarP(target, "timeout", "w", defaultValue, help)
	cmd.Flags().StringVarP(&legacy, legacyFlag, "t", "", help+" (DEPRECATED short alias of -w)")
	_ = cmd.Flags().MarkHidden(legacyFlag)
	prevPreRun := cmd.PreRunE
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		if c.Flags().Changed(legacyFlag) {
			fmt.Fprintln(os.Stderr, deprecationMsg)
			*target = legacy
		}
		if prevPreRun != nil {
			return prevPreRun(c, args)
		}
		return nil
	}
}

// warnExpiringCerts inspects the configured TLS cert files (global +
// per-listener overrides) and prints a stderr warning for any cert that
// is expired or expires within 60 days. Inline PEM material and absent
// paths (auto-generated ephemeral certs) are skipped silently. Best-
// effort: a missing or unreadable file is also skipped, since the agent's
// own TLS validation (in agent.New) will raise the actual error with
// better context if it matters.
// warnIgnoredConfigFields warns about configuration fields that are still
// accepted for backward compatibility but no longer do anything, so operators
// are not misled into thinking a security control is active when it is not.
func warnIgnoredConfigFields(cfg *config.Config) {
	// shell.password_hash and file_transfer.password_hash predate the unified
	// management-key authorization model. They are parsed and redacted but
	// never enforced: shell, file transfer, and dynamic mutations are all
	// gated by the Ed25519 management signing key. Setting them grants no
	// protection.
	if cfg.Shell.PasswordHash != "" {
		fmt.Fprintln(os.Stderr, "Warning: shell.password_hash is set but ignored; shell access is authorized by the management signing key (management.signing_public_key). Remove it to avoid confusion.")
	}
	if cfg.FileTransfer.PasswordHash != "" {
		fmt.Fprintln(os.Stderr, "Warning: file_transfer.password_hash is set but ignored; file transfer is authorized by the management signing key (management.signing_public_key). Remove it to avoid confusion.")
	}
}

func warnExpiringCerts(cfg *config.Config) {
	type certEntry struct {
		path  string
		label string
	}
	var entries []certEntry
	if cfg.TLS.Cert != "" {
		entries = append(entries, certEntry{cfg.TLS.Cert, "tls.cert"})
	}
	for i, l := range cfg.Listeners {
		if l.TLS.Cert != "" {
			entries = append(entries, certEntry{l.TLS.Cert, fmt.Sprintf("listeners[%d].tls.cert", i)})
		}
	}
	if len(entries) == 0 {
		return
	}
	now := time.Now()
	for _, e := range entries {
		info, err := certutil.GetCertInfoFromFile(e.path)
		if err != nil || info == nil {
			continue
		}
		band, daysLeft := certutil.ClassifyExpiry(info.NotAfter, now)
		expiryDate := info.NotAfter.Format("2006-01-02")
		switch band {
		case certutil.CertExpired:
			fmt.Fprintf(os.Stderr, "WARNING: %s (%s) is EXPIRED (since %s); rotate immediately.\n",
				e.label, e.path, expiryDate)
		case certutil.CertExpiringSoon:
			fmt.Fprintf(os.Stderr, "WARNING: %s (%s) expires in %d days (%s); rotate now.\n",
				e.label, e.path, daysLeft, expiryDate)
		case certutil.CertExpiringPlan:
			fmt.Fprintf(os.Stderr, "Notice: %s (%s) expires in %d days (%s); plan rotation.\n",
				e.label, e.path, daysLeft, expiryDate)
		}
	}
}

// wakeWindowHint returns a short human-readable estimate of how long
// sleeping agents take to reattach after a wake flood. Best-effort: queries
// /sleep/status for the agent's actual poll_interval/jitter and falls back
// to the documented defaults (5m + 30%) on any network or decode failure
// rather than blocking the operator. The `verbose` flag picks between the
// short prompt-inline form ("up to ~6m30s") and the standalone form
// ("Reattachment expected within ~6m30s (poll_interval=5m, +30% jitter)").
func wakeWindowHint(ep apiclient.Endpoint, verbose bool) string {
	pollSec, jitter := fetchWakeCadence(ep)
	upper := time.Duration(pollSec*(1+jitter)) * time.Second
	if verbose {
		return fmt.Sprintf("Reattachment expected within ~%s (poll_interval=%s, +%.0f%% jitter)",
			roundDuration(upper),
			roundDuration(time.Duration(pollSec)*time.Second),
			jitter*100)
	}
	return fmt.Sprintf("up to ~%s", roundDuration(upper))
}

// fetchWakeCadence queries /sleep/status for the agent's configured poll
// interval and jitter. Best-effort; returns the documented defaults
// (300s, 0.3) on any failure so the prompt always has something to print.
func fetchWakeCadence(ep apiclient.Endpoint) (pollSec, jitter float64) {
	pollSec, jitter = 300.0, 0.3

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTPURL("/sleep/status"), nil)
	if err != nil {
		return
	}
	setAuthToken(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var status struct {
		PollIntervalSec    float64 `json:"poll_interval_sec"`
		PollIntervalJitter float64 `json:"poll_interval_jitter"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return
	}
	if status.PollIntervalSec > 0 {
		pollSec = status.PollIntervalSec
	}
	if status.PollIntervalJitter > 0 {
		jitter = status.PollIntervalJitter
	}
	return
}

// roundDuration formats d as "5m30s" / "6m" / "45s", the shape humans
// expect in operator-facing output.
func roundDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
	}
	d = d.Round(time.Second)
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	if secs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm%ds", mins, secs)
}

// localAgentSummary returns a short display label for the agent at ep,
// suitable for confirmation prompts ("local (1659dc4c)" or
// "local agent: 1659dc4c"). Best-effort: on any failure returns the
// endpoint string so the prompt still makes sense.
func localAgentSummary(ep apiclient.Endpoint) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTPURL("/api/dashboard"), nil)
	if err != nil {
		return ep.Host
	}
	setAuthToken(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ep.Host
	}
	defer resp.Body.Close()

	var dash struct {
		Agent struct {
			ID          string `json:"id"`
			ShortID     string `json:"short_id"`
			DisplayName string `json:"display_name"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dash); err != nil {
		return ep.Host
	}
	short := dash.Agent.ShortID
	if short == "" && len(dash.Agent.ID) >= 8 {
		short = dash.Agent.ID[:8]
	}
	if dash.Agent.DisplayName != "" {
		return fmt.Sprintf("%s (%s)", dash.Agent.DisplayName, short)
	}
	if short != "" {
		return short
	}
	return ep.Host
}

// reachableAgentCount returns the number of mesh agents reachable from ep.
// Uses the cached GET form of /api/mesh-test (refreshes only every 30s).
// Best-effort; returns -1 on any failure so prompts can soften ("~unknown").
func reachableAgentCount(ep apiclient.Endpoint) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTPURL("/api/mesh-test"), nil)
	if err != nil {
		return -1
	}
	setAuthToken(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()

	var result struct {
		ReachableCount int `json:"reachable_count"`
		TotalCount     int `json:"total_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return -1
	}
	return result.ReachableCount
}

// confirmAction prompts the user with a y/N question. Returns true if the
// user accepted, false if they declined.
//
// When yes is true (e.g. --yes / -y supplied), returns true immediately
// without prompting.
//
// When stdin is not a terminal AND yes is false, returns errConfirmationRequired.
// This matches kubectl/aws behavior: scripts must declare intent with --yes
// rather than silently proceeding through what would have been a prompt.
//
// The prompt is written to stderr so it does not contaminate piped stdout.
func confirmAction(prompt string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("%w: stdin is not a terminal; pass --yes to skip confirmation", errConfirmationRequired)
	}
	fmt.Fprint(os.Stderr, prompt)
	if !strings.HasSuffix(prompt, " ") {
		fmt.Fprint(os.Stderr, " ")
	}
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes", nil
}

// confirmTyped prompts the user to type an exact phrase to confirm a
// particularly destructive action (default-route add, etc.). Returns true
// only if the user typed the phrase exactly. Same TTY/--yes semantics as
// confirmAction.
func confirmTyped(prompt, expected string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("%w: stdin is not a terminal; pass --yes to skip confirmation", errConfirmationRequired)
	}
	fmt.Fprint(os.Stderr, prompt)
	if !strings.HasSuffix(prompt, " ") {
		fmt.Fprint(os.Stderr, " ")
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.TrimSpace(line) == expected, nil
}

// parseDuration parses a duration string (e.g., "5m", "30s") or plain seconds.
// Returns duration in seconds. Supports "0" for no timeout.
func parseDuration(s string) (int, error) {
	if s == "" || s == "0" {
		return 0, nil
	}

	// Try parsing as duration first (e.g., "5m", "30s", "1h")
	d, err := time.ParseDuration(s)
	if err == nil {
		return int(d.Seconds()), nil
	}

	// Fall back to parsing as integer seconds for backwards compatibility
	secs, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration '%s' (use format like '30s', '5m', or plain seconds)", s)
	}
	return secs, nil
}

// resolveAgentID resolves a short agent ID prefix to a full agent ID.
// If the ID is already 32 hex characters, it's returned as-is.
// Otherwise, it queries the /agents endpoint to find matching agents.
func resolveAgentID(shortID string, ep apiclient.Endpoint) (string, error) {
	// Check if it's already a full ID (32 hex chars = 16 bytes = 128-bit AgentID)
	if len(shortID) == 32 && isHexString(shortID) {
		return shortID, nil
	}

	// Query the agents endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := ep.HTTPURL("/agents")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	setAuthToken(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// If we can't reach the API, assume the user provided a valid full ID
		return shortID, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return shortID, nil
	}

	var agents []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return shortID, nil
	}

	var matches []string
	for _, a := range agents {
		if strings.HasPrefix(strings.ToLower(a.ID), strings.ToLower(shortID)) {
			matches = append(matches, a.ID)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no agent found matching prefix: %s", shortID)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous prefix '%s' matches %d agents: %s...",
			shortID, len(matches), strings.Join(matches[:min(3, len(matches))], ", "))
	}
}

// isHexString checks if a string contains only hexadecimal characters.
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// isRemotePathAbsolute checks if a path is absolute, supporting both Unix and Windows formats.
// This is needed because the CLI may run on a different OS than the target agent.
// Unix absolute paths start with '/'.
// Windows absolute paths start with a drive letter followed by ':' (e.g., C:/ or C:\).
func isRemotePathAbsolute(path string) bool {
	if len(path) == 0 {
		return false
	}
	// Unix-style absolute path
	if path[0] == '/' {
		return true
	}
	// Windows-style absolute path (e.g., C:/ or C:\)
	if len(path) >= 3 && path[1] == ':' && (path[2] == '/' || path[2] == '\\') {
		c := path[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// Progress bar helper functions

// printProgress prints a progress bar to stdout.
func printProgress(current, total int64, startTime time.Time) {
	elapsed := time.Since(startTime).Seconds()
	if elapsed == 0 {
		elapsed = 0.001 // Avoid division by zero
	}
	speed := float64(current) / elapsed

	var pct float64
	if total > 0 {
		pct = float64(current) / float64(total) * 100
	}

	// Calculate ETA
	var eta string
	if speed > 0 && total > 0 {
		remaining := float64(total-current) / speed
		eta = formatProgressDuration(time.Duration(remaining) * time.Second)
	} else {
		eta = "--:--"
	}

	// Render simple ASCII bar: [=====>    ] 45% 1.2 MB/s ETA 2m30s
	bar := renderProgressBar(pct, 30)
	fmt.Printf("\r%s %.1f%% %s/s ETA %s  ", bar, pct, humanize.Bytes(uint64(speed)), eta)
}

// renderProgressBar renders an ASCII progress bar.
func renderProgressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	if filled > 0 {
		return "[" + strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", empty) + "]"
	}
	return "[" + strings.Repeat(" ", width) + "]"
}

// formatProgressDuration formats a duration for progress display.
func formatProgressDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// progressTrackingReader wraps an io.Reader and reports progress.
type progressTrackingReader struct {
	reader      io.Reader
	total       int64
	written     *int64
	startTime   time.Time
	quiet       bool
	lastPrinted time.Time
}

// Read implements io.Reader with progress tracking.
func (p *progressTrackingReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	if n > 0 {
		*p.written += int64(n)

		// Update progress bar (throttle to every 100ms to avoid flickering)
		if !p.quiet && time.Since(p.lastPrinted) > 100*time.Millisecond {
			printProgress(*p.written, p.total, p.startTime)
			p.lastPrinted = time.Now()
		}
	}
	return n, err
}

// progressTrackingWriter wraps an io.Writer and reports progress.
type progressTrackingWriter struct {
	writer      io.Writer
	total       int64
	written     *int64
	startTime   time.Time
	quiet       bool
	lastPrinted time.Time
}

// Write implements io.Writer with progress tracking.
func (p *progressTrackingWriter) Write(buf []byte) (int, error) {
	n, err := p.writer.Write(buf)
	if n > 0 {
		*p.written += int64(n)

		// Update progress bar (throttle to every 100ms to avoid flickering)
		if !p.quiet && time.Since(p.lastPrinted) > 100*time.Millisecond {
			printProgress(*p.written, p.total, p.startTime)
			p.lastPrinted = time.Now()
		}
	}
	return n, err
}

// sleepCmd creates the sleep command.
func sleepCmd() *cobra.Command {
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "sleep",
		Short: "Put mesh into sleep mode",
		Long: `Put the mesh network into sleep mode.

When triggered, sleep mode:
  - Floods a sleep command to all agents in the mesh
  - Closes all peer connections
  - Stops SOCKS5 and listener services
  - Periodically polls for queued messages

Note: Sleep mode must be enabled in the agent configuration:
  sleep:
    enabled: true
    poll_interval: 5m

Examples:
  # Put mesh to sleep via local agent
  muti-metroo sleep

  # Via a different agent
  muti-metroo sleep -a 192.168.1.10:8080`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			origin := localAgentSummary(ep)
			reach := reachableAgentCount(ep)
			reachStr := "an unknown number of"
			if reach >= 0 {
				reachStr = fmt.Sprintf("~%d", reach)
			}
			wakeCost := wakeWindowHint(ep, false)
			prompt := fmt.Sprintf(
				"Will flood SLEEP from %s to %s reachable mesh agents.\nThis closes peer connections mesh-wide; a subsequent wake takes %s. Continue? [y/N]:",
				origin, reachStr, wakeCost)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			url := ep.HTTPURL("/sleep")
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: sleep failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: sleep failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Sleep mode triggered: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())

	return cmd
}

// wakeCmd creates the wake command.
func wakeCmd() *cobra.Command {
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "wake",
		Short: "Wake mesh from sleep mode",
		Long: `Wake the mesh network from sleep mode.

When triggered, wake mode:
  - Reconnects to all configured peers
  - Restarts SOCKS5 and listener services
  - Floods a wake command to all agents in the mesh

Note: Sleep mode must be enabled in the agent configuration.

Examples:
  # Wake mesh via local agent
  muti-metroo wake

  # Via a different agent
  muti-metroo wake -a 192.168.1.10:8080`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			origin := localAgentSummary(ep)
			windowVerbose := wakeWindowHint(ep, true)
			prompt := fmt.Sprintf(
				"Will flood WAKE from %s to all sleeping mesh agents.\n%s\nContinue? [y/N]:",
				origin, windowVerbose)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			url := ep.HTTPURL("/wake")
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: wake failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: wake failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Wake mode triggered: %s\n", result.Message)
			fmt.Println(windowVerbose)
			fmt.Println("Use 'muti-metroo sleep-status' on each agent to verify reattachment.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())

	return cmd
}

func stopCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Gracefully stop a Muti Metroo agent",
		Long: `Gracefully stop a Muti Metroo agent.

The agent drains in-flight streams (up to 10 seconds), closes peer
connections, and exits the process.

Note: agents running under a service manager (systemd Restart=on-failure,
launchd KeepAlive, Windows SCM recovery) may be restarted automatically.
For permanent removal, use ` + "`muti-metroo service uninstall`" + ` on the host
before stopping.

Examples:
  # Stop the local agent
  muti-metroo stop

  # Stop a remote agent by short ID
  muti-metroo stop -t kreata

  # Stop via a different local API
  muti-metroo stop -a 192.168.1.10:8080 -t target-11`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			var who string
			if targetID == "" {
				who = "local agent " + localAgentSummary(ep)
			} else {
				who = "agent " + targetID
			}
			prompt := fmt.Sprintf(
				"Will stop %s.\nThe process will exit; any service manager (systemd/launchd/SCM) may restart it. Continue? [y/N]:",
				who)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			url, err := manageURL(ep, targetID, "shutdown")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: stop failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: stop failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Shutdown triggered: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// sleepStatusCmd creates the sleep-status command.
func sleepStatusCmd() *cobra.Command {
	var agentAddr string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "sleep-status",
		Short: "Check sleep mode status",
		Long: `Display the current sleep mode status of the agent.

Shows:
  - Current state (AWAKE, SLEEPING, or POLLING)
  - Whether sleep mode is enabled
  - Sleep start time (if sleeping)
  - Last poll time
  - Next scheduled poll time
  - Number of peers with queued state

Examples:
  # Check sleep status of local agent
  muti-metroo sleep-status

  # Via a different agent with JSON output
  muti-metroo sleep-status -a 192.168.1.10:8080 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			url := ep.HTTPURL("/sleep/status")
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var status struct {
				State          string    `json:"state"`
				Enabled        bool      `json:"enabled"`
				SleepStartTime time.Time `json:"sleep_start_time,omitempty"`
				LastPollTime   time.Time `json:"last_poll_time,omitempty"`
				NextPollTime   time.Time `json:"next_poll_time,omitempty"`
				QueuedPeers    int       `json:"queued_peers"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}

			fmt.Printf("Sleep Mode Status\n")
			fmt.Printf("=================\n")
			fmt.Printf("State:          %s\n", status.State)
			fmt.Printf("Enabled:        %v\n", status.Enabled)
			if !status.SleepStartTime.IsZero() {
				fmt.Printf("Sleep Started:  %s\n", status.SleepStartTime.Format(time.RFC3339))
			}
			if !status.LastPollTime.IsZero() {
				fmt.Printf("Last Poll:      %s\n", status.LastPollTime.Format(time.RFC3339))
			}
			if !status.NextPollTime.IsZero() {
				fmt.Printf("Next Poll:      %s\n", status.NextPollTime.Format(time.RFC3339))
			}
			fmt.Printf("Queued Peers:   %d\n", status.QueuedPeers)

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// routeCmd creates the route parent command with add/remove/list subcommands.
func routeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Manage dynamic CIDR exit routes",
		Long: `Manage dynamic CIDR exit routes at runtime.

Dynamic routes are ephemeral (lost on restart). They allow promoting
transit-only agents to exit agents on the fly, or adjusting routing
without restarting agents.

Examples:
  # Add a route to the local agent
  muti-metroo route add 10.0.0.0/8

  # Add a route with custom metric and comment
  muti-metroo route add 192.168.0.0/16 --metric 5 --comment "office LAN"

  # Edit the comment on any route (config-seeded or dynamic)
  muti-metroo route comment 10.0.0.0/8 "internal corp network"

  # Add a route on a remote agent
  muti-metroo route add 10.0.0.0/8 --target abc123

  # List dynamic routes (configuration view)
  muti-metroo route list

  # Show the live route table the agent uses right now (operational view)
  muti-metroo route status

  # Remove a route
  muti-metroo route remove 10.0.0.0/8

  # Trace what this agent would do with a destination
  muti-metroo route trace 1.2.3.4
  muti-metroo route trace example.com --target abc123`,
	}

	cmd.AddCommand(routeAddCmd())
	cmd.AddCommand(routeRemoveCmd())
	cmd.AddCommand(routeCommentCmd())
	cmd.AddCommand(routeSourceIPCmd())
	cmd.AddCommand(routeInterfaceCmd())
	cmd.AddCommand(routeListCmd())
	cmd.AddCommand(routeTraceCmd())
	cmd.AddCommand(routeStatusCmd())

	return cmd
}

// newStatusCmd builds a `<noun> status` subcommand that runs `runner` to
// fetch /api/dashboard data and renders the operational view. Used by
// both `peer status` and `route status` so the flag set + help shape stay
// in lockstep -- if a third operational view shows up, copy this pattern.
func newStatusCmd(short, long string, runner func(addr string, jsonOut bool) error) *cobra.Command {
	var addr string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: short,
		Long:  long,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runner(addr, jsonOut)
		},
	}
	cmd.Flags().StringVarP(&addr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output in JSON format")
	return cmd
}

// routeStatusCmd is the canonical replacement for the deprecated top-level
// `muti-metroo routes` command. It prints the live route table the agent is
// using right now (next-hop, origin, metric, hop count) -- distinct from
// `muti-metroo route list` which shows config-seeded + dynamic configuration.
func routeStatusCmd() *cobra.Command {
	return newStatusCmd(
		"Show the live route table (operational view)",
		`Show the route table as the agent uses it right now: each network's
selected next-hop, origin agent, metric, and hop count.

Distinct from 'muti-metroo route list', which reports config-seeded and
runtime-added route definitions (a configuration view) -- 'route status'
is the operational view answering "where would my traffic actually go?".`,
		runRoutesStatus,
	)
}

// routeAddCmd creates the route add subcommand.
func routeAddCmd() *cobra.Command {
	var (
		agentAddr         string
		targetID          string
		metric            uint16
		comment           string
		sourceIP          string
		iface             string
		allowDefaultRoute bool
	)

	cmd := &cobra.Command{
		Use:   "add <cidr>",
		Short: "Add a dynamic CIDR exit route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cidr := args[0]

			// Validate CIDR and (if set) source IP family-match locally so a
			// typo errors out before the round trip.
			ipnet, _, err := config.ParseCIDRWithSource(cidr, sourceIP)
			if err != nil {
				return fmt.Errorf("%w: %v", errUsageError, err)
			}

			// Default-route guard: 0.0.0.0/0 or ::/0 require an explicit
			// opt-in flag OR a typed confirmation. This catches both the
			// runbook fat-finger and the operator-on-autopilot.
			ones, _ := ipnet.Mask.Size()
			isDefaultRoute := ones == 0
			if isDefaultRoute && !allowDefaultRoute {
				prompt := fmt.Sprintf(
					"%s is a DEFAULT ROUTE. Adding it advertises this agent as the exit\nfor any destination not covered by a more-specific route. Type 'add default route' to confirm:",
					cidr)
				ok, err := confirmTyped(prompt, "add default route", assumeYes)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Aborted. Re-run with --allow-default-route to skip this prompt.")
					return nil
				}
			}

			reqBody := struct {
				Action    string `json:"action"`
				Network   string `json:"network"`
				Metric    uint16 `json:"metric"`
				Comment   string `json:"comment,omitempty"`
				SourceIP  string `json:"source_ip,omitempty"`
				Interface string `json:"interface,omitempty"`
			}{
				Action:    "add",
				Network:   cidr,
				Metric:    metric,
				Comment:   comment,
				SourceIP:  sourceIP,
				Interface: iface,
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "routes/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: route add failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: route add failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Route added: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().Uint16VarP(&metric, "metric", "m", 0, "Route metric")
	cmd.Flags().StringVar(&comment, "comment", "", "Operator comment (propagated to peers in route advertisements)")
	cmd.Flags().StringVar(&sourceIP, "source-ip", "", "Local IP the exit binds outbound dials to for this route (like curl --interface; local-only, not advertised to peers)")
	cmd.Flags().StringVar(&iface, "interface", "", "Egress network interface the exit binds outbound dials to (unprivileged when source is unset or on the interface; off-interface source needs Linux + root; local-only)")
	cmd.Flags().BoolVar(&allowDefaultRoute, "allow-default-route", false, "Skip the typed confirmation when adding 0.0.0.0/0 or ::/0")

	return cmd
}

// routeRemoveCmd creates the route remove subcommand.
func routeRemoveCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "remove <cidr>",
		Short: "Remove a dynamic CIDR exit route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cidr := args[0]

			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("%w: invalid CIDR %q: %v", errUsageError, cidr, err)
			}

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			detail := previewRoute(ep, targetID, cidr)
			prompt := fmt.Sprintf("Remove route %s%s? [y/N]:", cidr, detail)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			reqBody := struct {
				Action  string `json:"action"`
				Network string `json:"network"`
			}{
				Action:  "remove",
				Network: cidr,
			}
			body, _ := json.Marshal(reqBody)

			url, err := manageURL(ep, targetID, "routes/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: route remove failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: route remove failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Route removed: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// previewRoute returns " (next-hop X via Y)" for the given CIDR, or empty
// string if the route can't be looked up. Best-effort, used only for
// confirmation prompts.
func previewRoute(ep apiclient.Endpoint, targetID, cidr string) string {
	url, err := manageURL(ep, targetID, "routes/manage")
	if err != nil {
		return ""
	}
	body, _ := json.Marshal(struct {
		Action string `json:"action"`
		Scope  string `json:"scope"`
	}{Action: "list", Scope: "all"})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthToken(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Routes []struct {
			Network string `json:"network"`
			Metric  uint16 `json:"metric"`
			Dynamic bool   `json:"dynamic"`
			Comment string `json:"comment,omitempty"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	for _, r := range result.Routes {
		if r.Network == cidr {
			kind := "config"
			if r.Dynamic {
				kind = "dynamic"
			}
			extra := ""
			if r.Comment != "" {
				extra = ", \"" + r.Comment + "\""
			}
			return fmt.Sprintf(" (metric %d, %s%s)", r.Metric, kind, extra)
		}
	}
	return ""
}

// routeFieldSetSpec describes one of the per-field route-mutation
// subcommands (route comment / route source-ip / route interface). Each
// posts {"action": <action>, "network": <cidr>, <fieldKey>: <value>} to
// /routes/manage. The three subcommands differ only in cobra wiring
// (use/short/long), the field name on the wire, the user-facing label
// ("comment" / "source IP" / "interface"), and an optional pre-flight
// CIDR+value validation closure.
type routeFieldSetSpec struct {
	use      string                         // cobra Use string, e.g. "comment <cidr> <text>"
	short    string                         // help short text
	long     string                         // multi-line help
	action   string                         // ManageRoute action verb, e.g. "set_comment"
	fieldKey string                         // JSON field name on /routes/manage
	label    string                         // human-readable label for "Route X updated" + "route X failed"
	validate func(cidr, value string) error // optional CIDR+value pre-flight; nil means "just net.ParseCIDR(cidr)"
}

// newRouteFieldSetCmd builds one of the per-field route-mutation subcommands
// from a routeFieldSetSpec. Collapses ~85 lines of byte-identical HTTP
// boilerplate that previously lived in routeCommentCmd / routeSourceIPCmd /
// routeInterfaceCmd. When adding the next per-route knob, define a new spec
// instead of cloning a function.
func newRouteFieldSetCmd(spec routeFieldSetSpec) *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long:  spec.long,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cidr := args[0]
			value := args[1]

			if spec.validate != nil {
				if err := spec.validate(cidr, value); err != nil {
					return fmt.Errorf("%w: %v", errUsageError, err)
				}
			} else if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("invalid CIDR %q: %w", cidr, err)
			}

			body, _ := json.Marshal(map[string]any{
				"action":      spec.action,
				"network":     cidr,
				spec.fieldKey: value,
			})

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "routes/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("route %s failed: %s", spec.label, result.Error)
				}
				return fmt.Errorf("route %s failed: %s", spec.label, resp.Status)
			}

			fmt.Printf("Route %s updated: %s\n", spec.label, result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// routeCommentCmd: muti-metroo route comment <cidr> <text>. Pass "" to clear.
func routeCommentCmd() *cobra.Command {
	return newRouteFieldSetCmd(routeFieldSetSpec{
		use:   "comment <cidr> <text>",
		short: "Set or clear the comment on a CIDR exit route",
		long: `Set or clear the operator comment on a CIDR exit route.

Works on both config-seeded and dynamically-added routes; the override is
journalled and survives agent restart. Pass an empty string to clear the
comment. Comments propagate to peers in route advertisements.`,
		action:   "set_comment",
		fieldKey: "comment",
		label:    "comment",
	})
}

// routeSourceIPCmd: muti-metroo route source-ip <cidr> <ip>. Pass "" to clear.
func routeSourceIPCmd() *cobra.Command {
	return newRouteFieldSetCmd(routeFieldSetSpec{
		use:   "source-ip <cidr> <ip>",
		short: "Set or clear the local source IP the exit binds outbound dials to for a CIDR",
		long: `Set or clear the local source IP the exit binds outbound dials to for
this CIDR (equivalent to curl --interface). Pass an empty string to clear
the binding. The source IP must already be bound on the exit host and
match the CIDR's address family. Local-only -- never advertised to peers.`,
		action:   "set_source_ip",
		fieldKey: "source_ip",
		label:    "source IP",
		validate: func(cidr, value string) error {
			_, _, err := config.ParseCIDRWithSource(cidr, value)
			return err
		},
	})
}

// routeInterfaceCmd: muti-metroo route interface <cidr> <name>. Pass "" to clear.
// Portable (unprivileged) when the source is unset or on the interface; an
// off-interface source uses SO_BINDTODEVICE and is Linux + root only.
func routeInterfaceCmd() *cobra.Command {
	return newRouteFieldSetCmd(routeFieldSetSpec{
		use:   "interface <cidr> <name>",
		short: "Set or clear the egress NIC the exit binds outbound dials to for a CIDR (off-interface source: Linux + root)",
		long: `Set or clear the egress network interface the exit binds outbound dials
to for this CIDR. Pass an empty string to clear.
With no source_ip, or a source_ip already on the interface, the bind is
portable and unprivileged (Linux, macOS, Windows). A source_ip that is not
assigned to the interface uses SO_BINDTODEVICE and is Linux + root only.`,
		action:   "set_interface",
		fieldKey: "interface",
		label:    "interface",
	})
}

// routeListEntry mirrors one element of the /routes/manage list response. Kept
// next to routeListCmd because both the JSON decode target and the column
// renderer reference it.
type routeListEntry struct {
	Network   string `json:"network"`
	Metric    uint16 `json:"metric"`
	Dynamic   bool   `json:"dynamic"`
	Comment   string `json:"comment,omitempty"`
	SourceIP  string `json:"source_ip,omitempty"`
	Interface string `json:"interface,omitempty"`
}

// routeListCmd creates the route list subcommand.
func routeListCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		jsonOutput bool
		allRoutes  bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List CIDR exit routes",
		Long: `List CIDR exit routes on the target agent.

By default lists only dynamic (runtime-added) routes. With --all, lists
both config-seeded and dynamic routes, including any operator comments
set or overridden via the management API.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := struct {
				Action string `json:"action"`
				Scope  string `json:"scope,omitempty"`
			}{
				Action: "list",
			}
			if allRoutes {
				reqBody.Scope = "all"
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "routes/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status string           `json:"status"`
				Error  string           `json:"error,omitempty"`
				Routes []routeListEntry `json:"routes"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("route list failed: %s", result.Error)
				}
				return fmt.Errorf("route list failed: %s", resp.Status)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Routes) == 0 {
				if allRoutes {
					fmt.Println("No routes configured")
				} else {
					fmt.Println("No dynamic routes configured")
				}
				return nil
			}

			// Only show SOURCE / INTERFACE columns when at least one route
			// has them set, to keep the common single-homed case uncluttered.
			showSource := false
			showInterface := false
			for _, r := range result.Routes {
				if r.SourceIP != "" {
					showSource = true
				}
				if r.Interface != "" {
					showInterface = true
				}
				if showSource && showInterface {
					break
				}
			}

			title := "Dynamic Routes"
			if allRoutes {
				title = "Local Routes"
			}
			fmt.Printf("%s (%d)\n", title, len(result.Routes))

			type col struct {
				header string
				width  int
				get    func(routeListEntry) string
			}
			cols := []col{
				{"NETWORK", 24, func(r routeListEntry) string { return r.Network }},
				{"METRIC", 7, func(r routeListEntry) string { return strconv.FormatUint(uint64(r.Metric), 10) }},
			}
			if allRoutes {
				cols = append(cols, col{"KIND", 8, func(r routeListEntry) string {
					if r.Dynamic {
						return "dynamic"
					}
					return "config"
				}})
			}
			if showSource {
				cols = append(cols, col{"SOURCE", 18, func(r routeListEntry) string { return r.SourceIP }})
			}
			if showInterface {
				cols = append(cols, col{"INTERFACE", 12, func(r routeListEntry) string { return r.Interface }})
			}
			// COMMENT is the trailing column; print without a width so long
			// comments are not truncated and short ones do not pad to a fixed width.
			cols = append(cols, col{"COMMENT", 0, func(r routeListEntry) string { return r.Comment }})

			printRow := func(values []string) {
				for i, c := range cols {
					if c.width == 0 {
						fmt.Print(values[i])
					} else {
						fmt.Printf("%-*s ", c.width, values[i])
					}
				}
				fmt.Println()
			}

			headers := make([]string, len(cols))
			for i, c := range cols {
				headers[i] = c.header
			}
			printRow(headers)

			for _, r := range result.Routes {
				values := make([]string, len(cols))
				for i, c := range cols {
					values[i] = c.get(r)
				}
				printRow(values)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVar(&allRoutes, "all", false, "Include config-seeded routes (default: dynamic only)")

	return cmd
}

// routeTraceResult mirrors health.RouteTraceResult for CLI decoding. Other
// subcommands in this file follow the same convention of inlining their
// response shape rather than importing the server's types.
type routeTraceResult struct {
	Status     string `json:"status"`
	Dest       string `json:"dest"`
	Hostname   string `json:"hostname"`
	ResolverOK bool   `json:"resolver_ok"`
	IPs        []struct {
		IP         string `json:"ip"`
		NoMatch    bool   `json:"no_match"`
		Candidates []struct {
			Network     string `json:"network"`
			PrefixLen   int    `json:"prefix_len"`
			Metric      uint16 `json:"metric"`
			Origin      string `json:"origin"`
			OriginName  string `json:"origin_name"`
			NextHop     string `json:"next_hop"`
			NextHopName string `json:"next_hop_name"`
			HopCount    int    `json:"hop_count"`
			LastUpdate  string `json:"last_update"`
			Winner      bool   `json:"winner"`
			Reason      string `json:"reason"`
			Comment     string `json:"comment,omitempty"`
		} `json:"candidates"`
	} `json:"ips"`
	Domain *struct {
		Matched     bool   `json:"matched"`
		Pattern     string `json:"pattern"`
		IsWildcard  bool   `json:"is_wildcard"`
		Metric      uint16 `json:"metric"`
		Origin      string `json:"origin"`
		OriginName  string `json:"origin_name"`
		NextHop     string `json:"next_hop"`
		NextHopName string `json:"next_hop_name"`
		HopCount    int    `json:"hop_count"`
	} `json:"domain"`
	Error string `json:"error"`
}

// routeTraceCmd creates the route trace subcommand.
func routeTraceCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "trace <ip|hostname>",
		Short: "Explain which route this agent would pick for a destination",
		Long: `Explain which route this agent would pick for a destination.

Accepts an IP address or a hostname. Hostnames are resolved using the
selected agent's own resolver. All CIDR routes that match the destination
are shown; the winner is chosen by longest prefix, then lowest metric,
then lowest origin ID -- the same rules the live stream-open path uses.

Examples:
  muti-metroo route trace 8.8.8.8
  muti-metroo route trace example.com
  muti-metroo route trace 10.1.2.3 --target abc123 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := args[0]

			body, _ := json.Marshal(struct {
				Dest string `json:"dest"`
			}{Dest: dest})

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "route/trace")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("failed to read response: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				var errResp struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(raw, &errResp) == nil && errResp.Error != "" {
					return fmt.Errorf("route trace failed: %s", errResp.Error)
				}
				return fmt.Errorf("route trace failed: %s", resp.Status)
			}

			if jsonOutput {
				fmt.Println(string(raw))
				return nil
			}

			var result routeTraceResult
			if err := json.Unmarshal(raw, &result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			printRouteTrace(&result)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// printRouteTrace renders a route trace result in human-readable form.
func printRouteTrace(r *routeTraceResult) {
	fmt.Printf("Trace for %s\n", r.Dest)

	if r.Hostname != "" && r.Domain != nil {
		if r.Domain.Matched {
			nextHop := r.Domain.NextHop
			if r.Domain.NextHopName != "" {
				nextHop = r.Domain.NextHopName + " (" + r.Domain.NextHop + ")"
			}
			origin := r.Domain.Origin
			if r.Domain.OriginName != "" {
				origin = r.Domain.OriginName + " (" + r.Domain.Origin + ")"
			}
			fmt.Printf("  Domain route: %s  metric=%d  next_hop=%s  origin=%s\n",
				r.Domain.Pattern, r.Domain.Metric, nextHop, origin)
		} else {
			fmt.Println("  Domain route: no match")
		}
	}

	if r.Hostname != "" {
		if r.Error != "" {
			fmt.Printf("  Resolver error: %s\n", r.Error)
			return
		}
		if r.ResolverOK && len(r.IPs) > 0 {
			ips := make([]string, 0, len(r.IPs))
			for _, e := range r.IPs {
				ips = append(ips, e.IP)
			}
			fmt.Printf("  Resolved: %s\n", strings.Join(ips, ", "))
		}
	}

	for _, ip := range r.IPs {
		if r.Hostname != "" {
			fmt.Printf("\n  %s:\n", ip.IP)
		}
		if ip.NoMatch {
			fmt.Println("    no matching CIDR route")
			continue
		}
		for _, c := range ip.Candidates {
			nextHop := c.NextHop
			if c.NextHopName != "" {
				nextHop = c.NextHopName + " (" + c.NextHop + ")"
			}
			origin := c.Origin
			if c.OriginName != "" {
				origin = c.OriginName + " (" + c.Origin + ")"
			}
			marker := "   "
			if c.Winner {
				marker = "-> "
			}
			reason := ""
			if !c.Winner && c.Reason != "" {
				reason = "  [" + c.Reason + "]"
			}
			comment := ""
			if c.Comment != "" {
				comment = "  # " + c.Comment
			}
			fmt.Printf("    %s%-20s metric=%d hops=%d next_hop=%s origin=%s%s%s\n",
				marker, c.Network, c.Metric, c.HopCount, nextHop, origin, reason, comment)
		}
	}
}

// manageURL builds a local-or-remote management URL. When targetID is empty,
// it points at the local agent (/{suffix}); otherwise at the remote variant
// (/agents/{id}/{suffix}) after resolving a short ID prefix.
func manageURL(ep apiclient.Endpoint, targetID, suffix string) (string, error) {
	if targetID == "" {
		return ep.HTTPURL("/" + suffix), nil
	}

	resolvedID, err := resolveAgentID(targetID, ep)
	if err != nil {
		return "", fmt.Errorf("failed to resolve agent ID: %w", err)
	}

	return ep.HTTPURL(fmt.Sprintf("/agents/%s/%s", resolvedID, suffix)), nil
}

// forwardCmd creates the forward parent command with add/remove/list subcommands.
func forwardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Manage dynamic forward listeners",
		Long: `Manage dynamic forward listeners at runtime.

Dynamic forward listeners are ephemeral (lost on restart). They allow adding
port forward ingress listeners on the fly without restarting agents.
Config-file listeners are protected from modification but appear in the list.

Examples:
  # Add a forward listener on the local agent
  muti-metroo forward add web-server :9090

  # Add with connection limit
  muti-metroo forward add web-server :9090 --max-connections 100

  # Add on a remote agent
  muti-metroo forward add web-server :9090 --target abc123

  # List all forward listeners
  muti-metroo forward list

  # Remove a dynamic forward listener
  muti-metroo forward remove web-server`,
	}

	cmd.AddCommand(forwardAddCmd())
	cmd.AddCommand(forwardRemoveCmd())
	cmd.AddCommand(forwardListCmd())
	cmd.AddCommand(forwardEndpointCmd())

	return cmd
}

// forwardEndpointCmd groups the dynamic endpoint subcommands under
// `muti-metroo forward endpoint ...`. Endpoints are the exit-side mapping from a
// routing key to a local target host:port; listeners (the existing top-level
// `muti-metroo forward add/remove/list`) are their ingress-side counterpart.
func forwardEndpointCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoint",
		Short: "Manage dynamic forward endpoints",
		Long: `Manage dynamic forward endpoints at runtime.

Dynamic endpoints persist across restarts when agent.data_dir is configured;
otherwise they are ephemeral. Config-file endpoints are protected from
modification but appear in the list.

Examples:
  # Add a forward endpoint on the local agent
  muti-metroo forward endpoint add web-server 127.0.0.1:3000

  # Add on a remote agent
  muti-metroo forward endpoint add web-server 127.0.0.1:3000 --target abc123

  # List all forward endpoints
  muti-metroo forward endpoint list

  # Remove a dynamic forward endpoint
  muti-metroo forward endpoint remove web-server`,
	}

	cmd.AddCommand(forwardEndpointAddCmd())
	cmd.AddCommand(forwardEndpointRemoveCmd())
	cmd.AddCommand(forwardEndpointListCmd())

	return cmd
}

// forwardEndpointAddCmd creates the `forward endpoint add` subcommand.
func forwardEndpointAddCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "add <key> <target>",
		Short: "Add a dynamic forward endpoint",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			target := args[1]

			reqBody := struct {
				Action string `json:"action"`
				Key    string `json:"key"`
				Target string `json:"target"`
			}{
				Action: "add",
				Key:    key,
				Target: target,
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "forward/endpoints/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("forward endpoint add failed: %s", result.Error)
				}
				return fmt.Errorf("forward endpoint add failed: %s", resp.Status)
			}

			fmt.Printf("Forward endpoint added: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// forwardEndpointRemoveCmd creates the `forward endpoint remove` subcommand.
func forwardEndpointRemoveCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "remove <key>",
		Short: "Remove a dynamic forward endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			prompt := fmt.Sprintf("Remove forward endpoint %q?\nIngress agents will no longer be able to route to this exit. Continue? [y/N]:", key)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			reqBody := struct {
				Action string `json:"action"`
				Key    string `json:"key"`
			}{
				Action: "remove",
				Key:    key,
			}
			body, _ := json.Marshal(reqBody)

			url, err := manageURL(ep, targetID, "forward/endpoints/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: forward endpoint remove failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: forward endpoint remove failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Forward endpoint removed: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// forwardEndpointListCmd creates the `forward endpoint list` subcommand.
func forwardEndpointListCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List forward endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := struct {
				Action string `json:"action"`
			}{
				Action: "list",
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "forward/endpoints/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status    string `json:"status"`
				Error     string `json:"error,omitempty"`
				Endpoints []struct {
					Key     string `json:"key"`
					Target  string `json:"target"`
					Dynamic bool   `json:"dynamic"`
				} `json:"endpoints"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("forward endpoint list failed: %s", result.Error)
				}
				return fmt.Errorf("forward endpoint list failed: %s", resp.Status)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Endpoints) == 0 {
				fmt.Println("No forward endpoints configured")
				return nil
			}

			fmt.Printf("Forward Endpoints (%d)\n", len(result.Endpoints))
			fmt.Printf("%-20s %-24s %s\n", "KEY", "TARGET", "TYPE")
			for _, e := range result.Endpoints {
				endpointType := "static"
				if e.Dynamic {
					endpointType = "dynamic"
				}
				fmt.Printf("%-20s %-24s %s\n", e.Key, e.Target, endpointType)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// forwardAddCmd creates the forward add subcommand.
func forwardAddCmd() *cobra.Command {
	var (
		agentAddr      string
		targetID       string
		maxConnections int
	)

	cmd := &cobra.Command{
		Use:   "add <key> <address>",
		Short: "Add a dynamic forward listener",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			address := args[1]

			reqBody := struct {
				Action         string `json:"action"`
				Key            string `json:"key"`
				Address        string `json:"address"`
				MaxConnections int    `json:"max_connections"`
			}{
				Action:         "add",
				Key:            key,
				Address:        address,
				MaxConnections: maxConnections,
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "forward/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("forward add failed: %s", result.Error)
				}
				return fmt.Errorf("forward add failed: %s", resp.Status)
			}

			fmt.Printf("Forward listener added: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().IntVar(&maxConnections, "max-connections", 0, "Maximum concurrent connections (0 = unlimited)")

	return cmd
}

// forwardRemoveCmd creates the forward remove subcommand.
func forwardRemoveCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "remove <key>",
		Short: "Remove a dynamic forward listener",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			prompt := fmt.Sprintf("Remove forward listener %q?\nThis closes the listening socket and rejects new ingress connections. Continue? [y/N]:", key)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			reqBody := struct {
				Action string `json:"action"`
				Key    string `json:"key"`
			}{
				Action: "remove",
				Key:    key,
			}
			body, _ := json.Marshal(reqBody)

			url, err := manageURL(ep, targetID, "forward/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: forward remove failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: forward remove failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Forward listener removed: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// forwardListCmd creates the forward list subcommand.
func forwardListCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List forward listeners",
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := struct {
				Action string `json:"action"`
			}{
				Action: "list",
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "forward/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status    string `json:"status"`
				Error     string `json:"error,omitempty"`
				Listeners []struct {
					Key            string `json:"key"`
					Address        string `json:"address"`
					MaxConnections int    `json:"max_connections"`
					Dynamic        bool   `json:"dynamic"`
				} `json:"listeners"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("forward list failed: %s", result.Error)
				}
				return fmt.Errorf("forward list failed: %s", resp.Status)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Listeners) == 0 {
				fmt.Println("No forward listeners configured")
				return nil
			}

			fmt.Printf("Forward Listeners (%d)\n", len(result.Listeners))
			fmt.Printf("%-20s %-24s %-8s %s\n", "KEY", "ADDRESS", "TYPE", "MAX_CONN")
			for _, l := range result.Listeners {
				listenerType := "static"
				if l.Dynamic {
					listenerType = "dynamic"
				}
				maxConn := "unlimited"
				if l.MaxConnections > 0 {
					maxConn = fmt.Sprintf("%d", l.MaxConnections)
				}
				fmt.Printf("%-20s %-24s %-8s %s\n", l.Key, l.Address, listenerType, maxConn)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// peerCmd creates the peer parent command with add/remove/list subcommands
// for managing outbound peer connections at runtime.
func peerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peer",
		Short: "Manage dynamic peer connections",
		Long: `Manage dynamic outbound peer connections at runtime.

Dynamic peers are ephemeral (lost on agent restart). They let you tell an
already-running agent to dial a new peer without editing config.yaml. This
is useful when a new agent (e.g. F) can only accept connections but existing
agents (e.g. D) must initiate the dial (firewall, NAT).

Peers are uniquely keyed by dial address. TLS settings are inherited from
the agent's global config. Config-file peers are protected from removal
but appear in the list.

Examples:
  # Tell the local agent to dial a new peer via QUIC
  muti-metroo peer add newagent.example:4433

  # Add over HTTP/2 with a specific expected peer ID
  muti-metroo peer add newagent.example:443 --transport h2 --id <agent-id>

  # Tell remote agent D (reachable via mesh) to dial F
  muti-metroo peer add F.example:4433 --target <agent-d-id>

  # List all peers (dynamic + config)
  muti-metroo peer list

  # Show currently-connected peers (live transport state, RTT, role)
  muti-metroo peer status

  # Remove a dynamic peer (tears down live connection, cancels reconnect)
  muti-metroo peer remove newagent.example:4433`,
	}

	cmd.AddCommand(peerAddCmd())
	cmd.AddCommand(peerRemoveCmd())
	cmd.AddCommand(peerListCmd())
	cmd.AddCommand(peerStatusCmd())

	return cmd
}

// peerStatusCmd is the canonical replacement for the deprecated top-level
// `muti-metroo peers` command. It prints currently-connected peers (live state,
// RTT, role) -- distinct from `muti-metroo peer list` which shows the
// configured + dynamic + inbound peer set.
func peerStatusCmd() *cobra.Command {
	return newStatusCmd(
		"Show currently-connected peers (live transport state)",
		`Show currently-connected peers as the agent sees them right now: live
transport state, role (dialer/listener), and round-trip time.

Distinct from 'muti-metroo peer list', which reports the configured + dynamic
+ inbound peer set (a configuration view) -- 'peer status' is the
operational view answering "who am I talking to right now?".`,
		runPeersStatus,
	)
}

// peerAddCmd creates the peer add subcommand.
func peerAddCmd() *cobra.Command {
	var (
		agentAddr     string
		targetID      string
		transportType string
		peerID        string
		tcpKeepAlive  string
	)

	cmd := &cobra.Command{
		Use:   "add <address>",
		Short: "Add a dynamic peer connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]

			// Validate the keepalive duration client-side for immediate feedback.
			if tcpKeepAlive != "" {
				if _, err := time.ParseDuration(tcpKeepAlive); err != nil {
					return fmt.Errorf("%w: invalid --tcp-keepalive %q: %v", errUsageError, tcpKeepAlive, err)
				}
			}

			// Without --id, the agent accepts whatever identity answers at
			// <address>. For critical-systems posture, warn loudly so the
			// operator pins the expected ID via --id <hex>.
			if peerID == "" || strings.EqualFold(peerID, "auto") {
				fmt.Fprintf(os.Stderr,
					"Warning: --id not specified; this peer connection will accept any agent identity at %s.\n"+
						"For peer pinning use --id <agent-id>.\n",
					address)
			}

			reqBody := struct {
				Action       string `json:"action"`
				Address      string `json:"address"`
				Transport    string `json:"transport,omitempty"`
				ID           string `json:"id,omitempty"`
				TCPKeepAlive string `json:"tcp_keepalive,omitempty"`
			}{
				Action:       "add",
				Address:      address,
				Transport:    transportType,
				ID:           peerID,
				TCPKeepAlive: tcpKeepAlive,
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "peers/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: peer add failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: peer add failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Peer added: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().StringVar(&transportType, "transport", "quic", "Transport type: quic, h2, or ws")
	cmd.Flags().StringVar(&peerID, "id", "", "Expected peer agent ID (omit/auto = accept any; pinning is recommended for production)")
	cmd.Flags().StringVar(&tcpKeepAlive, "tcp-keepalive", "", "SO_KEEPALIVE period for h2/ws transports (e.g. 20s; omit = inherit, 0 = disable)")

	return cmd
}

// peerRemoveCmd creates the peer remove subcommand.
func peerRemoveCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "remove <address>",
		Short: "Remove a dynamic peer connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			detail := previewPeerImpact(ep, address)
			prompt := fmt.Sprintf(
				"Remove peer %s%s?\nThis tears down the live connection and cancels reconnect. Continue? [y/N]:",
				address, detail)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			reqBody := struct {
				Action  string `json:"action"`
				Address string `json:"address"`
			}{
				Action:  "remove",
				Address: address,
			}
			body, _ := json.Marshal(reqBody)

			url, err := manageURL(ep, targetID, "peers/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: peer remove failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: peer remove failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Peer removed: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// previewPeerImpact returns a short " (current state, N routes through it)"
// suffix for confirmation prompts. Best-effort; empty string on any failure.
func previewPeerImpact(ep apiclient.Endpoint, address string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.HTTPURL("/api/dashboard"), nil)
	if err != nil {
		return ""
	}
	setAuthToken(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var dash struct {
		Peers []struct {
			ShortID     string `json:"short_id"`
			DisplayName string `json:"display_name"`
			Address     string `json:"address"`
			State       string `json:"state"`
		} `json:"peers"`
		Routes []struct {
			NextHopID   string `json:"next_hop_id"`
			NextHopName string `json:"next_hop_name"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dash); err != nil {
		return ""
	}
	for _, p := range dash.Peers {
		if p.Address != address {
			continue
		}
		// Count routes whose next-hop is this peer.
		impacted := 0
		for _, r := range dash.Routes {
			if r.NextHopID == p.ShortID || r.NextHopName == p.DisplayName {
				impacted++
			}
		}
		name := p.DisplayName
		if name == "" {
			name = p.ShortID
		}
		if impacted > 0 {
			return fmt.Sprintf(" (peer %s, state=%s, %d route(s) flow through it)", name, p.State, impacted)
		}
		return fmt.Sprintf(" (peer %s, state=%s)", name, p.State)
	}
	return ""
}

// peerListCmd creates the peer list subcommand.
func peerListCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List peer connections",
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := struct {
				Action string `json:"action"`
			}{
				Action: "list",
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "peers/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status string `json:"status"`
				Error  string `json:"error,omitempty"`
				Peers  []struct {
					Address    string `json:"address"`
					Transport  string `json:"transport"`
					ExpectedID string `json:"expected_id,omitempty"`
					RemoteID   string `json:"remote_id,omitempty"`
					Connected  bool   `json:"connected"`
					Persistent bool   `json:"persistent"`
					Dynamic    bool   `json:"dynamic"`
					Direction  string `json:"direction,omitempty"`
				} `json:"peers"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("peer list failed: %s", result.Error)
				}
				return fmt.Errorf("peer list failed: %s", resp.Status)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Peers) == 0 {
				fmt.Println("No peers configured")
				return nil
			}

			fmt.Printf("Peers (%d)\n", len(result.Peers))
			fmt.Printf("%-30s %-6s %-10s %-10s %s\n", "ADDRESS", "TRANS", "CONNECTED", "TYPE", "REMOTE_ID")
			for _, p := range result.Peers {
				peerType := "static"
				switch {
				case p.Direction == "inbound":
					peerType = "inbound"
				case p.Dynamic:
					peerType = "dynamic"
				}
				connStr := "no"
				if p.Connected {
					connStr = "yes"
				}
				address := p.Address
				if address == "" {
					address = "-"
				}
				remoteID := p.RemoteID
				if remoteID == "" {
					remoteID = "-"
				}
				fmt.Printf("%-30s %-6s %-10s %-10s %s\n", address, p.Transport, connStr, peerType, remoteID)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func listenerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "listener",
		Short: "Manage dynamic transport listeners",
		Long: `Manage dynamic transport listeners at runtime.

Transport listeners are the QUIC/HTTP2/WebSocket sockets that accept inbound
peer connections. Dynamic listeners let you open or close one on an
already-running agent without editing config.yaml; they are persisted to the
agent's data directory, so they survive restarts and sleep/wake.

Listeners are uniquely keyed by listen address. They reuse the agent's global
TLS certificate (or an auto-generated self-signed one) and the global mTLS
setting; --plaintext starts a WebSocket listener without TLS for use behind a
trusted reverse proxy. Config-file listeners are protected from removal but
appear in the list.

Examples:
  # Open a new QUIC listener on the local agent
  muti-metroo listener add quic :4434

  # Open a plaintext WebSocket listener behind a reverse proxy
  muti-metroo listener add ws 127.0.0.1:3002 --path /ws/ --plaintext

  # Open a listener and advertise its public dial target to the mesh
  muti-metroo listener add quic :4433 --advertise-address vps.example.com:4433

  # Tell remote agent D (reachable via mesh) to open a listener
  muti-metroo listener add quic :4433 --target <agent-d-id>

  # List all transport listeners (dynamic + config)
  muti-metroo listener list

  # Close a dynamic listener
  muti-metroo listener remove :4434`,
	}

	cmd.AddCommand(listenerAddCmd())
	cmd.AddCommand(listenerRemoveCmd())
	cmd.AddCommand(listenerListCmd())

	return cmd
}

// listenerAddCmd creates the listener add subcommand.
func listenerAddCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		path       string
		plaintext  bool
		advAddress string
		advPath    string
		advTrans   string
	)

	cmd := &cobra.Command{
		Use:   "add <transport> <address>",
		Short: "Add a dynamic transport listener",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			transportType := args[0]
			address := args[1]

			reqBody := struct {
				Action             string `json:"action"`
				Transport          string `json:"transport"`
				Address            string `json:"address"`
				Path               string `json:"path,omitempty"`
				PlainText          bool   `json:"plaintext,omitempty"`
				AdvertiseAddress   string `json:"advertise_address,omitempty"`
				AdvertisePath      string `json:"advertise_path,omitempty"`
				AdvertiseTransport string `json:"advertise_transport,omitempty"`
			}{
				Action:             "add",
				Transport:          transportType,
				Address:            address,
				Path:               path,
				PlainText:          plaintext,
				AdvertiseAddress:   advAddress,
				AdvertisePath:      advPath,
				AdvertiseTransport: advTrans,
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "listeners/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: listener add failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: listener add failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Listener added: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().StringVar(&path, "path", "", "HTTP path for h2/ws listeners (e.g. /ws/)")
	cmd.Flags().BoolVar(&plaintext, "plaintext", false, "Start a WebSocket listener without TLS (ws only; for use behind a trusted reverse proxy)")
	cmd.Flags().StringVar(&advAddress, "advertise-address", "", "Public host:port to advertise to the mesh so other agents can dial this listener")
	cmd.Flags().StringVar(&advPath, "advertise-path", "", "Path to advertise for h2/ws (defaults to --path)")
	cmd.Flags().StringVar(&advTrans, "advertise-transport", "", "Transport to advertise (defaults to the listener transport)")

	return cmd
}

// listenerRemoveCmd creates the listener remove subcommand.
func listenerRemoveCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "remove <address>",
		Short: "Remove a dynamic transport listener",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]

			prompt := fmt.Sprintf(
				"Remove listener %s?\nThis stops accepting new inbound connections on it. Continue? [y/N]:",
				address)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			reqBody := struct {
				Action  string `json:"action"`
				Address string `json:"address"`
			}{
				Action:  "remove",
				Address: address,
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "listeners/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: listener remove failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: listener remove failed: %s", errOperationRejected, resp.Status)
			}

			fmt.Printf("Listener removed: %s\n", result.Message)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// listenerListCmd creates the listener list subcommand.
func listenerListCmd() *cobra.Command {
	var (
		agentAddr  string
		targetID   string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List transport listeners",
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := struct {
				Action string `json:"action"`
			}{
				Action: "list",
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "listeners/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status    string `json:"status"`
				Error     string `json:"error,omitempty"`
				Listeners []struct {
					Transport        string `json:"transport"`
					Address          string `json:"address"`
					Path             string `json:"path,omitempty"`
					PlainText        bool   `json:"plaintext"`
					AdvertiseAddress string `json:"advertise_address,omitempty"`
					Dynamic          bool   `json:"dynamic"`
				} `json:"listeners"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("listener list failed: %s", result.Error)
				}
				return fmt.Errorf("listener list failed: %s", resp.Status)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Listeners) == 0 {
				fmt.Println("No listeners configured")
				return nil
			}

			fmt.Printf("Listeners (%d)\n", len(result.Listeners))
			fmt.Printf("%-24s %-6s %-12s %-10s %-8s %s\n", "ADDRESS", "TRANS", "PATH", "TLS", "TYPE", "ADVERTISE")
			for _, l := range result.Listeners {
				listenerType := "static"
				if l.Dynamic {
					listenerType = "dynamic"
				}
				tlsMode := "tls"
				if l.PlainText {
					tlsMode = "plaintext"
				}
				path := l.Path
				if path == "" {
					path = "-"
				}
				adv := l.AdvertiseAddress
				if adv == "" {
					adv = "-"
				}
				fmt.Printf("%-24s %-6s %-12s %-10s %-8s %s\n", l.Address, l.Transport, path, tlsMode, listenerType, adv)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// displayNameCmd creates the display-name parent command with set/get subcommands.
func displayNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "display-name",
		Short: "Manage agent display name",
		Long: `Manage the agent display name at runtime.

Dynamic display names are ephemeral (lost on restart). They allow renaming
agents on the fly to label topology maps without touching config files.
Setting an empty name reverts to the config value.

Examples:
  # Set display name on the local agent
  muti-metroo display-name set "gateway-us-east"

  # Set display name on a remote agent
  muti-metroo display-name set "exit-eu-west" --target abc123

  # Get current display name
  muti-metroo display-name get

  # Revert to config value
  muti-metroo display-name set ""`,
	}

	cmd.AddCommand(displayNameSetCmd())
	cmd.AddCommand(displayNameGetCmd())

	return cmd
}

// displayNameSetCmd creates the display-name set subcommand.
func displayNameSetCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set the agent display name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			// Empty name reverts to the config-file value -- silent reverts
			// are surprising under pressure, so confirm explicitly.
			if name == "" {
				prompt := "An empty name reverts the agent's display name to the config-file value.\nContinue? [y/N]:"
				ok, err := confirmAction(prompt, assumeYes)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Aborted.")
					return nil
				}
			}

			reqBody := struct {
				Action string `json:"action"`
				Name   string `json:"name"`
			}{
				Action: "set",
				Name:   name,
			}
			body, _ := json.Marshal(reqBody)

			url, err := manageURL(ep, targetID, "display-name/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Name    string `json:"name"`
				Error   string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					return fmt.Errorf("%w: %s", errAuthFailed, result.Error)
				}
				if result.Error != "" {
					return fmt.Errorf("%w: display name set failed: %s", errOperationRejected, result.Error)
				}
				return fmt.Errorf("%w: display name set failed: %s", errOperationRejected, resp.Status)
			}

			if name == "" {
				fmt.Printf("Display name reverted to config value: %s\n", result.Name)
			} else {
				fmt.Printf("Display name set: %s\n", result.Name)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// displayNameGetCmd creates the display-name get subcommand.
func displayNameGetCmd() *cobra.Command {
	var (
		agentAddr string
		targetID  string
	)

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the current agent display name",
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := struct {
				Action string `json:"action"`
			}{
				Action: "get",
			}
			body, _ := json.Marshal(reqBody)

			ep, err := apiclient.Parse(agentAddr)
			if err != nil {
				return err
			}

			url, err := manageURL(ep, targetID, "display-name/manage")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")
			setAuthToken(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("%w: %v", errAgentUnreachable, err)
			}
			defer resp.Body.Close()

			var result struct {
				Status string `json:"status"`
				Name   string `json:"name"`
				Error  string `json:"error,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to decode response: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				if result.Error != "" {
					return fmt.Errorf("display name get failed: %s", result.Error)
				}
				return fmt.Errorf("display name get failed: %s", resp.Status)
			}

			fmt.Println(result.Name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVarP(&targetID, "target", "t", "", "Target agent ID (omit for local agent)")

	return cmd
}

// hostCmd creates the host parent command with add/remove/list subcommands.
//
// The mesh-host registry is local-only in v1: there is no /agents/{id}/hosts
// per-agent variant, so these subcommands omit the --target flag that other
// management subcommands carry.
func hostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Manage mesh hostname registry",
		Long: `Manage the agent's mesh hostname registry.

Mesh hosts map symbolic names like "target-11.mesh" to IPs. The map is
served to local Mutiauk clients via /api/dashboard so they can
resolve mesh-suffixed names without disturbing the operator's normal
DNS. Entries are journalled and survive restart.

Examples:
  muti-metroo host add target-11.mesh --ipv4 10.8.0.11 --comment "edge"
  muti-metroo host add gateway.mesh --ipv4 10.0.0.1 --ipv6 fd00::1
  muti-metroo host list
  muti-metroo host list --json
  muti-metroo host remove target-11.mesh
  muti-metroo host suffix add alt
  muti-metroo host suffix list
  muti-metroo host suffix remove alt`,
	}

	cmd.AddCommand(hostAddCmd())
	cmd.AddCommand(hostRemoveCmd())
	cmd.AddCommand(hostListCmd())
	cmd.AddCommand(hostSuffixCmd())

	return cmd
}

func hostSuffixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suffix",
		Short: "Manage mesh hostname TLD suffixes",
		Long: `Manage the active set of TLD suffixes (e.g. mesh, lan, alt) the agent
accepts when validating mesh hostnames. Suffixes seeded by the
mesh_hosts_suffixes config block can be removed at runtime, and new
ones can be added without a restart. All mutations are journalled.

Examples:
  muti-metroo host suffix add alt
  muti-metroo host suffix list
  muti-metroo host suffix list --json
  muti-metroo host suffix remove alt`,
	}

	cmd.AddCommand(hostSuffixAddCmd())
	cmd.AddCommand(hostSuffixRemoveCmd())
	cmd.AddCommand(hostSuffixListCmd())

	return cmd
}

func hostSuffixAddCmd() *cobra.Command {
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "add <suffix>",
		Short: "Add a TLD suffix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(struct {
				Action string `json:"action"`
				Suffix string `json:"suffix"`
			}{Action: "add", Suffix: args[0]})

			result, err := postHostSuffixManage(agentAddr, body)
			if err != nil {
				return fmt.Errorf("suffix add failed: %w", err)
			}
			fmt.Printf("Suffix added. Active suffixes: %s\n", strings.Join(result.Suffixes, ", "))
			return nil
		},
	}
	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	return cmd
}

func hostSuffixRemoveCmd() *cobra.Command {
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "remove <suffix>",
		Short: "Remove a TLD suffix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			suffix := args[0]

			// Last-suffix protection: removing the only active suffix
			// invalidates every existing mesh host. Make the operator
			// confirm explicitly.
			listBody, _ := json.Marshal(struct {
				Action string `json:"action"`
			}{Action: "list"})
			if existing, err := postHostSuffixManage(agentAddr, listBody); err == nil {
				if len(existing.Suffixes) == 1 && strings.EqualFold(existing.Suffixes[0], suffix) {
					prompt := fmt.Sprintf("%q is the only active suffix.\nRemoving it invalidates every existing mesh host. Continue? [y/N]:", suffix)
					ok, err := confirmAction(prompt, assumeYes)
					if err != nil {
						return err
					}
					if !ok {
						fmt.Println("Aborted.")
						return nil
					}
				}
			}

			body, _ := json.Marshal(struct {
				Action string `json:"action"`
				Suffix string `json:"suffix"`
			}{Action: "remove", Suffix: suffix})

			result, err := postHostSuffixManage(agentAddr, body)
			if err != nil {
				return fmt.Errorf("%w: suffix remove failed: %v", errOperationRejected, err)
			}
			fmt.Printf("Suffix removed. Active suffixes: %s\n", strings.Join(result.Suffixes, ", "))
			return nil
		},
	}
	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	return cmd
}

func hostSuffixListCmd() *cobra.Command {
	var (
		agentAddr  string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active TLD suffixes",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(struct {
				Action string `json:"action"`
			}{Action: "list"})

			result, err := postHostSuffixManage(agentAddr, body)
			if err != nil {
				return fmt.Errorf("suffix list failed: %w", err)
			}
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			if len(result.Suffixes) == 0 {
				fmt.Println("No suffixes configured")
				return nil
			}
			fmt.Printf("Active suffixes (%d):\n", len(result.Suffixes))
			for _, s := range result.Suffixes {
				fmt.Printf("  .%s\n", s)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	return cmd
}

func hostAddCmd() *cobra.Command {
	var (
		agentAddr string
		ipv4      string
		ipv6      string
		comment   string
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a mesh host entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ipv4 == "" && ipv6 == "" {
				return fmt.Errorf("at least one of --ipv4 or --ipv6 is required")
			}
			body, _ := json.Marshal(struct {
				Action  string `json:"action"`
				Name    string `json:"name"`
				IPv4    string `json:"ipv4,omitempty"`
				IPv6    string `json:"ipv6,omitempty"`
				Comment string `json:"comment,omitempty"`
			}{
				Action:  "add",
				Name:    args[0],
				IPv4:    ipv4,
				IPv6:    ipv6,
				Comment: comment,
			})

			result, err := postHostManage(agentAddr, body)
			if err != nil {
				return fmt.Errorf("host add failed: %w", err)
			}
			added := findHost(result.Hosts, strings.ToLower(strings.TrimSpace(args[0])))
			if added == nil {
				fmt.Println("Host added")
				return nil
			}
			fmt.Printf("Host added: %s -> %s\n", added.Name, hostIPSummary(added))
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().StringVar(&ipv4, "ipv4", "", "IPv4 address")
	cmd.Flags().StringVar(&ipv6, "ipv6", "", "IPv6 address")
	cmd.Flags().StringVar(&comment, "comment", "", "Optional comment")

	return cmd
}

func hostRemoveCmd() *cobra.Command {
	var agentAddr string

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a mesh host entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			prompt := fmt.Sprintf("Remove mesh host %q?\nMutiauk clients can no longer resolve this name. Continue? [y/N]:", name)
			ok, err := confirmAction(prompt, assumeYes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

			body, _ := json.Marshal(struct {
				Action string `json:"action"`
				Name   string `json:"name"`
			}{
				Action: "remove",
				Name:   name,
			})

			if _, err := postHostManage(agentAddr, body); err != nil {
				return fmt.Errorf("%w: host remove failed: %v", errOperationRejected, err)
			}
			fmt.Printf("Host removed: %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())

	return cmd
}

func hostListCmd() *cobra.Command {
	var (
		agentAddr  string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List mesh host entries (config + dynamic)",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, _ := json.Marshal(struct {
				Action string `json:"action"`
			}{Action: "list"})

			result, err := postHostManage(agentAddr, body)
			if err != nil {
				return fmt.Errorf("host list failed: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			if len(result.Hosts) == 0 {
				fmt.Println("No mesh hosts configured")
				return nil
			}
			fmt.Printf("Mesh Hosts (%d)\n", len(result.Hosts))
			fmt.Printf("%-32s %-18s %-22s %s\n", "NAME", "IPV4", "IPV6", "COMMENT")
			for _, h := range result.Hosts {
				fmt.Printf("%-32s %-18s %-22s %s\n", h.Name, h.IPv4, h.IPv6, h.Comment)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&agentAddr, "agent", "a", agentAddrDefault, agentAddrHelp())
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// hostCLIEntry mirrors hosts.Host for CLI decoding without importing the
// internal package (cmd/muti-metroo keeps its response shapes inline, matching
// the route/forward/peer subcommands).
type hostCLIEntry struct {
	Name    string `json:"name"`
	IPv4    string `json:"ipv4,omitempty"`
	IPv6    string `json:"ipv6,omitempty"`
	Comment string `json:"comment,omitempty"`
}

type hostManageCLIResult struct {
	Hosts []hostCLIEntry `json:"hosts"`
	Error string         `json:"error,omitempty"`
}

type hostSuffixManageCLIResult struct {
	Suffixes []string `json:"suffixes"`
	Error    string   `json:"error,omitempty"`
}

func postHostManage(agentAddr string, body []byte) (*hostManageCLIResult, error) {
	ep, err := apiclient.Parse(agentAddr)
	if err != nil {
		return nil, err
	}
	url, err := manageURL(ep, "", "hosts/manage")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthToken(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAgentUnreachable, err)
	}
	defer resp.Body.Close()

	var result hostManageCLIResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if result.Error != "" {
			return nil, fmt.Errorf("%s", result.Error)
		}
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return &result, nil
}

func postHostSuffixManage(agentAddr string, body []byte) (*hostSuffixManageCLIResult, error) {
	ep, err := apiclient.Parse(agentAddr)
	if err != nil {
		return nil, err
	}
	url, err := manageURL(ep, "", "hosts/suffixes/manage")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthToken(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAgentUnreachable, err)
	}
	defer resp.Body.Close()

	var result hostSuffixManageCLIResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if result.Error != "" {
			return nil, fmt.Errorf("%s", result.Error)
		}
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return &result, nil
}

func findHost(list []hostCLIEntry, name string) *hostCLIEntry {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func hostIPSummary(h *hostCLIEntry) string {
	switch {
	case h.IPv4 != "" && h.IPv6 != "":
		return h.IPv4 + ", " + h.IPv6
	case h.IPv4 != "":
		return h.IPv4
	default:
		return h.IPv6
	}
}

// configCmd creates the parent `muti-metroo config` command. It groups read-only
// helpers that operate on a YAML file or a binary with embedded config; no
// HTTP / running-agent introspection here, by design.
func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate Muti Metroo configuration files",
		Long: `Inspect and validate Muti Metroo configuration without starting an agent.

Both subcommands work on a YAML file or a binary with embedded config:
  muti-metroo config validate ./config.yaml
  muti-metroo config validate /usr/local/bin/muti-metroo
  muti-metroo config show ./config.yaml --diff-from-default
`,
	}
	cmd.AddCommand(configValidateCmd())
	cmd.AddCommand(configShowCmd())
	return cmd
}

// loadConfigForInspect loads a config from either a YAML file or a binary
// with embedded config. The file/binary distinction is by extension: paths
// ending in .yaml/.yml are read as YAML; everything else is tried as an
// embedded-config binary first, then falls back to YAML for the
// extensionless case (e.g. "config" or stdin pipes).
func loadConfigForInspect(path string) (*config.Config, bool, error) {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		cfg, err := config.Load(path)
		return cfg, false, err
	}
	cfg, embedded, err := config.LoadOrEmbeddedFrom(path, "")
	if err != nil {
		// Fall back to plain YAML so a user who points at a non-binary
		// non-yaml path still gets a clear error.
		if cfg2, err2 := config.Load(path); err2 == nil {
			return cfg2, false, nil
		}
		return nil, false, err
	}
	return cfg, embedded, nil
}

// configValidateCmd creates `muti-metroo config validate <path>`.
func configValidateCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "validate <path>",
		Short: "Parse and validate a Muti Metroo config file (or binary with embedded config)",
		Long: `Validate a Muti Metroo config without starting an agent.

The argument may be a YAML file (.yaml/.yml) or a Muti Metroo binary that has
been embedded with a config via 'muti-metroo setup'. Validation runs the same
checks the agent applies at startup: schema, key formats, listener/peer
shape, CIDR/domain/host validity, and management-key consistency.

Exit codes:
  0  config is valid
  2  validation failed (config parsed but is rejected)
  1  I/O failure (file unreadable, embedded data missing)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, embedded, err := loadConfigForInspect(args[0])
			if err != nil {
				if jsonOutput {
					return jsonReport(map[string]interface{}{
						"path":  args[0],
						"ok":    false,
						"error": err.Error(),
					})
				}
				return fmt.Errorf("%w: %v", errOperationRejected, err)
			}
			// Load already calls Validate(), but be explicit so future
			// refactors don't silently bypass it.
			if vErr := cfg.Validate(); vErr != nil {
				if jsonOutput {
					return jsonReport(map[string]interface{}{
						"path":  args[0],
						"ok":    false,
						"error": vErr.Error(),
					})
				}
				return fmt.Errorf("%w: %v", errOperationRejected, vErr)
			}
			if jsonOutput {
				return jsonReport(map[string]interface{}{
					"path":     args[0],
					"ok":       true,
					"embedded": embedded,
				})
			}
			source := "file"
			if embedded {
				source = "embedded in binary"
			}
			fmt.Printf("OK: %s (%s)\n", args[0], source)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit a JSON result instead of human-readable text")
	return cmd
}

// configShowCmd creates `muti-metroo config show [<path>]`.
func configShowCmd() *cobra.Command {
	var (
		jsonOutput      bool
		diffFromDefault bool
	)
	cmd := &cobra.Command{
		Use:   "show [<path>]",
		Short: "Print the effective configuration with secrets redacted",
		Long: `Print the effective configuration after defaults, env-var expansion, and
identity hydration -- with secrets replaced by "<redacted>".

If <path> is omitted, defaults to ./config.yaml. The path may also point at
a Muti Metroo binary with embedded config.

Redacted fields: agent.private_key, tls.key/key_pem, peer tls keys, peer
proxy_auth.password, socks5 user passwords/hashes, http.token_hash,
management.private_key, management.signing_private_key. Set fields appear
as "<redacted>"; unset fields appear as the empty value, so the output
still answers "is this secret set?".

Use --diff-from-default to print only the keys that differ from the
built-in defaults; useful for diffing two deployments quickly.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "./config.yaml"
			if len(args) > 0 {
				path = args[0]
			}
			cfg, _, err := loadConfigForInspect(path)
			if err != nil {
				return fmt.Errorf("%w: %v", errOperationRejected, err)
			}
			redacted := config.Redacted(cfg)

			if diffFromDefault {
				return emitConfig(redacted, jsonOutput, diffFromDefault)
			}
			return emitConfig(redacted, jsonOutput, false)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit JSON instead of YAML")
	cmd.Flags().BoolVar(&diffFromDefault, "diff-from-default", false, "Show only keys that differ from the built-in defaults")
	return cmd
}

// emitConfig writes cfg to stdout, optionally as JSON and/or after diffing
// against config.Default(). The diff is computed on the marshalled YAML
// representation so it stays in sync with what the agent actually parses.
func emitConfig(cfg *config.Config, asJSON, diff bool) error {
	if asJSON {
		out := map[string]interface{}{}
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal config: %w", err)
		}
		if err := yaml.Unmarshal(raw, &out); err != nil {
			return fmt.Errorf("re-encode config: %w", err)
		}
		if diff {
			defRaw, err := yaml.Marshal(config.Redacted(config.Default()))
			if err != nil {
				return fmt.Errorf("marshal defaults: %w", err)
			}
			defMap := map[string]interface{}{}
			if err := yaml.Unmarshal(defRaw, &defMap); err != nil {
				return fmt.Errorf("re-encode defaults: %w", err)
			}
			out = diffMaps(out, defMap)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	if !diff {
		return yaml.NewEncoder(os.Stdout).Encode(cfg)
	}

	// Diff path: marshal both, decode into maps, drop matching keys.
	cfgRaw, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	defRaw, err := yaml.Marshal(config.Redacted(config.Default()))
	if err != nil {
		return fmt.Errorf("marshal defaults: %w", err)
	}
	cfgMap := map[string]interface{}{}
	defMap := map[string]interface{}{}
	if err := yaml.Unmarshal(cfgRaw, &cfgMap); err != nil {
		return fmt.Errorf("re-encode config: %w", err)
	}
	if err := yaml.Unmarshal(defRaw, &defMap); err != nil {
		return fmt.Errorf("re-encode defaults: %w", err)
	}
	diffed := diffMaps(cfgMap, defMap)
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	return enc.Encode(diffed)
}

// diffMaps returns the subset of cfg whose values differ from defaults at
// the same key path. Recurses through nested maps; non-map differences and
// missing keys in defaults are kept as-is.
func diffMaps(cfg, defaults map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range cfg {
		dv, ok := defaults[k]
		if !ok {
			out[k] = v
			continue
		}
		if cm, cmok := v.(map[string]interface{}); cmok {
			if dm, dmok := dv.(map[string]interface{}); dmok {
				if sub := diffMaps(cm, dm); len(sub) > 0 {
					out[k] = sub
				}
				continue
			}
		}
		if !equalYAMLValues(v, dv) {
			out[k] = v
		}
	}
	return out
}

// equalYAMLValues compares two values decoded from YAML for structural
// equality. Slices are compared element-wise; maps via diffMaps + length;
// scalars via fmt formatting (good enough for a diff display).
func equalYAMLValues(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch av := a.(type) {
	case []interface{}:
		bv, ok := b.([]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalYAMLValues(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, vv := range av {
			if !equalYAMLValues(vv, bv[k]) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

// jsonReport writes obj as indented JSON to stdout. Helper for --json output
// across the config commands.
func jsonReport(obj map[string]interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(obj)
}
