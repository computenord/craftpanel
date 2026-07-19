// Package node implements multi-host remote agents that report servers to a panel.
package node

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// Node is a registered remote agent.
type Node struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	TokenHash    string    `json:"tokenHash"`
	CreatedAt    time.Time `json:"createdAt"`
	LastSeen     time.Time `json:"lastSeen,omitempty"`
	Version      string    `json:"version,omitempty"`
	Online       bool      `json:"online"`
	ServerCount  int       `json:"serverCount"`
}

// NodeView is the safe API representation (no token hash).
type NodeView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	LastSeen    time.Time `json:"lastSeen,omitempty"`
	Version     string    `json:"version,omitempty"`
	Online      bool      `json:"online"`
	ServerCount int       `json:"serverCount"`
	// Token is only set on enroll, once.
	Token string `json:"token,omitempty"`
}

// RemoteServer is a server snapshot pushed by an agent.
type RemoteServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
	MemoryMB int   `json:"memoryMB"`
}

// Command is a queued action for an agent.
type Command struct {
	ID       string `json:"id"`
	Op       string `json:"op"` // start | stop | restart | kill
	ServerID string `json:"serverId"`
}

// SyncRequest is posted by the agent.
type SyncRequest struct {
	Version string         `json:"version"`
	Servers []RemoteServer `json:"servers"`
	Results []CommandResult `json:"results,omitempty"`
}

type CommandResult struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// SyncResponse is returned to the agent.
type SyncResponse struct {
	Commands []Command `json:"commands"`
}

// Registry persists nodes and in-memory command queues / server caches.
type Registry struct {
	mu       sync.Mutex
	path     string
	nodes    []Node
	queues   map[string][]Command         // nodeID -> pending
	servers  map[string][]RemoteServer    // nodeID -> last report
	onlineFor time.Duration
}

func NewRegistry(dataDir string) (*Registry, error) {
	r := &Registry{
		path:      filepath.Join(dataDir, "nodes.json"),
		queues:    map[string][]Command{},
		servers:   map[string][]RemoteServer{},
		onlineFor: 45 * time.Second,
	}
	if err := fsutil.ReadJSON(r.path, &r.nodes); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if r.nodes == nil {
		r.nodes = []Node{}
	}
	return r, nil
}

func (r *Registry) saveLocked() error {
	return fsutil.WriteJSONAtomic(r.path, r.nodes)
}

func (r *Registry) Enroll(name string) (NodeView, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 {
		return NodeView{}, errors.New("name must be 1-40 characters")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return NodeView{}, err
	}
	idRaw := make([]byte, 8)
	if _, err := rand.Read(idRaw); err != nil {
		return NodeView{}, err
	}
	token := "node_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	n := Node{
		ID:        hex.EncodeToString(idRaw),
		Name:      name,
		TokenHash: hex.EncodeToString(sum[:]),
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = append(r.nodes, n)
	if err := r.saveLocked(); err != nil {
		r.nodes = r.nodes[:len(r.nodes)-1]
		return NodeView{}, err
	}
	return NodeView{ID: n.ID, Name: n.Name, CreatedAt: n.CreatedAt, Token: token}, nil
}

func (r *Registry) List() []NodeView {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	out := make([]NodeView, 0, len(r.nodes))
	for _, n := range r.nodes {
		online := !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < r.onlineFor
		out = append(out, NodeView{
			ID: n.ID, Name: n.Name, CreatedAt: n.CreatedAt, LastSeen: n.LastSeen,
			Version: n.Version, Online: online, ServerCount: len(r.servers[n.ID]),
		})
	}
	return out
}

func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.nodes {
		if n.ID != id {
			continue
		}
		r.nodes = append(r.nodes[:i], r.nodes[i+1:]...)
		delete(r.queues, id)
		delete(r.servers, id)
		return r.saveLocked()
	}
	return errors.New("node not found")
}

func (r *Registry) Authenticate(token string) (Node, bool) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "node_") {
		return Node{}, false
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.nodes {
		if n.TokenHash == hash {
			return n, true
		}
	}
	return Node{}, false
}

func (r *Registry) Sync(nodeID string, req SyncRequest) SyncResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.nodes {
		if r.nodes[i].ID != nodeID {
			continue
		}
		r.nodes[i].LastSeen = time.Now().UTC()
		r.nodes[i].Version = req.Version
		r.nodes[i].ServerCount = len(req.Servers)
		_ = r.saveLocked()
		break
	}
	r.servers[nodeID] = req.Servers
	cmds := r.queues[nodeID]
	r.queues[nodeID] = nil
	if cmds == nil {
		cmds = []Command{}
	}
	return SyncResponse{Commands: cmds}
}

func (r *Registry) Enqueue(nodeID, op, serverID string) (Command, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, n := range r.nodes {
		if n.ID == nodeID {
			found = true
			break
		}
	}
	if !found {
		return Command{}, errors.New("node not found")
	}
	idRaw := make([]byte, 8)
	_, _ = rand.Read(idRaw)
	cmd := Command{ID: hex.EncodeToString(idRaw), Op: op, ServerID: serverID}
	r.queues[nodeID] = append(r.queues[nodeID], cmd)
	return cmd, nil
}

func (r *Registry) AllServers() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	out := []map[string]any{}
	for _, n := range r.nodes {
		online := !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < r.onlineFor
		for _, s := range r.servers[n.ID] {
			out = append(out, map[string]any{
				"id":       n.ID + "/" + s.ID,
				"name":     s.Name,
				"type":     s.Type,
				"version":  s.Version,
				"port":     s.Port,
				"status":   s.Status,
				"memoryMB": s.MemoryMB,
				"nodeId":   n.ID,
				"nodeName": n.Name,
				"nodeOnline": online,
			})
		}
	}
	return out
}

// ParseCompositeID splits "nodeId/serverId".
func ParseCompositeID(id string) (nodeID, serverID string, ok bool) {
	i := strings.IndexByte(id, '/')
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}

// DebugJSON helper for tests.
func (r *Registry) DebugJSON() string {
	b, _ := json.Marshal(r.List())
	return string(b)
}
