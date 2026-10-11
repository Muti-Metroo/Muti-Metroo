//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
)

var (
	modAdvapi32              = windows.NewLazySystemDLL("advapi32.dll")
	procOpenSCManager        = modAdvapi32.NewProc("OpenSCManagerW")
	procCreateService        = modAdvapi32.NewProc("CreateServiceW")
	procOpenService          = modAdvapi32.NewProc("OpenServiceW")
	procDeleteService        = modAdvapi32.NewProc("DeleteService")
	procCloseServiceHandle   = modAdvapi32.NewProc("CloseServiceHandle")
	procStartService         = modAdvapi32.NewProc("StartServiceW")
	procControlService       = modAdvapi32.NewProc("ControlService")
	procQueryServiceStatus   = modAdvapi32.NewProc("QueryServiceStatus")
	procCheckTokenMembership = modAdvapi32.NewProc("CheckTokenMembership")
)

const (
	SC_MANAGER_ALL_ACCESS     = 0xF003F
	SERVICE_ALL_ACCESS        = 0xF01FF
	SERVICE_WIN32_OWN_PROCESS = 0x10
	SERVICE_AUTO_START        = 0x2
	SERVICE_ERROR_NORMAL      = 0x1
	SERVICE_CONTROL_STOP      = 0x1
	SERVICE_STOPPED           = 0x1
	SERVICE_START_PENDING     = 0x2
	SERVICE_STOP_PENDING      = 0x3
	SERVICE_RUNNING           = 0x4
)

type serviceStatus struct {
	serviceType             uint32
	currentState            uint32
	controlsAccepted        uint32
	win32ExitCode           uint32
	serviceSpecificExitCode uint32
	checkPoint              uint32
	waitHint                uint32
}

// isRootImpl checks if running as Administrator on Windows.
func isRootImpl() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	member, err := isTokenMemberOfSid(windows.Token(0), sid)
	if err != nil {
		return false
	}

	return member
}

func isTokenMemberOfSid(token windows.Token, sid *windows.SID) (bool, error) {
	var isMember int32
	r1, _, err := procCheckTokenMembership.Call(
		uintptr(token),
		uintptr(unsafe.Pointer(sid)),
		uintptr(unsafe.Pointer(&isMember)),
	)
	if r1 == 0 {
		return false, err
	}
	return isMember != 0, nil
}

// installImpl installs the service on Windows.
func installImpl(cfg ServiceConfig, execPath string) error {
	return installWindowsService(cfg, execPath, false)
}

// installImplEmbedded installs Windows service for embedded config binary.
func installImplEmbedded(cfg ServiceConfig, execPath string) error {
	return installWindowsService(cfg, execPath, true)
}

// installWindowsService installs a Windows service with optional embedded config mode.
func installWindowsService(cfg ServiceConfig, execPath string, embedded bool) error {
	scManager, err := openSCManager()
	if err != nil {
		return fmt.Errorf("failed to open service control manager: %w", err)
	}
	defer closeSCHandle(scManager)

	existingService, _ := openService(scManager, cfg.Name)
	if existingService != 0 {
		closeSCHandle(existingService)
		return fmt.Errorf("service %s is already installed", cfg.Name)
	}

	var cmdLine string
	if embedded {
		cmdLine = fmt.Sprintf(`"%s" run`, execPath)
	} else {
		cmdLine = fmt.Sprintf(`"%s" run -c "%s"`, execPath, cfg.ConfigPath)
	}

	namePtr, _ := syscall.UTF16PtrFromString(cfg.Name)
	displayNamePtr, _ := syscall.UTF16PtrFromString(cfg.DisplayName)
	cmdLinePtr, _ := syscall.UTF16PtrFromString(cmdLine)

	r1, _, err := procCreateService.Call(
		scManager,
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(displayNamePtr)),
		SERVICE_ALL_ACCESS,
		SERVICE_WIN32_OWN_PROCESS,
		SERVICE_AUTO_START,
		SERVICE_ERROR_NORMAL,
		uintptr(unsafe.Pointer(cmdLinePtr)),
		0, // No load order group
		0, // No tag
		0, // No dependencies
		0, // LocalSystem account
		0, // No password
	)
	if r1 == 0 {
		return fmt.Errorf("failed to create service: %w", err)
	}
	serviceHandle := r1
	defer closeSCHandle(serviceHandle)

	fmt.Printf("Created Windows service: %s\n", cfg.Name)

	if cfg.Description != "" {
		setServiceDescription(serviceHandle, cfg.Description)
	}

	r1, _, err = procStartService.Call(serviceHandle, 0, 0)
	if r1 == 0 {
		fmt.Printf("Note: service created but failed to start: %v\n", err)
		fmt.Println("You may need to start it manually with: net start", cfg.Name)
	} else {
		fmt.Printf("Started Windows service: %s\n", cfg.Name)
	}

	return nil
}

