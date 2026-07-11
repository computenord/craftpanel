package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
	"github.com/computenord/craftpanel/internal/mc"
)

const (
	syncInterval  = 10 * time.Second
	configFile    = "agent.json"
	stateFile     = "agent-state.json"
	httpTimeout   = 20 * time.Second
	enrollBackoff = 15 * time.Second
)

// Config is what the Infra API bootstrap injects into the VM.
type Config struct {
	ControlPlaneURL string `json:"controlPlaneUrl"`
	InstanceID      string `json:"instanceId"`
	EnrollmentToken string `json:"enrollmentToken"`
}

// state is persisted after a successful enrollment.
type state struct {
	InstanceToken string `json:"instanceToken"`
	SSOPublicKey  string `json:"ssoPublicKey"`
}

type hostSampler struct {
	prevTotal uint64
	prevIdle  uint64
}

// UpdateFunc self-updates the panel to the given version. Injected by main so
// the agent reuses the panel's verified self-update path.
type UpdateFunc func(version string) error

// Agent runs the managed-mode control loop.
type Agent struct {
	cfg     Config
	dataDir string
	manager *mc.Manager
	version string
	client  *http.Client
	sampler hostSampler
	bootID  string
	seq     int64
	update  UpdateFunc

	mu       sync.Mutex
	st       state
	lock     atomic.Value // string, current lock level
	pending  []CommandResult
	appliedV string // last panelVersion we acted on, avoids update loops
}

// Load reads agent.json from the data directory. Returns (nil, nil) when the
// panel is not provisioned for managed mode, so the caller can run normally.
func Load(dataDir string, manager *mc.Manager, version string, update UpdateFunc) (*Agent, error) {
	var cfg Config
	if err := fsutil.ReadJSON(filepath.Join(dataDir, configFile), &cfg); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if cfg.ControlPlaneURL == "" || cfg.InstanceID == "" {
		return nil, errors.New("agent.json is missing controlPlaneUrl or instanceId")
	}
	cfg.ControlPlaneURL = strings.TrimRight(cfg.ControlPlaneURL, "/")

	bid := make([]byte, 8)
	rand.Read(bid)
	a := &Agent{
		cfg:     cfg,
		dataDir: dataDir,
		manager: manager,
		version: version,
		client:  &http.Client{Timeout: httpTimeout},
		bootID:  hex.EncodeToString(bid),
		update:  update,
	}
	a.lock.Store(LockNone)
	_ = fsutil.ReadJSON(filepath.Join(dataDir, stateFile), &a.st)
	return a, nil
}

// Lock reports the current lock level for the web layer to enforce.
func (a *Agent) Lock() string {
	v, _ := a.lock.Load().(string)
	if v == "" {
		return LockNone
	}
	return v
}

// SSOPublicKey returns the pinned control-plane key (PEM) for verifying SSO
// jumps, or "" before enrollment.
func (a *Agent) SSOPublicKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.st.SSOPublicKey
}

// Run drives enrollment then the sync loop until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	if a.st.InstanceToken == "" {
		if !a.enroll(ctx) {
			return // ctx cancelled during enrollment
		}
	}
	log.Printf("agent: managed mode active, syncing to %s as %s", a.cfg.ControlPlaneURL, a.cfg.InstanceID)
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	a.sync(ctx) // first sync immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sync(ctx)
		}
	}
}

func (a *Agent) enroll(ctx context.Context) bool {
	req := EnrollRequest{
		ProtocolVersion: ProtocolVersion,
		InstanceID:      a.cfg.InstanceID,
		EnrollmentToken: a.cfg.EnrollmentToken,
		AgentVersion:    a.version,
		PanelVersion:    a.version,
		BootID:          a.bootID,
	}
	for {
		var resp EnrollResponse
		err := a.post(ctx, "/api/agent/enroll", "", req, &resp)
		if err == nil && resp.InstanceToken != "" {
			a.mu.Lock()
			a.st = state{InstanceToken: resp.InstanceToken, SSOPublicKey: resp.SSOPublicKey}
			a.mu.Unlock()
			if werr := fsutil.WriteJSONAtomic(filepath.Join(a.dataDir, stateFile), a.st); werr != nil {
				log.Printf("agent: persist state: %v", werr)
			}
			log.Printf("agent: enrolled as %s", a.cfg.InstanceID)
			return true
		}
		if err != nil {
			log.Printf("agent: enroll failed, retrying: %v", err)
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(enrollBackoff):
		}
	}
}

