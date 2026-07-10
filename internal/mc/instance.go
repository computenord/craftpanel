package mc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const (
	StateInstalling    = "installing"
	StateInstallFailed = "install_failed"

	basePort        = 25565
	bedrockBasePort = 19132
	metaFile        = "server.json"
	dataSubdir      = "data"
	jarName         = "server.jar"
	MinMemoryMB     = 512
	MaxMemoryMB     = 65536
)

var (
	ErrNotFound   = errors.New("server not found")
	ErrNotStopped = errors.New("server must be stopped first")

	slugRe        = regexp.MustCompile(`[^a-z0-9-]+`)
	eulaLineRe    = regexp.MustCompile(`(?m)^\s*eula\s*=.*$`)
	propKeyRe     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	nameMaxLength = 40
)

// Instance is the persisted configuration of one Minecraft server.
type Instance struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // vanilla | paper
	Version   string    `json:"version"`
	Port      int       `json:"port"`
	MemoryMB  int       `json:"memoryMB"`
	JavaPath  string    `json:"javaPath,omitempty"`
	Autostart bool      `json:"autostart"`
	CreatedAt time.Time `json:"createdAt"`
	// JavaMajor is the Java version this Minecraft release requires, as
	// declared by Mojang. 0 means unknown, in which case no check is enforced.
	JavaMajor int `json:"javaMajor,omitempty"`

	RestartOnCrash bool   `json:"restartOnCrash"`
	BackupAuto     bool   `json:"backupAuto"`
	BackupTime     string `json:"backupTime,omitempty"` // "04:00"
	BackupKeep     int    `json:"backupKeep,omitempty"`
}

// ServerView is what the API returns: config plus runtime state.
type ServerView struct {
	Instance
	Status     string      `json:"status"`
	Progress   float64     `json:"progress"`
	Error      string      `json:"error,omitempty"`
	UptimeS    int64       `json:"uptimeS"`
	EULA       bool        `json:"eula"`
	Players    *PingResult `json:"players,omitempty"`
	CPUPct     float64     `json:"cpuPct"`
	RSSMB      int         `json:"rssMB"`
	DiskMB     int64       `json:"diskMB"`
	BackupBusy bool        `json:"backupBusy,omitempty"`
}

// Server couples persisted config with its supervisor process and install state.
type Server struct {
	mu   sync.Mutex
	meta Instance
	dir  string
	proc *Proc

	installing      bool
	installProgress float64
	installErr      string
	deleting        bool

	backupBusy     bool
	lastAutoBackup string
	crashCount     int
	restartTimer   *time.Timer

	cpuPrev  cpuSample
	diskMB   int64
	diskAt   time.Time
	diskBusy bool
	lastPing *PingResult
	pingAt   time.Time
	pingBusy bool

	players   *PlayersInfo
	playersAt time.Time
}

// Manager owns all server instances below <dataDir>/servers.
type Manager struct {
	mu           sync.Mutex
	root         string // <dataDir>/servers
	dataDir      string
	settingsPath string
	settings     PanelSettings
	items        map[string]*Server
	versions     *Versions
}

func NewManager(dataDir string, versions *Versions) (*Manager, error) {
	root := filepath.Join(dataDir, "servers")
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	m := &Manager{
		root:         root,
		dataDir:      dataDir,
		settingsPath: filepath.Join(dataDir, "config.json"),
		items:        map[string]*Server{},
		versions:     versions,
	}
	if err := fsutil.ReadJSON(m.settingsPath, &m.settings); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		var meta Instance
		if err := fsutil.ReadJSON(filepath.Join(dir, metaFile), &meta); err != nil {
			log.Printf("skipping %s: %v", dir, err)
			continue
		}
		srv := &Server{meta: meta, dir: dir, proc: NewProc(dir, filepath.Join(dir, dataSubdir))}
		if !srv.binaryExists() {
			srv.installing = false
			srv.installErr = "server files missing, retry the installation"
		}
		m.attachHooks(srv)
		// A previous panel instance (self update, crash) may have left this
		// server running; pick it back up instead of reporting it stopped.
		if srv.proc.TryAdopt() {
			log.Printf("reattached to running server %s (pid %d)", meta.ID, srv.proc.PID())
		}
		m.items[meta.ID] = srv
	}
	go m.runBackupScheduler()
	return m, nil
}

