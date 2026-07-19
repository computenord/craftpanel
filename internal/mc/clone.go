package mc

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// CloneRequest creates a copy of an existing server under a new name/port.
type CloneRequest struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// Clone duplicates server meta + data directory into a new instance. The
// source must be stopped. Install state is skipped (binary already present).
func (m *Manager) Clone(srcID string, req CloneRequest) (ServerView, error) {
	src, err := m.get(srcID)
	if err != nil {
		return ServerView{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > nameMaxLength {
		return ServerView{}, fmt.Errorf("name must be 1-%d characters", nameMaxLength)
	}
	src.mu.Lock()
	if src.installing || src.deleting || src.backupBusy || src.proc.State() != StateStopped {
		src.mu.Unlock()
		return ServerView{}, ErrNotStopped
	}
	meta := src.meta
	srcData := src.DataDir()
	src.mu.Unlock()

	if req.Port != 0 && (req.Port < 1024 || req.Port > 65535) {
		return ServerView{}, errors.New("port must be between 1024 and 65535")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if req.Port == 0 {
		req.Port = m.freePortLocked(meta.Type)
	} else {
		used := m.usedPortsLocked()
		if used[req.Port] || (meta.Type == TypeBedrock && used[req.Port+1]) {
			return ServerView{}, fmt.Errorf("port %d is already used by another server", req.Port)
		}
	}
	id := m.uniqueIDLocked(name)
	dir := filepath.Join(m.root, id)
	dataDir := filepath.Join(dir, dataSubdir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return ServerView{}, err
	}
	if err := copyDir(srcData, dataDir); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, fmt.Errorf("copy data: %w", err)
	}

	newMeta := meta
	newMeta.ID = id
	newMeta.Name = name
	newMeta.Port = req.Port
	newMeta.CreatedAt = time.Now().UTC()
	newMeta.Autostart = false
	newMeta.NetworkServers = nil
	if err := applyPortToData(dataDir, newMeta.Type, name, req.Port); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	if err := fsutil.WriteJSONAtomic(filepath.Join(dir, metaFile), newMeta); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	srv := &Server{meta: newMeta, dir: dir, proc: NewProc(dir, dataDir)}
	m.attachHooks(srv)
	m.startWatch(srv)
	m.items[id] = srv
	v := srv.view()
	decorateDomain(&v, m.domainSnapshotLocked())
	m.TriggerDNSSync("server cloned")
	return v, nil
}

func applyPortToData(dataDir, typ, name string, port int) error {
	if typ == TypeVelocity {
		return writeVelocityConfig(dataDir, name, port)
	}
	if typ == TypeWaterfall {
		cfg := fmt.Sprintf("listeners:\n- query_port: %d\n  host: 0.0.0.0:%d\n  motd: '%s'\n", port, port, sanitizeMOTD(name))
		return fsutil.WriteFileAtomic(filepath.Join(dataDir, "config.yml"), []byte(cfg), 0o644)
	}
	propsPath := filepath.Join(dataDir, "server.properties")
	data, err := os.ReadFile(propsPath)
	if err != nil {
		// Create a minimal properties file if missing.
		var props string
		if typ == TypeBedrock {
			props = fmt.Sprintf("server-name=%s\nserver-port=%d\nserver-portv6=%d\n", sanitizeMOTD(name), port, port+1)
		} else {
			props = fmt.Sprintf("server-port=%d\nmotd=%s\n", port, sanitizeMOTD(name))
		}
		return fsutil.WriteFileAtomic(propsPath, []byte(props), 0o644)
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	seenPort, seenV6, seenName := false, false, false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "server-port=") && !strings.HasPrefix(trim, "server-portv6=") {
			out = append(out, fmt.Sprintf("server-port=%d", port))
			seenPort = true
			continue
		}
		if strings.HasPrefix(trim, "server-portv6=") {
			out = append(out, fmt.Sprintf("server-portv6=%d", port+1))
			seenV6 = true
			continue
		}
		if strings.HasPrefix(trim, "server-name=") {
			out = append(out, "server-name="+sanitizeMOTD(name))
			seenName = true
			continue
		}
		out = append(out, line)
	}
	if !seenPort {
		out = append(out, fmt.Sprintf("server-port=%d", port))
	}
	if typ == TypeBedrock && !seenV6 {
		out = append(out, fmt.Sprintf("server-portv6=%d", port+1))
	}
	if typ == TypeBedrock && !seenName {
		out = append(out, "server-name="+sanitizeMOTD(name))
	}
	return fsutil.WriteFileAtomic(propsPath, []byte(strings.Join(out, "\n")), 0o644)
}

// CreateFromBackup creates a new server by restoring a backup zip of another
// server (or any craftpanel data zip with craftpanel-pack.json).
type CreateFromBackupRequest struct {
	SourceID   string `json:"sourceId"`
	BackupName string `json:"backupName"`
	Name       string `json:"name"`
	Port       int    `json:"port"`
}

func (m *Manager) CreateFromBackup(req CreateFromBackupRequest) (ServerView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > nameMaxLength {
		return ServerView{}, fmt.Errorf("name must be 1-%d characters", nameMaxLength)
	}
	if !validBackupName(req.BackupName) {
		return ServerView{}, errors.New("invalid backup name")
	}
	if _, err := m.get(req.SourceID); err != nil {
		return ServerView{}, err
	}
	zipPath := filepath.Join(m.backupDirFor(req.SourceID), req.BackupName)
	if _, err := os.Stat(zipPath); err != nil {
		return ServerView{}, errors.New("backup not found")
	}
	man, err := readPackManifestFromZip(zipPath)
	if err != nil {
		return ServerView{}, err
	}
	if man.Type == "" || man.Version == "" {
		return ServerView{}, errors.New("backup has no craftpanel-pack.json metadata")
	}
	if !validServerType(man.Type) {
		return ServerView{}, fmt.Errorf("unsupported server type %q in backup", man.Type)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	port := req.Port
	if port == 0 {
		port = m.freePortLocked(man.Type)
	} else {
		used := m.usedPortsLocked()
		if used[port] || (man.Type == TypeBedrock && used[port+1]) {
			return ServerView{}, fmt.Errorf("port %d is already used by another server", port)
		}
	}
	id := m.uniqueIDLocked(name)
	dir := filepath.Join(m.root, id)
	dataDir := filepath.Join(dir, dataSubdir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return ServerView{}, err
	}
	if err := extractZipTo(zipPath, dataDir); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	mem := man.MemoryMB
	if mem == 0 {
		mem = 2048
	}
	meta := Instance{
		ID: id, Name: name, Type: man.Type, Version: man.Version,
		LoaderVersion: man.LoaderVersion, Port: port, MemoryMB: mem,
		Modpack: man.Modpack, CreatedAt: time.Now().UTC(),
	}
	if err := applyPortToData(dataDir, meta.Type, name, port); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	if err := fsutil.WriteJSONAtomic(filepath.Join(dir, metaFile), meta); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	srv := &Server{meta: meta, dir: dir, proc: NewProc(dir, dataDir)}
	if !srv.binaryExists() {
		srv.installErr = "server files missing, retry the installation"
	}
	m.attachHooks(srv)
	m.startWatch(srv)
	m.items[id] = srv
	v := srv.view()
	decorateDomain(&v, m.domainSnapshotLocked())
	m.TriggerDNSSync("server restored as new")
	return v, nil
}

// ImportServerRequest imports a zip of a data directory (or world-containing zip).
type ImportServerRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Version  string `json:"version"`
	MemoryMB int    `json:"memoryMB"`
	Port     int    `json:"port"`
	ZipPath  string // set by handler after saving upload
}

func (m *Manager) ImportServer(req ImportServerRequest) (ServerView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > nameMaxLength {
		return ServerView{}, fmt.Errorf("name must be 1-%d characters", nameMaxLength)
	}
	if req.ZipPath == "" {
		return ServerView{}, errors.New("zip is required")
	}
	man, _ := readPackManifestFromZip(req.ZipPath)
	typ := req.Type
	version := req.Version
	if man.Type != "" {
		typ = man.Type
	}
	if man.Version != "" {
		version = man.Version
	}
	if !validServerType(typ) {
		return ServerView{}, errors.New("type must be a supported server type")
	}
	if version == "" {
		return ServerView{}, errors.New("version is required (or provide a craftpanel backup zip)")
	}
	mem := req.MemoryMB
	if mem == 0 {
		mem = man.MemoryMB
	}
	if mem == 0 {
		mem = 2048
	}
	if mem < MinMemoryMB || mem > MaxMemoryMB {
		return ServerView{}, fmt.Errorf("memory must be between %d and %d MB", MinMemoryMB, MaxMemoryMB)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	port := req.Port
	if port == 0 {
		port = m.freePortLocked(typ)
	} else {
		used := m.usedPortsLocked()
		if used[port] || (typ == TypeBedrock && used[port+1]) {
			return ServerView{}, fmt.Errorf("port %d is already used by another server", port)
		}
	}
	id := m.uniqueIDLocked(name)
	dir := filepath.Join(m.root, id)
	dataDir := filepath.Join(dir, dataSubdir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return ServerView{}, err
	}
	if err := extractZipTo(req.ZipPath, dataDir); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	// If the zip was a single world folder (level.dat at root), nest it.
	if _, err := os.Stat(filepath.Join(dataDir, "level.dat")); err == nil {
		world := filepath.Join(dataDir, "world")
		tmp := dataDir + ".world-tmp"
		_ = os.Rename(dataDir, tmp)
		_ = os.MkdirAll(dataDir, 0o750)
		if err := os.Rename(tmp, world); err != nil {
			os.RemoveAll(dir)
			return ServerView{}, err
		}
		eula := "# Generated by ComputeBox Craftpanel\neula=false\n"
		_ = fsutil.WriteFileAtomic(filepath.Join(dataDir, "eula.txt"), []byte(eula), 0o644)
	}
	meta := Instance{
		ID: id, Name: name, Type: typ, Version: version,
		LoaderVersion: man.LoaderVersion, Port: port, MemoryMB: mem,
		Modpack: man.Modpack, CreatedAt: time.Now().UTC(),
	}
	if err := applyPortToData(dataDir, typ, name, port); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	if err := fsutil.WriteJSONAtomic(filepath.Join(dir, metaFile), meta); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	srv := &Server{meta: meta, dir: dir, proc: NewProc(dir, dataDir)}
	needInstall := !srv.binaryExists()
	if needInstall {
		srv.installing = true
	}
	m.attachHooks(srv)
	m.startWatch(srv)
	m.items[id] = srv
	if needInstall {
		go m.runInstall(srv, version)
	}
	v := srv.view()
	decorateDomain(&v, m.domainSnapshotLocked())
	m.TriggerDNSSync("server imported")
	return v, nil
}

func readPackManifestFromZip(zipPath string) (backupPackManifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return backupPackManifest{}, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		base := path.Base(zf.Name)
		if base != "craftpanel-pack.json" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return backupPackManifest{}, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return backupPackManifest{}, err
		}
		var man backupPackManifest
		if err := json.Unmarshal(data, &man); err != nil {
			return backupPackManifest{}, err
		}
		return man, nil
	}
	return backupPackManifest{}, errors.New("craftpanel-pack.json not found in zip")
}

func extractZipTo(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return err
	}
	for _, zf := range zr.File {
		rel, ok := safeZipRel(zf.Name)
		if !ok {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
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
		_, copyErr := io.Copy(out, in)
		out.Close()
		in.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
