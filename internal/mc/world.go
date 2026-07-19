package mc

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImportWorld replaces the server's world folder with the contents of a zip.
// The server must be stopped. Zip may contain level.dat at root or a single
// top-level world directory.
func (m *Manager) ImportWorld(id string, zipPath string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	if srv.installing || srv.deleting || srv.backupBusy || srv.proc.State() != StateStopped {
		srv.mu.Unlock()
		return ErrNotStopped
	}
	typ := srv.meta.Type
	dataDir := srv.DataDir()
	srv.mu.Unlock()

	if typ == TypeVelocity {
		return errors.New("velocity has no world")
	}
	worldName := "world"
	if typ != TypeBedrock {
		if props, err := os.ReadFile(filepath.Join(dataDir, "server.properties")); err == nil {
			for _, line := range strings.Split(string(props), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "level-name=") {
					worldName = strings.TrimSpace(strings.TrimPrefix(line, "level-name="))
					if worldName == "" {
						worldName = "world"
					}
					break
				}
			}
		}
	} else {
		worldName = "worlds"
	}

	worldDir := filepath.Join(dataDir, worldName)
	tmp := filepath.Join(dataDir, ".world-import-tmp")
	os.RemoveAll(tmp)
	if err := extractZipTo(zipPath, tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	// Normalize: if tmp has a single directory and no level.dat at root, use that.
	src := tmp
	if typ != TypeBedrock {
		if _, err := os.Stat(filepath.Join(tmp, "level.dat")); err != nil {
			entries, _ := os.ReadDir(tmp)
			dirs := 0
			var only string
			for _, e := range entries {
				if e.IsDir() {
					dirs++
					only = e.Name()
				}
			}
			if dirs == 1 {
				cand := filepath.Join(tmp, only)
				if _, err := os.Stat(filepath.Join(cand, "level.dat")); err == nil {
					src = cand
				}
			}
		}
		if _, err := os.Stat(filepath.Join(src, "level.dat")); err != nil {
			os.RemoveAll(tmp)
			return errors.New("zip does not look like a Minecraft world (missing level.dat)")
		}
	}

	old := worldDir + ".pre-import"
	os.RemoveAll(old)
	if _, err := os.Stat(worldDir); err == nil {
		if err := os.Rename(worldDir, old); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(worldDir), 0o750); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(src, worldDir); err != nil {
		// Fall back to copy when rename crosses devices.
		if copyErr := copyDir(src, worldDir); copyErr != nil {
			if _, e := os.Stat(old); e == nil {
				_ = os.Rename(old, worldDir)
			}
			os.RemoveAll(tmp)
			return fmt.Errorf("import world: %w", copyErr)
		}
	}
	os.RemoveAll(tmp)
	os.RemoveAll(old)
	srv.proc.Note("World imported")
	return nil
}

// peekZipHasLevelDat is a tiny helper for uploads (optional).
func peekZipHasLevelDat(zipPath string) bool {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, zf := range zr.File {
		base := filepath.Base(zf.Name)
		if base == "level.dat" {
			return true
		}
	}
	return false
}

// ExportWorld zips only the world folder into a temp file. Caller must Remove
// the returned path when done. Running Java servers get a brief save freeze.
func (m *Manager) ExportWorld(id string) (path string, filename string, err error) {
	srv, err := m.get(id)
	if err != nil {
		return "", "", err
	}
	srv.mu.Lock()
	typ := srv.meta.Type
	name := srv.meta.Name
	running := srv.proc.State() == StateRunning
	srv.mu.Unlock()
	if typ == TypeVelocity || typ == TypeWaterfall {
		return "", "", errors.New("proxies have no world")
	}
	worldDir := filepath.Join(srv.DataDir(), srv.worldName())
	if typ == TypeBedrock {
		worldDir = filepath.Join(srv.DataDir(), "worlds")
	}
	if st, err := os.Stat(worldDir); err != nil || !st.IsDir() {
		return "", "", errors.New("world folder not found")
	}

	if running && typ != TypeBedrock {
		_ = srv.proc.SendCommand("save-off")
		_ = srv.proc.SendCommand("save-all flush")
		time.Sleep(1500 * time.Millisecond)
		defer func() { _ = srv.proc.SendCommand("save-on") }()
	} else if running && typ == TypeBedrock {
		_ = srv.proc.SendCommand("save hold")
		time.Sleep(1500 * time.Millisecond)
		defer func() { _ = srv.proc.SendCommand("save resume") }()
	}

	tmp, err := os.CreateTemp("", "craftpanel-world-*.zip")
	if err != nil {
		return "", "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	if err := zipDir(worldDir, tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", "", err
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	if safe == "" {
		safe = id
	}
	return tmpPath, safe + "-world.zip", nil
}