// stopServiceImpl stops a running Windows service.
func stopServiceImpl(serviceName string) error {
	if output, err := runCommand("net", "stop", serviceName); err != nil {
		outputStr := strings.TrimSpace(output)
		// Ignore "not started" errors
		if !strings.Contains(strings.ToLower(outputStr), "not started") &&
			!strings.Contains(strings.ToLower(outputStr), "is not started") {
			return fmt.Errorf("failed to stop service: %s", outputStr)
		}
	}
	return nil
}

// startServiceImpl starts a Windows service.
func startServiceImpl(serviceName string) error {
	if output, err := runCommand("net", "start", serviceName); err != nil {
		return fmt.Errorf("failed to start service: %s", strings.TrimSpace(output))
	}
	return nil
}

// uninstallImpl removes the Windows service.
func uninstallImpl(serviceName string) error {
	// Open Service Control Manager
	scManager, err := openSCManager()
	if err != nil {
		return fmt.Errorf("failed to open service control manager: %w", err)
	}
	defer closeSCHandle(scManager)

	// Open the service
	serviceHandle, err := openService(scManager, serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed: %w", serviceName, err)
	}
	defer closeSCHandle(serviceHandle)

	// Stop the service if running
	var status serviceStatus
	procQueryServiceStatus.Call(serviceHandle, uintptr(unsafe.Pointer(&status)))
	if status.currentState != SERVICE_STOPPED {
		fmt.Printf("Stopping service: %s\n", serviceName)
		procControlService.Call(serviceHandle, SERVICE_CONTROL_STOP, uintptr(unsafe.Pointer(&status)))
		// Wait a bit for it to stop
		for i := 0; i < 30; i++ {
			procQueryServiceStatus.Call(serviceHandle, uintptr(unsafe.Pointer(&status)))
			if status.currentState == SERVICE_STOPPED {
				break
			}
			windows.SleepEx(1000, false)
		}
		fmt.Printf("Stopped service: %s\n", serviceName)
	}

	// Delete the service
	r1, _, err := procDeleteService.Call(serviceHandle)
	if r1 == 0 {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	fmt.Printf("Removed Windows service: %s\n", serviceName)
	return nil
}

// statusImpl returns the service status on Windows.
func statusImpl(serviceName string) (string, error) {
	scManager, err := openSCManager()
	if err != nil {
		return "", fmt.Errorf("failed to open service control manager: %w", err)
	}
	defer closeSCHandle(scManager)

	serviceHandle, err := openService(scManager, serviceName)
	if err != nil {
		return "not installed", nil
	}
	defer closeSCHandle(serviceHandle)

	var status serviceStatus
	r1, _, _ := procQueryServiceStatus.Call(serviceHandle, uintptr(unsafe.Pointer(&status)))
	if r1 == 0 {
		return "unknown", nil
	}

	switch status.currentState {
	case SERVICE_STOPPED:
		return "stopped", nil
	case SERVICE_START_PENDING:
		return "starting", nil
	case SERVICE_STOP_PENDING:
		return "stopping", nil
	case SERVICE_RUNNING:
		return "running", nil
	default:
		return "unknown", nil
	}
}

// isInstalledImpl checks if the service is installed on Windows.
func isInstalledImpl(serviceName string) bool {
	scManager, err := openSCManager()
	if err != nil {
		return false
	}
	defer closeSCHandle(scManager)

	serviceHandle, err := openService(scManager, serviceName)
	if err != nil {
		return false
	}
	closeSCHandle(serviceHandle)
	return true
}

func openSCManager() (uintptr, error) {
	r1, _, err := procOpenSCManager.Call(0, 0, SC_MANAGER_ALL_ACCESS)
	if r1 == 0 {
		return 0, err
	}
	return r1, nil
}

func openService(scManager uintptr, name string) (uintptr, error) {
	namePtr, _ := syscall.UTF16PtrFromString(name)
	r1, _, err := procOpenService.Call(scManager, uintptr(unsafe.Pointer(namePtr)), SERVICE_ALL_ACCESS)
	if r1 == 0 {
		return 0, err
	}
	return r1, nil
}

func closeSCHandle(handle uintptr) {
	procCloseServiceHandle.Call(handle)
}

func setServiceDescription(serviceHandle uintptr, description string) {
	// SERVICE_DESCRIPTION structure
	type serviceDescription struct {
		description *uint16
	}

	descPtr, _ := syscall.UTF16PtrFromString(description)
	sd := serviceDescription{description: descPtr}

	// ChangeServiceConfig2W with SERVICE_CONFIG_DESCRIPTION (1)
	modAdvapi32 := windows.NewLazySystemDLL("advapi32.dll")
	procChangeServiceConfig2 := modAdvapi32.NewProc("ChangeServiceConfig2W")
	procChangeServiceConfig2.Call(serviceHandle, 1, uintptr(unsafe.Pointer(&sd)))
}

// isInteractiveImpl returns true if the process is running interactively (not as a Windows service).
func isInteractiveImpl() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		// If we can't determine, assume interactive
		return true
	}
	return !isService
}

