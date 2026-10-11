// Package service provides cross-platform service management for Muti Metroo.
// It supports systemd on Linux, launchd on macOS, and Windows Service on Windows.
package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// User-service backend identifiers. Used by InstallUserWithMethod, the
// --user-method CLI flag, and the wizard. Only "systemd" is currently
// honoured outside the auto-detect path; on non-Linux platforms these
// constants exist for cross-platform call-site readability but the
// install/uninstall flow rejects user-mode anyway.
const (
	UserMethodAuto    = "auto"
	UserMethodSystemd = "systemd"
	UserMethodCron    = "cron"
)

// ServiceRunner is the interface that the agent must implement to run as a service.
type ServiceRunner interface {
	// Start starts the service. Should return quickly after initializing.
	Start() error

	// Stop stops the service gracefully.
	StopWithContext(ctx context.Context) error
}

// ServiceConfig holds configuration for installing the service.
type ServiceConfig struct {
	// Name is the service name (used in systemd/Windows service)
	Name string

	// DisplayName is the human-readable name (Windows only)
	DisplayName string

	// Description is the service description
	Description string

	// ConfigPath is the absolute path to the config file
	ConfigPath string

	// WorkingDir is the working directory for the service
	WorkingDir string

	// DataDir is the data directory that needs write access (Linux systemd only)
	DataDir string

	// User is the user to run the service as (Linux only, empty for root)
	User string

	// Group is the group to run the service as (Linux only, empty for root)
	Group string
}

// DefaultConfig returns a default service configuration.
func DefaultConfig(configPath string) ServiceConfig {
	absPath, _ := filepath.Abs(configPath)
	workDir := filepath.Dir(absPath)

	return ServiceConfig{
		Name:        "muti-metroo",
		DisplayName: "Muti Metroo Mesh Agent",
		Description: "Userspace mesh networking agent for virtual TCP tunnels",
		ConfigPath:  absPath,
		WorkingDir:  workDir,
	}
}

// ServiceNames holds all identifiers derived from a service name.
// Using DeriveNames ensures consistent naming across platforms.
type ServiceNames struct {
	DirName       string // "." + serviceName (e.g., ".muti-metroo")
	PIDFileName   string // serviceName + ".pid"
	LogFileName   string // serviceName + ".log"
	ScriptName    string // serviceName + ".sh"
	CronMarker    string // "# " + serviceName + "-cron"
	TaskName      string // ToRegistryValueName(serviceName) + "Start"
	RegistryValue string // ToRegistryValueName(serviceName)
}

// DeriveNames computes all platform identifiers from a single service name.
// The default "muti-metroo" produces values identical to the former hardcoded constants.
func DeriveNames(serviceName string) ServiceNames {
	regVal := ToRegistryValueName(serviceName)
	return ServiceNames{
		DirName:       "." + serviceName,
		PIDFileName:   serviceName + ".pid",
		LogFileName:   serviceName + ".log",
		ScriptName:    serviceName + ".sh",
		CronMarker:    "# " + serviceName + "-cron",
		TaskName:      regVal + "Start",
		RegistryValue: regVal,
	}
}

// ToRegistryValueName converts a service name to a valid Registry value name.
// Removes spaces and special characters, converts to PascalCase-like format.
// Examples: "muti-metroo" -> "Muti Metroo", "Tunnel Manager" -> "TunnelManager"
func ToRegistryValueName(serviceName string) string {
	// Replace hyphens and underscores with spaces for word splitting
	name := strings.ReplaceAll(serviceName, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	// Split into words and capitalize each
	words := strings.Fields(name)
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
	}

	return strings.Join(words, "")
}

// IsRoot returns true if the current process is running with elevated privileges.
// On Linux, this checks for UID 0 (root).
// On Windows, this checks for Administrator privileges.
func IsRoot() bool {
	return isRootImpl()
}

// Install installs the application as a system service.
// On Linux, this creates and enables a systemd unit.
// On macOS, this creates and loads a launchd plist.
// On Windows, this registers a Windows service.
func Install(cfg ServiceConfig) error {
	if !IsRoot() {
		return fmt.Errorf("must run as root/administrator to install service")
	}

	// Get the executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the real path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	return installImpl(cfg, execPath)
}