// DetachAll leaves running servers alive across a panel restart. Their run
// state is already persisted on disk, the next panel instance adopts them.
func (m *Manager) DetachAll() {
	for _, v := range m.List() {
		if v.Status != StateRunning && v.Status != StateStarting {
			continue
		}
		if srv, err := m.get(v.ID); err == nil {
			srv.proc.Note("Panel is restarting for an update, the server keeps running")
		}
		log.Printf("detaching %s, server keeps running", v.ID)
	}
}

// attachHooks wires the process exit event to the crash restart logic.
func (m *Manager) attachHooks(srv *Server) {
	srv.proc.SetExitHook(func(crashed bool, uptime time.Duration) {
		m.onServerExit(srv, crashed, uptime)
	})
}

func (m *Manager) onServerExit(srv *Server, crashed bool, uptime time.Duration) {
	srv.mu.Lock()
	if !crashed {
		srv.crashCount = 0
		srv.mu.Unlock()
		return
	}
	if srv.deleting || !srv.meta.RestartOnCrash {
		srv.mu.Unlock()
		return
	}
	// A long healthy run resets the backoff.
	if uptime > 5*time.Minute {
		srv.crashCount = 0
	}
	srv.crashCount++
	shift := srv.crashCount - 1
	if shift > 6 {
		shift = 6
	}
	delay := time.Duration(5<<uint(shift)) * time.Second
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	attempt := srv.crashCount
	srv.restartTimer = time.AfterFunc(delay, func() {
		if err := srv.start(); err != nil {
			srv.proc.Note("Automatic restart failed: " + err.Error())
		}
	})
	srv.mu.Unlock()
	srv.proc.Note(fmt.Sprintf("Server crashed, restarting in %s (attempt %d)", delay, attempt))
}

// cancelRestartLocked stops a pending crash restart. Callers hold srv.mu.
func (s *Server) cancelRestartLocked() {
	if s.restartTimer != nil {
		s.restartTimer.Stop()
		s.restartTimer = nil
	}
}

// DataDir returns the working directory (world, configs) of a server.
func (s *Server) DataDir() string { return filepath.Join(s.dir, dataSubdir) }

// binaryExists reports whether the server's executable payload is installed.
func (s *Server) binaryExists() bool {
	name := jarName
	if s.meta.Type == TypeBedrock {
		name = BedrockBinaryName()
	}
	_, err := os.Stat(filepath.Join(s.DataDir(), name))
	return err == nil
}

func (s *Server) view() ServerView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := ServerView{Instance: s.meta}
	switch {
	case s.installing:
		v.Status = StateInstalling
		v.Progress = s.installProgress
	case s.installErr != "":
		v.Status = StateInstallFailed
		v.Error = s.installErr
	default:
		v.Status = s.proc.State()
		v.UptimeS = int64(s.proc.Uptime().Seconds())
	}
	v.EULA = s.eulaAccepted()
	v.BackupBusy = s.backupBusy

	if v.Status == StateRunning || v.Status == StateStarting {
		cpu, rss, next := procUsage(s.proc.PID(), s.cpuPrev)
		s.cpuPrev = next
		v.CPUPct = float64(int(cpu*10)) / 10
		v.RSSMB = rss
	} else {
		s.cpuPrev = cpuSample{}
	}
	if v.Status == StateRunning {
		v.Players = s.cachedPingLocked()
	}
	v.DiskMB = s.cachedDiskLocked()
	return v
}

