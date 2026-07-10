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

	basePort    = 25565
	metaFile    = "server.json"
	dataSubdir  = "data"
	jarName     = "server.jar"
	MinMemoryMB = 512
	MaxMemoryMB = 65536
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
}

// ServerView is what the API returns: config plus runtime state.
type ServerView struct {
	Instance
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`
	UptimeS  int64   `json:"uptimeS"`
	EULA     bool    `json:"eula"`
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
}

// Manager owns all server instances below <dataDir>/servers.
type Manager struct {
	mu       sync.Mutex
	root     string // <dataDir>/servers
	items    map[string]*Server
	versions *Versions
}

func NewManager(dataDir string, versions *Versions) (*Manager, error) {
	root := filepath.Join(dataDir, "servers")
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	m := &Manager{root: root, items: map[string]*Server{}, versions: versions}

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
		srv := &Server{meta: meta, dir: dir, proc: NewProc(filepath.Join(dir, dataSubdir))}
		if !srv.jarExists() {
			srv.installing = false
			srv.installErr = "server jar missing, retry the installation"
		}
		m.items[meta.ID] = srv
	}
	return m, nil
}

// DataDir returns the working directory (world, configs) of a server.
func (s *Server) DataDir() string { return filepath.Join(s.dir, dataSubdir) }

func (s *Server) jarExists() bool {
	_, err := os.Stat(filepath.Join(s.DataDir(), jarName))
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
	return v
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
	if req.Type != TypeVanilla && req.Type != TypePaper {
		return ServerView{}, errors.New("type must be vanilla or paper")
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
		req.Port = m.freePortLocked()
	} else {
		for _, s := range m.items {
			if s.meta.Port == req.Port {
				return ServerView{}, fmt.Errorf("port %d is already used by another server", req.Port)
			}
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

	eula := "# Generated by ComputeBox Craftpanel\n# Read the Minecraft EULA at https://aka.ms/MinecraftEULA\neula=false\n"
	if err := fsutil.WriteFileAtomic(filepath.Join(dataDir, "eula.txt"), []byte(eula), 0o644); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	props := fmt.Sprintf("# Generated by ComputeBox Craftpanel\nserver-port=%d\nmotd=%s\n", req.Port, sanitizeMOTD(name))
	if err := fsutil.WriteFileAtomic(filepath.Join(dataDir, "server.properties"), []byte(props), 0o644); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}
	if err := fsutil.WriteJSONAtomic(filepath.Join(dir, metaFile), meta); err != nil {
		os.RemoveAll(dir)
		return ServerView{}, err
	}

	srv := &Server{meta: meta, dir: dir, proc: NewProc(dataDir), installing: true}
	m.items[id] = srv
	go m.runInstall(srv)
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
	srv.mu.Unlock()
	go m.runInstall(srv)
	return nil
}

func (m *Manager) runInstall(srv *Server) {
	srv.mu.Lock()
	typ, version := srv.meta.Type, srv.meta.Version
	srv.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	dest := filepath.Join(srv.DataDir(), jarName)
	err := m.versions.DownloadServerJar(ctx, typ, version, dest, func(done, total int64) {
		if total <= 0 {
			return
		}
		srv.mu.Lock()
		srv.installProgress = float64(done) / float64(total)
		srv.mu.Unlock()
	})

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
	log.Printf("install %s: downloaded %s %s", srv.meta.ID, typ, version)
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
	if srv.installing || srv.deleting || srv.proc.State() != StateStopped {
		srv.mu.Unlock()
		m.mu.Unlock()
		return ErrNotStopped
	}
	srv.deleting = true
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
	Name      *string `json:"name"`
	MemoryMB  *int    `json:"memoryMB"`
	JavaPath  *string `json:"javaPath"`
	Autostart *bool   `json:"autostart"`
}

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
	if s.deleting {
		return ErrNotFound
	}
	if s.installing {
		return errors.New("installation is still in progress")
	}
	if s.installErr != "" {
		return errors.New("installation failed, retry it first")
	}
	if !s.jarExists() {
		return errors.New("server jar is missing, retry the installation")
	}
	if !s.eulaAccepted() {
		return errors.New("eula not accepted")
	}
	javaPath := s.meta.JavaPath
	if javaPath == "" {
		javaPath = "java"
	}
	if _, err := exec.LookPath(javaPath); err != nil {
		return fmt.Errorf("java not found (%s): install a Java runtime or set a java path in the server settings", javaPath)
	}
	return s.proc.Start(javaPath, s.meta.MemoryMB, jarName)
}

func (m *Manager) Stop(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	return srv.proc.Stop()
}

func (m *Manager) Kill(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
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
func (m *Manager) StartAutostarts() {
	for _, v := range m.List() {
		if !v.Autostart {
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

func (m *Manager) freePortLocked() int {
	used := map[int]bool{}
	for _, s := range m.items {
		used[s.meta.Port] = true
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
