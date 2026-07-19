package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/computenord/craftpanel/internal/mc"
)

// Agent pushes local server state to a panel and executes queued commands.
type Agent struct {
	PanelURL string
	Token    string
	Version  string
	Manager  *mc.Manager
	Client   *http.Client
}

func (a *Agent) Run(ctx context.Context) {
	if a.Client == nil {
		a.Client = &http.Client{Timeout: 30 * time.Second}
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	a.syncOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.syncOnce(ctx)
		}
	}
}

func (a *Agent) syncOnce(ctx context.Context) {
	servers := []RemoteServer{}
	for _, v := range a.Manager.List() {
		servers = append(servers, RemoteServer{
			ID: v.ID, Name: v.Name, Type: v.Type, Version: v.Version,
			Port: v.Port, Status: v.Status, MemoryMB: v.MemoryMB,
		})
	}
	body, _ := json.Marshal(SyncRequest{Version: a.Version, Servers: servers})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stringsTrimRight(a.PanelURL)+"/api/nodes/sync", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.Token)
	resp, err := a.Client.Do(req)
	if err != nil {
		log.Printf("node sync: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("node sync: %s", resp.Status)
		return
	}
	var out SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return
	}
	for _, cmd := range out.Commands {
		msg := ""
		ok := true
		var err error
		switch cmd.Op {
		case "start":
			err = a.Manager.Start(cmd.ServerID)
		case "stop":
			err = a.Manager.Stop(cmd.ServerID)
		case "restart":
			err = a.Manager.Restart(cmd.ServerID)
		case "kill":
			err = a.Manager.Kill(cmd.ServerID)
		default:
			err = fmt.Errorf("unknown op %q", cmd.Op)
		}
		if err != nil {
			ok = false
			msg = err.Error()
			log.Printf("node command %s %s: %v", cmd.Op, cmd.ServerID, err)
		} else {
			log.Printf("node command %s %s: ok", cmd.Op, cmd.ServerID)
		}
		_ = ok
		_ = msg
	}
}

func stringsTrimRight(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