// cachedPingLocked returns the last server list ping result and refreshes it
// in the background when stale. Never blocks the API.
func (s *Server) cachedPingLocked() *PingResult {
	if time.Since(s.pingAt) > 10*time.Second && !s.pingBusy {
		s.pingBusy = true
		port := s.meta.Port
		typ := s.meta.Type
		go func() {
			var res *PingResult
			var err error
			if typ == TypeBedrock {
				res, err = PingBedrock(port, 2*time.Second)
			} else {
				res, err = PingStatus(port, 2*time.Second)
			}
			s.mu.Lock()
			if err != nil {
				s.lastPing = nil
			} else {
				s.lastPing = res
			}
			s.pingAt = time.Now()
			s.pingBusy = false
			s.mu.Unlock()
		}()
	}
	return s.lastPing
}

// cachedDiskLocked returns the server directory size, refreshed at most once
// a minute in the background.
func (s *Server) cachedDiskLocked() int64 {
	if time.Since(s.diskAt) > time.Minute && !s.diskBusy {
		s.diskBusy = true
		dir := s.dir
		go func() {
			size := dirSizeMB(dir)
			s.mu.Lock()
			s.diskMB = size
			s.diskAt = time.Now()
			s.diskBusy = false
			s.mu.Unlock()
		}()
	}
	return s.diskMB
}

func (s *Server) eulaAccepted() bool {
	data, err := os.ReadFile(filepath.Join(s.DataDir(), "eula.txt"))
	if err != nil {
		return false
	}
	return eulaLineRe.MatchString(string(data)) &&
		strings.Contains(strings.ReplaceAll(strings.ToLower(eulaLineRe.FindString(string(data))), " ", ""), "eula=true")
}

func (m *Manager) get(id string) (*Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return srv, nil
}

// Get returns the server or ErrNotFound. The returned handle is safe for
// concurrent use.
func (m *Manager) Get(id string) (*Server, error) { return m.get(id) }

func (m *Manager) List() []ServerView {
	m.mu.Lock()
	servers := make([]*Server, 0, len(m.items))
	for _, s := range m.items {
		servers = append(servers, s)
	}
	m.mu.Unlock()

	out := make([]ServerView, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.view())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Manager) View(id string) (ServerView, error) {
	srv, err := m.get(id)
	if err != nil {
		return ServerView{}, err
	}
	return srv.view(), nil
}

type CreateRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Version  string `json:"version"`
	MemoryMB int    `json:"memoryMB"`
	Port     int    `json:"port"`
}

func (m *Manager) Create(req CreateRequest) (ServerView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > nameMaxLength {
		return ServerView{}, fmt.Errorf("name must be 1-%d characters", nameMaxLength)
	}
	if req.Type != TypeVanilla && req.Type != TypePaper && req.Type != TypeBedrock {
		return ServerView{}, errors.New("type must be vanilla, paper or bedrock")
	}
	if req.Version == "" {
		return ServerView{}, errors.New("version is required")
	}
	if req.MemoryMB == 0 {
		req.MemoryMB = 2048
	}
	if req.MemoryMB < MinMemoryMB || req.MemoryMB > MaxMemoryMB {
		return ServerView{}, fmt.Errorf("memory must be between %d and %d MB", MinMemoryMB, MaxMemoryMB)
	}
	if req.Port != 0 && (req.Port < 1024 || req.Port > 65535) {
		return ServerView{}, errors.New("port must be between 1024 and 65535")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if req.Port == 0 {
		req.Port = m.freePortLocked(req.Type)
	} else {
		used := m.usedPortsLocked()
		if used[req.Port] || (req.Type == TypeBedrock && used[req.Port+1]) {
			return ServerView{}, fmt.Errorf("port %d is already used by another server", req.Port)
		}
	}

	id := m.uniqueIDLocked(name)
	dir := filepath.Join(m.root, id)
	dataDir := filepath.Join(dir, dataSubdir)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return ServerView{}, err
	}

	meta := Instance{
		ID:        id,
		Name:      name,
		Type:      req.Type,
		Version:   req.Version,
		Port:      req.Port,
		MemoryMB:  req.MemoryMB,
		CreatedAt: time.Now().UTC(),
	}

	// Bedrock has no eula.txt mechanism, the panel still keeps one as its own
	// record that the operator agreed to the Minecraft EULA.
	eula := "# Generated by ComputeBox Craftpanel\n# Read the Minecraft EULA at https://aka.ms/MinecraftEULA\neula=false\n"
	if err := fsutil.WriteFileAtomic(filepath.Join(dataDir, "eula.txt"), []byte(eula), 0o644); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	var props string
	if req.Type == TypeBedrock {
		props = fmt.Sprintf("server-name=%s\nserver-port=%d\nserver-portv6=%d\n", sanitizeMOTD(name), req.Port, req.Port+1)
	} else {
		props = fmt.Sprintf("# Generated by ComputeBox Craftpanel\nserver-port=%d\nmotd=%s\n", req.Port, sanitizeMOTD(name))
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(dataDir, "server.properties"), []byte(props), 0o644); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	if err := fsutil.WriteJSONAtomic(filepath.Join(dir, metaFile), meta); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}

	srv := &Server{meta: meta, dir: dir, proc: NewProc(dir, dataDir), installing: true}
	m.attachHooks(srv)
	m.items[id] = srv
	go m.runInstall(srv, meta.Version)
	return srv.view(), nil
}

