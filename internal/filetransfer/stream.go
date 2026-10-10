package filetransfer

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/postalsys/muti-metroo/internal/crypto"
)

// Wire codes carried in TransferMetadata.ErrorCode / BrowseResponse.ErrorCode
// / RemoteError.Code so callers can classify auth failures without
// string-matching.
const (
	// ErrCodeAuthRequired signals a missing management signature (or a
	// target agent that has no public key configured to verify against).
	ErrCodeAuthRequired = "auth_required"
	// ErrCodeInvalidCredentials signals a signature that did not verify.
	ErrCodeInvalidCredentials = "invalid_credentials"
)

// AuthErrorCode maps a management-auth error to its wire code, or "" for
// any other error. Callers use this to set BrowseResponse.ErrorCode /
// TransferMetadata.ErrorCode so frontends can distinguish auth failures.
func AuthErrorCode(err error) string {
	switch {
	case errors.Is(err, crypto.ErrAuthMissing):
		return ErrCodeAuthRequired
	case errors.Is(err, crypto.ErrAuthBadSignature),
		errors.Is(err, crypto.ErrAuthWrongTarget),
		errors.Is(err, crypto.ErrAuthStale):
		return ErrCodeInvalidCredentials
	default:
		return ""
	}
}

// AuthVerifier checks a file-transfer request's management signature.
// Returning nil authorizes the request; any error rejects it. Typical
// implementations wrap crypto.VerifyManagement with the agent's self ID
// and configured public key.
type AuthVerifier func(env *crypto.AuthEnvelope) error

// RemoteError carries a typed error returned by a remote agent across the
// mesh. HTTP handlers use errors.As to extract Code and map it to a status.
type RemoteError struct {
	Code    string
	Message string
}

func (e *RemoteError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return fmt.Sprintf("remote error: %s (%s)", e.Message, e.Code)
	}
	return "remote error: " + e.Message
}

// TransferMetadata is sent as the first data frame in a file transfer stream.
type TransferMetadata struct {
	Path         string               `json:"path"`                    // Absolute destination path
	Mode         uint32               `json:"mode"`                    // File permissions (e.g., 0644)
	Size         int64                `json:"size"`                    // File size in bytes (-1 for directories)
	IsDirectory  bool                 `json:"is_directory"`            // True if transferring a directory
	Auth         *crypto.AuthEnvelope `json:"auth,omitempty"`          // Management-key signature (required on the wire)
	Compress     bool                 `json:"compress"`                // Whether data is gzip compressed
	Checksum     string               `json:"checksum,omitempty"`      // Optional SHA256 checksum (hex)
	RateLimit    int64                `json:"rate_limit,omitempty"`    // Max bytes per second (0 = unlimited)
	Offset       int64                `json:"offset,omitempty"`        // Resume from this byte offset (uncompressed)
	OriginalSize int64                `json:"original_size,omitempty"` // Expected file size for resume validation
	Error        string               `json:"error,omitempty"`         // Error message (set when transfer fails)
	// ErrorCode is a stable, machine-readable tag for Error. Frontends
	// switch on this to distinguish auth failures from other errors. See
	// ErrCodeAuthRequired / ErrCodeInvalidCredentials.
	ErrorCode string `json:"error_code,omitempty"`
}

// TransferResult is sent back after a transfer completes (in download response metadata).
type TransferResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Written int64  `json:"written"` // Bytes written
}

// StreamConfig holds configuration for stream-based file transfer.
type StreamConfig struct {
	Enabled      bool
	MaxFileSize  int64    // 0 = unlimited
	AllowedPaths []string // Empty = no paths allowed, ["*"] = all paths, otherwise glob patterns
	Compression  bool     // Whether to compress data (default true)
}

// StreamHandler handles file transfer stream operations.
type StreamHandler struct {
	cfg    StreamConfig
	verify AuthVerifier
}

// NewStreamHandler creates a new stream-based file transfer handler.
// verify MUST be non-nil -- when the agent has no signing public key
// configured, pass a verifier that always returns crypto.ErrAuthMissing
// (or similar) so inbound transfers are rejected.
func NewStreamHandler(cfg StreamConfig, verify AuthVerifier) *StreamHandler {
	return &StreamHandler{cfg: cfg, verify: verify}
}

