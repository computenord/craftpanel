package mc

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// copyDir recursively copies src into dest. dest must not already exist as a
// non-empty directory of interest; it is created as needed.
func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dest, 0o750)
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if d.Name() == "session.lock" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return copyFile(p, target)
	})
}

// safeZipRel rejects zip-slip paths.
func safeZipRel(name string) (string, bool) {
	rel := filepath.ToSlash(name)
	rel = strings.TrimPrefix(rel, "./")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", false
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, `\`) {
		return "", false
	}
	return rel, true
}