// RetryInstall restarts a failed jar download.
func (m *Manager) RetryInstall(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	if srv.installing {
		srv.mu.Unlock()
		return errors.New("installation already in progress")
	}
	if srv.deleting {
		srv.mu.Unlock()
		return ErrNotFound
	}
	if srv.proc.State() != StateStopped {
		srv.mu.Unlock()
		return ErrNotStopped
	}
	srv.installing = true
	srv.installErr = ""
	srv.installProgress = 0
	version := srv.meta.Version
	srv.mu.Unlock()
	go m.runInstall(srv, version)
	return nil
}

// Upgrade downloads a different Minecraft version for an existing server. The
// world is kept; meta is only updated once the new jar is verified on disk.
func (m *Manager) Upgrade(id, version string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return errors.New("version is required")
	}
	srv.mu.Lock()
	if srv.installing {
		srv.mu.Unlock()
		return errors.New("installation already in progress")
	}
	if srv.deleting || srv.backupBusy || srv.proc.State() != StateStopped {
		srv.mu.Unlock()
		return ErrNotStopped
	}
	srv.installing = true
	srv.installErr = ""
	srv.installProgress = 0
	srv.mu.Unlock()
	go m.runInstall(srv, version)
	return nil
}

// runInstall downloads the server jar for targetVersion and, on success,
// persists the (possibly changed) version and its Java requirement.
func (m *Manager) runInstall(srv *Server, targetVersion string) {
	srv.mu.Lock()
	typ := srv.meta.Type
	version := targetVersion
	srv.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	progress := func(done, total int64) {
		if total <= 0 {
			return
		}
		srv.mu.Lock()
		srv.installProgress = float64(done) / float64(total)
		srv.mu.Unlock()
	}

	var err error
	javaMajor := 0
	if typ == TypeBedrock {
		var installed string
		installed, err = m.versions.InstallBedrock(ctx, srv.DataDir(), progress)
		if err == nil {
			version = installed
		}
	} else {
		dest := filepath.Join(srv.DataDir(), jarName)
		err = m.versions.DownloadServerJar(ctx, typ, version, dest, progress)
		// Record the Java requirement while we are online anyway, so a later
		// start can fail fast instead of dying with a JVM exit status.
		if err == nil {
			if major, jerr := m.versions.JavaMajor(ctx, version); jerr == nil {
				javaMajor = major
			} else {
				log.Printf("install %s: java requirement lookup failed: %v", srv.meta.ID, jerr)
			}
		}
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.installing = false
	if err != nil {
		srv.installErr = err.Error()
		log.Printf("install %s: %v", srv.meta.ID, err)
		return
	}
	srv.installErr = ""
	srv.installProgress = 1
	changed := false
	if srv.meta.Version != version {
		srv.meta.Version = version
		changed = true
	}
	if javaMajor > 0 && srv.meta.JavaMajor != javaMajor {
		srv.meta.JavaMajor = javaMajor
		changed = true
	}
	if changed {
		if werr := fsutil.WriteJSONAtomic(filepath.Join(srv.dir, metaFile), srv.meta); werr != nil {
			log.Printf("install %s: persist meta: %v", srv.meta.ID, werr)
		}
	}
	log.Printf("install %s: downloaded %s %s (needs java %d)", srv.meta.ID, typ, version, javaMajor)
}

// Delete removes a stopped server. The directory removal happens outside both
// locks: it can take a while on a large world and would otherwise stall every
// other request, including the dashboard poll.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	srv, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	srv.mu.Lock()
	if srv.installing || srv.deleting || srv.backupBusy || srv.proc.State() != StateStopped {
		srv.mu.Unlock()
		m.mu.Unlock()
		return ErrNotStopped
	}
	srv.deleting = true
	srv.cancelRestartLocked()
	srv.mu.Unlock()
	delete(m.items, id)
	m.mu.Unlock()

	if err := os.RemoveAll(srv.dir); err != nil {
		m.mu.Lock()
		srv.mu.Lock()
		srv.deleting = false
		srv.mu.Unlock()
		m.items[id] = srv
		m.mu.Unlock()
		return err
	}
	return nil
}