// GetInstallPath returns the standard installation path for a service binary.
// Linux/macOS: /usr/local/bin/<name>
// Windows: C:\Program Files\<name>\<name>.exe
func GetInstallPath(serviceName string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramFiles"), serviceName, serviceName+".exe")
	default:
		return filepath.Join("/usr/local/bin", serviceName)
	}
}

// InstallWithEmbedded installs a service using a binary with embedded configuration.
// It copies the embedded binary to the standard system location and creates
// a service that runs without the -c config flag.
func InstallWithEmbedded(cfg ServiceConfig, embeddedBinaryPath string) error {
	if !IsRoot() {
		return fmt.Errorf("must run as root/administrator to install service")
	}

	destPath := GetInstallPath(cfg.Name)

	if err := deployBinary(embeddedBinaryPath, destPath); err != nil {
		return err
	}

	fmt.Printf("Installed binary: %s\n", destPath)

	// Clear ConfigPath to indicate embedded config mode
	cfg.ConfigPath = ""

	return installImplEmbedded(cfg, destPath)
}

// InstallWithDeployment installs a service by copying the binary to a standard
// system location and creating a service that uses an external config file.
// This is like InstallWithEmbedded but for traditional config file deployments.
func InstallWithDeployment(cfg ServiceConfig) error {
	if !IsRoot() {
		return fmt.Errorf("must run as root/administrator to install service")
	}

	// Get the current executable path
	srcPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	srcPath, err = filepath.EvalSymlinks(srcPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Determine destination path
	destPath := GetInstallPath(cfg.Name)

	// Check if source and destination are the same
	if srcPath == destPath {
		fmt.Printf("Binary already at install location: %s\n", destPath)
		return installImpl(cfg, destPath)
	}

	if err := deployBinary(srcPath, destPath); err != nil {
		return err
	}

	fmt.Printf("Installed binary: %s\n", destPath)

	return installImpl(cfg, destPath)
}

// deployBinary copies a binary to destPath, creating the parent directory on Windows.
func deployBinary(srcPath, destPath string) error {
	if runtime.GOOS == "windows" {
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create program directory: %w", err)
		}
	}

	if err := copyFile(srcPath, destPath); err != nil {
		return fmt.Errorf("failed to copy binary to %s: %w", destPath, err)
	}

	return nil
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcStat, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = dstFile.ReadFrom(srcFile)
	return err
}

// Uninstall removes the system service.
// On Linux, this stops, disables, and removes the systemd unit.
// On macOS, this unloads and removes the launchd plist.
// On Windows, this stops and removes the Windows service.
func Uninstall(serviceName string) error {
	// Check for user service first (Linux and Windows)
	if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && IsUserInstalled(serviceName) {
		return UninstallUser(serviceName)
	}

	if !IsRoot() {
		return fmt.Errorf("must run as root/administrator to uninstall service")
	}

	return uninstallImpl(serviceName)
}

// InstallUser installs as a user-level service. On Linux this auto-detects
// between systemd --user and the cron+nohup fallback. On Windows, use
// InstallUserWindows instead. Does not require root privileges.
func InstallUser(cfg ServiceConfig) error {
	return InstallUserWithMethod(cfg, "auto")
}

// InstallUserWithMethod installs as a user-level service using the given
// backend. On Linux, method is one of "auto", "systemd", or "cron". An empty
// string is treated as "auto". On other platforms the method is ignored
// (and the call returns the platform-appropriate error/behaviour).
func InstallUserWithMethod(cfg ServiceConfig, method string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("user service installation is only supported on Linux (use InstallUserWindows on Windows)")
	}

	// Get the executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the real path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	return installUserImpl(cfg, execPath, method)
}

// InstallUserWindows installs as a Windows user service using Registry Run key + DLL.
// This does not require Administrator privileges.
// The service runs at user logon via rundll32.exe with the specified DLL.
// The serviceName is used as the Registry value name (visible in startup apps).
func InstallUserWindows(serviceName, dllPath, configPath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("InstallUserWindows is only supported on Windows")
	}

	// Check if already installed
	if IsUserInstalled(serviceName) {
		return fmt.Errorf("user service is already installed")
	}

	return installUserWithDLLImpl(serviceName, dllPath, configPath)
}