// runAsServiceImpl runs the ServiceRunner as a Windows service.
func runAsServiceImpl(name string, runner ServiceRunner) error {
	return svc.Run(name, &windowsServiceHandler{runner: runner})
}

// windowsServiceHandler implements svc.Handler for Windows services.
type windowsServiceHandler struct {
	runner ServiceRunner
}

// Execute implements svc.Handler.Execute.
func (h *windowsServiceHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	// Report Running immediately so the SCM does not time out while the
	// agent performs its startup delay.
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// Start the service in a goroutine so we can handle SCM stop/shutdown
	// signals during the (potentially long) startup delay.
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- h.runner.Start()
	}()

	// Wait for Start() to complete or an SCM stop during startup
	for {
		select {
		case err := <-startErrCh:
			if err != nil {
				changes <- svc.Status{State: svc.StopPending}
				return false, 1
			}
			goto running
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				h.runner.StopWithContext(ctx)
				cancel()
				<-startErrCh
				return false, 0
			}
		}
	}

running:
	// Normal operation - wait for stop/shutdown signal
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err := h.runner.StopWithContext(ctx)
				cancel()
				if err != nil {
					return false, 2
				}
				return false, 0
			}
		}
	}
}

// =============================================================================
// Windows User Service (Registry Run key + rundll32)
// =============================================================================

// User service constants for Registry-based installation.
const (
	userInfoFileName = "service.info"
	// Registry path for user logon startup
	registryRunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// getSystemRoot returns the Windows system root directory (typically C:\Windows).
func getSystemRoot() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return root
	}
	return `C:\Windows`
}

// hasCrontab returns false on Windows (use Registry Run key instead).
func hasCrontab() bool {
	return false
}

// ErrCrontabNotFound is returned when crontab is not available.
var ErrCrontabNotFound = errors.New("crontab not available on Windows - use Registry Run key")

// getUserServiceDir returns %USERPROFILE%\.<serviceName>
func getUserServiceDir(serviceName string) (string, error) {
	userProfile := os.Getenv("USERPROFILE")
	if userProfile == "" {
		return "", fmt.Errorf("USERPROFILE environment variable not set")
	}
	names := DeriveNames(serviceName)
	return filepath.Join(userProfile, names.DirName), nil
}

// installUserImpl is not supported for regular user service on Windows.
// Use InstallUserWindows for Registry-based installation.
func installUserImpl(cfg ServiceConfig, execPath, method string) error {
	return fmt.Errorf("use InstallUserWindows for Windows user service installation")
}

