//go:build darwin

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const launchdPlistPath = "/Library/LaunchDaemons"

// isRootImpl checks if running as root on macOS.
func isRootImpl() bool {
	return os.Getuid() == 0
}

// installImpl installs the service on macOS using launchd.
func installImpl(cfg ServiceConfig, execPath string) error {
	return installLaunchdService(cfg, execPath, false)
}

// installLaunchdService installs a launchd service with optional embedded config mode.
func installLaunchdService(cfg ServiceConfig, execPath string, embedded bool) error {
	plistName := "com." + cfg.Name + ".plist"
	plistPath := filepath.Join(launchdPlistPath, plistName)

	if _, err := os.Stat(plistPath); err == nil {
		return fmt.Errorf("service %s is already installed at %s", cfg.Name, plistPath)
	}

	plist := generateLaunchdPlist(cfg, execPath, embedded)

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("failed to write launchd plist file: %w", err)
	}

	fmt.Printf("Created launchd plist: %s\n", plistPath)

	label := "com." + cfg.Name
	if output, err := runCommand("launchctl", "load", "-w", plistPath); err != nil {
		os.Remove(plistPath)
		return fmt.Errorf("failed to load service: %s: %w", output, err)
	}

	fmt.Printf("Loaded service: %s\n", label)

	status, _ := statusImpl(cfg.Name)
	if status == "running" {
		fmt.Printf("Service is running\n")
	} else {
		fmt.Printf("Service status: %s\n", status)
	}

	return nil
}

// stopServiceImpl stops a running launchd service.
func stopServiceImpl(serviceName string) error {
	label := "com." + serviceName
	if output, err := runCommand("launchctl", "stop", label); err != nil {
		if !strings.Contains(output, "Could not find specified service") {
			return fmt.Errorf("failed to stop service: %s", strings.TrimSpace(output))
		}
	}
	return nil
}

// startServiceImpl starts a launchd service.
func startServiceImpl(serviceName string) error {
	label := "com." + serviceName
	if output, err := runCommand("launchctl", "start", label); err != nil {
		return fmt.Errorf("failed to start service: %s", strings.TrimSpace(output))
	}
	return nil
}

// uninstallImpl removes the launchd service on macOS.
func uninstallImpl(serviceName string) error {
	plistName := "com." + serviceName + ".plist"
	plistPath := filepath.Join(launchdPlistPath, plistName)

	// Check if installed
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return fmt.Errorf("service %s is not installed", serviceName)
	}

	label := "com." + serviceName

	// Unload the service (stop it first)
	if output, err := runCommand("launchctl", "unload", "-w", plistPath); err != nil {
		// Check if it's just not loaded
		if !strings.Contains(output, "Could not find specified service") {
			fmt.Printf("Note: could not unload service: %s\n", strings.TrimSpace(output))
		}
	} else {
		fmt.Printf("Unloaded service: %s\n", label)
	}

	// Remove the plist file
	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("failed to remove launchd plist file: %w", err)
	}

	fmt.Printf("Removed launchd plist: %s\n", plistPath)

	return nil
}

// statusImpl returns the service status on macOS.
func statusImpl(serviceName string) (string, error) {
	label := "com." + serviceName

	// Use launchctl list to check if service is loaded and running
	output, err := runCommand("launchctl", "list", label)
	if err != nil {
		// Service not loaded
		if strings.Contains(output, "Could not find service") {
			return "not installed", nil
		}
		return "unknown", nil
	}

	// Parse the output to determine status
	// launchctl list <label> outputs: PID, exit code, label
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == label {
			pid := fields[0]
			if pid == "-" {
				return "stopped", nil
			}
			return "running", nil
		}
	}

	// Alternative: check if the process is running
	output, _ = runCommand("launchctl", "print", "system/"+label)
	if strings.Contains(output, "state = running") {
		return "running", nil
	}
	if strings.Contains(output, "state = not running") {
		return "stopped", nil
	}

	return "loaded", nil
}

