package mc

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

var backupNameRe = regexp.MustCompile(`^[a-z]+-[0-9]{8}-[0-9]{6}\.zip$`)

// PanelSettings is panel-wide configuration, persisted as config.json in the
// data directory.
type PanelSettings struct {
	BackupDir string `json:"backupDir,omitempty"`

	// Domain is the base domain of the whitelabel mapping: every server is
	// reachable as <id>.<Domain> once *.<Domain> points at this host.
	Domain string `json:"domain,omitempty"`
	// DNSProvider is empty (operator manages the wildcard record) or
	// "cloudflare" (the panel maintains wildcard and SRV records itself).
	DNSProvider string `json:"dnsProvider,omitempty"`
	// DNSToken is the Cloudflare API token. It never leaves this host.
	DNSToken string `json:"dnsToken,omitempty"`
	// DNSTarget pins the wildcard record target; empty auto-detects the
	// host's public IP and follows it like a DynDNS client.
	DNSTarget string `json:"dnsTarget,omitempty"`
}

type BackupInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Time int64  `json:"time"` // unix milliseconds
}

func (m *Manager) Settings() PanelSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// SetBackupDir points backups at a new directory. Empty resets to the default
// below the data directory. The path must be absolute and writable.
func (m *Manager) SetBackupDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return errors.New("backup directory must be an absolute path")
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create backup directory: %w", err)
		}
		probe, err := os.CreateTemp(dir, ".craftpanel-write-test-*")
		if err != nil {
			return fmt.Errorf("backup directory is not writable: %w", err)
		}
		probe.Close()
		os.Remove(probe.Name())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings.BackupDir = dir
	return fsutil.WriteJSONAtomic(m.settingsPath, m.settings)
}

func (m *Manager) backupRoot() string {
	m.mu.Lock()
	dir := m.settings.BackupDir
	m.mu.Unlock()
	if dir == "" {
		return filepath.Join(m.dataDir, "backups")
	}
	return dir
}

func (m *Manager) backupDirFor(id string) string {
	return filepath.Join(m.backupRoot(), id)
}

func validBackupName(name string) bool {
	return backupNameRe.MatchString(name) && !strings.Contains(name, "..")
}