type UpdateRequest struct {
	Name           *string `json:"name"`
	MemoryMB       *int    `json:"memoryMB"`
	JavaPath       *string `json:"javaPath"`
	Autostart      *bool   `json:"autostart"`
	RestartOnCrash *bool   `json:"restartOnCrash"`
	BackupAuto     *bool   `json:"backupAuto"`
	BackupTime     *string `json:"backupTime"`
	BackupKeep     *int    `json:"backupKeep"`
}

var backupTimeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (m *Manager) Update(id string, req UpdateRequest) (ServerView, error) {
	srv, err := m.get(id)
	if err != nil {
		return ServerView{}, err
	}
	srv.mu.Lock()
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > nameMaxLength {
			srv.mu.Unlock()
			return ServerView{}, fmt.Errorf("name must be 1-%d characters", nameMaxLength)
		}
		srv.meta.Name = name
	}
	if req.MemoryMB != nil {
		if *req.MemoryMB < MinMemoryMB || *req.MemoryMB > MaxMemoryMB {
			srv.mu.Unlock()
			return ServerView{}, fmt.Errorf("memory must be between %d and %d MB", MinMemoryMB, MaxMemoryMB)
		}
		srv.meta.MemoryMB = *req.MemoryMB
	}
	if req.JavaPath != nil {
		srv.meta.JavaPath = strings.TrimSpace(*req.JavaPath)
	}
	if req.Autostart != nil {
		srv.meta.Autostart = *req.Autostart
	}
	if req.RestartOnCrash != nil {
		srv.meta.RestartOnCrash = *req.RestartOnCrash
	}
	if req.BackupAuto != nil {
		srv.meta.BackupAuto = *req.BackupAuto
	}
	if req.BackupTime != nil {
		bt := strings.TrimSpace(*req.BackupTime)
		if bt != "" && !backupTimeRe.MatchString(bt) {
			srv.mu.Unlock()
			return ServerView{}, errors.New("backup time must be HH:MM")
		}
		srv.meta.BackupTime = bt
	}
	if req.BackupKeep != nil {
		if *req.BackupKeep < 1 || *req.BackupKeep > 365 {
			srv.mu.Unlock()
			return ServerView{}, errors.New("backup keep count must be between 1 and 365")
		}
		srv.meta.BackupKeep = *req.BackupKeep
	}
	err = fsutil.WriteJSONAtomic(filepath.Join(srv.dir, metaFile), srv.meta)
	srv.mu.Unlock()
	if err != nil {
		return ServerView{}, err
	}
	return srv.view(), nil
}

