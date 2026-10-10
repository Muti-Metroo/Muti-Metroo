//go:build linux

package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

const systemdUnitPath = "/etc/systemd/system"

// isRootImpl checks if running as root on Linux.
func isRootImpl() bool {
	return os.Getuid() == 0
}

// installImpl installs the service on Linux using systemd.
func installImpl(cfg ServiceConfig, execPath string) error {
	return installSystemdService(cfg, execPath, false)
}

// installSystemdService installs a systemd service with optional embedded config mode.
func installSystemdService(cfg ServiceConfig, execPath string, embedded bool) error {
	unitName := cfg.Name + ".service"
	unitPath := filepath.Join(systemdUnitPath, unitName)

	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("service %s is already installed at %s", cfg.Name, unitPath)
	}

	unit := generateSystemdUnit(cfg, execPath, embedded)

	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("failed to write systemd unit file: %w", err)
	}

	fmt.Printf("Created systemd unit: %s\n", unitPath)

	if output, err := runCommand("systemctl", "daemon-reload"); err != nil {
		os.Remove(unitPath)
		return fmt.Errorf("failed to reload systemd: %s: %w", output, err)
	}

	if output, err := runCommand("systemctl", "enable", cfg.Name); err != nil {
		return fmt.Errorf("failed to enable service: %s: %w", output, err)
	}

	fmt.Printf("Enabled service: %s\n", cfg.Name)

	if output, err := runCommand("systemctl", "start", cfg.Name); err != nil {
		return fmt.Errorf("failed to start service: %s: %w", output, err)
	}

	fmt.Printf("Started service: %s\n", cfg.Name)

	return nil
}

// stopServiceImpl stops a running systemd service.
func stopServiceImpl(serviceName string) error {
	if output, err := runCommand("systemctl", "stop", serviceName); err != nil {
		if !strings.Contains(output, "not loaded") {
			return fmt.Errorf("failed to stop service: %s", strings.TrimSpace(output))
		}
	}
	return nil
}

// startServiceImpl starts a systemd service.
func startServiceImpl(serviceName string) error {
	if output, err := runCommand("systemctl", "start", serviceName); err != nil {
		return fmt.Errorf("failed to start service: %s", strings.TrimSpace(output))
	}
	return nil
}

// uninstallImpl removes the systemd service on Linux.
func uninstallImpl(serviceName string) error {
	unitName := serviceName + ".service"
	unitPath := filepath.Join(systemdUnitPath, unitName)

	// Check if installed
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return fmt.Errorf("service %s is not installed", serviceName)
	}

	// Stop the service (ignore error if not running)
	if output, err := runCommand("systemctl", "stop", serviceName); err != nil {
		// Check if it's just not running
		if !strings.Contains(output, "not loaded") {
			fmt.Printf("Note: could not stop service: %s\n", strings.TrimSpace(output))
		}
	} else {
		fmt.Printf("Stopped service: %s\n", serviceName)
	}

	// Disable the service
	if output, err := runCommand("systemctl", "disable", serviceName); err != nil {
		if !strings.Contains(output, "not loaded") {
			fmt.Printf("Note: could not disable service: %s\n", strings.TrimSpace(output))
		}
	} else {
		fmt.Printf("Disabled service: %s\n", serviceName)
	}

	// Remove the unit file
	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("failed to remove systemd unit file: %w", err)
	}

	fmt.Printf("Removed systemd unit: %s\n", unitPath)

	// Reload systemd
	if _, err := runCommand("systemctl", "daemon-reload"); err != nil {
		fmt.Println("Note: failed to reload systemd daemon")
	}

	// Reset failed state
	runCommand("systemctl", "reset-failed", serviceName)

	return nil
}

// statusImpl returns the service status on Linux.
func statusImpl(serviceName string) (string, error) {
	output, err := runCommand("systemctl", "is-active", serviceName)
	status := strings.TrimSpace(output)

	if err != nil {
		if status == "inactive" || status == "unknown" {
			return status, nil
		}
		return "", fmt.Errorf("failed to get service status: %w", err)
	}

	return status, nil
}

