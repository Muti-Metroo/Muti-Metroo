package filetransfer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PartialInfo stores information about a partial/incomplete file transfer.
// This is stored in a .partial.json sidecar file alongside the .partial data file.
type PartialInfo struct {
	// OriginalSize is the expected final size of the file (from the source).
	OriginalSize int64 `json:"original_size"`

	// BytesWritten is the number of uncompressed bytes already written to the partial file.
	BytesWritten int64 `json:"bytes_written"`

	// StartedAt is when the transfer was first started.
	StartedAt time.Time `json:"started_at"`

	// UpdatedAt is when the partial info was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// SourcePath is the original remote path (for validation).
	SourcePath string `json:"source_path,omitempty"`
}

// GetPartialPath returns the .partial path for a given file path.
// For example, "/tmp/file.iso" becomes "/tmp/file.iso.partial"
func GetPartialPath(path string) string {
	return path + ".partial"
}

// GetPartialInfoPath returns the .partial.json path for tracking metadata.
// For example, "/tmp/file.iso" becomes "/tmp/file.iso.partial.json"
func GetPartialInfoPath(path string) string {
	return path + ".partial.json"
}

// WritePartialInfo writes partial transfer info to a .partial.json file.
// The info is written atomically by writing to a temp file and renaming.
func WritePartialInfo(path string, info *PartialInfo) error {
	infoPath := GetPartialInfoPath(path)

	// Update the UpdatedAt timestamp
	info.UpdatedAt = time.Now()

	// Marshal to JSON
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal partial info: %w", err)
	}

	// Write atomically using temp file + rename
	tmpPath := infoPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write partial info: %w", err)
	}

	if err := os.Rename(tmpPath, infoPath); err != nil {
		os.Remove(tmpPath) // Clean up temp file on failure
		return fmt.Errorf("failed to rename partial info: %w", err)
	}

	return nil
}

// ReadPartialInfo reads partial transfer info from a .partial.json file.
// Returns nil, nil if the file doesn't exist.
func ReadPartialInfo(path string) (*PartialInfo, error) {
	infoPath := GetPartialInfoPath(path)

	data, err := os.ReadFile(infoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read partial info: %w", err)
	}

	var info PartialInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse partial info: %w", err)
	}

	return &info, nil
}

// CleanupPartial removes both .partial and .partial.json files for the given path.
// It ignores errors if the files don't exist.
func CleanupPartial(path string) error {
	partialPath := GetPartialPath(path)
	infoPath := GetPartialInfoPath(path)

	var errs []error

	if err := os.Remove(partialPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove partial file: %w", err))
	}

	if err := os.Remove(infoPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to remove partial info: %w", err))
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// HasPartialFile checks if a partial file and its info exist and are valid.
// Returns the partial info if valid, nil if no partial exists or is invalid.
func HasPartialFile(path string) (*PartialInfo, error) {
	partialPath := GetPartialPath(path)

	// Check if partial data file exists
	partialInfo, err := os.Stat(partialPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat partial file: %w", err)
	}

	// Check if partial info file exists
	info, err := ReadPartialInfo(path)
	if err != nil {
		return nil, err
	}
	if info == nil {
		// Partial data exists but no info - delete the orphan partial
		os.Remove(partialPath)
		return nil, nil
	}

	// Use actual file size for BytesWritten - this handles the case where
	// the process was killed before UpdatePartialProgress could run.
	// The partial file's actual size is the authoritative source of truth.
	actualSize := partialInfo.Size()
	if actualSize != info.BytesWritten {
		// Update BytesWritten to match actual file size
		info.BytesWritten = actualSize
	}

	return info, nil
}

// FinalizePartial renames the .partial file to the final path and cleans up metadata.
// This should be called when a transfer completes successfully.
func FinalizePartial(path string, mode os.FileMode) error {
	partialPath := GetPartialPath(path)

	// Set the file mode before renaming
	if err := os.Chmod(partialPath, mode); err != nil {
		return fmt.Errorf("failed to set file mode: %w", err)
	}

	// Rename .partial to final path
	if err := os.Rename(partialPath, path); err != nil {
		return fmt.Errorf("failed to rename partial to final: %w", err)
	}

	// Remove the info file (ignore errors, it's just cleanup)
	os.Remove(GetPartialInfoPath(path))

	return nil
}

// CreatePartialFile creates a new .partial file and initializes the tracking info.
// If a partial file already exists, it will be truncated.
func CreatePartialFile(path string, originalSize int64, sourcePath string, mode os.FileMode) (*os.File, error) {
	partialPath := GetPartialPath(path)

	// Create parent directories if needed
	if err := os.MkdirAll(filepath.Dir(partialPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Create the partial file
	f, err := os.OpenFile(partialPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to create partial file: %w", err)
	}

	// Write initial partial info
	info := &PartialInfo{
		OriginalSize: originalSize,
		BytesWritten: 0,
		StartedAt:    time.Now(),
		SourcePath:   sourcePath,
	}
	if err := WritePartialInfo(path, info); err != nil {
		f.Close()
		os.Remove(partialPath)
		return nil, err
	}

	return f, nil
}

// OpenPartialFileForAppend opens an existing .partial file for appending.
// Returns the file handle seeked to the end (at BytesWritten position).
func OpenPartialFileForAppend(path string) (*os.File, error) {
	partialPath := GetPartialPath(path)

	f, err := os.OpenFile(partialPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open partial file for append: %w", err)
	}

	return f, nil
}

// UpdatePartialProgress updates the BytesWritten field in the partial info file.
// This should be called periodically during a transfer to track progress.
func UpdatePartialProgress(path string, bytesWritten int64) error {
	info, err := ReadPartialInfo(path)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("partial info not found")
	}

	info.BytesWritten = bytesWritten
	return WritePartialInfo(path, info)
}
