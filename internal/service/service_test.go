package service

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDeriveNamesDefault(t *testing.T) {
	names := DeriveNames("muti-metroo")

	if names.DirName != ".muti-metroo" {
		t.Errorf("DirName = %q, want %q", names.DirName, ".muti-metroo")
	}
	if names.PIDFileName != "muti-metroo.pid" {
		t.Errorf("PIDFileName = %q, want %q", names.PIDFileName, "muti-metroo.pid")
	}
	if names.LogFileName != "muti-metroo.log" {
		t.Errorf("LogFileName = %q, want %q", names.LogFileName, "muti-metroo.log")
	}
	if names.ScriptName != "muti-metroo.sh" {
		t.Errorf("ScriptName = %q, want %q", names.ScriptName, "muti-metroo.sh")
	}
	if names.CronMarker != "# muti-metroo-cron" {
		t.Errorf("CronMarker = %q, want %q", names.CronMarker, "# muti-metroo-cron")
	}
	if names.TaskName != "MutiMetrooStart" {
		t.Errorf("TaskName = %q, want %q", names.TaskName, "MutiMetrooStart")
	}
	if names.RegistryValue != "MutiMetroo" {
		t.Errorf("RegistryValue = %q, want %q", names.RegistryValue, "MutiMetroo")
	}
}

func TestDeriveNamesCustom(t *testing.T) {
	names := DeriveNames("my-tunnel")

	if names.DirName != ".my-tunnel" {
		t.Errorf("DirName = %q, want %q", names.DirName, ".my-tunnel")
	}
	if names.PIDFileName != "my-tunnel.pid" {
		t.Errorf("PIDFileName = %q, want %q", names.PIDFileName, "my-tunnel.pid")
	}
	if names.LogFileName != "my-tunnel.log" {
		t.Errorf("LogFileName = %q, want %q", names.LogFileName, "my-tunnel.log")
	}
	if names.ScriptName != "my-tunnel.sh" {
		t.Errorf("ScriptName = %q, want %q", names.ScriptName, "my-tunnel.sh")
	}
	if names.CronMarker != "# my-tunnel-cron" {
		t.Errorf("CronMarker = %q, want %q", names.CronMarker, "# my-tunnel-cron")
	}
	if names.TaskName != "MyTunnelStart" {
		t.Errorf("TaskName = %q, want %q", names.TaskName, "MyTunnelStart")
	}
	if names.RegistryValue != "MyTunnel" {
		t.Errorf("RegistryValue = %q, want %q", names.RegistryValue, "MyTunnel")
	}
}

func TestToRegistryValueName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"muti-metroo", "MutiMetroo"},
		{"My Tunnel", "MyTunnel"},
		{"Tunnel  Manager", "TunnelManager"},
		{"my_tunnel_service", "MyTunnelService"},
		{"my-tunnel_service name", "MyTunnelServiceName"},
		{"MyTunnel", "Mytunnel"},
		{"MYTUNNEL", "Mytunnel"},
		{"tunnel", "Tunnel"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ToRegistryValueName(tt.input)
			if result != tt.expected {
				t.Errorf("ToRegistryValueName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	configPath := "/path/to/config.yaml"
	cfg := DefaultConfig(configPath)

	if cfg.Name != "muti-metroo" {
		t.Errorf("Name = %q, want %q", cfg.Name, "muti-metroo")
	}

	if cfg.DisplayName != "Muti Metroo Mesh Agent" {
		t.Errorf("DisplayName = %q, want %q", cfg.DisplayName, "Muti Metroo Mesh Agent")
	}

	if cfg.Description != "Userspace mesh networking agent for virtual TCP tunnels" {
		t.Errorf("Description = %q, want %q", cfg.Description, "Userspace mesh networking agent for virtual TCP tunnels")
	}

	// ConfigPath should be absolute
	if !filepath.IsAbs(cfg.ConfigPath) {
		t.Errorf("ConfigPath = %q, should be absolute", cfg.ConfigPath)
	}

	// WorkingDir should be the directory of the config file
	expectedDir := filepath.Dir(cfg.ConfigPath)
	if cfg.WorkingDir != expectedDir {
		t.Errorf("WorkingDir = %q, want %q", cfg.WorkingDir, expectedDir)
	}

	// User and Group should be empty by default
	if cfg.User != "" {
		t.Errorf("User = %q, want empty", cfg.User)
	}
	if cfg.Group != "" {
		t.Errorf("Group = %q, want empty", cfg.Group)
	}
}

func TestDefaultConfigRelativePath(t *testing.T) {
	// Test with relative path
	cfg := DefaultConfig("./config.yaml")

	// ConfigPath should still be made absolute
	if !filepath.IsAbs(cfg.ConfigPath) {
		t.Errorf("ConfigPath = %q, should be absolute", cfg.ConfigPath)
	}
}

func TestServiceConfigFields(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "test-service",
		DisplayName: "Test Service",
		Description: "A test service",
		ConfigPath:  "/etc/test/config.yaml",
		WorkingDir:  "/etc/test",
		User:        "testuser",
		Group:       "testgroup",
	}

	if cfg.Name != "test-service" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test-service")
	}
	if cfg.DisplayName != "Test Service" {
		t.Errorf("DisplayName = %q, want %q", cfg.DisplayName, "Test Service")
	}
	if cfg.Description != "A test service" {
		t.Errorf("Description = %q, want %q", cfg.Description, "A test service")
	}
	if cfg.ConfigPath != "/etc/test/config.yaml" {
		t.Errorf("ConfigPath = %q, want %q", cfg.ConfigPath, "/etc/test/config.yaml")
	}
	if cfg.WorkingDir != "/etc/test" {
		t.Errorf("WorkingDir = %q, want %q", cfg.WorkingDir, "/etc/test")
	}
	if cfg.User != "testuser" {
		t.Errorf("User = %q, want %q", cfg.User, "testuser")
	}
	if cfg.Group != "testgroup" {
		t.Errorf("Group = %q, want %q", cfg.Group, "testgroup")
	}
}

