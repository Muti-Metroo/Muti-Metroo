//go:build !windows

package shell

import (
	"os"
	"os/signal"
	"syscall"
)

// setupResizeSignal sets up SIGWINCH handling for terminal resizing on Unix systems.
func setupResizeSignal(sigCh chan os.Signal) {
	signal.Notify(sigCh, syscall.SIGWINCH)
}
