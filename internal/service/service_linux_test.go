//go:build linux

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		DisplayName: "Muti Metroo Mesh Agent",
		Description: "Userspace mesh networking agent",
		ConfigPath:  "/etc/muti-metroo/config.yaml",
		WorkingDir:  "/etc/muti-metroo",
	}
	execPath := "/usr/local/bin/muti-metroo"

	unit := generateSystemdUnit(cfg, execPath, false)

	// Check that required sections exist
	if !strings.Contains(unit, "[Unit]") {
		t.Error("Unit file missing [Unit] section")
	}
	if !strings.Contains(unit, "[Service]") {
		t.Error("Unit file missing [Service] section")
	}
	if !strings.Contains(unit, "[Install]") {
		t.Error("Unit file missing [Install] section")
	}

	// Check description
	if !strings.Contains(unit, "Description=Userspace mesh networking agent") {
		t.Error("Unit file missing description")
	}

	// Check ExecStart
	expectedExec := "ExecStart=/usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml"
	if !strings.Contains(unit, expectedExec) {
		t.Errorf("Unit file missing ExecStart, expected: %s", expectedExec)
	}

	// Check working directory
	if !strings.Contains(unit, "WorkingDirectory=/etc/muti-metroo") {
		t.Error("Unit file missing WorkingDirectory")
	}

	// Check security settings
	if !strings.Contains(unit, "NoNewPrivileges=true") {
		t.Error("Unit file missing NoNewPrivileges security setting")
	}
	if !strings.Contains(unit, "ProtectSystem=strict") {
		t.Error("Unit file missing ProtectSystem security setting")
	}
	if !strings.Contains(unit, "PrivateTmp=true") {
		t.Error("Unit file missing PrivateTmp security setting")
	}

	// Check restart settings
	if !strings.Contains(unit, "Restart=on-failure") {
		t.Error("Unit file missing Restart setting")
	}
	if !strings.Contains(unit, "RestartSec=5") {
		t.Error("Unit file missing RestartSec setting")
	}

	// Check logging
	if !strings.Contains(unit, "StandardOutput=journal") {
		t.Error("Unit file missing StandardOutput setting")
	}
	if !strings.Contains(unit, "SyslogIdentifier=muti-metroo") {
		t.Error("Unit file missing SyslogIdentifier")
	}

	// Check installation target
	if !strings.Contains(unit, "WantedBy=multi-user.target") {
		t.Error("Unit file missing WantedBy setting")
	}

	// Check network dependency
	if !strings.Contains(unit, "After=network-online.target") {
		t.Error("Unit file missing network dependency")
	}
}

func TestGenerateSystemdUnitWithUser(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		Description: "Test service",
		ConfigPath:  "/etc/config.yaml",
		WorkingDir:  "/etc",
		User:        "muti-metroo",
		Group:       "muti-metroo",
	}
	execPath := "/usr/bin/muti-metroo"

	unit := generateSystemdUnit(cfg, execPath, false)

	// Check User setting
	if !strings.Contains(unit, "User=muti-metroo") {
		t.Error("Unit file missing User setting when User is specified")
	}

	// Check Group setting
	if !strings.Contains(unit, "Group=muti-metroo") {
		t.Error("Unit file missing Group setting when Group is specified")
	}
}

func TestGenerateSystemdUnitWithoutUser(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		Description: "Test service",
		ConfigPath:  "/etc/config.yaml",
		WorkingDir:  "/etc",
		// User and Group are empty
	}
	execPath := "/usr/bin/muti-metroo"

	unit := generateSystemdUnit(cfg, execPath, false)

	// Should not contain User= or Group= lines when empty
	if strings.Contains(unit, "User=") {
		t.Error("Unit file should not contain User= when User is empty")
	}
	if strings.Contains(unit, "Group=") {
		t.Error("Unit file should not contain Group= when Group is empty")
	}
}

func TestGenerateSystemdUnitEmbedded(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		Description: "Test service",
		ConfigPath:  "/etc/config.yaml",
		WorkingDir:  "/etc",
	}
	execPath := "/usr/bin/muti-metroo"

	unit := generateSystemdUnit(cfg, execPath, true)

	// ExecStart should NOT include -c flag for embedded mode
	if strings.Contains(unit, "-c") {
		t.Error("Embedded unit file should not contain -c flag")
	}

	// ExecStart should be just "run" without config path
	expectedExec := "ExecStart=/usr/bin/muti-metroo run"
	if !strings.Contains(unit, expectedExec) {
		t.Errorf("Embedded unit file missing ExecStart, expected: %s", expectedExec)
	}
}

