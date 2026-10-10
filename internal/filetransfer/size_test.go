package filetransfer

import (
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		// Decimal units (1KB = 1000 bytes)
		{"100B", 100, false},
		{"1KB", 1000, false},
		{"1MB", 1000 * 1000, false},
		{"1GB", 1000 * 1000 * 1000, false},

		// Binary units (1KiB = 1024 bytes)
		{"1KiB", 1024, false},
		{"1MiB", 1024 * 1024, false},
		{"1GiB", 1024 * 1024 * 1024, false},

		// With spaces
		{"100 KB", 100 * 1000, false},
		{"10 MiB", 10 * 1024 * 1024, false},

		// Lowercase
		{"100kb", 100 * 1000, false},
		{"1mb", 1000 * 1000, false},

		// Plain numbers (interpreted as bytes)
		{"1024", 1024, false},
		{"0", 0, false},

		// Common rate limit values
		{"100KB", 100 * 1000, false},
		{"500KB", 500 * 1000, false},
		{"1MB", 1000 * 1000, false},
		{"10MB", 10 * 1000 * 1000, false},

		// Errors
		{"", 0, true},
		{"invalid", 0, true},
		{"-100KB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{1536, "1.5 KiB"},
		{-100, "-100 B"}, // Negative values
	}

	for _, tt := range tests {
		got := FormatSize(tt.input)
		if got != tt.expected {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatSizeDecimal(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1000, "1.0 kB"},
		{1000 * 1000, "1.0 MB"},
		{1000 * 1000 * 1000, "1.0 GB"},
		{1500, "1.5 kB"},
	}

	for _, tt := range tests {
		got := FormatSizeDecimal(tt.input)
		if got != tt.expected {
			t.Errorf("FormatSizeDecimal(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
