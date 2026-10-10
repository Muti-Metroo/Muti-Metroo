package filetransfer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/postalsys/muti-metroo/internal/crypto"
)

const (
	defaultBrowseLimit = 100
	maxBrowseLimit     = 5000
)

// BrowseRequest is the request payload for file browsing operations.
type BrowseRequest struct {
	Action    string               `json:"action"`              // "list", "stat", "roots", "chmod", "delete"
	Path      string               `json:"path,omitempty"`      // Required for "list", "stat", "chmod", and "delete"
	Auth      *crypto.AuthEnvelope `json:"auth,omitempty"`      // Management-key signature (required on the wire)
	Offset    int                  `json:"offset,omitempty"`    // Pagination offset (list only)
	Limit     int                  `json:"limit,omitempty"`     // Pagination limit (list only, default 100, max 5000)
	Mode      string               `json:"mode,omitempty"`      // Octal permission string, e.g. "0755" (chmod only)
	Recursive bool                 `json:"recursive,omitempty"` // Required for deleting non-empty directories (delete only)
}

// BrowseResponse is the response payload for file browsing operations.
type BrowseResponse struct {
	Path      string      `json:"path,omitempty"`     // Echoed back for list/stat
	Entries   []FileEntry `json:"entries,omitempty"`  // Directory entries (list only)
	Total     int         `json:"total"`              // Total entry count before pagination (list only)
	Truncated bool        `json:"truncated"`          // True when more entries exist beyond offset+limit
	Entry     *FileEntry  `json:"entry,omitempty"`    // Single entry (stat only)
	Roots     []string    `json:"roots,omitempty"`    // Browsable root paths (roots only)
	Wildcard  bool        `json:"wildcard,omitempty"` // True when allowed_paths contains "*" (roots only)
	Home      string      `json:"home,omitempty"`     // Agent user's home directory (roots only; for client-side ~ expansion)
	Error     string      `json:"error,omitempty"`    // Error message
	// ErrorCode is a stable, machine-readable tag for Error. See
	// ErrCodeAuthRequired / ErrCodeInvalidCredentials.
	ErrorCode string `json:"error_code,omitempty"`
}

// FileEntry represents a single file or directory entry.
type FileEntry struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`     // e.g. "0755"
	ModTime    string `json:"mod_time"` // ISO 8601
	IsDir      bool   `json:"is_dir"`
	IsSymlink  bool   `json:"is_symlink,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

// Browse handles file browsing requests (list, stat, roots, chmod, delete).
func (h *StreamHandler) Browse(req *BrowseRequest) *BrowseResponse {
	if !h.cfg.Enabled {
		return &BrowseResponse{Error: "file transfer is disabled"}
	}

	if err := h.verify(req.Auth); err != nil {
		return &BrowseResponse{Error: err.Error(), ErrorCode: AuthErrorCode(err)}
	}

	switch req.Action {
	case "list", "":
		return h.browseList(req)
	case "stat":
		return h.browseStat(req)
	case "roots":
		return h.browseRoots()
	case "chmod":
		return h.browseChmod(req)
	case "delete":
		return h.browseDelete(req)
	default:
		return &BrowseResponse{Error: fmt.Sprintf("unknown action: %s", req.Action)}
	}
}

// requirePath validates that the request has a non-empty path within allowed paths,
// and returns the cleaned path. If validation fails, it returns an error response.
// A leading "~" or "~/" (or "~\" on Windows) is expanded to the agent user's
// home directory so clients can navigate to it without knowing the real path.
func (h *StreamHandler) requirePath(path string) (string, *BrowseResponse) {
	if path == "" {
		return "", &BrowseResponse{Error: "path is required"}
	}
	expanded := expandTildeHome(path)
	if err := h.validatePath(expanded); err != nil {
		return "", &BrowseResponse{Error: err.Error()}
	}
	return filepath.Clean(expanded), nil
}

