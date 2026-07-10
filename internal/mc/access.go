package mc

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

var (
	playerNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{2,16}$`)

	ErrBadPlayerName = errors.New("invalid player name")
	ErrUnknownPlayer = errors.New("unknown player name")
)

type WhitelistEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type OpEntry struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Level  int    `json:"level"`
	Bypass bool   `json:"bypassesPlayerLimit"`
}

type AccessInfo struct {
	Bedrock     bool             `json:"bedrock"`
	OnlineMode  bool             `json:"onlineMode"`
	WhitelistOn bool             `json:"whitelistOn"`
	Whitelist   []WhitelistEntry `json:"whitelist"`
	Ops         []OpEntry        `json:"ops"`
}

// bedrockAllowEntry is one entry in Bedrock's allowlist.json. The server fills
// in the xuid itself when the player first joins.
type bedrockAllowEntry struct {
	IgnoresPlayerLimit bool   `json:"ignoresPlayerLimit"`
	Name               string `json:"name"`
	XUID               string `json:"xuid,omitempty"`
}

// Xbox gamertags: 3-16 characters, letters, digits and single spaces.
var gamertagRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{1,18}[A-Za-z0-9]$`)

/* ---------- Mojang name resolution ---------- */

type mojangHit struct {
	uuid  string
	name  string
	found bool
	at    time.Time
}

var (
	mojangMu    sync.Mutex
	mojangCache = map[string]mojangHit{}
)

// resolveMojang looks a player name up at Mojang and returns the dashed UUID
// plus the canonical capitalization. Hits are cached, misses briefly too.
func resolveMojang(ctx context.Context, name string) (uuid, canonical string, found bool, err error) {
	key := strings.ToLower(name)
	mojangMu.Lock()
	if hit, ok := mojangCache[key]; ok {
		ttl := time.Hour
		if !hit.found {
			ttl = 10 * time.Minute
		}
		if time.Since(hit.at) < ttl {
			mojangMu.Unlock()
			return hit.uuid, hit.name, hit.found, nil
		}
	}
	mojangMu.Unlock()

	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	err = getJSON(ctx, "https://api.mojang.com/users/profiles/minecraft/"+name, &resp)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "204") {
			mojangMu.Lock()
			mojangCache[key] = mojangHit{found: false, at: time.Now()}
			mojangMu.Unlock()
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("mojang profile lookup: %w", err)
	}
	if len(resp.ID) != 32 {
		return "", "", false, fmt.Errorf("mojang returned an unexpected id %q", resp.ID)
	}
	uuid = dashUUID(resp.ID)
	mojangMu.Lock()
	mojangCache[key] = mojangHit{uuid: uuid, name: resp.Name, found: true, at: time.Now()}
	mojangMu.Unlock()
	return uuid, resp.Name, true, nil
}

func dashUUID(id string) string {
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32]
}

// offlineUUID mirrors Java's UUID.nameUUIDFromBytes("OfflinePlayer:"+name),
// which is what offline-mode servers use.
func offlineUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = (sum[6] & 0x0f) | 0x30 // version 3
	sum[8] = (sum[8] & 0x3f) | 0x80 // IETF variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

/* ---------- manager operations ---------- */