func (m *Manager) Start(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	return srv.start()
}

// start holds s.mu for the whole launch so that a concurrent Delete or
// RetryInstall cannot slip in between the checks and the actual process start.
func (s *Server) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelRestartLocked()
	if s.deleting {
		return ErrNotFound
	}
	if s.backupBusy {
		return errors.New("a backup or restore is running")
	}
	if s.installing {
		return errors.New("installation is still in progress")
	}
	if s.installErr != "" {
		return errors.New("installation failed, retry it first")
	}
	if !s.binaryExists() {
		return errors.New("server jar is missing, retry the installation")
	}
	if !s.eulaAccepted() {
		return errors.New("eula not accepted")
	}

	if s.meta.Type == TypeBedrock {
		bin := filepath.Join(s.DataDir(), BedrockBinaryName())
		// The BDS binary links its bundled libraries from its own directory.
		return s.proc.Start(bin, nil, []string{"LD_LIBRARY_PATH=."}, "Server started.")
	}

	javaPath := s.meta.JavaPath
	if javaPath == "" {
		javaPath = "java"
	}
	if _, err := exec.LookPath(javaPath); err != nil {
		return fmt.Errorf("java not found (%s): install a Java runtime or set a java path in the server settings", javaPath)
	}
	// Refuse the launch instead of letting the JVM die with a bare exit status.
	if s.meta.JavaMajor > 0 {
		if have, _ := DetectJava(javaPath); have > 0 && have < s.meta.JavaMajor {
			return &JavaTooOldError{Need: s.meta.JavaMajor, Have: have}
		}
	}
	memMB := s.meta.MemoryMB
	xms := memMB
	if xms > 512 {
		xms = 512
	}
	args := []string{
		fmt.Sprintf("-Xms%dM", xms),
		fmt.Sprintf("-Xmx%dM", memMB),
		"-Dfile.encoding=UTF-8",
		// Defense in depth for pre-1.18.1 servers (Log4Shell).
		"-Dlog4j2.formatMsgNoLookups=true",
		"-jar", jarName,
		"nogui",
	}
	return s.proc.Start(javaPath, args, nil, "]: Done (")
}

// JavaTooOldError reports that the installed JVM predates what the chosen
// Minecraft version requires.
type JavaTooOldError struct {
	Need int
	Have int
}

func (e *JavaTooOldError) Error() string {
	return fmt.Sprintf("this server needs Java %d or newer, but the host has Java %d", e.Need, e.Have)
}

func (m *Manager) Stop(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	srv.cancelRestartLocked()
	srv.crashCount = 0
	srv.mu.Unlock()
	return srv.proc.Stop()
}

func (m *Manager) Kill(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	srv.cancelRestartLocked()
	srv.crashCount = 0
	srv.mu.Unlock()
	return srv.proc.Kill()
}

func (m *Manager) Restart(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	if err := srv.proc.Stop(); err != nil {
		return err
	}
	return srv.start()
}

func (m *Manager) SendCommand(id, command string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	return srv.proc.SendCommand(command)
}

func (m *Manager) Subscribe(id string) (history []string, ch chan Event, cancel func(), err error) {
	srv, err := m.get(id)
	if err != nil {
		return nil, nil, nil, err
	}
	history, ch, cancel = srv.proc.Subscribe()
	return history, ch, cancel, nil
}

// SetEULA rewrites the eula line in eula.txt, preserving surrounding content.
func (m *Manager) SetEULA(id string, accept bool) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	path := filepath.Join(srv.DataDir(), "eula.txt")
	val := "false"
	if accept {
		val = "true"
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	if eulaLineRe.MatchString(content) {
		content = eulaLineRe.ReplaceAllString(content, "eula="+val)
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "eula=" + val + "\n"
	}
	return fsutil.WriteFileAtomic(path, []byte(content), 0o644)
}