func TestIsRootImplLinux(t *testing.T) {
	// Test that isRootImpl returns a consistent value
	result1 := isRootImpl()
	result2 := isRootImpl()

	if result1 != result2 {
		t.Error("isRootImpl() returned inconsistent results")
	}
}

// =============================================================================
// systemd --user Tests
// =============================================================================

func TestGenerateSystemdUserUnit(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		Description: "Userspace mesh networking agent",
		ConfigPath:  "/home/alice/muti-metroo/config.yaml",
		WorkingDir:  "/home/alice/muti-metroo",
	}
	execPath := "/home/alice/.local/bin/muti-metroo"

	unit := generateSystemdUserUnit(cfg, execPath, false)

	if !strings.Contains(unit, "Description=Userspace mesh networking agent") {
		t.Error("user unit missing Description")
	}
	if !strings.Contains(unit, "ExecStart=/home/alice/.local/bin/muti-metroo run -c /home/alice/muti-metroo/config.yaml") {
		t.Errorf("user unit missing ExecStart, got:\n%s", unit)
	}
	if !strings.Contains(unit, "WorkingDirectory=/home/alice/muti-metroo") {
		t.Error("user unit missing WorkingDirectory")
	}
	if !strings.Contains(unit, "WantedBy=default.target") {
		t.Error("user unit should target default.target, not multi-user.target")
	}
	if strings.Contains(unit, "WantedBy=multi-user.target") {
		t.Error("user unit must not use multi-user.target")
	}
	// User units must not carry User=/Group= directives.
	if strings.Contains(unit, "User=") {
		t.Error("user unit should not contain User= directive")
	}
	if strings.Contains(unit, "Group=") {
		t.Error("user unit should not contain Group= directive")
	}
	// Hardening flags that still apply to user units.
	if !strings.Contains(unit, "NoNewPrivileges=true") {
		t.Error("user unit missing NoNewPrivileges")
	}
	if !strings.Contains(unit, "PrivateTmp=true") {
		t.Error("user unit missing PrivateTmp")
	}
	// We deliberately drop ProtectSystem=strict and ProtectHome for user units.
	if strings.Contains(unit, "ProtectSystem=strict") {
		t.Error("user unit must not use ProtectSystem=strict (incompatible with user services)")
	}
	if strings.Contains(unit, "ProtectHome=") {
		t.Error("user unit must not set ProtectHome (the unit lives under ~/.config)")
	}
	// Logging
	if !strings.Contains(unit, "StandardOutput=journal") {
		t.Error("user unit missing journald StandardOutput")
	}
	if !strings.Contains(unit, "SyslogIdentifier=muti-metroo") {
		t.Error("user unit missing SyslogIdentifier")
	}
	if !strings.Contains(unit, "Restart=on-failure") {
		t.Error("user unit missing Restart=on-failure")
	}
}

func TestGenerateSystemdUserUnitEmbedded(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		Description: "Test",
		ConfigPath:  "/etc/cfg.yaml",
		WorkingDir:  "/etc",
	}
	unit := generateSystemdUserUnit(cfg, "/usr/bin/muti-metroo", true)

	// Embedded mode means the binary contains its own config; no -c flag.
	if strings.Contains(unit, "-c") {
		t.Error("embedded user unit must not contain -c flag")
	}
	if !strings.Contains(unit, "ExecStart=/usr/bin/muti-metroo run") {
		t.Errorf("embedded user unit missing ExecStart, got:\n%s", unit)
	}
}

func TestGenerateSystemdUserUnitWithDataDir(t *testing.T) {
	cfg := ServiceConfig{
		Name:        "muti-metroo",
		Description: "Test",
		ConfigPath:  "/home/alice/muti-metroo/config.yaml",
		WorkingDir:  "/home/alice/muti-metroo",
		DataDir:     "/var/lib/muti-metroo",
	}
	unit := generateSystemdUserUnit(cfg, "/usr/bin/muti-metroo", false)

	// DataDir should be appended to ReadWritePaths so the agent can write to it.
	if !strings.Contains(unit, "ReadWritePaths=/home/alice/muti-metroo /var/lib/muti-metroo") {
		t.Errorf("user unit missing combined ReadWritePaths, got:\n%s", unit)
	}
}