func TestPlatform(t *testing.T) {
	platform := Platform()

	switch runtime.GOOS {
	case "linux":
		if platform != "linux" {
			t.Errorf("Platform() = %q, want %q on Linux", platform, "linux")
		}
	case "windows":
		if platform != "windows" {
			t.Errorf("Platform() = %q, want %q on Windows", platform, "windows")
		}
	case "darwin":
		if platform != "darwin" {
			t.Errorf("Platform() = %q, want %q on macOS", platform, "darwin")
		}
	default:
		if platform != "unsupported" {
			t.Errorf("Platform() = %q, want %q on unsupported OS", platform, "unsupported")
		}
	}
}

func TestIsSupported(t *testing.T) {
	supported := IsSupported()

	switch runtime.GOOS {
	case "linux", "windows", "darwin":
		if !supported {
			t.Errorf("IsSupported() = false, want true on %s", runtime.GOOS)
		}
	default:
		if supported {
			t.Errorf("IsSupported() = true, want false on %s", runtime.GOOS)
		}
	}
}

func TestIsRoot(t *testing.T) {
	// On non-Linux/non-Windows platforms, IsRoot should return false
	// On Linux/Windows, it depends on actual privileges
	isRoot := IsRoot()

	// We can't assert the exact value since it depends on test environment,
	// but we can verify it returns a boolean without panicking
	_ = isRoot
}

func TestIsInstalled(t *testing.T) {
	// Test with a service name that definitely doesn't exist
	installed := IsInstalled("definitely-not-installed-service-12345")

	// Should return false for a non-existent service
	if installed {
		t.Error("IsInstalled() = true for non-existent service, want false")
	}
}

func TestStatusNonExistent(t *testing.T) {
	// Test status of a non-existent service
	status, err := Status("definitely-not-installed-service-12345")

	// Behavior depends on platform
	switch runtime.GOOS {
	case "linux":
		// On Linux, should return "inactive" or "unknown" without error,
		// or an error if systemctl fails
		if err == nil {
			if status != "inactive" && status != "unknown" {
				t.Errorf("Status() = %q, expected 'inactive' or 'unknown'", status)
			}
		}
	case "darwin":
		// On macOS, should return "not installed" or "unknown" without error
		if err == nil {
			if status != "not installed" && status != "unknown" {
				t.Errorf("Status() = %q, expected 'not installed' or 'unknown'", status)
			}
		}
	default:
		// On unsupported platforms, should return an error
		if err == nil {
			t.Error("Status() should return error on unsupported platform")
		}
	}
}

func TestInstallWithoutRoot(t *testing.T) {
	// Skip if running as root (unlikely in test environment)
	if IsRoot() {
		t.Skip("Skipping test that requires non-root user")
	}

	cfg := DefaultConfig("/tmp/test-config.yaml")
	err := Install(cfg)

	if err == nil {
		t.Error("Install() should return error when not running as root")
	}

	if err.Error() != "must run as root/administrator to install service" {
		t.Errorf("Install() error = %q, want root/administrator error", err.Error())
	}
}

func TestUninstallWithoutRoot(t *testing.T) {
	// Skip if running as root (unlikely in test environment)
	if IsRoot() {
		t.Skip("Skipping test that requires non-root user")
	}

	err := Uninstall("test-service")

	if err == nil {
		t.Error("Uninstall() should return error when not running as root")
	}

	if err.Error() != "must run as root/administrator to uninstall service" {
		t.Errorf("Uninstall() error = %q, want root/administrator error", err.Error())
	}
}

// =============================================================================
// User Service Tests (Public API)
// =============================================================================

func TestIsUserInstalled(t *testing.T) {
	// Test that IsUserInstalled returns a boolean without panicking
	result := IsUserInstalled("muti-metroo")
	_ = result // Result depends on system state
}