// expandTildeHome replaces a leading "~" or "~/" (or "~\" on Windows) with
// the agent user's home directory. Returns the input unchanged when there is
// no tilde prefix or the home directory cannot be resolved — the path then
// flows through normal validation, which will surface a clear error.
func expandTildeHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// browseList lists directory contents with pagination.
func (h *StreamHandler) browseList(req *BrowseRequest) *BrowseResponse {
	cleanPath, errResp := h.requirePath(req.Path)
	if errResp != nil {
		return errResp
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return &BrowseResponse{Error: fmt.Sprintf("path not found: %s", cleanPath)}
	}
	if !info.IsDir() {
		return &BrowseResponse{Error: fmt.Sprintf("not a directory: %s", cleanPath)}
	}

	dirEntries, err := os.ReadDir(cleanPath)
	if err != nil {
		return &BrowseResponse{Error: fmt.Sprintf("failed to read directory: %v", err)}
	}

	entries := make([]FileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entries = append(entries, buildFileEntry(cleanPath, de))
	}

	// Sort: directories first, then alphabetical by name
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})

	total := len(entries)

	// Apply pagination
	limit := req.Limit
	if limit <= 0 {
		limit = defaultBrowseLimit
	}
	if limit > maxBrowseLimit {
		limit = maxBrowseLimit
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	if offset >= total {
		return &BrowseResponse{
			Path:      cleanPath,
			Entries:   []FileEntry{},
			Total:     total,
			Truncated: false,
		}
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return &BrowseResponse{
		Path:      cleanPath,
		Entries:   entries[offset:end],
		Total:     total,
		Truncated: end < total,
	}
}

// browseStat returns info about a single path.
func (h *StreamHandler) browseStat(req *BrowseRequest) *BrowseResponse {
	cleanPath, errResp := h.requirePath(req.Path)
	if errResp != nil {
		return errResp
	}

	entry, err := statPath(cleanPath)
	if err != nil {
		return &BrowseResponse{Error: err.Error()}
	}

	return &BrowseResponse{
		Path:  cleanPath,
		Entry: entry,
	}
}

// browseChmod changes file permissions.
func (h *StreamHandler) browseChmod(req *BrowseRequest) *BrowseResponse {
	cleanPath, errResp := h.requirePath(req.Path)
	if errResp != nil {
		return errResp
	}

	mode, err := parseOctalMode(req.Mode)
	if err != nil {
		return &BrowseResponse{Error: err.Error()}
	}

	if err := os.Chmod(cleanPath, mode); err != nil {
		return &BrowseResponse{Error: fmt.Sprintf("chmod failed: %v", err)}
	}

	entry, err := statPath(cleanPath)
	if err != nil {
		return &BrowseResponse{Error: err.Error()}
	}

	return &BrowseResponse{
		Path:  cleanPath,
		Entry: entry,
	}
}

// browseDelete deletes a file or directory.
func (h *StreamHandler) browseDelete(req *BrowseRequest) *BrowseResponse {
	cleanPath, errResp := h.requirePath(req.Path)
	if errResp != nil {
		return errResp
	}

	// Capture entry info before deletion
	entry, err := statPath(cleanPath)
	if err != nil {
		return &BrowseResponse{Error: err.Error()}
	}

	// Guard: non-empty directories require the recursive flag
	if entry.IsDir {
		dirEntries, err := os.ReadDir(cleanPath)
		if err != nil {
			return &BrowseResponse{Error: fmt.Sprintf("failed to read directory: %v", err)}
		}
		if len(dirEntries) > 0 && !req.Recursive {
			return &BrowseResponse{Error: "directory is not empty (use recursive to delete)"}
		}
	}

	// RemoveAll handles non-empty directories; Remove handles files and empty directories
	removeFunc := os.Remove
	if req.Recursive && entry.IsDir {
		removeFunc = os.RemoveAll
	}
	if err := removeFunc(cleanPath); err != nil {
		return &BrowseResponse{Error: fmt.Sprintf("delete failed: %v", err)}
	}

	return &BrowseResponse{
		Path:  cleanPath,
		Entry: entry,
	}
}

// parseOctalMode parses an octal permission string (e.g. "0755") and validates
// it is within the valid range (0-0777).
func parseOctalMode(s string) (os.FileMode, error) {
	if s == "" {
		return 0, fmt.Errorf("mode is required")
	}
	mode, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode: %s (expected octal, e.g. 0755)", s)
	}
	if mode > 0777 {
		return 0, fmt.Errorf("mode out of range: %s (max 0777)", s)
	}
	return os.FileMode(mode), nil
}