// validateCommon performs validation common to both upload and download operations.
// It also expands a leading "~" in meta.Path so upload/download paths behave
// consistently with the browse endpoint.
func (h *StreamHandler) validateCommon(meta *TransferMetadata) error {
	if !h.cfg.Enabled {
		return fmt.Errorf("file transfer is disabled")
	}
	if err := h.verify(meta.Auth); err != nil {
		return err
	}
	meta.Path = expandTildeHome(meta.Path)
	return h.validatePath(meta.Path)
}

// ValidateUploadMetadata validates upload metadata and returns an error if invalid.
func (h *StreamHandler) ValidateUploadMetadata(meta *TransferMetadata) error {
	if err := h.validateCommon(meta); err != nil {
		return err
	}

	// Reject a destination that already exists as a symlink pointing outside
	// the allowlist: writing through it would land the upload outside the
	// allowed paths even though the path itself validated. A destination that
	// does not exist yet, the normal upload case, passes through untouched.
	if err := h.validateSymlinkTarget(meta.Path); err != nil {
		return err
	}

	// Check size limit (if not directory and size is known)
	if !meta.IsDirectory && meta.Size > 0 && h.cfg.MaxFileSize > 0 && meta.Size > h.cfg.MaxFileSize {
		return fmt.Errorf("file too large: %d bytes (max %d)", meta.Size, h.cfg.MaxFileSize)
	}

	return nil
}

// ValidateDownloadMetadata validates download metadata and returns an error if invalid.
func (h *StreamHandler) ValidateDownloadMetadata(meta *TransferMetadata) error {
	if err := h.validateCommon(meta); err != nil {
		return err
	}

	// Check for symlinks and validate their targets
	if err := h.validateSymlinkTarget(meta.Path); err != nil {
		return err
	}

	// Check if path exists (follows symlinks)
	info, err := os.Stat(meta.Path)
	if err != nil {
		return fmt.Errorf("path not found: %w", err)
	}

	// Check size limit for files
	if !info.IsDir() && h.cfg.MaxFileSize > 0 && info.Size() > h.cfg.MaxFileSize {
		return fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), h.cfg.MaxFileSize)
	}

	return nil
}

// validateSymlinkTarget checks if a path is a symlink and validates that its target
// is within the allowed paths. This prevents symlink-based escape attacks.
func (h *StreamHandler) validateSymlinkTarget(path string) error {
	// Use Lstat to check if the path itself is a symlink (doesn't follow symlinks)
	info, err := os.Lstat(path)
	if err != nil {
		// Path doesn't exist or can't be accessed - let later checks handle this
		return nil
	}

	// If not a symlink, nothing to validate
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}

	// Resolve the symlink target
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("cannot resolve symlink: %w", err)
	}

	// Validate the resolved target path
	if err := h.validateResolvedPath(target); err != nil {
		return fmt.Errorf("symlink target not allowed: %w", err)
	}

	return nil
}

// validateResolvedPath checks a symlink-resolved path against the allowlist.
//
// It compares resolved against resolved. An allowed root may itself sit behind
// a symlink -- on macOS /tmp is a link to /private/tmp -- and in that case a
// resolved target never matches the literal pattern even when it names a file
// squarely inside the allowed directory. Comparing only literal patterns would
// reject those legitimate paths.
func (h *StreamHandler) validateResolvedPath(resolved string) error {
	if err := h.validatePath(resolved); err == nil {
		return nil
	}

	// No "*" case here: validatePath above already returns nil for a wildcard
	// allowlist, and short-circuiting it a second time would also skip the
	// dangerous-character and absolute-path checks it performs.
	normalized := normalizePath(resolved)
	for _, pattern := range h.cfg.AllowedPaths {
		// Resolve only the leading glob-free directory of the pattern, then
		// re-attach the glob. "/tmp/**" becomes "/private/tmp/**" on macOS,
		// which still matches everything the operator intended to allow.
		base, rest := splitGlobBase(pattern)
		if base == "" {
			continue
		}
		resolvedBase, err := filepath.EvalSymlinks(base)
		if err != nil {
			continue
		}
		if isPathAllowed(normalized, resolvedBase+rest) {
			return nil
		}
	}

	return fmt.Errorf("path not in allowed list: %s", resolved)
}

// globMeta is the set of glob metacharacters recognised in allowed-path
// patterns. Shared so splitGlobBase and isPathAllowed cannot disagree.
const globMeta = "*?["

