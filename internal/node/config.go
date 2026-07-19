package node

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// AgentConfig is persisted on the node host as node-agent.json.
type AgentConfig struct {
	PanelURL string `json:"panelUrl"`
	Token    string `json:"token"`
	// Listen is the local control API address (default :8421).
	Listen string `json:"listen,omitempty"`
	// ApiURL is the URL the panel should use to reach this node.
	// Empty → derived from CRAFTPANEL_NODE_URL or the first non-loopback IP.
	ApiURL string `json:"apiUrl,omitempty"`
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
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.ApiURL = strings.TrimRight(strings.TrimSpace(cfg.ApiURL), "/")
	if cfg.PanelURL == "" || cfg.Token == "" {
		return AgentConfig{}, errors.New("node-agent.json incomplete")
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8421"
	}
	return cfg, nil
}

func WriteAgentConfig(dataDir string, cfg AgentConfig) error {
	cfg.PanelURL = strings.TrimRight(strings.TrimSpace(cfg.PanelURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.ApiURL = strings.TrimRight(strings.TrimSpace(cfg.ApiURL), "/")
	if cfg.PanelURL == "" || cfg.Token == "" {
		return errors.New("panelUrl and token are required")
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8421"
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	return fsutil.WriteJSONAtomic(AgentConfigPath(dataDir), cfg)
}

// ResolveApiURL picks the advertised control URL for sync.
func ResolveApiURL(cfg AgentConfig) string {
	if cfg.ApiURL != "" {
		return cfg.ApiURL
	}
	if u := strings.TrimRight(strings.TrimSpace(os.Getenv("CRAFTPANEL_NODE_URL")), "/"); u != "" {
		return u
	}
	host := firstNonLoopbackIP()
	if host == "" {
		host = "127.0.0.1"
	}
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil || port == "" {
		if strings.HasPrefix(cfg.Listen, ":") {
			port = strings.TrimPrefix(cfg.Listen, ":")
		} else {
			port = "8421"
		}
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

func firstNonLoopbackIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}
