//go:build windows

// Package main provides Windows DLL entry points for rundll32.exe execution.
//
// Run entry point (starts the agent):
//
//	rundll32.exe muti-metroo.dll,Run C:\path\to\config.yaml
//
// Or with embedded config:
//
//	rundll32.exe muti-metroo.dll,Run
//
// Install entry point (installs as user service):
//
//	rundll32.exe muti-metroo.dll,Install C:\path\to\config.yaml
//	rundll32.exe muti-metroo.dll,Install my-service C:\path\to\config.yaml
//
// Separate amd64 and arm64 DLLs are produced (muti-metroo-windows-amd64.dll and
// muti-metroo-windows-arm64.dll). Use the DLL matching the host architecture and
// invoke it with the native rundll32.exe -- no emulation layer is required.
package main

/*
#include <windows.h>
*/
import "C"

import (
	"strings"
	"syscall"
	"unsafe"

	"github.com/Muti-Metroo/Muti-Metroo/internal/agent"
	"github.com/Muti-Metroo/Muti-Metroo/internal/config"
	"github.com/Muti-Metroo/Muti-Metroo/internal/service"
	"github.com/Muti-Metroo/Muti-Metroo/internal/sysinfo"
)

// Windows API constants
const getModuleHandleExFlagFromAddress = 0x00000004

// Windows API functions (lazy-loaded)
var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandleEx = kernel32.NewProc("GetModuleHandleExW")
	procGetModuleFileName = kernel32.NewProc("GetModuleFileNameW")
)

// Version is set at build time via ldflags.
var Version = "dev"

func init() {
	// Sync version with sysinfo for consistency across the codebase
	if Version == "dev" {
		Version = sysinfo.Version
	} else {
		sysinfo.Version = Version
	}
}

// getDLLPath returns the full path to this DLL using GetModuleHandleEx
// with GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS to locate the module handle.
// Returns an empty string if the path cannot be determined.
func getDLLPath() string {
	var hModule uintptr

	// Get module handle by passing address of a variable in this DLL's memory space
	ret, _, _ := procGetModuleHandleEx.Call(
		getModuleHandleExFlagFromAddress,
		uintptr(unsafe.Pointer(&Version)),
		uintptr(unsafe.Pointer(&hModule)),
	)
	if ret == 0 {
		return ""
	}

	// Buffer for path (MAX_PATH is 260, but long paths can be up to 32767)
	buf := make([]uint16, 32768)
	ret, _, _ = procGetModuleFileName.Call(
		hModule,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if ret == 0 {
		return ""
	}

	return syscall.UTF16ToString(buf[:ret])
}

// Run is the exported entry point for rundll32.exe.
// Signature matches Windows rundll32 callback convention:
//
//	void CALLBACK Run(HWND hwnd, HINSTANCE hinst, LPSTR lpszCmdLine, int nCmdShow)
//
//export Run
func Run(hwnd C.HWND, hinst C.HINSTANCE, lpszCmdLine *C.char, nCmdShow C.int) {
	cmdLine := strings.TrimSpace(C.GoString(lpszCmdLine))
	dllPath := getDLLPath()

	cfg, _, err := config.LoadOrEmbeddedFrom(dllPath, cmdLine)
	if err != nil {
		return
	}

	a, err := agent.New(cfg)
	if err != nil {
		return
	}

	if err := a.Start(); err != nil {
		return
	}

	// Replay persisted dynamic config (routes, forwards, peers, display-name)
	// so user changes survive a service restart. Errors are logged by the
	// method itself; we never fail startup because of them.
	_ = a.ReplayDynamicConfig()

	// Block forever - signal handling is unreliable in DLL context
	select {}
}

// Install is the exported entry point for installing the DLL as a user service.
// It handles upgrades by stopping and uninstalling any existing service first,
// then installs using the Registry Run key via service.InstallUserWindows.
//
// The lpszCmdLine can be either:
//   - "C:\path\to\config.yaml" (service name defaults to "muti-metroo")
//   - "my-service C:\path\to\config.yaml" (custom service name)
//
// Signature matches Windows rundll32 callback convention:
//
//	void CALLBACK Install(HWND hwnd, HINSTANCE hinst, LPSTR lpszCmdLine, int nCmdShow)
//
//export Install
func Install(hwnd C.HWND, hinst C.HINSTANCE, lpszCmdLine *C.char, nCmdShow C.int) {
	cmdLine := strings.TrimSpace(C.GoString(lpszCmdLine))
	dllPath := getDLLPath()

	// Parse: "serviceName configPath" or just "configPath"
	// If two tokens, first is service name; if one, default to "muti-metroo"
	serviceName := "muti-metroo"
	configPath := cmdLine
	if parts := strings.SplitN(cmdLine, " ", 2); len(parts) == 2 {
		serviceName = parts[0]
		configPath = parts[1]
	}

	// Handle upgrade: stop and uninstall existing service
	if service.IsUserInstalled(serviceName) {
		_ = service.StopUser(serviceName)
		_ = service.UninstallUser(serviceName)
	}

	// Install: sets registry Run key, writes service.info, starts via schtasks
	_ = service.InstallUserWindows(serviceName, dllPath, configPath)
}

// main is required for c-shared buildmode but will not be called when loaded as DLL.
func main() {}