// splitGlobBase splits an allowed-path pattern into the leading directory that
// contains no glob metacharacters and the remainder. "/tmp/**" splits into
// "/tmp" and "/**"; a pattern with no glob returns itself and an empty rest.
// The base is a real directory that can be resolved on disk, the rest is not.
func splitGlobBase(pattern string) (base, rest string) {
	idx := strings.IndexAny(pattern, globMeta)
	if idx < 0 {
		return pattern, ""
	}
	// Back up to the separator preceding the first glob metacharacter so the
	// base ends on a whole path segment.
	sep := strings.LastIndex(pattern[:idx], "/")
	if sep <= 0 {
		return "", pattern
	}
	return pattern[:sep], pattern[sep:]
}

// containsDangerousChars checks for null bytes and other dangerous characters in paths.
func containsDangerousChars(path string) bool {
	for _, r := range path {
		// Check for null bytes
		if r == 0 {
			return true
		}
		// Check for control characters (except common whitespace)
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return true
		}
	}
	return false
}

// normalizePath applies Unicode NFC normalization and cleans the path.
func normalizePath(path string) string {
	// Apply NFC normalization to prevent Unicode normalization attacks
	normalized := norm.NFC.String(path)
	// Clean the path to resolve . and .. components
	return filepath.Clean(normalized)
}

// isPathUnderPrefix checks if path is exactly prefix or is under prefix directory.
// This prevents prefix bypass attacks like /var/wwwevil matching /var/www.
func isPathUnderPrefix(path, prefix string) bool {
	// Normalize both paths
	cleanPath := normalizePath(path)
	cleanPrefix := normalizePath(prefix)

	// Exact match
	if cleanPath == cleanPrefix {
		return true
	}

	// Path must be under prefix (prefix + separator + something)
	// Ensure prefix ends with separator for proper matching
	if !strings.HasSuffix(cleanPrefix, string(filepath.Separator)) {
		cleanPrefix += string(filepath.Separator)
	}

	return strings.HasPrefix(cleanPath, cleanPrefix)
}

// validatePath checks if the path is allowed.
// The AllowedPaths configuration works as follows:
//   - Empty list: No paths are allowed (feature disabled)
//   - ["*"]: All absolute paths are allowed
//   - Specific paths: Only listed paths/patterns are allowed
//
// Patterns support glob syntax (using filepath.Match):
//   - "/tmp/*": Any file directly in /tmp
//   - "/tmp/**": Any file in /tmp or subdirectories (recursive)
//   - "/home/*/uploads": uploads directory for any user
func (h *StreamHandler) validatePath(path string) error {
	// Check for null bytes and dangerous characters first (before any processing)
	if containsDangerousChars(path) {
		return fmt.Errorf("path contains dangerous characters")
	}

	// Apply Unicode normalization
	normalizedPath := normalizePath(path)

	// Must be absolute
	if !filepath.IsAbs(normalizedPath) {
		return fmt.Errorf("path must be absolute: %s", path)
	}

	// Check for directory traversal (after cleaning)
	if strings.Contains(normalizedPath, "..") {
		return fmt.Errorf("directory traversal not allowed")
	}

	// Empty list = no paths allowed (consistent with RPC whitelist)
	if len(h.cfg.AllowedPaths) == 0 {
		return fmt.Errorf("no paths are allowed (allowed_paths is empty)")
	}

	// Check allowed paths with prefix matching and glob support
	for _, pattern := range h.cfg.AllowedPaths {
		// Wildcard allows all absolute paths
		if pattern == "*" {
			return nil
		}
		if isPathAllowed(normalizedPath, pattern) {
			return nil
		}
	}

	return fmt.Errorf("path not in allowed list: %s", path)
}

// isPathAllowed checks if a path matches an allowed pattern.
// Supports:
//   - Exact prefix matching: "/tmp" allows "/tmp" and "/tmp/foo"
//   - Glob patterns: "/tmp/*.txt" allows "/tmp/file.txt"
//   - Recursive glob: "/tmp/**" allows any path under /tmp
func isPathAllowed(path, pattern string) bool {
	// Normalize the pattern
	cleanPattern := normalizePath(pattern)

	// Check for recursive glob pattern "**"
	if strings.HasSuffix(cleanPattern, "/**") {
		// Get the base directory
		baseDir := strings.TrimSuffix(cleanPattern, "/**")
		return isPathUnderPrefix(path, baseDir)
	}

	// Check if pattern contains glob characters
	if strings.ContainsAny(cleanPattern, globMeta) {
		// Use filepath.Match for glob patterns
		matched, err := filepath.Match(cleanPattern, path)
		if err == nil && matched {
			return true
		}

		// Also check if any parent matches (for patterns like "/tmp/*")
		// This allows "/tmp/*" to match "/tmp/foo/bar" by checking "/tmp/foo"
		dir := path
		for dir != "/" && dir != "." {
			matched, err := filepath.Match(cleanPattern, dir)
			if err == nil && matched {
				return true
			}
			dir = filepath.Dir(dir)
		}

		return false
	}

	// No glob characters - use prefix matching
	return isPathUnderPrefix(path, cleanPattern)
}

