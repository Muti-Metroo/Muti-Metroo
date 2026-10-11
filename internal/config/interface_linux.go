//go:build linux

package config

import (
	"fmt"
	"os"
)

// validateInterfacePrivilege enforces the policy "route.interface only when
// the agent runs as root on Linux". Non-root deployments are rejected at
// config load with a clear message; we explicitly do not support a
// CAP_NET_RAW workaround.
func validateInterfacePrivilege() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("requires root (euid=0); current euid=%d. route.interface uses SO_BINDTODEVICE which is privileged on Linux", os.Geteuid())
	}
	return nil
}