// isInstalledImpl checks if the service is installed on Linux.
func isInstalledImpl(serviceName string) bool {
	unitPath := filepath.Join(systemdUnitPath, serviceName+".service")
	_, err := os.Stat(unitPath)
	return err == nil
}

// isInteractiveImpl always returns true on Linux.
// Linux uses systemd which manages the process lifecycle externally.
func isInteractiveImpl() bool {
	return true
}

// runAsServiceImpl is a no-op on Linux.
// Linux uses systemd which manages the process lifecycle externally.
func runAsServiceImpl(name string, runner ServiceRunner) error {
	// On Linux, systemd manages the service. The 'run' command just runs normally
	// and systemd handles start/stop/restart. No special service handler needed.
	return nil
}

// generateSystemdUnit generates a systemd unit file for the system manager.
// When embedded is true, no -c flag is included (config is embedded in binary).
func generateSystemdUnit(cfg ServiceConfig, execPath string, embedded bool) string {
	return generateSystemdUnitGeneric(cfg, execPath, embedded, false)
}

// generateSystemdUnitGeneric is shared between the system and user-manager
// flavours. User units drop User=/Group= (they always run as the invoking
// user), drop ProtectSystem/ProtectHome (incompatible with ~/.config), and
// install into default.target instead of multi-user.target.
func generateSystemdUnitGeneric(cfg ServiceConfig, execPath string, embedded, userMode bool) string {
	var userGroup string
	if !userMode {
		if cfg.User != "" {
			userGroup += fmt.Sprintf("User=%s\n", cfg.User)
		}
		if cfg.Group != "" {
			userGroup += fmt.Sprintf("Group=%s\n", cfg.Group)
		}
	}

	protections := "ProtectSystem=strict\nProtectHome=read-only\n"
	wantedBy := "multi-user.target"
	if userMode {
		protections = ""
		wantedBy = "default.target"
	}

	execStart := fmt.Sprintf("%s run -c %s", execPath, cfg.ConfigPath)
	if embedded {
		execStart = execPath + " run"
	}

	readWritePaths := cfg.WorkingDir
	if cfg.DataDir != "" && cfg.DataDir != cfg.WorkingDir {
		readWritePaths = cfg.WorkingDir + " " + cfg.DataDir
	}

	return fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
WorkingDirectory=%s
%sRestart=on-failure
RestartSec=5
TimeoutStopSec=30

# Security hardening
NoNewPrivileges=true
%sPrivateTmp=true
ReadWritePaths=%s

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=%s

[Install]
WantedBy=%s
`, cfg.Description, execStart, cfg.WorkingDir, userGroup, protections, readWritePaths, cfg.Name, wantedBy)
}

// installImplEmbedded installs systemd service for embedded config binary.
func installImplEmbedded(cfg ServiceConfig, execPath string) error {
	return installSystemdService(cfg, execPath, true)
}

// =============================================================================
// User Service Dispatcher
// =============================================================================
//
// On Linux, two user-service backends are supported:
//   - systemd --user: writes ~/.config/systemd/user/<name>.service
//   - cron+nohup:     writes ~/.<name>/<name>.sh and a @reboot crontab line
//
// The dispatchers below pick a backend based on the requested method
// (auto/systemd/cron) on install, or by detecting which backend is already
// installed on uninstall/status/start/stop.

// installUserImpl is a thin dispatcher that routes to the requested backend.
// method is one of UserMethodAuto, UserMethodSystemd, UserMethodCron.
// An empty string is treated as UserMethodAuto.
func installUserImpl(cfg ServiceConfig, execPath, method string) error {
	if method == "" {
		method = UserMethodAuto
	}

	// Refuse if the *other* backend is already installed; ask the user to
	// uninstall first rather than silently leaving stale state behind.
	if existing := detectUserMethod(cfg.Name); existing != "" {
		return fmt.Errorf("user service is already installed (%s); run 'muti-metroo service uninstall' first", existing)
	}

	switch method {
	case UserMethodSystemd:
		if !hasSystemdUserAvailable() {
			return fmt.Errorf("systemd --user is not available (need systemctl, loginctl, and an active user session bus)")
		}
		return installUserSystemdImpl(cfg, execPath)
	case UserMethodCron:
		if !hasCrontab() {
			return ErrCrontabNotFound
		}
		return installUserCronImpl(cfg, execPath)
	case UserMethodAuto:
		if hasSystemdUserAvailable() {
			fmt.Println("Selected backend: systemd --user")
			return installUserSystemdImpl(cfg, execPath)
		}
		if !hasCrontab() {
			return fmt.Errorf("no usable user-service backend found (need systemd --user or crontab)")
		}
		fmt.Println("Selected backend: cron + nohup (systemd --user not available)")
		return installUserCronImpl(cfg, execPath)
	default:
		return fmt.Errorf("unknown user service method %q (expected auto, systemd, or cron)", method)
	}
}

// uninstallUserImpl detects which backend is installed and removes it.
func uninstallUserImpl(serviceName string) error {
	switch detectUserMethod(serviceName) {
	case UserMethodSystemd:
		return uninstallUserSystemdImpl(serviceName)
	case UserMethodCron:
		return uninstallUserCronImpl(serviceName)
	default:
		return fmt.Errorf("user service is not installed")
	}
}

// statusUserImpl returns the status of whichever user-service backend is
// installed, or "not installed" if neither is.
func statusUserImpl(serviceName string) (string, error) {
	switch detectUserMethod(serviceName) {
	case UserMethodSystemd:
		return statusUserSystemdImpl(serviceName)
	case UserMethodCron:
		return statusUserCronImpl(serviceName)
	default:
		return "not installed", nil
	}
}

// startUserImpl starts whichever backend is installed.
func startUserImpl(serviceName string) error {
	switch detectUserMethod(serviceName) {
	case UserMethodSystemd:
		return startUserSystemdImpl(serviceName)
	case UserMethodCron:
		return startUserCronImpl(serviceName)
	default:
		return fmt.Errorf("user service is not installed")
	}
}

// stopUserImpl stops whichever backend is installed.
func stopUserImpl(serviceName string) error {
	switch detectUserMethod(serviceName) {
	case UserMethodSystemd:
		return stopUserSystemdImpl(serviceName)
	case UserMethodCron:
		return stopUserCronImpl(serviceName)
	default:
		return fmt.Errorf("user service is not installed")
	}
}

// isUserInstalledImpl returns true if either backend is installed.
func isUserInstalledImpl(serviceName string) bool {
	return detectUserMethod(serviceName) != ""
}

// getUserServiceInfoImpl returns information about whichever backend is installed.
func getUserServiceInfoImpl(serviceName string) *UserServiceInfo {
	switch detectUserMethod(serviceName) {
	case UserMethodSystemd:
		return getUserSystemdServiceInfoImpl(serviceName)
	case UserMethodCron:
		return getUserCronServiceInfoImpl(serviceName)
	default:
		return nil
	}
}

// detectUserMethod returns the backend that is currently installed for
// serviceName, or the empty string if neither is. If both are present
// (e.g. mid-migration), systemd wins and we surface a warning.
func detectUserMethod(serviceName string) string {
	systemd := isUserSystemdInstalled(serviceName)
	cron := isUserCronInstalled(serviceName)
	switch {
	case systemd && cron:
		fmt.Fprintf(os.Stderr, "Warning: both systemd-user and cron+nohup entries exist for %q; preferring systemd. Remove the stale cron entry with 'crontab -e'.\n", serviceName)
		return UserMethodSystemd
	case systemd:
		return UserMethodSystemd
	case cron:
		return UserMethodCron
	default:
		return ""
	}
}

// =============================================================================
// systemd --user backend
// =============================================================================

// hasSystemdUserAvailable reports whether `systemctl --user` can talk to a
// user systemd instance. The result is cached for the lifetime of the
// process; one CLI invocation queries it multiple times across the install
// and dispatcher paths.
var (
	systemdUserOnce  sync.Once
	systemdUserAvail bool
)

func hasSystemdUserAvailable() bool {
	systemdUserOnce.Do(func() {
		if _, err := exec.LookPath("systemctl"); err != nil {
			return
		}
		if _, err := exec.LookPath("loginctl"); err != nil {
			return
		}
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			return
		}
		if _, err := os.Stat(runtimeDir); err != nil {
			return
		}
		// show-environment is a cheap reachability probe: it round-trips
		// the user manager bus without enumerating units.
		out, err := runCommand("systemctl", "--user", "show-environment")
		if err != nil {
			return
		}
		if strings.Contains(out, "Failed to connect") || strings.Contains(out, "No such file") {
			return
		}
		systemdUserAvail = true
	})
	return systemdUserAvail
}

// getSystemdUserUnitDir returns ~/.config/systemd/user (creating it is the
// caller's responsibility).
func getSystemdUserUnitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// getSystemdUserUnitPath returns the absolute unit-file path for serviceName.
func getSystemdUserUnitPath(serviceName string) (string, error) {
	dir, err := getSystemdUserUnitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, serviceName+".service"), nil
}

// generateSystemdUserUnit produces the unit text for the systemd --user
// backend. See generateSystemdUnitGeneric for the user-vs-system differences.
func generateSystemdUserUnit(cfg ServiceConfig, execPath string, embedded bool) string {
	return generateSystemdUnitGeneric(cfg, execPath, embedded, true)
}

// installUserSystemdImpl writes the unit file, reloads, enables --now, and
// best-effort enables linger so the unit survives logout.
func installUserSystemdImpl(cfg ServiceConfig, execPath string) error {
	return installUserSystemdImplEmbedded(cfg, execPath, false)
}

func installUserSystemdImplEmbedded(cfg ServiceConfig, execPath string, embedded bool) error {
	unitDir, err := getSystemdUserUnitDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", unitDir, err)
	}

	unitPath := filepath.Join(unitDir, cfg.Name+".service")
	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("user service %s is already installed at %s", cfg.Name, unitPath)
	}

	unit := generateSystemdUserUnit(cfg, execPath, embedded)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("failed to write user unit: %w", err)
	}
	fmt.Printf("Created user unit: %s\n", unitPath)

	if output, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		os.Remove(unitPath)
		return fmt.Errorf("failed to reload user systemd: %s: %w", output, err)
	}

	if output, err := runCommand("systemctl", "--user", "enable", "--now", cfg.Name+".service"); err != nil {
		// Best-effort cleanup so a failed install doesn't leave a half-enabled unit.
		_, _ = runCommand("systemctl", "--user", "disable", cfg.Name+".service")
		os.Remove(unitPath)
		_, _ = runCommand("systemctl", "--user", "daemon-reload")
		return fmt.Errorf("failed to enable user service: %s: %w", output, err)
	}
	fmt.Printf("Enabled and started user service: %s\n", cfg.Name)

	enableLinger()
	return nil
}

// enableLinger best-effort runs `loginctl enable-linger <user>` so the unit
// keeps running after logout. The command is idempotent on success.
// Failure is non-fatal -- without root, polkit usually rejects the request
// and the operator has to run it themselves.
func enableLinger() {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return
	}
	output, err := runCommand("loginctl", "enable-linger", u.Username)
	if err != nil {
		fmt.Printf("Note: linger is not enabled for %s. The service will stop when you log out.\n", u.Username)
		fmt.Printf("      To keep it running across logout, run as root:\n")
		fmt.Printf("        sudo loginctl enable-linger %s\n", u.Username)
		if msg := strings.TrimSpace(output); msg != "" {
			fmt.Printf("      (loginctl said: %s)\n", msg)
		}
		return
	}
	fmt.Printf("Linger active for user %s (service survives logout).\n", u.Username)
}

// isLingerEnabled reports whether loginctl says linger is on for username.
func isLingerEnabled(username string) bool {
	out, err := runCommand("loginctl", "show-user", username, "--property=Linger")
	if err != nil {
		return false
	}
	return strings.Contains(out, "Linger=yes")
}

// uninstallUserSystemdImpl disables, stops, removes the unit file, and reloads.
func uninstallUserSystemdImpl(serviceName string) error {
	unitPath, err := getSystemdUserUnitPath(serviceName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return fmt.Errorf("user service %s is not installed", serviceName)
	}

	// Stop and disable; ignore "not loaded" style failures.
	if output, err := runCommand("systemctl", "--user", "disable", "--now", serviceName+".service"); err != nil {
		if !strings.Contains(output, "not loaded") && !strings.Contains(output, "No such file") {
			fmt.Printf("Note: could not disable user service: %s\n", strings.TrimSpace(output))
		}
	} else {
		fmt.Printf("Stopped and disabled user service: %s\n", serviceName)
	}

	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("failed to remove user unit: %w", err)
	}
	fmt.Printf("Removed user unit: %s\n", unitPath)

	if _, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		fmt.Println("Note: failed to reload user systemd daemon")
	}
	_, _ = runCommand("systemctl", "--user", "reset-failed", serviceName+".service")
	return nil
}

// statusUserSystemdImpl reports active/inactive plus linger state.
func statusUserSystemdImpl(serviceName string) (string, error) {
	output, _ := runCommand("systemctl", "--user", "is-active", serviceName+".service")
	state := strings.TrimSpace(output)
	if state == "" {
		state = "unknown"
	}

	linger := "no"
	if u, err := user.Current(); err == nil {
		if isLingerEnabled(u.Username) {
			linger = "yes"
		}
	}
	return fmt.Sprintf("%s (systemd-user, linger=%s)", state, linger), nil
}

func startUserSystemdImpl(serviceName string) error {
	if output, err := runCommand("systemctl", "--user", "start", serviceName+".service"); err != nil {
		return fmt.Errorf("failed to start user service: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

func stopUserSystemdImpl(serviceName string) error {
	if output, err := runCommand("systemctl", "--user", "stop", serviceName+".service"); err != nil {
		return fmt.Errorf("failed to stop user service: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

// isUserSystemdInstalled checks for the unit file. We deliberately don't ask
// systemctl since the daemon may not be reachable in the current environment.
func isUserSystemdInstalled(serviceName string) bool {
	unitPath, err := getSystemdUserUnitPath(serviceName)
	if err != nil {
		return false
	}
	_, err = os.Stat(unitPath)
	return err == nil
}

// getUserSystemdServiceInfoImpl pulls the ExecStart line out of the unit so
// callers (typically the wizard) can show what config the unit points at.
func getUserSystemdServiceInfoImpl(serviceName string) *UserServiceInfo {
	unitPath, err := getSystemdUserUnitPath(serviceName)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(unitPath)
	if err != nil {
		return nil
	}

	var configPath string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		// Look for "-c <path>" within the ExecStart line.
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "-c" && i+1 < len(fields) {
				configPath = fields[i+1]
				break
			}
		}
		break
	}

	return &UserServiceInfo{
		Name:       serviceName,
		ConfigPath: configPath,
		// Logs go to journald, no file path to surface.
	}
}

// =============================================================================
// Cron+Nohup User Service (non-root installation, fallback)
// =============================================================================

// getCronServiceDir returns the path to ~/.<serviceName>
func getCronServiceDir(serviceName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	names := DeriveNames(serviceName)
	return filepath.Join(home, names.DirName), nil
}

// installUserCronImpl installs the service using cron @reboot and nohup.
func installUserCronImpl(cfg ServiceConfig, execPath string) error {
	serviceName := cfg.Name
	names := DeriveNames(serviceName)

	// Get service directory
	serviceDir, err := getCronServiceDir(serviceName)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	configPath, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Generate wrapper script
	scriptPath := filepath.Join(serviceDir, names.ScriptName)
	script := generateCronScript(configPath, execPath, serviceDir, serviceName)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write wrapper script: %w", err)
	}
	fmt.Printf("Created wrapper script: %s\n", scriptPath)

	// Add cron entry
	if err := addCronEntry(scriptPath, serviceName); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to add cron entry: %w", err)
	}
	fmt.Println("Added @reboot cron entry")

	// Start the service now
	if err := startUserCronImpl(serviceName); err != nil {
		fmt.Printf("Note: could not start service: %v\n", err)
		fmt.Println("The service will start on next reboot")
	} else {
		fmt.Println("Started service")
	}

	fmt.Printf("\nLog file: %s\n", filepath.Join(serviceDir, names.LogFileName))

	return nil
}

// uninstallUserCronImpl removes the cron-based user service.
func uninstallUserCronImpl(serviceName string) error {
	names := DeriveNames(serviceName)

	serviceDir, err := getCronServiceDir(serviceName)
	if err != nil {
		return err
	}

	if !isUserCronInstalled(serviceName) {
		return fmt.Errorf("user service is not installed")
	}

	// Stop the service if running
	if err := stopUserCronImpl(serviceName); err != nil {
		// Ignore errors, service might not be running
		fmt.Printf("Note: could not stop service: %v\n", err)
	} else {
		fmt.Println("Stopped service")
	}

	// Remove cron entry
	if err := removeCronEntry(serviceName); err != nil {
		fmt.Printf("Note: could not remove cron entry: %v\n", err)
	} else {
		fmt.Println("Removed cron entry")
	}

	// Remove wrapper script
	scriptPath := filepath.Join(serviceDir, names.ScriptName)
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Note: could not remove script: %v\n", err)
	} else {
		fmt.Printf("Removed wrapper script: %s\n", scriptPath)
	}

	// Remove PID file
	pidPath := filepath.Join(serviceDir, names.PIDFileName)
	os.Remove(pidPath)

	// Keep log file and directory for reference
	fmt.Printf("\nNote: Log file preserved at %s\n", filepath.Join(serviceDir, names.LogFileName))

	return nil
}

// statusUserCronImpl returns the status of the cron-based user service.
func statusUserCronImpl(serviceName string) (string, error) {
	names := DeriveNames(serviceName)

	serviceDir, err := getCronServiceDir(serviceName)
	if err != nil {
		return "", err
	}

	if !isUserCronInstalled(serviceName) {
		return "not installed", nil
	}

	// Check if process is running
	pidPath := filepath.Join(serviceDir, names.PIDFileName)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		return "inactive (no pid file)", nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return "inactive (invalid pid)", nil
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return "inactive", nil
	}

	// On Unix, FindProcess always succeeds. Use signal 0 to check if alive.
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return "inactive", nil
	}

	return fmt.Sprintf("active (pid %d, cron+nohup)", pid), nil
}

// startUserCronImpl starts the cron-based user service.
func startUserCronImpl(serviceName string) error {
	names := DeriveNames(serviceName)

	serviceDir, err := getCronServiceDir(serviceName)
	if err != nil {
		return err
	}

	scriptPath := filepath.Join(serviceDir, names.ScriptName)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("service not installed")
	}

	// Check if already running
	pidPath := filepath.Join(serviceDir, names.PIDFileName)
	if pidData, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("service already running (pid %d)", pid)
				}
			}
		}
	}

	// Execute the wrapper script
	cmd := exec.Command("/bin/bash", scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return nil
}

// stopUserCronImpl stops the cron-based user service.
func stopUserCronImpl(serviceName string) error {
	names := DeriveNames(serviceName)

	serviceDir, err := getCronServiceDir(serviceName)
	if err != nil {
		return err
	}

	pidPath := filepath.Join(serviceDir, names.PIDFileName)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("service not running (no pid file)")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("invalid pid file")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("process not found")
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("failed to send signal: %w", err)
	}

	os.Remove(pidPath)
	return nil
}

// isUserCronInstalled checks if the cron-based user service is installed.
func isUserCronInstalled(serviceName string) bool {
	names := DeriveNames(serviceName)

	// Check if cron entry exists
	output, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), names.CronMarker)
}

// getUserCronServiceInfoImpl returns information about the Linux user service.
func getUserCronServiceInfoImpl(serviceName string) *UserServiceInfo {
	names := DeriveNames(serviceName)

	serviceDir, err := getCronServiceDir(serviceName)
	if err != nil {
		return nil
	}

	// Read config path from the cron script
	scriptPath := filepath.Join(serviceDir, names.ScriptName)
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil
	}

	// Parse CONFIG= line from script
	var configPath string
	for _, line := range strings.Split(string(scriptData), "\n") {
		if strings.HasPrefix(line, "CONFIG=") {
			configPath = strings.Trim(strings.TrimPrefix(line, "CONFIG="), "\"")
			break
		}
	}

	return &UserServiceInfo{
		Name:       serviceName,
		ConfigPath: configPath,
		LogPath:    filepath.Join(serviceDir, names.LogFileName),
	}
}

// generateCronScript generates the wrapper script for nohup execution.
func generateCronScript(configPath, binaryPath, serviceDir, serviceName string) string {
	names := DeriveNames(serviceName)

	return fmt.Sprintf(`#!/bin/bash
# %s user service wrapper
# This script is managed by '%s service install --user'

PIDFILE="%s"
LOGFILE="%s"
CONFIG="%s"
BINARY="%s"

# Check if already running
if [ -f "$PIDFILE" ]; then
    PID=$(cat "$PIDFILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Already running (PID $PID)"
        exit 0
    fi
    rm -f "$PIDFILE"
fi

# Start with nohup
cd "$(dirname "$CONFIG")"
nohup "$BINARY" run -c "$CONFIG" >> "$LOGFILE" 2>&1 &
echo $! > "$PIDFILE"
echo "Started (PID $!)"
`,
		serviceName,
		serviceName,
		filepath.Join(serviceDir, names.PIDFileName),
		filepath.Join(serviceDir, names.LogFileName),
		configPath,
		binaryPath,
	)
}

// addCronEntry adds the @reboot entry to user's crontab.
func addCronEntry(scriptPath, serviceName string) error {
	names := DeriveNames(serviceName)

	// Get existing crontab
	output, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		// crontab -l fails if no crontab exists, that's OK
		if !strings.Contains(string(output), "no crontab") {
			return fmt.Errorf("failed to read crontab: %s", strings.TrimSpace(string(output)))
		}
		output = nil
	}

	// Check if already has our entry
	if strings.Contains(string(output), names.CronMarker) {
		return fmt.Errorf("cron entry already exists")
	}

	// Add our entry
	newEntry := fmt.Sprintf("@reboot %s %s\n", scriptPath, names.CronMarker)
	var newCrontab bytes.Buffer
	if len(output) > 0 {
		newCrontab.Write(output)
		// Ensure newline before our entry
		if !bytes.HasSuffix(output, []byte("\n")) {
			newCrontab.WriteByte('\n')
		}
	}
	newCrontab.WriteString(newEntry)

	// Write new crontab
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = &newCrontab
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to write crontab: %s", strings.TrimSpace(string(output)))
	}

	return nil
}

// removeCronEntry removes the service entry from user's crontab.
func removeCronEntry(serviceName string) error {
	names := DeriveNames(serviceName)

	// Get existing crontab
	output, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		return nil // No crontab, nothing to remove
	}

	// Filter out our entry
	var newCrontab bytes.Buffer
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, names.CronMarker) {
			continue // Skip our entry
		}
		if line == "" && newCrontab.Len() == 0 {
			continue // Skip leading empty lines
		}
		newCrontab.WriteString(line)
		newCrontab.WriteByte('\n')
	}

	// Write new crontab (or remove if empty)
	content := strings.TrimSpace(newCrontab.String())
	if content == "" {
		// Remove crontab entirely
		if output, err := exec.Command("crontab", "-r").CombinedOutput(); err != nil {
			// Ignore "no crontab" error
			if !strings.Contains(string(output), "no crontab") {
				return fmt.Errorf("failed to remove crontab: %s", strings.TrimSpace(string(output)))
			}
		}
		return nil
	}

	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(content + "\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to write crontab: %s", strings.TrimSpace(string(output)))
	}

	return nil
}

// hasCrontab checks if crontab command is available.
func hasCrontab() bool {
	_, err := exec.LookPath("crontab")
	return err == nil
}

// ErrCrontabNotFound is returned when crontab is not available.
var ErrCrontabNotFound = errors.New("crontab command not found - install cron to use user service")

// installUserWithDLLImpl is not supported on Linux.
func installUserWithDLLImpl(serviceName, dllPath, configPath string) error {
	return fmt.Errorf("DLL-based user service is only supported on Windows")
}