func (m *Manager) propValue(srv *Server, key, def string) string {
	data, err := os.ReadFile(filepath.Join(srv.DataDir(), "server.properties"))
	if err != nil {
		return def
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if k, v, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return def
}

func (m *Manager) AccessInfo(id string) (AccessInfo, error) {
	srv, err := m.get(id)
	if err != nil {
		return AccessInfo{}, err
	}
	info := AccessInfo{
		Whitelist: []WhitelistEntry{},
		Ops:       []OpEntry{},
	}
	if srv.meta.Type == TypeBedrock {
		info.Bedrock = true
		info.WhitelistOn = m.propValue(srv, "allow-list", "false") == "true"
		var entries []bedrockAllowEntry
		fsutil.ReadJSON(filepath.Join(srv.DataDir(), "allowlist.json"), &entries)
		for _, e := range entries {
			info.Whitelist = append(info.Whitelist, WhitelistEntry{Name: e.Name, UUID: e.XUID})
		}
		return info, nil
	}
	info.OnlineMode = m.propValue(srv, "online-mode", "true") != "false"
	info.WhitelistOn = m.propValue(srv, "white-list", "false") == "true"
	fsutil.ReadJSON(filepath.Join(srv.DataDir(), "whitelist.json"), &info.Whitelist)
	fsutil.ReadJSON(filepath.Join(srv.DataDir(), "ops.json"), &info.Ops)
	if info.Whitelist == nil {
		info.Whitelist = []WhitelistEntry{}
	}
	if info.Ops == nil {
		info.Ops = []OpEntry{}
	}
	return info, nil
}

// resolvePlayer validates the name and produces the UUID the server will use,
// respecting online-mode.
func (m *Manager) resolvePlayer(ctx context.Context, srv *Server, name string) (uuid, canonical string, err error) {
	name = strings.TrimSpace(name)
	if !playerNameRe.MatchString(name) {
		return "", "", ErrBadPlayerName
	}
	if m.propValue(srv, "online-mode", "true") != "false" {
		uuid, canonical, found, err := resolveMojang(ctx, name)
		if err != nil {
			return "", "", err
		}
		if !found {
			return "", "", ErrUnknownPlayer
		}
		return uuid, canonical, nil
	}
	return offlineUUID(name), name, nil
}

// AccessAdd puts a player on the whitelist or op list. On a running server the
// panel goes through console commands so Minecraft reloads its own state; on a
// stopped server it edits the JSON files directly.
func (m *Manager) AccessAdd(ctx context.Context, id, list, name string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	if srv.meta.Type == TypeBedrock {
		return m.bedrockAccessAdd(srv, list, name)
	}
	uuid, canonical, err := m.resolvePlayer(ctx, srv, name)
	if err != nil {
		return err
	}

	running := srv.proc.State() == StateRunning
	if running {
		var cmd string
		switch list {
		case "whitelist":
			cmd = "whitelist add " + canonical
		case "ops":
			cmd = "op " + canonical
		default:
			return errors.New("unknown list")
		}
		if err := srv.proc.SendCommand(cmd); err != nil {
			return err
		}
		time.Sleep(700 * time.Millisecond) // give the server time to write its files
		return nil
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	switch list {
	case "whitelist":
		path := filepath.Join(srv.DataDir(), "whitelist.json")
		var entries []WhitelistEntry
		fsutil.ReadJSON(path, &entries)
		for _, e := range entries {
			if strings.EqualFold(e.Name, canonical) {
				return nil
			}
		}
		entries = append(entries, WhitelistEntry{UUID: uuid, Name: canonical})
		return fsutil.WriteJSONAtomic(path, entries)
	case "ops":
		path := filepath.Join(srv.DataDir(), "ops.json")
		var entries []OpEntry
		fsutil.ReadJSON(path, &entries)
		for _, e := range entries {
			if strings.EqualFold(e.Name, canonical) {
				return nil
			}
		}
		entries = append(entries, OpEntry{UUID: uuid, Name: canonical, Level: 4})
		return fsutil.WriteJSONAtomic(path, entries)
	}
	return errors.New("unknown list")
}

// bedrockAccessAdd handles the allowlist of a Bedrock server. Gamertags are
// not validated against Xbox (that would need authentication); the server
// resolves the XUID itself when the player first joins.
func (m *Manager) bedrockAccessAdd(srv *Server, list, name string) error {
	if list != "whitelist" {
		return errors.New("bedrock servers manage operators in game via /op")
	}
	name = strings.TrimSpace(name)
	if !gamertagRe.MatchString(name) {
		return ErrBadPlayerName
	}
	if srv.proc.State() == StateRunning {
		if err := srv.proc.SendCommand(fmt.Sprintf("allowlist add %q", name)); err != nil {
			return err
		}
		time.Sleep(700 * time.Millisecond)
		return nil
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	path := filepath.Join(srv.DataDir(), "allowlist.json")
	var entries []bedrockAllowEntry
	fsutil.ReadJSON(path, &entries)
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return nil
		}
	}
	entries = append(entries, bedrockAllowEntry{Name: name})
	return fsutil.WriteJSONAtomic(path, entries)
}

func (m *Manager) bedrockAccessRemove(srv *Server, list, name string) error {
	if list != "whitelist" {
		return errors.New("bedrock servers manage operators in game via /deop")
	}
	name = strings.TrimSpace(name)
	if !gamertagRe.MatchString(name) {
		return ErrBadPlayerName
	}
	if srv.proc.State() == StateRunning {
		if err := srv.proc.SendCommand(fmt.Sprintf("allowlist remove %q", name)); err != nil {
			return err
		}
		time.Sleep(700 * time.Millisecond)
		return nil
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	path := filepath.Join(srv.DataDir(), "allowlist.json")
	var entries []bedrockAllowEntry
	fsutil.ReadJSON(path, &entries)
	kept := entries[:0]
	for _, e := range entries {
		if !strings.EqualFold(e.Name, name) {
			kept = append(kept, e)
		}
	}
	return fsutil.WriteJSONAtomic(path, kept)
}

func (m *Manager) AccessRemove(id, list, name string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	if srv.meta.Type == TypeBedrock {
		return m.bedrockAccessRemove(srv, list, name)
	}
	name = strings.TrimSpace(name)
	if !playerNameRe.MatchString(name) {
		return ErrBadPlayerName
	}

	if srv.proc.State() == StateRunning {
		var cmd string
		switch list {
		case "whitelist":
			cmd = "whitelist remove " + name
		case "ops":
			cmd = "deop " + name
		default:
			return errors.New("unknown list")
		}
		if err := srv.proc.SendCommand(cmd); err != nil {
			return err
		}
		time.Sleep(700 * time.Millisecond)
		return nil
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	switch list {
	case "whitelist":
		path := filepath.Join(srv.DataDir(), "whitelist.json")
		var entries []WhitelistEntry
		fsutil.ReadJSON(path, &entries)
		kept := entries[:0]
		for _, e := range entries {
			if !strings.EqualFold(e.Name, name) {
				kept = append(kept, e)
			}
		}
		return fsutil.WriteJSONAtomic(path, kept)
	case "ops":
		path := filepath.Join(srv.DataDir(), "ops.json")
		var entries []OpEntry
		fsutil.ReadJSON(path, &entries)
		kept := entries[:0]
		for _, e := range entries {
			if !strings.EqualFold(e.Name, name) {
				kept = append(kept, e)
			}
		}
		return fsutil.WriteJSONAtomic(path, kept)
	}
	return errors.New("unknown list")
}

// SetWhitelistEnforced flips the white-list property and, on a running server,
// applies it immediately via console command.
func (m *Manager) SetWhitelistEnforced(id string, on bool) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	val := "false"
	cmd := "whitelist off"
	prop := "white-list"
	if srv.meta.Type == TypeBedrock {
		prop = "allow-list"
		cmd = "allowlist off"
	}
	if on {
		val = "true"
		if srv.meta.Type == TypeBedrock {
			cmd = "allowlist on"
		} else {
			cmd = "whitelist on"
		}
	}
	if err := m.SetProperties(id, map[string]string{prop: val}); err != nil {
		return err
	}
	if srv.proc.State() == StateRunning {
		return srv.proc.SendCommand(cmd)
	}
	return nil
}