func (m *Manager) ListBackups(id string) ([]BackupInfo, error) {
	if _, err := m.get(id); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(m.backupDirFor(id))
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupInfo{}, nil
		}
		return nil, err
	}
	out := []BackupInfo{}
	for _, e := range entries {
		if e.IsDir() || !validBackupName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{Name: e.Name(), Size: info.Size(), Time: info.ModTime().UnixMilli()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// StartBackup kicks off an asynchronous backup. kind is "manual" or "auto".
func (m *Manager) StartBackup(id, kind string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	if srv.backupBusy {
		srv.mu.Unlock()
		return errors.New("a backup or restore is already running")
	}
	if srv.installing || srv.deleting {
		srv.mu.Unlock()
		return ErrNotStopped
	}
	srv.backupBusy = true
	keep := srv.meta.BackupKeep
	srv.mu.Unlock()

	go func() {
		name, size, err := m.doBackup(srv, kind)
		srv.mu.Lock()
		srv.backupBusy = false
		srv.mu.Unlock()
		if err != nil {
			log.Printf("backup %s: %v", id, err)
			srv.proc.Note("Backup failed: " + err.Error())
			m.notify(srv, notifyBackup, "backupFail", err.Error())
			return
		}
		m.notify(srv, notifyBackup, "backupOk", fmt.Sprintf("%s, %.1f MB", name, float64(size)/(1<<20)))
		if kind == "auto" {
			m.pruneAutoBackups(id, keep)
		}
	}()
	return nil
}

func (m *Manager) doBackup(srv *Server, kind string) (string, int64, error) {
	dir := m.backupDirFor(srv.meta.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, err
	}
	name := fmt.Sprintf("%s-%s.zip", kind, time.Now().Format("20060102-150405"))
	dest := filepath.Join(dir, name)

	// Freeze world saving while the copy runs, so region files are consistent.
	// Velocity proxies have no world, nothing to freeze there.
	if srv.proc.State() == StateRunning && srv.meta.Type != TypeVelocity {
		if srv.meta.Type == TypeBedrock {
			srv.proc.SendCommand("save hold")
			for i := 0; i < 15; i++ {
				srv.proc.SendCommand("save query")
				if srv.proc.WaitForLine("Data saved", time.Second) {
					break
				}
			}
			defer srv.proc.SendCommand("save resume")
		} else {
			srv.proc.SendCommand("save-off")
			srv.proc.SendCommand("save-all flush")
			srv.proc.WaitForLine("Saved the game", 20*time.Second)
			defer srv.proc.SendCommand("save-on")
		}
	}

	if err := zipDir(srv.DataDir(), dest); err != nil {
		return "", 0, err
	}
	var size int64
	if fi, err := os.Stat(dest); err == nil {
		size = fi.Size()
	}
	log.Printf("backup %s: created %s", srv.meta.ID, name)
	srv.proc.Note("Backup created: " + name)
	return name, size, nil
}

func zipDir(src, dest string) error {
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	zw := zip.NewWriter(f)

	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			_, err := zw.CreateHeader(&zip.FileHeader{Name: rel + "/"})
			return err
		}
		if d.Name() == "session.lock" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil
		}
		hdr.Name = rel
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return nil // file vanished or locked, skip
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	if err != nil {
		zw.Close()
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// RestoreBackup replaces the server's data directory with the backup content.
// Runs asynchronously; progress is visible through the backupBusy flag.
func (m *Manager) RestoreBackup(id, name string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	if !validBackupName(name) {
		return errors.New("invalid backup name")
	}
	zipPath := filepath.Join(m.backupDirFor(id), name)
	if _, err := os.Stat(zipPath); err != nil {
		return errors.New("backup not found")
	}
	srv.mu.Lock()
	if srv.backupBusy {
		srv.mu.Unlock()
		return errors.New("a backup or restore is already running")
	}
	if srv.installing || srv.deleting || srv.proc.State() != StateStopped {
		srv.mu.Unlock()
		return ErrNotStopped
	}
	srv.backupBusy = true
	srv.mu.Unlock()

	go func() {
		err := restoreZip(zipPath, srv.dir, srv.DataDir())
		srv.mu.Lock()
		srv.backupBusy = false
		srv.mu.Unlock()
		if err != nil {
			log.Printf("restore %s: %v", id, err)
			srv.proc.Note("Restore failed: " + err.Error())
			return
		}
		log.Printf("restore %s: restored %s", id, name)
		srv.proc.Note("Backup restored: " + name)
	}()
	return nil
}

func restoreZip(zipPath, serverDir, dataDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	tmp := filepath.Join(serverDir, "data.restore-tmp")
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	for _, zf := range zr.File {
		rel := path.Clean(zf.Name)
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) || strings.Contains(rel, "\\") {
			continue
		}
		target := filepath.Join(tmp, filepath.FromSlash(rel))
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		in, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			in.Close()
			return err
		}
		out.Close()
		in.Close()
	}

	old := filepath.Join(serverDir, "data.pre-restore")
	os.RemoveAll(old)
	if err := os.Rename(dataDir, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, dataDir); err != nil {
		// Try to roll back so the server is not left without a data dir.
		os.Rename(old, dataDir)
		return err
	}
	return os.RemoveAll(old)
}

func (m *Manager) DeleteBackup(id, name string) error {
	if _, err := m.get(id); err != nil {
		return err
	}
	if !validBackupName(name) {
		return errors.New("invalid backup name")
	}
	return os.Remove(filepath.Join(m.backupDirFor(id), name))
}

// OpenBackup opens a backup file for streaming to the client.
func (m *Manager) OpenBackup(id, name string) (*os.File, int64, error) {
	if _, err := m.get(id); err != nil {
		return nil, 0, err
	}
	if !validBackupName(name) {
		return nil, 0, errors.New("invalid backup name")
	}
	f, err := os.Open(filepath.Join(m.backupDirFor(id), name))
	if err != nil {
		return nil, 0, errors.New("backup not found")
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// pruneAutoBackups keeps the newest `keep` automatic backups. Manual backups
// are never touched.
func (m *Manager) pruneAutoBackups(id string, keep int) {
	if keep <= 0 {
		keep = 7
	}
	backups, err := m.ListBackups(id)
	if err != nil {
		return
	}
	autos := backups[:0:0]
	for _, b := range backups {
		if strings.HasPrefix(b.Name, "auto-") {
			autos = append(autos, b)
		}
	}
	for i := keep; i < len(autos); i++ {
		if err := os.Remove(filepath.Join(m.backupDirFor(id), autos[i].Name)); err == nil {
			log.Printf("backup %s: pruned %s", id, autos[i].Name)
		}
	}
}
