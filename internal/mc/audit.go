package mc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry is one append-only audit log line.
type AuditEntry struct {
	Time     time.Time `json:"time"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	ServerID string    `json:"serverId,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	IP       string    `json:"ip,omitempty"`
}

var auditMu sync.Mutex

func (m *Manager) auditPath() string {
	return filepath.Join(m.dataDir, "audit.jsonl")
}

// Audit appends one event. Failures are ignored so auditing never breaks APIs.
func (m *Manager) Audit(actor, action, serverID, detail, ip string) {
	if m == nil {
		return
	}
	entry := AuditEntry{
		Time:     time.Now().UTC(),
		Actor:    actor,
		Action:   action,
		ServerID: serverID,
		Detail:   detail,
		IP:       ip,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')
	auditMu.Lock()
	defer auditMu.Unlock()
	f, err := os.OpenFile(m.auditPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	_, _ = f.Write(line)
	_ = f.Close()
}

// ListAudit returns the newest entries (up to limit). Newest first.
func (m *Manager) ListAudit(limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	data, err := os.ReadFile(m.auditPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []AuditEntry{}, nil
		}
		return nil, err
	}
	lines := splitLines(data)
	out := make([]AuditEntry, 0, limit)
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		var e AuditEntry
		if json.Unmarshal(lines[i], &e) != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
