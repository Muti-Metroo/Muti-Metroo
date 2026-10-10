package filetransfer

import (
	"fmt"
	"strings"

	"github.com/dustin/go-humanize"
)

// ParseSize parses a human-readable size string to bytes.
// Supported formats:
//   - Decimal units: 100B, 10KB, 1MB, 1GB, 1TB (1KB = 1000 bytes)
//   - Binary units: 10KiB, 1MiB, 1GiB, 1TiB (1KiB = 1024 bytes)
//   - Plain number: 1024 (interpreted as bytes)
//
// Returns the size in bytes and an error if parsing fails.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// go-humanize.ParseBytes returns uint64
	bytes, err := humanize.ParseBytes(s)
	if err != nil {
		return 0, fmt.Errorf("invalid size format '%s': %w", s, err)
	}

	return int64(bytes), nil
}

// FormatSize formats bytes as a human-readable size string.
// Uses IEC binary units (KiB, MiB, GiB, etc.) for clarity.
func FormatSize(bytes int64) string {
	if bytes < 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return humanize.IBytes(uint64(bytes))
}

// FormatSizeDecimal formats bytes as a human-readable size string.
// Uses SI decimal units (KB, MB, GB, etc.).
func FormatSizeDecimal(bytes int64) string {
	if bytes < 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return humanize.Bytes(uint64(bytes))
}
