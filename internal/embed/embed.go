// Package embed provides embedded configuration handling for Muti Metroo.
// It allows configuration YAML to be XOR-obfuscated and appended to the binary,
// enabling single-file deployments without external config files.
//
// Binary format:
//
//	[executable][XOR'd YAML config][8-byte length (little-endian)][8-byte magic]
//
// The XOR obfuscation prevents casual inspection of config content but is NOT
// cryptographic security. Sensitive values should use environment variables or
// external secrets management.
package embed

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Magic is the marker at the end of binary indicating embedded config presence.
var Magic = [8]byte{'M', 'U', 'T', 'I', 'C', 'F', 'G', 0x00}

// XORKey is the static 32-byte key used for config obfuscation.
// This provides simple obfuscation to prevent casual inspection of config.
// It is NOT cryptographic security - config should not contain highly sensitive
// data that would be compromised if the binary is reverse-engineered.
var XORKey = [32]byte{
	0x4d, 0x55, 0x54, 0x49, 0x4d, 0x45, 0x54, 0x52, // MUTIMETR
	0x4f, 0x4f, 0x5f, 0x43, 0x4f, 0x4e, 0x46, 0x49, // OO_CONFI
	0x47, 0x5f, 0x4b, 0x45, 0x59, 0x5f, 0x32, 0x30, // G_KEY_20
	0x32, 0x36, 0x5f, 0x56, 0x31, 0x5f, 0x30, 0x30, // 26_V1_00
}

// FooterSize is the size of the embedded config footer (length + magic).
const FooterSize = 16 // 8 bytes length + 8 bytes magic

var (
	// ErrNoEmbeddedConfig is returned when no embedded config is found.
	ErrNoEmbeddedConfig = errors.New("no embedded configuration found")

	// ErrInvalidMagic is returned when magic marker doesn't match.
	ErrInvalidMagic = errors.New("invalid magic marker")

	// ErrConfigTooLarge is returned when config length exceeds file size.
	ErrConfigTooLarge = errors.New("embedded config size exceeds file size")

	// ErrAlreadyEmbedded is returned when trying to embed into binary that already has config.
	ErrAlreadyEmbedded = errors.New("binary already has embedded configuration")
)

// XOR applies XOR obfuscation to data using the static key.
// The same function is used for both encoding and decoding since XOR is symmetric.
func XOR(data []byte) []byte {
	result := make([]byte, len(data))
	keyLen := len(XORKey)
	for i, b := range data {
		result[i] = b ^ XORKey[i%keyLen]
	}
	return result
}

// HasEmbeddedConfig checks if the binary at the given path has embedded config.
func HasEmbeddedConfig(binaryPath string) (bool, error) {
	f, err := os.Open(binaryPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		return false, err
	}

	if stat.Size() < FooterSize {
		return false, nil // File too small to have embedded config
	}

	// Seek to the last 8 bytes for magic marker
	if _, err := f.Seek(-8, io.SeekEnd); err != nil {
		return false, err
	}

	var magic [8]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false, err
	}

	return magic == Magic, nil
}

// ReadEmbeddedConfig reads and decodes embedded config from a binary file.
func ReadEmbeddedConfig(binaryPath string) ([]byte, error) {
	f, err := os.Open(binaryPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()

	if fileSize < FooterSize {
		return nil, ErrNoEmbeddedConfig
	}

	// Read footer (last 16 bytes)
	footer := make([]byte, FooterSize)
	if _, err := f.ReadAt(footer, fileSize-FooterSize); err != nil {
		return nil, fmt.Errorf("failed to read footer: %w", err)
	}

	// Verify magic (last 8 bytes of footer)
	var magic [8]byte
	copy(magic[:], footer[8:])
	if magic != Magic {
		return nil, ErrNoEmbeddedConfig
	}

	// Read config length (first 8 bytes of footer)
	configLen := binary.LittleEndian.Uint64(footer[:8])
	if configLen == 0 {
		return nil, ErrNoEmbeddedConfig
	}

	// Validate config length doesn't exceed file boundaries
	if int64(configLen) > fileSize-FooterSize {
		return nil, ErrConfigTooLarge
	}

	// Read XOR'd config
	configStart := fileSize - FooterSize - int64(configLen)
	xorConfig := make([]byte, configLen)
	if _, err := f.ReadAt(xorConfig, configStart); err != nil {
		return nil, fmt.Errorf("failed to read config data: %w", err)
	}

	// Decode XOR and return
	return XOR(xorConfig), nil
}

// ReadFromSelf reads embedded config from the currently running binary.
func ReadFromSelf() ([]byte, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the real binary path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve executable path: %w", err)
	}

	return ReadEmbeddedConfig(execPath)
}

