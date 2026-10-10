//go:build !linux && !windows && !darwin

package service

import (
	"errors"
	"fmt"
)

// isRootImpl checks if running as root (unsupported platforms).
func isRootImpl() bool {
	return false
}

// installImpl is not supported on this platform.
func installImpl(cfg ServiceConfig, execPath string) error {
	return fmt.Errorf("service installation is not supported on this platform")
}

// installImplEmbedded is not supported on this platform.
func installImplEmbedded(cfg ServiceConfig, execPath string) error {
	return fmt.Errorf("service installation is not supported on this platform")
}

// uninstallImpl is not supported on this platform.
func uninstallImpl(serviceName string) error {
	return fmt.Errorf("service uninstallation is not supported on this platform")
}

// statusImpl is not supported on this platform.
func statusImpl(serviceName string) (string, error) {
	return "", fmt.Errorf("service status is not supported on this platform")
}

// isInstalledImpl is not supported on this platform.
func isInstalledImpl(serviceName string) bool {
	return false
}

// isInteractiveImpl always returns true on unsupported platforms.
func isInteractiveImpl() bool {
	return true
}

// runAsServiceImpl is not supported on this platform.
func runAsServiceImpl(name string, runner ServiceRunner) error {
	return fmt.Errorf("running as service is not supported on this platform")
}

// stopServiceImpl is not supported on this platform.
func stopServiceImpl(serviceName string) error {
	return fmt.Errorf("service management is not supported on this platform")
}

// startServiceImpl is not supported on this platform.
func startServiceImpl(serviceName string) error {
	return fmt.Errorf("service management is not supported on this platform")
}

// =============================================================================
// User service stubs (not supported on this platform)
// =============================================================================

// hasCrontab returns false on unsupported platforms.
func hasCrontab() bool {
	return false
}

// ErrCrontabNotFound is returned when crontab is not available.
var ErrCrontabNotFound = errors.New("user service installation is only supported on Linux")

// installUserImpl is not supported on this platform.
func installUserImpl(cfg ServiceConfig, execPath, method string) error {
	return fmt.Errorf("user service installation is only supported on Linux")
}

// uninstallUserImpl is not supported on this platform.
func uninstallUserImpl(serviceName string) error {
	return fmt.Errorf("user service is only supported on Linux")
}

// statusUserImpl is not supported on this platform.
func statusUserImpl(serviceName string) (string, error) {
	return "", fmt.Errorf("user service is only supported on Linux")
}

// startUserImpl is not supported on this platform.
func startUserImpl(serviceName string) error {
	return fmt.Errorf("user service is only supported on Linux")
}

// stopUserImpl is not supported on this platform.
func stopUserImpl(serviceName string) error {
	return fmt.Errorf("user service is only supported on Linux")
}

// isUserInstalledImpl returns false on unsupported platforms.
func isUserInstalledImpl(serviceName string) bool {
	return false
}

// installUserWithDLLImpl is not supported on this platform.
func installUserWithDLLImpl(serviceName, dllPath, configPath string) error {
	return fmt.Errorf("DLL-based user service is only supported on Windows")
}

// getUserServiceInfoImpl returns nil on unsupported platforms.
func getUserServiceInfoImpl(serviceName string) *UserServiceInfo {
	return nil
}
