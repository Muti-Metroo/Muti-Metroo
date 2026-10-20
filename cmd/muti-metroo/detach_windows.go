//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/Muti-Metroo/Muti-Metroo/internal/service"
)

// IsProcessInJob is not exported by golang.org/x/sys/windows@v0.39.0.
var (
	modkernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procIsProcessInJob = modkernel32.NewProc("IsProcessInJob")
)

// detachSelf re-spawns this process outside the Windows Job Object that
// OpenSSH creates per session so the agent survives SSH disconnect.
func detachSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("detach: could not resolve executable: %w", err)
	}

	parentInJob, breakawayOK, limitFlags, err := breakawayState()
	if err != nil {
		return fmt.Errorf("detach: could not inspect parent Job Object: %w", err)
	}
	if parentInJob && !breakawayOK {
		return fmt.Errorf("detach: current Job Object forbids breakaway (LimitFlags=0x%x); "+
			"your OpenSSH host does not set JOB_OBJECT_LIMIT_BREAKAWAY_OK -- %s",
			limitFlags, breakawayServiceHint(service.IsRoot()))
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("detach: could not open NUL: %w", err)
	}
	defer devNull.Close()

	creationFlags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW)
	if parentInJob {
		creationFlags |= windows.CREATE_BREAKAWAY_FROM_JOB
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Dir = childDir(exe)
	cmd.Env = scrubbedEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: creationFlags}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("detach: failed to spawn detached process: %w", err)
	}

	// Best-effort post-spawn verification. Open the child once; both the
	// breakaway check and the exit-code probe reuse the handle.
	if h, oerr := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE,
		false, uint32(cmd.Process.Pid),
	); oerr == nil {
		defer windows.CloseHandle(h)

		if parentInJob {
			if childInJob, ierr := isProcessInJob(h); ierr == nil && childInJob {
				fmt.Fprintln(os.Stderr,
					"detach: warning: child process is still inside a Job Object; "+
						"it may not survive SSH disconnect")
			}
		}

		if err := probeChildExit(h); err != nil {
			return err
		}
	}

	_ = cmd.Process.Release()
	fmt.Fprintf(os.Stderr, "Detached agent started (PID %d)\n", cmd.Process.Pid)
	return nil
}

func breakawayState() (inJob bool, ok bool, limitFlags uint32, err error) {
	inJob, err = isProcessInJob(windows.CurrentProcess())
	if err != nil {
		return false, false, 0, err
	}
	if !inJob {
		return false, true, 0, nil
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if qerr := windows.QueryInformationJobObject(
		0,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); qerr != nil {
		return true, false, 0, fmt.Errorf("QueryInformationJobObject: %w", qerr)
	}

	limitFlags = info.BasicLimitInformation.LimitFlags
	ok = limitFlags&(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK|windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK) != 0
	return true, ok, limitFlags, nil
}

func isProcessInJob(proc windows.Handle) (bool, error) {
	var result int32
	r1, _, e1 := procIsProcessInJob.Call(
		uintptr(proc),
		0,
		uintptr(unsafe.Pointer(&result)),
	)
	if r1 == 0 {
		return false, fmt.Errorf("IsProcessInJob: %w", e1)
	}
	return result != 0, nil
}

// probeChildExit returns an error if the child has already exited by the
// time WaitForSingleObject returns. Uses a 200ms wait so a successful
// detach doesn't pay the latency on the happy path -- the wait returns
// immediately when the child crashes.
func probeChildExit(h windows.Handle) error {
	event, err := windows.WaitForSingleObject(h, 200)
	if err != nil {
		return nil
	}
	if event != windows.WAIT_OBJECT_0 {
		return nil
	}
	var code uint32
	if gerr := windows.GetExitCodeProcess(h, &code); gerr != nil {
		return nil
	}
	return fmt.Errorf("detach: agent exited code %d shortly after spawn -- "+
		"run `muti-metroo run -c <path>` without -D to see startup errors", code)
}

// childDir is the working directory of the detached agent: the current
// one, so a relative -c path (including the ./config.yaml default) and
// relative paths inside the config resolve as they would without -D.
func childDir(exe string) string {
	if wd, err := os.Getwd(); err == nil && wd != "" {
		return wd
	}
	return durableDir(exe)
}

// durableDir is the fallback working directory when the current one is
// unknown.
func durableDir(exe string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if d := filepath.Dir(exe); d != "" {
		return d
	}
	return os.TempDir()
}

func scrubbedEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src)+1)
	for _, e := range src {
		if strings.HasPrefix(e, "SSH_") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, detachedEnvVar+"=1")
	return out
}
