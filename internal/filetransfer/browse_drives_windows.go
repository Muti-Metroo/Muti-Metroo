//go:build windows

package filetransfer

import "golang.org/x/sys/windows"

// numDriveLetters is the count of possible Windows drive letters (A..Z).
const numDriveLetters = 26

// wildcardRoots returns the set of browsable root paths to advertise when
// allowed_paths contains "*". On Windows there is no single filesystem root,
// so we enumerate the actual logical drives (C:\, D:\, ...) and return them
// as separate roots. The Manager UI presents these as a drive selector and
// can navigate into each one independently.
func wildcardRoots() []string {
	var mask uint32
	if m, err := windows.GetLogicalDrives(); err == nil {
		mask = m
	}
	drives := make([]string, 0, numDriveLetters)
	for i := uint(0); i < numDriveLetters; i++ {
		if mask&(1<<i) != 0 {
			drives = append(drives, string(byte('A'+i))+`:\`)
		}
	}
	if len(drives) == 0 {
		// Shouldn't happen on a real Windows host (GetLogicalDrives error
		// or zero mask); keep the browser usable.
		return []string{`C:\`}
	}
	return drives
}
