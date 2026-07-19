package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// AgentConfig is persisted on the node host as node-agent.json.
type AgentConfig struct {
	PanelURL string `json:"panelUrl"`
	Token    string `json:"token"`
}

func AgentConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "node-agent.json")
}

func LoadAgentConfig(dataDir string) (AgentConfig, error) {
	var cfg AgentConfig
	if err := fsutil.ReadJSON(AgentConfigPath(dataDir), &cfg); err != nil {
		return AgentConfig{}, err
	}
	cfg.PanelURL = strings.TrimRight(strings.TrimSpace(cfg.PanelURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.PanelURL == "" || cfg.Token == "" {
		return AgentConfig{}, errors.New("node-agent.json incomplete")
	}
	return cfg, nil
}

func WriteAgentConfig(dataDir string, cfg AgentConfig) error {
	cfg.PanelURL = strings.TrimRight(strings.TrimSpace(cfg.PanelURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.PanelURL == "" || cfg.Token == "" {
		return errors.New("panelUrl and token are required")
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	return fsutil.WriteJSONAtomic(AgentConfigPath(dataDir), cfg)
}
