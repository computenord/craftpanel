package mc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	geyserModrinth    = "geyser"
	floodgateModrinth = "floodgate"
)

// GeyserStatus reports whether Geyser/Floodgate jars are present.
type GeyserStatus struct {
	Supported bool   `json:"supported"`
	Geyser    bool   `json:"geyser"`
	Floodgate bool   `json:"floodgate"`
	Hint      string `json:"hint,omitempty"`
	UDPPort   int    `json:"udpPort,omitempty"`
}

func (m *Manager) GeyserStatus(id string) (GeyserStatus, error) {
	srv, err := m.get(id)
	if err != nil {
		return GeyserStatus{}, err
	}
	typ := srv.meta.Type
	st := GeyserStatus{}
	switch typ {
	case TypePaper, TypePurpur, TypeFolia, TypeFabric, TypeQuilt, TypeVelocity:
		st.Supported = true
	default:
		st.Hint = "Geyser is available on Paper, Purpur, Folia, Fabric, Quilt and Velocity"
		return st, nil
	}
	dir := filepath.Join(srv.DataDir(), "plugins")
	if IsModded(typ) {
		dir = filepath.Join(srv.DataDir(), "mods")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if strings.Contains(n, "geyser") && strings.HasSuffix(n, ".jar") {
			st.Geyser = true
		}
		if strings.Contains(n, "floodgate") && strings.HasSuffix(n, ".jar") {
			st.Floodgate = true
		}
	}
	st.UDPPort = 19132
	if !st.Geyser {
		st.Hint = "Install Geyser to let Bedrock clients join this Java server"
	}
	return st, nil
}

// InstallGeyser installs Geyser (and optionally Floodgate) from Modrinth.
func (m *Manager) InstallGeyser(ctx context.Context, id string, withFloodgate bool) error {
	st, err := m.GeyserStatus(id)
	if err != nil {
		return err
	}
	if !st.Supported {
		return errors.New(st.Hint)
	}
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	typ := srv.meta.Type
	if IsModded(typ) {
		if _, err := m.InstallMod(ctx, id, geyserModrinth); err != nil {
			return fmt.Errorf("geyser: %w", err)
		}
		if withFloodgate {
			if _, err := m.InstallMod(ctx, id, floodgateModrinth); err != nil {
				return fmt.Errorf("floodgate: %w", err)
			}
		}
		return nil
	}
	if _, err := m.InstallPlugin(ctx, id, geyserModrinth); err != nil {
		return fmt.Errorf("geyser: %w", err)
	}
	if withFloodgate {
		if _, err := m.InstallPlugin(ctx, id, floodgateModrinth); err != nil {
			return fmt.Errorf("floodgate: %w", err)
		}
	}
	return nil
}
