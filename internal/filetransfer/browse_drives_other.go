//go:build !windows

package filetransfer

// wildcardRoots returns the set of browsable root paths to advertise when
// allowed_paths contains "*". On Unix-like systems "/" is the single
// filesystem root.
func wildcardRoots() []string {
	return []string{"/"}
}