func TestIsUserInstalledConsistent(t *testing.T) {
	// Test that IsUserInstalled returns consistent values
	result1 := IsUserInstalled("muti-metroo")
	result2 := IsUserInstalled("muti-metroo")

	if result1 != result2 {
		t.Error("IsUserInstalled() returned inconsistent results")
	}
}

func TestStatusUserNotInstalled(t *testing.T) {
	// Skip on non-Linux platforms (user service is Linux-only)
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux-only test on " + runtime.GOOS)
	}

	// Skip if user service is installed
	if IsUserInstalled("muti-metroo") {
		t.Skip("Skipping test because user service is installed")
	}

	status, err := StatusUser("muti-metroo")
	if err != nil {
		t.Fatalf("StatusUser() error: %v", err)
	}

	if status != "not installed" {
		t.Errorf("StatusUser() = %q, want %q", status, "not installed")
	}
}

func TestInstallUserUnsupportedPlatform(t *testing.T) {
	// Skip on Linux and Windows (they support user service)
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("Skipping unsupported platform test on " + runtime.GOOS)
	}

	cfg := DefaultConfig("/tmp/test-config.yaml")
	err := InstallUser(cfg)

	// Should return an error on unsupported platforms (e.g., macOS)
	if err == nil {
		t.Error("InstallUser() should return error on unsupported platform")
	}
}

func TestUninstallUserUnsupportedPlatform(t *testing.T) {
	// Skip on Linux and Windows (they support user service)
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("Skipping unsupported platform test on " + runtime.GOOS)
	}

	err := UninstallUser("muti-metroo")

	// Should return an error on unsupported platforms (e.g., macOS)
	if err == nil {
		t.Error("UninstallUser() should return error on unsupported platform")
	}
}

func TestStartUserUnsupportedPlatform(t *testing.T) {
	// Skip on Linux and Windows (they support user service)
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("Skipping unsupported platform test on " + runtime.GOOS)
	}

	err := StartUser("muti-metroo")

	// Should return an error on unsupported platforms (e.g., macOS)
	if err == nil {
		t.Error("StartUser() should return error on unsupported platform")
	}
}

func TestStopUserUnsupportedPlatform(t *testing.T) {
	// Skip on Linux and Windows (they support user service)
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("Skipping unsupported platform test on " + runtime.GOOS)
	}

	err := StopUser("muti-metroo")

	// Should return an error on unsupported platforms (e.g., macOS)
	if err == nil {
		t.Error("StopUser() should return error on unsupported platform")
	}
}

func TestInstallUserWindowsUnsupportedPlatform(t *testing.T) {
	// Skip on Windows (it supports this function)
	if runtime.GOOS == "windows" {
		t.Skip("Skipping unsupported platform test on " + runtime.GOOS)
	}

	err := InstallUserWindows("test-service", "/path/to/dll", "/path/to/config")

	if err == nil {
		t.Error("InstallUserWindows() should return error on unsupported platform")
	}

	expected := "InstallUserWindows is only supported on Windows"
	if err.Error() != expected {
		t.Errorf("InstallUserWindows() error = %q, want %q", err.Error(), expected)
	}
}

func TestStatusUserUnsupportedPlatform(t *testing.T) {
	// Skip on Linux and Windows (they support user service)
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		t.Skip("Skipping unsupported platform test on " + runtime.GOOS)
	}

	_, err := StatusUser("muti-metroo")

	if err == nil {
		t.Error("StatusUser() should return error on unsupported platform")
	}
}

func TestGetUserServiceInfoNotInstalled(t *testing.T) {
	// Skip on platforms where user service might be installed
	if IsUserInstalled("muti-metroo") {
		t.Skip("Skipping test because user service is installed")
	}

	info := GetUserServiceInfo("muti-metroo")

	// Should return nil when no user service is installed
	if info != nil {
		t.Error("GetUserServiceInfo() should return nil when not installed")
	}
}

func TestUserServiceInfoStruct(t *testing.T) {
	// Test that UserServiceInfo struct has expected fields
	info := UserServiceInfo{
		Name:       "test-service",
		DLLPath:    "C:\\path\\to\\dll",
		ConfigPath: "/path/to/config",
		LogPath:    "/path/to/log",
	}

	if info.Name != "test-service" {
		t.Errorf("Name = %q, want %q", info.Name, "test-service")
	}
	if info.DLLPath != "C:\\path\\to\\dll" {
		t.Errorf("DLLPath = %q, want %q", info.DLLPath, "C:\\path\\to\\dll")
	}
	if info.ConfigPath != "/path/to/config" {
		t.Errorf("ConfigPath = %q, want %q", info.ConfigPath, "/path/to/config")
	}
	if info.LogPath != "/path/to/log" {
		t.Errorf("LogPath = %q, want %q", info.LogPath, "/path/to/log")
	}
}