// WriteUploadedFile writes uploaded data from a reader to the specified path.
// If isDirectory is true, it expects tar.gz data and extracts it.
// Returns the number of bytes written.
//
// ctx aborts the copy at the next Read after cancellation, so an agent
// shutdown (or peer disconnect) does not leave the goroutine wedged on a
// stalled source.
func (h *StreamHandler) WriteUploadedFile(ctx context.Context, path string, r io.Reader, mode uint32, isDirectory bool, compressed bool) (int64, error) {
	path = filepath.Clean(path)
	r = withCtx(ctx, r)

	if isDirectory {
		// For directories, extract tar.gz to the path
		// UntarDirectory handles gzip decompression internally
		if err := UntarDirectory(ctx, r, path); err != nil {
			return 0, fmt.Errorf("failed to extract directory: %w", err)
		}
		// Calculate extracted size
		size, _ := CalculateDirectorySize(path)
		return size, nil
	}

	// For files, create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create the file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode))
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// If compressed, wrap with gzip reader
	var reader io.Reader = r
	if compressed {
		gzr, err := gzip.NewReader(r)
		if err != nil {
			return 0, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzr.Close()
		reader = gzr
	}

	// Copy data with size enforcement
	var written int64
	if h.cfg.MaxFileSize > 0 {
		written, err = io.Copy(f, io.LimitReader(reader, h.cfg.MaxFileSize+1))
		if err != nil {
			return written, fmt.Errorf("failed to write file: %w", err)
		}
		if written > h.cfg.MaxFileSize {
			return written, fmt.Errorf("file data exceeds max size: %d bytes (max %d)", written, h.cfg.MaxFileSize)
		}
	} else {
		written, err = io.Copy(f, reader)
		if err != nil {
			return written, fmt.Errorf("failed to write file: %w", err)
		}
	}

	return written, nil
}

// gzipPipeReader creates a pipe that gzip-compresses data from the source file.
// The file is closed when compression completes or on error. ctx aborts the
// background copy so a stalled consumer does not leak the goroutine when the
// agent shuts down.
func gzipPipeReader(ctx context.Context, f *os.File) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		gzw := gzip.NewWriter(pw)
		_, copyErr := io.Copy(gzw, withCtx(ctx, f))
		f.Close()
		gzw.Close()
		if copyErr != nil {
			pw.CloseWithError(copyErr)
		} else {
			pw.Close()
		}
	}()
	return pr
}

// ReadFileForDownload creates a reader for the given path.
// If the path is a directory, it returns a tar.gz stream.
// The returned reader should be read fully and closed is handled by the caller.
// Returns: reader, size (-1 for directories), mode, isDirectory, error
//
// ctx cancels the background tar/gzip goroutine on shutdown.
func (h *StreamHandler) ReadFileForDownload(ctx context.Context, path string, compress bool) (io.Reader, int64, uint32, bool, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("path not found: %w", err)
	}

	if info.IsDir() {
		// For directories, create a tar.gz stream via pipe
		pr, pw := io.Pipe()

		go func() {
			err := TarDirectory(ctx, path, pw)
			if err != nil {
				pw.CloseWithError(err)
			} else {
				pw.Close()
			}
		}()

		return pr, -1, uint32(info.Mode().Perm()), true, nil
	}

	// For files, open and optionally wrap with gzip
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("failed to open file: %w", err)
	}

	if compress {
		return gzipPipeReader(ctx, f), -1, uint32(info.Mode().Perm()), false, nil
	}

	// Wrap f so a stalled consumer cannot keep a Read syscall pinned past
	// shutdown -- the next Read sees ctx.Err and returns immediately.
	return withCtx(ctx, f), info.Size(), uint32(info.Mode().Perm()), false, nil
}

