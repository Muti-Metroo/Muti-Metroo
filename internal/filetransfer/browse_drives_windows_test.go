//go:build windows

package filetransfer

import (
	"regexp"
	"testing"
)

// TestWildcardRoots_Windows asserts that on a real Windows host wildcardRoots
// returns at least one entry shaped like a drive letter (X:\). The actual set
// of drives is environment-dependent (the CI/dev box will have at least C:),
// so we only assert the shape and the presence of the system drive.
func TestWildcardRoots_Windows(t *testing.T) {
	roots := wildcardRoots()
	if len(roots) == 0 {
		t.Fatal("wildcardRoots returned no drives on Windows")
	}
	driveRe := regexp.MustCompile(`^[A-Z]:\\$`)
	for _, root := range roots {
		if !driveRe.MatchString(root) {
			t.Errorf("root %q does not match drive letter shape X:\\", root)
		}
	}
	// Every Windows host has C:\, so we can assert it's present.
	hasC := false
	for _, root := range roots {
		if root == `C:\` {
			hasC = true
			break
		}
	}
	if !hasC {
		t.Errorf("expected C:\\ in roots, got %v", roots)
	}
}