// HasEmbeddedConfigSelf checks if the running binary has embedded config.
func HasEmbeddedConfigSelf() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}

	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return false
	}

	has, _ := HasEmbeddedConfig(execPath)
	return has
}

// AppendConfig appends XOR-obfuscated config to a binary file.
// srcBinary is the source binary path, dstBinary is the output path.
// The source and destination can be the same file.
func AppendConfig(srcBinary, dstBinary string, config []byte) error {
	// Read source binary
	srcData, err := os.ReadFile(srcBinary)
	if err != nil {
		return fmt.Errorf("failed to read source binary: %w", err)
	}

	// Check if source already has embedded config
	if len(srcData) >= FooterSize {
		var magic [8]byte
		copy(magic[:], srcData[len(srcData)-8:])
		if magic == Magic {
			return ErrAlreadyEmbedded
		}
	}

	// XOR the config
	xorConfig := XOR(config)

	// Build footer: [8-byte length][8-byte magic]
	footer := make([]byte, FooterSize)
	binary.LittleEndian.PutUint64(footer[:8], uint64(len(xorConfig)))
	copy(footer[8:], Magic[:])

	// Get source file permissions
	srcStat, err := os.Stat(srcBinary)
	if err != nil {
		return fmt.Errorf("failed to stat source binary: %w", err)
	}

	// Create output file
	out, err := os.OpenFile(dstBinary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	// Write: original binary + XOR'd config + footer
	if _, err := out.Write(srcData); err != nil {
		return fmt.Errorf("failed to write binary data: %w", err)
	}
	if _, err := out.Write(xorConfig); err != nil {
		return fmt.Errorf("failed to write config data: %w", err)
	}
	if _, err := out.Write(footer); err != nil {
		return fmt.Errorf("failed to write footer: %w", err)
	}

	return nil
}

// GetOriginalBinarySize returns the size of the original binary without embedded config.
// Returns the file size if no embedded config is found.
func GetOriginalBinarySize(binaryPath string) (int64, error) {
	f, err := os.Open(binaryPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := stat.Size()

	if fileSize < FooterSize {
		return fileSize, nil // No embedded config possible
	}

	// Read footer
	footer := make([]byte, FooterSize)
	if _, err := f.ReadAt(footer, fileSize-FooterSize); err != nil {
		return fileSize, nil
	}

	// Check magic
	var magic [8]byte
	copy(magic[:], footer[8:])
	if magic != Magic {
		return fileSize, nil // No embedded config
	}

	// Calculate original size
	configLen := binary.LittleEndian.Uint64(footer[:8])
	return fileSize - FooterSize - int64(configLen), nil
}

// CopyBinaryWithoutConfig copies a binary to a new location, stripping any embedded config.
func CopyBinaryWithoutConfig(srcPath, dstPath string) error {
	origSize, err := GetOriginalBinarySize(srcPath)
	if err != nil {
		return fmt.Errorf("failed to get original binary size: %w", err)
	}

	// Read only the original binary portion
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer f.Close()

	data := make([]byte, origSize)
	if _, err := io.ReadFull(f, data); err != nil {
		return fmt.Errorf("failed to read binary: %w", err)
	}

	// Get source permissions
	srcStat, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	// Write to destination
	if err := os.WriteFile(dstPath, data, srcStat.Mode()); err != nil {
		return fmt.Errorf("failed to write destination: %w", err)
	}

	return nil
}