// Properties returns the parsed server.properties as ordered key/value pairs.
func (m *Manager) Properties(id string) ([][2]string, error) {
	srv, err := m.get(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(srv.DataDir(), "server.properties"))
	if err != nil {
		if os.IsNotExist(err) {
			return [][2]string{}, nil
		}
		return nil, err
	}
	var out [][2]string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		if k, v, ok := strings.Cut(trimmed, "="); ok {
			out = append(out, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
		}
	}
	return out, nil
}

// SetProperties updates or appends the given keys in server.properties,
// preserving comments and unknown keys.
func (m *Manager) SetProperties(id string, set map[string]string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	for k, v := range set {
		if !propKeyRe.MatchString(k) {
			return fmt.Errorf("invalid property key %q", k)
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("property %q must be a single line", k)
		}
	}

	// The whole read-modify-write must be atomic, otherwise two concurrent
	// saves lose each other's keys.
	srv.mu.Lock()
	defer srv.mu.Unlock()

	path := filepath.Join(srv.DataDir(), "server.properties")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if v, in := set[k]; in {
			lines[i] = k + "=" + v
			seen[k] = true
		}
	}
	for k, v := range set {
		if !seen[k] {
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			lines = append(lines, k+"="+v, "")
		}
	}
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return err
	}

	// Keep the panel's port in sync when server-port is changed here.
	if portStr, ok := set["server-port"]; ok {
		if port, perr := parsePort(portStr); perr == nil {
			srv.meta.Port = port
			return fsutil.WriteJSONAtomic(filepath.Join(srv.dir, metaFile), srv.meta)
		}
	}
	return nil
}

// StartAutostarts starts all servers marked autostart (used on panel boot).
// Servers adopted from a previous panel instance are already running.
func (m *Manager) StartAutostarts() {
	for _, v := range m.List() {
		if !v.Autostart || v.Status != StateStopped {
			continue
		}
		if err := m.Start(v.ID); err != nil {
			log.Printf("autostart %s: %v", v.ID, err)
		} else {
			log.Printf("autostart %s", v.ID)
		}
	}
}

// StopAll gracefully stops every running server, bounded by ctx.
func (m *Manager) StopAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, v := range m.List() {
		if v.Status != StateRunning && v.Status != StateStarting && v.Status != StateStopping {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := m.Stop(id); err != nil {
				log.Printf("stop %s: %v", id, err)
			}
		}(v.ID)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("shutdown: timeout waiting for servers to stop")
	}
}

// usedPortsLocked collects every port claimed by a server. Bedrock servers
// occupy two consecutive UDP ports (IPv4 and IPv6).
func (m *Manager) usedPortsLocked() map[int]bool {
	used := map[int]bool{}
	for _, s := range m.items {
		used[s.meta.Port] = true
		if s.meta.Type == TypeBedrock {
			used[s.meta.Port+1] = true
		}
	}
	return used
}

func (m *Manager) freePortLocked(typ string) int {
	used := m.usedPortsLocked()
	if typ == TypeBedrock {
		for p := bedrockBasePort; p < 65534; p += 2 {
			if !used[p] && !used[p+1] {
				return p
			}
		}
		return bedrockBasePort
	}
	for p := basePort; p < 65536; p++ {
		if !used[p] {
			return p
		}
	}
	return basePort
}

func (m *Manager) uniqueIDLocked(name string) string {
	slug := slugRe.ReplaceAllString(strings.ToLower(name), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "server"
	}
	if len(slug) > 32 {
		slug = slug[:32]
	}
	id := slug
	for i := 2; ; i++ {
		if _, exists := m.items[id]; !exists {
			if _, err := os.Stat(filepath.Join(m.root, id)); os.IsNotExist(err) {
				return id
			}
		}
		id = fmt.Sprintf("%s-%d", slug, i)
	}
}

func sanitizeMOTD(s string) string {
	s = strings.ReplaceAll(s, "\\", "")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func parsePort(s string) (int, error) {
	var p int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &p); err != nil {
		return 0, err
	}
	if p < 1 || p > 65535 {
		return 0, errors.New("port out of range")
	}
	return p, nil
}
