//go:build windows

package shell

import (
	"os"
)

// setupResizeSignal is a no-op on Windows as SIGWINCH is not supported.
// Windows terminal resize is handled differently (via console API).
func setupResizeSignal(sigCh chan os.Signal) {
	// SIGWINCH doesn't exist on Windows
	// Terminal resize on Windows would need to use Windows Console API
}