// isInstalledImpl checks if the service is installed on macOS.
func isInstalledImpl(serviceName string) bool {
	plistPath := filepath.Join(launchdPlistPath, "com."+serviceName+".plist")
	_, err := os.Stat(plistPath)
	return err == nil
}

// isInteractiveImpl always returns true on macOS.
// macOS uses launchd which manages the process lifecycle externally.
func isInteractiveImpl() bool {
	return true
}

// runAsServiceImpl is a no-op on macOS.
// macOS uses launchd which manages the process lifecycle externally.
func runAsServiceImpl(name string, runner ServiceRunner) error {
	// On macOS, launchd manages the service. The 'run' command just runs normally
	// and launchd handles start/stop/restart. No special service handler needed.
	return nil
}

// generateLaunchdPlist generates a launchd plist file.
// When embedded is true, no -c flag is included and logs go to /var/log.
func generateLaunchdPlist(cfg ServiceConfig, execPath string, embedded bool) string {
	label := "com." + cfg.Name

	var logPath, errPath string
	if embedded {
		logPath = "/var/log/" + cfg.Name + ".log"
		errPath = "/var/log/" + cfg.Name + ".err.log"
	} else {
		logPath = filepath.Join(cfg.WorkingDir, cfg.Name+".log")
		errPath = filepath.Join(cfg.WorkingDir, cfg.Name+".err.log")
	}

	var programArgs string
	if embedded {
		programArgs = fmt.Sprintf(`    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
    </array>`, execPath)
	} else {
		programArgs = fmt.Sprintf(`    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
        <string>-c</string>
        <string>%s</string>
    </array>`, execPath, cfg.ConfigPath)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>

%s

    <key>WorkingDirectory</key>
    <string>%s</string>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>

    <key>ThrottleInterval</key>
    <integer>5</integer>

    <key>StandardOutPath</key>
    <string>%s</string>

    <key>StandardErrorPath</key>
    <string>%s</string>

    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
`, label, programArgs, cfg.WorkingDir, logPath, errPath)
}

// installImplEmbedded installs launchd service for embedded config binary.
func installImplEmbedded(cfg ServiceConfig, execPath string) error {
	return installLaunchdService(cfg, execPath, true)
}

// =============================================================================
// User service stubs (not supported on macOS - use launchd instead)
// =============================================================================

// hasCrontab returns false on macOS (user service not supported).
func hasCrontab() bool {
	return false
}

// ErrCrontabNotFound is returned when crontab is not available.
var ErrCrontabNotFound = errors.New("user service installation is only supported on Linux")

// installUserImpl is not supported on macOS.
func installUserImpl(cfg ServiceConfig, execPath, method string) error {
	return fmt.Errorf("user service installation is only supported on Linux")
}

// uninstallUserImpl is not supported on macOS.
func uninstallUserImpl(serviceName string) error {
	return fmt.Errorf("user service is only supported on Linux")
}

// statusUserImpl is not supported on macOS.
func statusUserImpl(serviceName string) (string, error) {
	return "", fmt.Errorf("user service is only supported on Linux")
}

// startUserImpl is not supported on macOS.
func startUserImpl(serviceName string) error {
	return fmt.Errorf("user service is only supported on Linux")
}

// stopUserImpl is not supported on macOS.
func stopUserImpl(serviceName string) error {
	return fmt.Errorf("user service is only supported on Linux")
}

// isUserInstalledImpl returns false on macOS.
func isUserInstalledImpl(serviceName string) bool {
	return false
}

// installUserWithDLLImpl is not supported on macOS.
func installUserWithDLLImpl(serviceName, dllPath, configPath string) error {
	return fmt.Errorf("DLL-based user service is only supported on Windows")
}

// getUserServiceInfoImpl returns nil on macOS (user service not supported).
func getUserServiceInfoImpl(serviceName string) *UserServiceInfo {
	return nil
}