// installUserWithDLLImpl installs a Windows user service using Registry Run key + rundll32.
// This creates a registry entry that runs at user logon without requiring administrator.
// The serviceName is used as the Registry value name (visible in Windows startup apps).
func installUserWithDLLImpl(serviceName, dllPath, configPath string) error {
	// Get service directory
	serviceDir, err := getUserServiceDir(serviceName)
	if err != nil {
		return err
	}

	// Create service directory if it doesn't exist
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	// Resolve absolute paths
	absDLLPath, err := filepath.Abs(dllPath)
	if err != nil {
		return fmt.Errorf("failed to resolve DLL path: %w", err)
	}

	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}

	// Verify DLL exists
	if _, err := os.Stat(absDLLPath); os.IsNotExist(err) {
		return fmt.Errorf("DLL file not found: %s", absDLLPath)
	}

	// Verify config exists
	if _, err := os.Stat(absConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", absConfigPath)
	}

	names := DeriveNames(serviceName)

	// Build the run command with full path to rundll32.exe
	// Using full path ensures the command works from Registry Run key at logon
	// Note: Paths must NOT be quoted - rundll32 doesn't parse quoted arguments correctly
	// The lpszCmdLine parameter to the DLL entry point will be empty if paths are quoted
	rundll32Path := filepath.Join(getSystemRoot(), "System32", "rundll32.exe")
	rundll32Cmd := fmt.Sprintf(`%s %s,Run %s`, rundll32Path, absDLLPath, absConfigPath)

	// Open HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer key.Close()

	// Set the value using the custom service name
	if err := key.SetStringValue(names.RegistryValue, rundll32Cmd); err != nil {
		return fmt.Errorf("failed to set registry value: %w", err)
	}

	// Save config info for status display and uninstall
	infoPath := filepath.Join(serviceDir, userInfoFileName)
	infoContent := fmt.Sprintf("name=%s\nregistry_value=%s\ndll=%s\nconfig=%s\n",
		serviceName, names.RegistryValue, absDLLPath, absConfigPath)
	if err := os.WriteFile(infoPath, []byte(infoContent), 0644); err != nil {
		// Non-fatal, just for status display
		fmt.Printf("Warning: could not save service info: %v\n", err)
	}

	// Start the service immediately after installation
	fmt.Println("Starting service...")
	if err := startUserImpl(serviceName); err != nil {
		fmt.Printf("Warning: could not start service immediately: %v\n", err)
		fmt.Println("The service will start automatically at next logon.")
	}

	return nil
}

// userServiceInfo holds information read from service.info file.
type userServiceInfo struct {
	Name          string // Service display name
	RegistryValue string // Registry value name (used as key in Run registry)
	DLLPath       string // Path to the DLL
	ConfigPath    string // Path to the config file
}

// readUserServiceInfo reads the service info from the service.info file.
// Returns nil if the file doesn't exist or can't be parsed.
func readUserServiceInfo(serviceName string) *userServiceInfo {
	serviceDir, err := getUserServiceDir(serviceName)
	if err != nil {
		return nil
	}

	infoBytes, err := os.ReadFile(filepath.Join(serviceDir, userInfoFileName))
	if err != nil {
		return nil
	}

	// Parse key=value pairs
	values := make(map[string]string)
	for _, line := range strings.Split(string(infoBytes), "\n") {
		if parts := strings.SplitN(strings.TrimSpace(line), "=", 2); len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}

	// RegistryValue is required
	if values["registry_value"] == "" {
		return nil
	}

	return &userServiceInfo{
		Name:          values["name"],
		RegistryValue: values["registry_value"],
		DLLPath:       values["dll"],
		ConfigPath:    values["config"],
	}
}

// uninstallUserImpl removes the Windows user service (Registry Run key entry).
func uninstallUserImpl(serviceName string) error {
	// First try to stop any running process
	_ = stopUserImpl(serviceName) // Ignore error, process may not be running

	// Read service info to get the registry value name
	info := readUserServiceInfo(serviceName)
	if info == nil {
		// No service.info, nothing to uninstall
		return nil
	}

	// Open registry key
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKeyPath, registry.SET_VALUE)
	if err != nil {
		// Key doesn't exist or can't be opened
		return nil
	}
	defer key.Close()

	// Delete the value using the stored registry value name
	err = key.DeleteValue(info.RegistryValue)
	if err != nil {
		// Value may not exist, that's OK
		if !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("failed to delete registry value: %w", err)
		}
	}

	// Clean up service directory
	serviceDir, err := getUserServiceDir(serviceName)
	if err == nil {
		infoPath := filepath.Join(serviceDir, userInfoFileName)
		os.Remove(infoPath)
		// Don't remove the whole directory as it may contain logs
	}

	return nil
}

// statusUserImpl returns the status of the Windows user service.
func statusUserImpl(serviceName string) (string, error) {
	// Read service info
	info := readUserServiceInfo(serviceName)
	if info == nil {
		return "not installed", nil
	}

	// Verify registry entry exists
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "not installed", nil
	}
	defer key.Close()

	_, _, err = key.GetStringValue(info.RegistryValue)
	if err != nil {
		return "not installed (registry entry missing)", nil
	}

	// Check if rundll32 is running with our specific DLL using PowerShell Get-CimInstance
	// This replaces the deprecated wmic command and provides accurate process detection
	psScript := `Get-CimInstance Win32_Process -Filter "Name='rundll32.exe'" | Select-Object ProcessId,CommandLine | ConvertTo-Csv -NoTypeInformation`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("installed as '%s' (status unknown)", info.Name), nil
	}

	// Check if our specific DLL is in any rundll32 command line
	if info.DLLPath != "" && strings.Contains(string(output), info.DLLPath) {
		return fmt.Sprintf("running as '%s'", info.Name), nil
	}

	return fmt.Sprintf("stopped (installed as '%s')", info.Name), nil
}