// browseRoots returns the browsable root paths derived from allowed_paths config.
func (h *StreamHandler) browseRoots() *BrowseResponse {
	if len(h.cfg.AllowedPaths) == 0 {
		return &BrowseResponse{Error: "no paths are allowed (allowed_paths is empty)"}
	}

	// The home dir is advisory metadata for the client (tilde expansion).
	// An error here means the agent's environment doesn't have $HOME set —
	// not fatal; the client just won't be able to expand ~.
	home, _ := os.UserHomeDir()

	for _, p := range h.cfg.AllowedPaths {
		if p == "*" {
			return &BrowseResponse{
				Roots:    wildcardRoots(),
				Wildcard: true,
				Home:     home,
			}
		}
	}

	// Derive root directories from allowed patterns
	seen := make(map[string]bool)
	var roots []string
	for _, pattern := range h.cfg.AllowedPaths {
		root := patternBaseDir(pattern)
		if root != "" && !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}

	sort.Strings(roots)

	return &BrowseResponse{
		Roots:    roots,
		Wildcard: false,
		Home:     home,
	}
}

// patternBaseDir extracts the base directory from an allowed path pattern.
// For example:
//
//	"/tmp"       -> "/tmp"
//	"/data/**"   -> "/data"
//	"/home/*/uploads" -> "/home"
//	"*"          -> "" (handled separately)
func patternBaseDir(pattern string) string {
	clean := normalizePath(pattern)

	// Strip recursive glob suffix
	if strings.HasSuffix(clean, "/**") {
		return strings.TrimSuffix(clean, "/**")
	}

	// If pattern contains glob characters, walk up to the first non-glob component
	if strings.ContainsAny(clean, "*?[") {
		parts := strings.Split(clean, string(filepath.Separator))
		var base []string
		for _, p := range parts {
			if strings.ContainsAny(p, "*?[") {
				break
			}
			base = append(base, p)
		}
		result := strings.Join(base, string(filepath.Separator))
		if result == "" {
			return "/"
		}
		return result
	}

	return clean
}

// populateFromFileInfo fills a FileEntry's size, mode, directory flag, and modification time
// from an os.FileInfo.
func populateFromFileInfo(entry *FileEntry, info os.FileInfo) {
	entry.Size = info.Size()
	entry.IsDir = info.IsDir()
	entry.Mode = fmt.Sprintf("%04o", info.Mode().Perm())
	entry.ModTime = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
}

// resolveSymlink populates symlink-specific fields on a FileEntry.
// It reads the link target and resolves the symlink to get the target's file info.
// If the symlink is broken, it falls back to the lstat info.
func resolveSymlink(entry *FileEntry, path string, linfo os.FileInfo) {
	entry.IsSymlink = true
	target, err := os.Readlink(path)
	if err == nil {
		entry.LinkTarget = target
	}

	info, err := os.Stat(path)
	if err != nil {
		// Broken symlink -- fall back to lstat info
		populateFromFileInfo(entry, linfo)
		return
	}
	populateFromFileInfo(entry, info)
}

// buildFileEntry creates a FileEntry from an os.DirEntry.
func buildFileEntry(dir string, de os.DirEntry) FileEntry {
	name := de.Name()
	fullPath := filepath.Join(dir, name)
	entry := FileEntry{Name: name}

	linfo, err := os.Lstat(fullPath)
	if err != nil {
		entry.Mode = "0000"
		entry.ModTime = "0001-01-01T00:00:00Z"
		return entry
	}

	if linfo.Mode()&os.ModeSymlink != 0 {
		resolveSymlink(&entry, fullPath, linfo)
		return entry
	}

	info, err := de.Info()
	if err != nil {
		entry.Mode = "0000"
		entry.ModTime = "0001-01-01T00:00:00Z"
		return entry
	}
	populateFromFileInfo(&entry, info)
	return entry
}

// statPath returns a FileEntry for a single path.
func statPath(path string) (*FileEntry, error) {
	linfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("path not found: %s", path)
	}

	entry := FileEntry{Name: filepath.Base(path)}

	if linfo.Mode()&os.ModeSymlink != 0 {
		resolveSymlink(&entry, path, linfo)
		return &entry, nil
	}

	populateFromFileInfo(&entry, linfo)
	return &entry, nil
}