// ReadFileForDownloadAtOffset creates a reader starting at the given byte offset.
// This is used for resuming downloads. The offset is in uncompressed bytes.
// The function seeks to the offset in the original file and starts a fresh gzip stream.
// Directories (tar archives) do not support resume - returns error if isDirectory.
// Returns: reader, remainingSize (-1 if compressed), mode, isDirectory, error
//
// ctx cancels the background gzip goroutine on shutdown / peer disconnect.
func (h *StreamHandler) ReadFileForDownloadAtOffset(ctx context.Context, path string, offset int64, compress bool) (io.Reader, int64, uint32, bool, error) {
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("path not found: %w", err)
	}

	if info.IsDir() {
		// Directories don't support resume
		return nil, 0, 0, true, fmt.Errorf("resume not supported for directories")
	}

	// Validate offset
	if offset < 0 {
		return nil, 0, 0, false, fmt.Errorf("invalid offset: %d", offset)
	}
	if offset > info.Size() {
		return nil, 0, 0, false, fmt.Errorf("offset %d exceeds file size %d", offset, info.Size())
	}

	// Open file and seek to offset
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("failed to open file: %w", err)
	}

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			f.Close()
			return nil, 0, 0, false, fmt.Errorf("failed to seek to offset: %w", err)
		}
	}

	remainingSize := info.Size() - offset

	if compress {
		return gzipPipeReader(ctx, f), -1, uint32(info.Mode().Perm()), false, nil
	}

	return withCtx(ctx, f), remainingSize, uint32(info.Mode().Perm()), false, nil
}

// ParseMetadata parses transfer metadata from JSON bytes.
func ParseMetadata(data []byte) (*TransferMetadata, error) {
	var meta TransferMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("invalid metadata: %w", err)
	}
	return &meta, nil
}

// EncodeMetadata encodes transfer metadata to JSON bytes.
func EncodeMetadata(meta *TransferMetadata) ([]byte, error) {
	return json.Marshal(meta)
}

// EncodeResult encodes transfer result to JSON bytes.
func EncodeResult(result *TransferResult) ([]byte, error) {
	return json.Marshal(result)
}

// ParseResult parses transfer result from JSON bytes.
func ParseResult(data []byte) (*TransferResult, error) {
	var result TransferResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid result: %w", err)
	}
	return &result, nil
}

// CountingWriter wraps a writer and counts bytes written.
type CountingWriter struct {
	W       io.Writer
	Written int64
}

func (cw *CountingWriter) Write(p []byte) (int, error) {
	n, err := cw.W.Write(p)
	cw.Written += int64(n)
	return n, err
}

// CountingReader wraps a reader and counts bytes read.
type CountingReader struct {
	R         io.Reader
	BytesRead int64
}

func (cr *CountingReader) Read(p []byte) (int, error) {
	n, err := cr.R.Read(p)
	cr.BytesRead += int64(n)
	return n, err
}

// ProgressWriter wraps a writer and reports progress.
type ProgressWriter struct {
	W          io.Writer
	Total      int64 // Total expected bytes (-1 if unknown)
	Written    int64
	OnProgress func(written, total int64)
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.W.Write(p)
	pw.Written += int64(n)
	if pw.OnProgress != nil {
		pw.OnProgress(pw.Written, pw.Total)
	}
	return n, err
}

// ProgressReader wraps a reader and reports progress.
type ProgressReader struct {
	R          io.Reader
	Total      int64 // Total expected bytes (-1 if unknown)
	BytesRead  int64
	OnProgress func(read, total int64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.R.Read(p)
	pr.BytesRead += int64(n)
	if pr.OnProgress != nil {
		pr.OnProgress(pr.BytesRead, pr.Total)
	}
	return n, err
}

// CtxReader makes a Read return ctx.Err() as soon as ctx is done. The check
// fires *before* the wrapped Read, so a blocked Read won't be interrupted
// mid-syscall -- but the next iteration of io.Copy will exit cleanly. Enough
// to unblock shutdown without making every underlying source cancellation-aware.
type CtxReader struct {
	Ctx context.Context
	R   io.Reader
}

func (c *CtxReader) Read(p []byte) (int, error) {
	if err := c.Ctx.Err(); err != nil {
		return 0, err
	}
	return c.R.Read(p)
}

// withCtx wraps r so reads honor ctx. nil ctx skips wrapping.
func withCtx(ctx context.Context, r io.Reader) io.Reader {
	if ctx == nil {
		return r
	}
	return &CtxReader{Ctx: ctx, R: r}
}