// UninstallUser removes the user-level service.
// On Linux: removes whichever backend is installed (systemd-user or cron+nohup)
// On Windows: removes Registry Run key entry
func UninstallUser(serviceName string) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return fmt.Errorf("user service is only supported on Linux and Windows")
	}
	return uninstallUserImpl(serviceName)
}

// IsUserInstalled checks if a user-level service is installed.
// On Linux: checks for either systemd-user or cron+nohup backend
// On Windows: checks for Registry Run key entry
func IsUserInstalled(serviceName string) bool {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return false
	}
	return isUserInstalledImpl(serviceName)
}

// StatusUser returns the status of the user-level service.
// On Linux: routes to whichever backend (systemd-user or cron+nohup) is installed
// On Windows: checks Registry Run key and process status
func StatusUser(serviceName string) (string, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return "", fmt.Errorf("user service is only supported on Linux and Windows")
	}
	return statusUserImpl(serviceName)
}

// StartUser starts the user-level service.
// On Linux: routes to whichever backend (systemd-user or cron+nohup) is installed
// On Windows: starts the DLL via rundll32
func StartUser(serviceName string) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return fmt.Errorf("user service is only supported on Linux and Windows")
	}
	return startUserImpl(serviceName)
}

// StopUser stops the user-level service.
// On Linux: routes to whichever backend (systemd-user or cron+nohup) is installed
// On Windows: terminates the rundll32 process
func StopUser(serviceName string) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return fmt.Errorf("user service is only supported on Linux and Windows")
	}
	return stopUserImpl(serviceName)
}

// Status returns the current status of the service.
// It auto-detects whether a system or user service is installed.
func Status(serviceName string) (string, error) {
	// Check for user service first (Linux and Windows)
	if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && IsUserInstalled(serviceName) {
		return StatusUser(serviceName)
	}
	return statusImpl(serviceName)
}

// IsInstalled checks if the service is already installed.
func IsInstalled(serviceName string) bool {
	return isInstalledImpl(serviceName)
}

// Platform returns the current platform type.
func Platform() string {
	if IsSupported() {
		return runtime.GOOS
	}
	return "unsupported"
}

// IsSupported returns true if service installation is supported on this platform.
func IsSupported() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

// runCommand executes a command and returns combined output.
func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// IsInteractive returns true if the process is running interactively (not as a service).
// On Windows, this detects if running under the Service Control Manager.
// On Linux/macOS, this always returns true (systemd handles service mode differently).
func IsInteractive() bool {
	return isInteractiveImpl()
}

// RunAsService runs the given ServiceRunner as a Windows service.
// This should only be called when IsInteractive() returns false.
// On non-Windows platforms, this is a no-op that returns nil.
func RunAsService(name string, runner ServiceRunner) error {
	return runAsServiceImpl(name, runner)
}

// UserServiceInfo contains information about a user-level service installation.
type UserServiceInfo struct {
	Name       string // Service display name
	DLLPath    string // Path to the DLL (Windows only)
	ConfigPath string // Path to the config file
	LogPath    string // Path to the log file (Linux only)
}

// StopService stops a running system service without uninstalling it.
// On Linux: systemctl stop
// On macOS: launchctl stop
// On Windows: net stop
func StopService(serviceName string) error {
	return stopServiceImpl(serviceName)
}

// StartService starts an installed system service.
// On Linux: systemctl start
// On macOS: launchctl start (via kickstart)
// On Windows: net start
func StartService(serviceName string) error {
	return startServiceImpl(serviceName)
}

// UpdateServiceBinary copies a new binary to the service's install path.
// The service should be stopped before calling this.
func UpdateServiceBinary(serviceName, newBinaryPath string) error {
	return deployBinary(newBinaryPath, GetInstallPath(serviceName))
}

// GetUserServiceInfo returns information about the installed user-level service.
// Returns nil if no user service is installed.
func GetUserServiceInfo(serviceName string) *UserServiceInfo {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		return nil
	}
	if !IsUserInstalled(serviceName) {
		return nil
	}
	return getUserServiceInfoImpl(serviceName)
}
