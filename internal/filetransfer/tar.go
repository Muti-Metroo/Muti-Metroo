// Package filetransfer provides file upload and download capabilities for agents.
package filetransfer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// TarDirectory streams a directory as a gzip-compressed tar archive to a writer.
// The directory contents are written with paths relative to the directory itself.
// ctx aborts the walk on shutdown so a large tree does not pin the goroutine.
func TarDirectory(ctx context.Context, dir string, w io.Writer) error {
	// Clean and validate the source directory
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	// Create gzip writer
	gzw := gzip.NewWriter(w)
	defer gzw.Close()

	// Create tar writer
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	// Walk the directory tree
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Use relative path with forward slashes (tar convention)
		header.Name = filepath.ToSlash(relPath)

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink: %w", err)
			}
			header.Linkname = link
		}

		// Write header
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// Write file content if it's a regular file
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tw, withCtx(ctx, file)); err != nil {
				return fmt.Errorf("failed to write file to tar: %w", err)
			}
		}

		return nil
	})
}

// UntarDirectory extracts a gzip-compressed tar archive from a reader to a destination directory.
// It creates the destination directory if it doesn't exist.
// For security, it validates paths to prevent directory traversal attacks.
// ctx cancels on shutdown so a slow source does not pin the goroutine.
func UntarDirectory(ctx context.Context, r io.Reader, destDir string) error {
	// Clean destination directory
	destDir = filepath.Clean(destDir)

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Create gzip reader on a ctx-wrapped source so a stalled producer aborts
	// on shutdown.
	gzr, err := gzip.NewReader(withCtx(ctx, r))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	for {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Validate and sanitize the path
		targetPath, err := sanitizeTarPath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}

		case tar.TypeReg:
			// Create parent directories if needed
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create file
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			// Copy content with size limit to prevent tar bombs
			if _, err := io.Copy(file, io.LimitReader(tr, 1<<30)); err != nil {
				file.Close()
				return fmt.Errorf("failed to write file %s: %w", targetPath, err)
			}
			file.Close()

		case tar.TypeSymlink:
			// Validate symlink target
			if err := validateSymlink(destDir, targetPath, header.Linkname); err != nil {
				return err
			}

			// Create parent directories if needed
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Remove existing file/symlink if any
			os.Remove(targetPath)

			// Create symlink
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", targetPath, err)
			}

		case tar.TypeLink:
			// Hard links - validate target is within destDir
			linkTarget, err := sanitizeTarPath(destDir, header.Linkname)
			if err != nil {
				return err
			}

			// Create parent directories if needed
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Remove existing file if any
			os.Remove(targetPath)

			// Create hard link
			if err := os.Link(linkTarget, targetPath); err != nil {
				return fmt.Errorf("failed to create hard link %s: %w", targetPath, err)
			}

		default:
			// Skip unsupported types (devices, fifos, etc.)
			continue
		}
	}

	return nil
}

// sanitizeTarPath validates and sanitizes a tar entry path to prevent directory traversal.
func sanitizeTarPath(destDir, name string) (string, error) {
	// Convert to OS path separator and clean
	name = filepath.FromSlash(name)
	name = filepath.Clean(name)

	// Check for absolute paths
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute paths not allowed in tar: %s", name)
	}

	// Check for directory traversal
	if strings.HasPrefix(name, ".."+string(filepath.Separator)) || strings.Contains(name, string(filepath.Separator)+".."+string(filepath.Separator)) || name == ".." {
		return "", fmt.Errorf("directory traversal not allowed: %s", name)
	}

	// Build target path
	targetPath := filepath.Join(destDir, name)

	// Verify the path is still within destDir (defense in depth)
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve destination: %w", err)
	}

	if !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) && absTarget != absDest {
		return "", fmt.Errorf("path escapes destination directory: %s", name)
	}

	return targetPath, nil
}

// validateSymlink checks if a symlink target is safe (doesn't escape the destination).
func validateSymlink(destDir, symlinkPath, target string) error {
	// If target is absolute, reject it
	if filepath.IsAbs(target) {
		return fmt.Errorf("absolute symlink targets not allowed: %s -> %s", symlinkPath, target)
	}

	// Resolve the symlink target relative to the symlink's directory
	symlinkDir := filepath.Dir(symlinkPath)
	resolvedTarget := filepath.Join(symlinkDir, target)
	resolvedTarget = filepath.Clean(resolvedTarget)

	// Get absolute paths
	absTarget, err := filepath.Abs(resolvedTarget)
	if err != nil {
		return fmt.Errorf("failed to resolve symlink target: %w", err)
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("failed to resolve destination: %w", err)
	}

	// Check if target is within destination
	if !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) && absTarget != absDest {
		return fmt.Errorf("symlink target escapes destination: %s -> %s", symlinkPath, target)
	}

	return nil
}

// CalculateDirectorySize calculates the total size of all files in a directory.
func CalculateDirectorySize(dir string) (int64, error) {
	var size int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
