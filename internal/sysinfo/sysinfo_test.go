package sysinfo

import (
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	// Version should either be a release version (set via ldflags)
	// or an enhanced dev version (dev-<commit> or dev-<timestamp>)
	t.Logf("Version: %s", Version)

	if Version == "dev" {
		t.Error("Version should not be plain 'dev' - enhanceDevVersion should have been called")
	}

	// Check valid version formats
	validFormats := []string{
		"dev-",   // Enhanced dev version (dev-abc1234, dev-abc1234-dirty, dev-20060102-150405)
		"v",      // Release version (v1.0.0)
		"latest", // Latest tag
	}

	hasValidFormat := false
	for _, prefix := range validFormats {
		if strings.HasPrefix(Version, prefix) {
			hasValidFormat = true
			break
		}
	}

	if !hasValidFormat {
		t.Errorf("Version %q has unexpected format", Version)
	}
}

func TestEnhanceDevVersion(t *testing.T) {
	version := enhanceDevVersion()
	t.Logf("Enhanced dev version: %s", version)

	if !strings.HasPrefix(version, "dev-") {
		t.Errorf("Enhanced version %q should start with 'dev-'", version)
	}

	// Should have something after "dev-"
	suffix := strings.TrimPrefix(version, "dev-")
	if suffix == "" {
		t.Error("Enhanced version should have content after 'dev-'")
	}
}

func TestDetectShells(t *testing.T) {
	shells := DetectShells()
	t.Logf("Detected shells: %v", shells)

	if len(shells) == 0 {
		t.Fatal("DetectShells() returned empty list; at least one shell should be available")
	}

	// Verify all entries are non-empty strings
	for i, sh := range shells {
		if sh == "" {
			t.Errorf("DetectShells()[%d] is empty", i)
		}
	}

	// Platform-specific checks
	if runtime.GOOS == "windows" {
		if !slices.Contains(shells, "cmd.exe") {
			t.Error("DetectShells() on Windows should include cmd.exe")
		}
	} else {
		if !slices.Contains(shells, "sh") {
			t.Error("DetectShells() on Unix should include sh")
		}
	}

	// Verify results are cached (same slice returned)
	shells2 := DetectShells()
	if len(shells) != len(shells2) {
		t.Error("DetectShells() should return cached results")
	}
}

func TestGetInterfaceAddresses(t *testing.T) {
	addrs := GetInterfaceAddresses()
	if len(addrs) == 0 {
		t.Fatal("GetInterfaceAddresses() returned empty list; at least loopback should be present")
	}

	foundLoopback := false
	for i, a := range addrs {
		if a.IP == "" {
			t.Errorf("GetInterfaceAddresses()[%d] has empty IP", i)
		}
		if a.Interface == "" {
			t.Errorf("GetInterfaceAddresses()[%d] has empty Interface", i)
		}
		if a.Family != "ipv4" && a.Family != "ipv6" {
			t.Errorf("GetInterfaceAddresses()[%d] has unexpected Family %q", i, a.Family)
		}
		// Global is exactly the negation of loopback || link-local.
		if a.Global == (a.Loopback || a.LinkLocal) {
			t.Errorf("GetInterfaceAddresses()[%d] inconsistent flags: global=%v loopback=%v linkLocal=%v",
				i, a.Global, a.Loopback, a.LinkLocal)
		}
		if a.IP == "127.0.0.1" {
			foundLoopback = true
			if !a.Loopback {
				t.Error("127.0.0.1 should be flagged as loopback")
			}
			if a.Global {
				t.Error("127.0.0.1 should not be flagged as global")
			}
		}
	}

	if !foundLoopback {
		t.Error("GetInterfaceAddresses() should include IPv4 loopback 127.0.0.1")
	}
}
