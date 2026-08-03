package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveUploadsDirAbs(uploadsDir string) (string, error) {
	if uploadsDir == "" {
		uploadsDir = "uploads"
	}
	if filepath.IsAbs(uploadsDir) {
		return filepath.Clean(uploadsDir), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(wd, uploadsDir)), nil
}

// resolveUploadsFilePath resolves an uploads-relative path stored in DB (e.g. "uploads/abc.png").
// to an absolute file path under uploadsDir, preventing path traversal.
func resolveUploadsFilePath(uploadsDir string, storedPath string) (string, error) {
	uploadsDirAbs, err := resolveUploadsDirAbs(uploadsDir)
	if err != nil {
		return "", err
	}
	uploadsDirAbs, err = filepath.Abs(uploadsDirAbs)
	if err != nil {
		return "", err
	}

	p := strings.TrimSpace(storedPath)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}

	// Normalize separators for prefix handling.
	p = strings.ReplaceAll(p, "\\", "/")

	// If an absolute path was stored, validate it is inside uploadsDir.
	if filepath.IsAbs(p) {
		abs := filepath.Clean(p)
		if !pathWithinRoot(uploadsDirAbs, abs) {
			return "", fmt.Errorf("path escapes uploads dir")
		}
		return validateResolvedUploadsPath(uploadsDirAbs, abs)
	}

	p = strings.TrimPrefix(p, "/")
	if strings.HasPrefix(p, "uploads/") {
		p = strings.TrimPrefix(p, "uploads/")
	}

	cleanRel := filepath.Clean(p)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid relative path")
	}

	abs := filepath.Join(uploadsDirAbs, cleanRel)
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(uploadsDirAbs, abs) {
		return "", fmt.Errorf("path escapes uploads dir")
	}
	return validateResolvedUploadsPath(uploadsDirAbs, abs)
}

func validateResolvedUploadsPath(uploadsDirAbs, abs string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(uploadsDirAbs)
	if err != nil {
		return "", fmt.Errorf("resolve uploads dir: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err == nil {
		if !pathWithinRoot(realRoot, realPath) {
			return "", fmt.Errorf("path escapes uploads dir through symlink")
		}
		return abs, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve uploads path: %w", err)
	}

	// 目标可能已被删除，但已有父目录仍可能通过符号链接逃逸。
	for parent := filepath.Dir(abs); ; parent = filepath.Dir(parent) {
		realParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr == nil {
			if !pathWithinRoot(realRoot, realParent) {
				return "", fmt.Errorf("path escapes uploads dir through symlink")
			}
			return abs, nil
		}
		if !os.IsNotExist(parentErr) {
			return "", fmt.Errorf("resolve uploads parent: %w", parentErr)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", fmt.Errorf("resolve uploads parent: no existing ancestor")
		}
	}
}

func pathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