func TestGetSystemdUserUnitPath(t *testing.T) {
	path, err := getSystemdUserUnitPath("muti-metroo")
	if err != nil {
		t.Fatalf("getSystemdUserUnitPath() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "systemd", "user", "muti-metroo.service")
	if path != expected {
		t.Errorf("getSystemdUserUnitPath() = %q, want %q", path, expected)
	}
}

// TestDetectUserMethod exercises the install-state detection logic by
// pointing HOME at a temp dir so we don't touch the developer's real
// ~/.config/systemd/user/. The crontab side is left to the existing
// cron integration tests since stubbing crontab cleanly requires a real
// crontab binary.
func TestDetectUserMethod(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	const svc = "muti-metroo-test-detect"

	// Neither backend installed.
	if got := detectUserMethod(svc); got != "" {
		t.Errorf("detectUserMethod with neither installed = %q, want \"\"", got)
	}

	// Drop a fake systemd-user unit file in place.
	unitDir := filepath.Join(tmpHome, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", unitDir, err)
	}
	unitPath := filepath.Join(unitDir, svc+".service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	defer os.Remove(unitPath)

	if got := detectUserMethod(svc); got != UserMethodSystemd {
		t.Errorf("detectUserMethod with systemd unit = %q, want %q", got, UserMethodSystemd)
	}

	// isUserSystemdInstalled should agree.
	if !isUserSystemdInstalled(svc) {
		t.Error("isUserSystemdInstalled = false, want true")
	}

	// Removing the unit returns us to the unconfigured state.
	os.Remove(unitPath)
	if got := detectUserMethod(svc); got != "" {
		t.Errorf("detectUserMethod after removal = %q, want \"\"", got)
	}
}

func TestHasSystemdUserAvailable(t *testing.T) {
	// Just verify the call returns a bool without panicking. The result
	// depends on the host environment (CI vs developer Mac vs Linux).
	_ = hasSystemdUserAvailable()
}

// TestSystemdUserLifecycle runs the full systemd --user install/uninstall
// lifecycle. Only runs when explicitly enabled, since it manipulates the
// user's actual systemd state.
func TestSystemdUserLifecycle(t *testing.T) {
	if os.Getenv("TEST_SYSTEMD_USER_INTEGRATION") != "1" {
		t.Skip("set TEST_SYSTEMD_USER_INTEGRATION=1 to run this test")
	}
	if !hasSystemdUserAvailable() {
		t.Skip("systemd --user not available in this environment")
	}

	const svc = "muti-metroo-systemd-user-test"
	if isUserSystemdInstalled(svc) {
		t.Skipf("user unit %q already installed; refusing to clobber", svc)
	}

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte("agent:\n  data_dir: "+tmp+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	binary := filepath.Join(tmp, "fake-muti-metroo")
	// A wrapper that just sleeps is enough for systemctl to consider the
	// unit started.
	if err := os.WriteFile(binary, []byte("#!/bin/bash\nsleep 60"), 0755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	cfg := ServiceConfig{
		Name:        svc,
		Description: "Integration test unit",
		ConfigPath:  configPath,
		WorkingDir:  tmp,
	}

	defer func() {
		// Best-effort cleanup whether or not the test passed.
		_ = uninstallUserSystemdImpl(svc)
	}()

	if err := installUserSystemdImpl(cfg, binary); err != nil {
		t.Fatalf("installUserSystemdImpl error: %v", err)
	}

	if !isUserSystemdInstalled(svc) {
		t.Error("isUserSystemdInstalled = false after install")
	}

	status, err := statusUserSystemdImpl(svc)
	if err != nil {
		t.Fatalf("statusUserSystemdImpl error: %v", err)
	}
	if !strings.Contains(status, "systemd-user") {
		t.Errorf("status string %q should mention systemd-user", status)
	}

	if err := uninstallUserSystemdImpl(svc); err != nil {
		t.Fatalf("uninstallUserSystemdImpl error: %v", err)
	}
	if isUserSystemdInstalled(svc) {
		t.Error("isUserSystemdInstalled = true after uninstall")
	}
}

// =============================================================================
// Cron Service Tests
// =============================================================================

func TestGetCronServiceDir(t *testing.T) {
	serviceDir, err := getCronServiceDir("muti-metroo")
	if err != nil {
		t.Fatalf("getCronServiceDir() error: %v", err)
	}

	// Should return path in home directory
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error: %v", err)
	}

	expected := filepath.Join(home, ".muti-metroo")
	if serviceDir != expected {
		t.Errorf("getCronServiceDir() = %q, want %q", serviceDir, expected)
	}

	// Path should be absolute
	if !filepath.IsAbs(serviceDir) {
		t.Errorf("getCronServiceDir() returned non-absolute path: %q", serviceDir)
	}
}

func TestGetCronServiceDirCustom(t *testing.T) {
	serviceDir, err := getCronServiceDir("my-tunnel")
	if err != nil {
		t.Fatalf("getCronServiceDir() error: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error: %v", err)
	}

	expected := filepath.Join(home, ".my-tunnel")
	if serviceDir != expected {
		t.Errorf("getCronServiceDir('my-tunnel') = %q, want %q", serviceDir, expected)
	}
}

func TestGenerateCronScript(t *testing.T) {
	configPath := "/home/testuser/config.yaml"
	binaryPath := "/usr/local/bin/muti-metroo"
	serviceDir := "/home/testuser/.muti-metroo"

	script := generateCronScript(configPath, binaryPath, serviceDir, "muti-metroo")

	// Check shebang
	if !strings.HasPrefix(script, "#!/bin/bash") {
		t.Error("Script missing bash shebang")
	}

	// Check header comment
	if !strings.Contains(script, "muti-metroo user service wrapper") {
		t.Error("Script missing header comment")
	}

	// Check PIDFILE variable
	expectedPIDFile := `PIDFILE="/home/testuser/.muti-metroo/muti-metroo.pid"`
	if !strings.Contains(script, expectedPIDFile) {
		t.Errorf("Script missing PIDFILE, expected: %s", expectedPIDFile)
	}

	// Check LOGFILE variable
	expectedLogFile := `LOGFILE="/home/testuser/.muti-metroo/muti-metroo.log"`
	if !strings.Contains(script, expectedLogFile) {
		t.Errorf("Script missing LOGFILE, expected: %s", expectedLogFile)
	}

	// Check CONFIG variable
	expectedConfig := `CONFIG="/home/testuser/config.yaml"`
	if !strings.Contains(script, expectedConfig) {
		t.Errorf("Script missing CONFIG, expected: %s", expectedConfig)
	}

	// Check BINARY variable
	expectedBinary := `BINARY="/usr/local/bin/muti-metroo"`
	if !strings.Contains(script, expectedBinary) {
		t.Errorf("Script missing BINARY, expected: %s", expectedBinary)
	}

	// Check "already running" logic
	if !strings.Contains(script, "kill -0") {
		t.Error("Script missing process check (kill -0)")
	}
	if !strings.Contains(script, "Already running") {
		t.Error("Script missing 'Already running' message")
	}

	// Check nohup start command
	if !strings.Contains(script, "nohup \"$BINARY\" run -c \"$CONFIG\"") {
		t.Error("Script missing nohup start command")
	}

	// Check PID capture
	if !strings.Contains(script, "echo $! > \"$PIDFILE\"") {
		t.Error("Script missing PID capture")
	}

	// Check working directory change
	if !strings.Contains(script, "cd \"$(dirname \"$CONFIG\")\"") {
		t.Error("Script missing directory change")
	}
}

func TestGenerateCronScriptWithSpaces(t *testing.T) {
	// Test with paths containing spaces
	configPath := "/home/test user/my config.yaml"
	binaryPath := "/usr/local/bin/muti-metroo"
	serviceDir := "/home/test user/.muti-metroo"

	script := generateCronScript(configPath, binaryPath, serviceDir, "muti-metroo")

	// Paths should be quoted in the script
	if !strings.Contains(script, `CONFIG="/home/test user/my config.yaml"`) {
		t.Error("Script should handle spaces in config path")
	}
	if !strings.Contains(script, `BINARY="/usr/local/bin/muti-metroo"`) {
		t.Error("Script should handle spaces in binary path")
	}
}

func TestHasCrontab(t *testing.T) {
	// Test that hasCrontab returns a boolean without panicking
	result := hasCrontab()
	_ = result // Result depends on system, just verify it doesn't panic
}

func TestErrCrontabNotFound(t *testing.T) {
	if ErrCrontabNotFound == nil {
		t.Error("ErrCrontabNotFound should not be nil")
	}
	if !strings.Contains(ErrCrontabNotFound.Error(), "crontab") {
		t.Error("ErrCrontabNotFound should mention crontab")
	}
}

func TestStatusUserImplNotInstalled(t *testing.T) {
	// Skip if running as root or if cron service is already installed
	if isUserInstalledImpl("muti-metroo") {
		t.Skip("Skipping test because user service is installed")
	}

	status, err := statusUserImpl("muti-metroo")
	if err != nil {
		t.Fatalf("statusUserImpl() error: %v", err)
	}

	if status != "not installed" {
		t.Errorf("statusUserImpl() = %q, want %q", status, "not installed")
	}
}

func TestIsUserInstalledImplConsistent(t *testing.T) {
	// Test that isUserInstalledImpl returns consistent values
	result1 := isUserInstalledImpl("muti-metroo")
	result2 := isUserInstalledImpl("muti-metroo")

	if result1 != result2 {
		t.Error("isUserInstalledImpl() returned inconsistent results")
	}
}

// TestCronEntryIntegration tests the full cron entry add/remove cycle.
// This test modifies the user's crontab and should only run in CI or isolated environments.
func TestCronEntryIntegration(t *testing.T) {
	testServiceName := "muti-metroo-test"

	// Skip if crontab is not available
	if !hasCrontab() {
		t.Skip("crontab not available")
	}

	// Skip if already installed (don't interfere with real installation)
	if isUserInstalledImpl(testServiceName) {
		t.Skip("user service already installed")
	}

	// Skip unless explicitly enabled
	if os.Getenv("TEST_CRON_INTEGRATION") != "1" {
		t.Skip("set TEST_CRON_INTEGRATION=1 to run this test")
	}

	// Create a temporary script path for testing
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "test-script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho test"), 0755); err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	// Test addCronEntry
	if err := addCronEntry(scriptPath, testServiceName); err != nil {
		t.Fatalf("addCronEntry() error: %v", err)
	}

	// Verify entry was added
	if !isUserInstalledImpl(testServiceName) {
		t.Error("isUserInstalledImpl() = false after addCronEntry, want true")
	}

	// Test adding duplicate entry should fail
	if err := addCronEntry(scriptPath, testServiceName); err == nil {
		t.Error("addCronEntry() should fail for duplicate entry")
	}

	// Test removeCronEntry
	if err := removeCronEntry(testServiceName); err != nil {
		t.Fatalf("removeCronEntry() error: %v", err)
	}

	// Verify entry was removed
	if isUserInstalledImpl(testServiceName) {
		t.Error("isUserInstalledImpl() = true after removeCronEntry, want false")
	}
}

// TestUserServiceLifecycle tests the full user service lifecycle.
// This test modifies the user's crontab and creates files in ~/.muti-metroo.
func TestUserServiceLifecycle(t *testing.T) {
	testServiceName := "muti-metroo-test"
	names := DeriveNames(testServiceName)

	// Skip if crontab is not available
	if !hasCrontab() {
		t.Skip("crontab not available")
	}

	// Skip if already installed (don't interfere with real installation)
	if isUserInstalledImpl(testServiceName) {
		t.Skip("user service already installed")
	}

	// Skip unless explicitly enabled
	if os.Getenv("TEST_CRON_INTEGRATION") != "1" {
		t.Skip("set TEST_CRON_INTEGRATION=1 to run this test")
	}

	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("agent:\n  data_dir: "+tempDir), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	// Create a fake binary that just exits
	binaryPath := filepath.Join(tempDir, "fake-muti-metroo")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/bash\nsleep 1"), 0755); err != nil {
		t.Fatalf("failed to create fake binary: %v", err)
	}

	cfg := ServiceConfig{
		Name:       testServiceName,
		ConfigPath: configPath,
	}

	// Get service dir before install for cleanup
	serviceDir, err := getCronServiceDir(testServiceName)
	if err != nil {
		t.Fatalf("getCronServiceDir() error: %v", err)
	}

	// Cleanup function
	cleanup := func() {
		removeCronEntry(testServiceName)
		os.RemoveAll(serviceDir)
	}
	defer cleanup()

	// Test install
	// Force the cron backend explicitly; the test exercises that path.
	if err := installUserImpl(cfg, binaryPath, UserMethodCron); err != nil {
		t.Fatalf("installUserImpl() error: %v", err)
	}

	// Verify installation
	if !isUserInstalledImpl(testServiceName) {
		t.Error("isUserInstalledImpl() = false after install, want true")
	}

	// Check wrapper script exists
	scriptPath := filepath.Join(serviceDir, names.ScriptName)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Error("wrapper script not created")
	}

	// Test status
	status, err := statusUserImpl(testServiceName)
	if err != nil {
		t.Fatalf("statusUserImpl() error: %v", err)
	}
	// Status should be either "active" or "inactive" (depending on if fake binary ran)
	if !strings.HasPrefix(status, "active") && !strings.HasPrefix(status, "inactive") {
		t.Errorf("statusUserImpl() = %q, want 'active...' or 'inactive...'", status)
	}

	// Test uninstall
	if err := uninstallUserImpl(testServiceName); err != nil {
		t.Fatalf("uninstallUserImpl() error: %v", err)
	}

	// Verify uninstallation
	if isUserInstalledImpl(testServiceName) {
		t.Error("isUserInstalledImpl() = true after uninstall, want false")
	}

	// Wrapper script should be removed
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Error("wrapper script still exists after uninstall")
	}
}
