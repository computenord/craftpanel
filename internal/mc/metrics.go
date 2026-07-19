package mc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MetricSample is one CPU/RAM observation for a server.
type MetricSample struct {
	Time   int64   `json:"t"` // unix ms
	CPUPct float64 `json:"cpu"`
	RSSMB  int     `json:"rss"`
}

const metricsKeep = 2880 // ~24h at 30s

var metricsMu sync.Mutex

func (m *Manager) metricsPath(id string) string {
	return filepath.Join(m.dataDir, "metrics", id+".jsonl")
}

// RecordMetricsSample appends one sample and prunes old lines.
func (m *Manager) RecordMetricsSample(id string, cpu float64, rss int) {
	if m == nil || id == "" {
		return
	}
	sample := MetricSample{Time: time.Now().UnixMilli(), CPUPct: cpu, RSSMB: rss}
	line, err := json.Marshal(sample)
	if err != nil {
		return
	}
	line = append(line, '\n')
	dir := filepath.Join(m.dataDir, "metrics")
	_ = os.MkdirAll(dir, 0o750)
	metricsMu.Lock()
	defer metricsMu.Unlock()
	path := m.metricsPath(id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	_, _ = f.Write(line)
	_ = f.Close()

	// Occasional prune
	if time.Now().Unix()%120 < 30 {
		pruneMetricsFile(path, metricsKeep)
	}
}

func pruneMetricsFile(path string, keep int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := splitLines(data)
	if len(lines) <= keep {
		return
	}
	lines = lines[len(lines)-keep:]
	var buf []byte
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		buf = append(buf, l...)
		buf = append(buf, '\n')
	}
	_ = os.WriteFile(path, buf, 0o640)
}

// ListMetrics returns samples newer than sinceMs (0 = all kept).
func (m *Manager) ListMetrics(id string, sinceMs int64) ([]MetricSample, error) {
	if _, err := m.get(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(m.metricsPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return []MetricSample{}, nil
		}
		return nil, err
	}
	out := []MetricSample{}
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var s MetricSample
		if json.Unmarshal(line, &s) != nil {
			continue
		}
		if sinceMs > 0 && s.Time < sinceMs {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// runMetricsSampler records CPU/RSS for running servers every 30s.
func (m *Manager) runMetricsSampler() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		servers := make([]*Server, 0, len(m.items))
		for _, s := range m.items {
			servers = append(servers, s)
		}
		m.mu.Unlock()
		for _, srv := range servers {
			v := srv.view()
			if v.Status != StateRunning {
				continue
			}
			m.RecordMetricsSample(v.ID, v.CPUPct, v.RSSMB)
		}
	}
}