// startUserImpl starts the Windows user service by running the DLL via rundll32.
func startUserImpl(serviceName string) error {
	// Read the service info to get the DLL and config paths
	info := readUserServiceInfo(serviceName)
	if info == nil {
		return fmt.Errorf("service info not found - is the service installed?")
	}

	if info.DLLPath == "" || info.ConfigPath == "" {
		return fmt.Errorf("invalid service info file")
	}

	// Get full path to rundll32.exe
	rundll32Path := filepath.Join(getSystemRoot(), "System32", "rundll32.exe")

	// Build the rundll32 command (paths must NOT be quoted)
	rundll32Cmd := fmt.Sprintf(`%s %s,Run %s`, rundll32Path, info.DLLPath, info.ConfigPath)

	// Use a temporary scheduled task to start the process.
	// This works reliably even in non-interactive sessions (SSH, services)
	// and runs the process in the user's interactive session.
	taskName := DeriveNames(serviceName).TaskName

	// Create a one-time scheduled task
	createCmd := exec.Command("schtasks", "/Create",
		"/TN", taskName,
		"/TR", rundll32Cmd,
		"/SC", "ONCE",
		"/ST", "00:00",
		"/F") // Force overwrite if exists
	createCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("failed to create scheduled task: %w", err)
	}

	// Run the task immediately
	runCmd := exec.Command("schtasks", "/Run", "/TN", taskName)
	runCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := runCmd.Run(); err != nil {
		// Clean up the task even if run fails
		exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()
		return fmt.Errorf("failed to run scheduled task: %w", err)
	}

	// Delete the temporary task
	deleteCmd := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	deleteCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	deleteCmd.Run() // Ignore errors on cleanup

	return nil
}

// stopUserImpl stops the Windows user service by terminating rundll32 processes.
func stopUserImpl(serviceName string) error {
	// Read service info to get the DLL path for targeted process termination
	info := readUserServiceInfo(serviceName)

	// Use PowerShell Get-CimInstance to find rundll32 processes with our DLL
	// This replaces the deprecated wmic command
	psScript := `Get-CimInstance Win32_Process -Filter "Name='rundll32.exe'" | Select-Object ProcessId,CommandLine | ConvertTo-Csv -NoTypeInformation`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	output, err := cmd.Output()
	if err != nil {
		// PowerShell may not be available or failed
		return nil // Can't determine which process to kill, skip
	}

	// Parse CSV output to find our process
	// Format: "ProcessId","CommandLine"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, `"ProcessId"`) {
			continue
		}

		// Check if this is our process by looking for our DLL path
		if info == nil || info.DLLPath == "" || !strings.Contains(line, info.DLLPath) {
			continue
		}

		// Extract PID from CSV (first field after removing quotes)
		// Line format: "1234","C:\...\rundll32.exe ..."
		line = strings.TrimPrefix(line, `"`)
		if idx := strings.Index(line, `"`); idx > 0 {
			pid := line[:idx]
			if pid != "" {
				killCmd := exec.Command("taskkill", "/PID", pid, "/F")
				killCmd.Run() // Ignore error
			}
		}
	}

	return nil
}

// isUserInstalledImpl checks if the Windows user service is installed.
func isUserInstalledImpl(serviceName string) bool {
	// Read service info to get the registry value name
	info := readUserServiceInfo(serviceName)
	if info == nil {
		return false
	}

	// Open registry key
	key, err := registry.OpenKey(registry.CURRENT_USER, registryRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	// Check if our value exists using the stored registry value name
	_, _, err = key.GetStringValue(info.RegistryValue)
	return err == nil
}

// getUserServiceInfoImpl returns information about the Windows user service.
func getUserServiceInfoImpl(serviceName string) *UserServiceInfo {
	info := readUserServiceInfo(serviceName)
	if info == nil {
		return nil
	}
	return &UserServiceInfo{
		Name:       info.Name,
		DLLPath:    info.DLLPath,
		ConfigPath: info.ConfigPath,
	}
}
