//go:build !linux

package config

import (
	"fmt"
	"runtime"
)

// validateInterfacePrivilege rejects route.interface on non-Linux platforms.
// SO_BINDTODEVICE is Linux-specific; supporting macOS/Windows would require
// different syscalls (IP_BOUND_IF, etc.) and was explicitly scoped out.
func validateInterfacePrivilege() error {
	return fmt.Errorf("route.interface is only supported on linux (current GOOS=%s)", runtime.GOOS)
}