func (a *Agent) sync(ctx context.Context) {
	a.mu.Lock()
	token := a.st.InstanceToken
	results := a.pending
	a.pending = nil
	a.mu.Unlock()

	body := SyncRequest{
		ProtocolVersion: ProtocolVersion,
		InstanceID:      a.cfg.InstanceID,
		Seq:             atomic.AddInt64(&a.seq, 1),
		BootID:          a.bootID,
		AgentVersion:    a.version,
		PanelVersion:    a.version,
		Host:            a.sampler.readHost(a.dataDir),
		Servers:         a.serverStats(),
		Results:         results,
	}
	var resp SyncResponse
	if err := a.post(ctx, "/api/agent/sync", token, body, &resp); err != nil {
		// Put the results back so they are re-reported next tick.
		if len(results) > 0 {
			a.mu.Lock()
			a.pending = append(results, a.pending...)
			a.mu.Unlock()
		}
		log.Printf("agent: sync failed: %v", err)
		return
	}
	a.applyDesired(ctx, resp.Desired)
	for _, cmd := range resp.Commands {
		a.runCommand(cmd)
	}
}

func (a *Agent) serverStats() []ServerStat {
	views := a.manager.List()
	out := make([]ServerStat, 0, len(views))
	for _, v := range views {
		s := ServerStat{
			ID: v.ID, Name: v.Name, Type: v.Type, Version: v.Version,
			Status: v.Status, RSSMB: v.RSSMB, CPUPct: v.CPUPct, DiskMB: v.DiskMB,
		}
		if v.Players != nil {
			s.Online = v.Players.Online
			s.Max = v.Players.Max
		}
		out = append(out, s)
	}
	return out
}

func (a *Agent) applyDesired(ctx context.Context, d Desired) {
	// Lock transitions.
	newLock := d.Lock
	if newLock == "" {
		newLock = LockNone
	}
	old := a.Lock()
	if newLock != old {
		a.lock.Store(newLock)
		log.Printf("agent: lock %s -> %s", old, newLock)
		if newLock == LockLocked || newLock == LockSuspended {
			// Stop the servers when the instance is locked or being suspended.
			go func() {
				sctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
				defer cancel()
				a.manager.StopAll(sctx)
			}()
		}
	}

	// Panel version reconciliation.
	if d.PanelVersion != "" && d.PanelVersion != a.version && d.PanelVersion != a.appliedV {
		a.appliedV = d.PanelVersion
		if a.update == nil {
			log.Printf("agent: desired panel version %s but no updater wired", d.PanelVersion)
			return
		}
		log.Printf("agent: updating panel to %s (control plane request)", d.PanelVersion)
		go func(v string) {
			if err := a.update(v); err != nil {
				log.Printf("agent: update to %s failed: %v", v, err)
			}
		}(d.PanelVersion)
	}
}

func (a *Agent) runCommand(cmd Command) {
	res := CommandResult{CommandID: cmd.ID, Status: "done"}
	switch cmd.Kind {
	case "forceBackup":
		id, _ := cmd.Args["serverId"].(string)
		if id == "" {
			res.Status, res.Detail = "error", "missing serverId"
		} else if err := a.manager.StartBackup(id, "manual"); err != nil {
			res.Status, res.Detail = "error", err.Error()
		}
	default:
		res.Status, res.Detail = "error", "unknown command "+cmd.Kind
	}
	a.mu.Lock()
	a.pending = append(a.pending, res)
	a.mu.Unlock()
}

// post sends a JSON request and decodes the JSON response. token, if set, is
// sent as a bearer credential.
func (a *Agent) post(ctx context.Context, path, token string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.ControlPlaneURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Craftpanel-Agent/"+a.version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized (token rejected)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}
