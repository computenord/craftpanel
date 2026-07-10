package mc

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// OnlinePlayer is one entry of the live player list.
type OnlinePlayer struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
	Op   bool   `json:"op"`
}

type BanEntry struct {
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
}

// PlayersInfo is the live view of who is on a server right now.
type PlayersInfo struct {
	Running bool           `json:"running"`
	Bedrock bool           `json:"bedrock"`
	Max     int            `json:"max"`
	Online  []OnlinePlayer `json:"online"`
	Banned  []BanEntry     `json:"banned"`
}

var (
	ErrActionUnsupported = errors.New("action not supported for this server type")

	javaListRe    = regexp.MustCompile(`There are (\d+) of a max of (\d+) players online:\s*(.*)$`)
	bedrockListRe = regexp.MustCompile(`There are (\d+)/(\d+) players online:`)
	javaNameUUID  = regexp.MustCompile(`^(.*?)\s*\(([0-9a-fA-F-]{32,36})\)$`)
)

const playersCacheTTL = 5 * time.Second

// OnlinePlayers reports who is online by asking the server console for its
// player list. Results are cached briefly, the UI polls this endpoint.
func (m *Manager) OnlinePlayers(id string) (PlayersInfo, error) {
	srv, err := m.get(id)
	if err != nil {
		return PlayersInfo{}, err
	}
	bedrock := srv.meta.Type == TypeBedrock
	info := PlayersInfo{Bedrock: bedrock, Online: []OnlinePlayer{}, Banned: []BanEntry{}}

	if !bedrock {
		var bans []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		}
		fsutil.ReadJSON(filepath.Join(srv.DataDir(), "banned-players.json"), &bans)
		for _, b := range bans {
			info.Banned = append(info.Banned, BanEntry{Name: b.Name, Reason: b.Reason})
		}
	}

	if srv.proc.State() != StateRunning {
		return info, nil
	}
	info.Running = true

	srv.mu.Lock()
	if srv.players != nil && time.Since(srv.playersAt) < playersCacheTTL {
		info.Online = srv.players.Online
		info.Max = srv.players.Max
		srv.mu.Unlock()
		return info, nil
	}
	srv.mu.Unlock()

	online, maxPlayers, qerr := queryList(srv.proc, bedrock)
	if qerr != nil {
		// The server did not answer (booting, overloaded, ancient version).
		// Report what we know instead of failing the endpoint.
		return info, nil
	}
	if !bedrock {
		var ops []OpEntry
		fsutil.ReadJSON(filepath.Join(srv.DataDir(), "ops.json"), &ops)
		opSet := map[string]bool{}
		for _, o := range ops {
			opSet[strings.ToLower(o.Name)] = true
		}
		for i := range online {
			online[i].Op = opSet[strings.ToLower(online[i].Name)]
		}
	}
	info.Online = online
	info.Max = maxPlayers

	srv.mu.Lock()
	cached := PlayersInfo{Online: online, Max: maxPlayers}
	srv.players = &cached
	srv.playersAt = time.Now()
	srv.mu.Unlock()
	return info, nil
}

// queryList sends `list` to the console and parses the reply from the
// console stream.
func queryList(p *Proc, bedrock bool) ([]OnlinePlayer, int, error) {
	_, ch, cancel := p.Subscribe()
	defer cancel()
	cmd := "list uuids"
	if bedrock {
		cmd = "list"
	}
	if err := p.SendCommandQuiet(cmd); err != nil {
		return nil, 0, err
	}

	deadline := time.After(3 * time.Second)
	expectNames := false
	maxPlayers := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type != "line" {
				continue
			}
			line := ev.Line
			if expectNames {
				// Bedrock prints the names on the line after the header.
				return parseBedrockNames(stripLogPrefix(line)), maxPlayers, nil
			}
			if bedrock {
				if m := bedrockListRe.FindStringSubmatch(line); m != nil {
					count, _ := strconv.Atoi(m[1])
					maxPlayers, _ = strconv.Atoi(m[2])
					if count == 0 {
						return []OnlinePlayer{}, maxPlayers, nil
					}
					expectNames = true
				}
				continue
			}
			if m := javaListRe.FindStringSubmatch(line); m != nil {
				maxPlayers, _ = strconv.Atoi(m[2])
				return parseJavaNames(m[3]), maxPlayers, nil
			}
		case <-deadline:
			return nil, 0, errors.New("no player list response")
		}
	}
}

func parseJavaNames(s string) []OnlinePlayer {
	out := []OnlinePlayer{}
	for _, part := range strings.Split(s, ", ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if m := javaNameUUID.FindStringSubmatch(part); m != nil {
			out = append(out, OnlinePlayer{Name: m[1], UUID: m[2]})
		} else {
			out = append(out, OnlinePlayer{Name: part})
		}
	}
	return out
}

func parseBedrockNames(s string) []OnlinePlayer {
	out := []OnlinePlayer{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, OnlinePlayer{Name: part})
		}
	}
	return out
}

func stripLogPrefix(line string) string {
	if i := strings.Index(line, "] "); i >= 0 && i < 48 {
		return line[i+2:]
	}
	return line
}

// PlayerAction runs a moderation command against a running server. The web
// UI asks the user for confirmation before calling this.
func (m *Manager) PlayerAction(id, action, name, reason string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	if srv.proc.State() != StateRunning {
		return errors.New("server is not running")
	}
	bedrock := srv.meta.Type == TypeBedrock
	name = strings.TrimSpace(name)
	if bedrock {
		if !gamertagRe.MatchString(name) {
			return ErrBadPlayerName
		}
	} else if !playerNameRe.MatchString(name) {
		return ErrBadPlayerName
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 100 {
		reason = reason[:100]
	}

	quoted := name
	if bedrock && strings.Contains(name, " ") {
		quoted = fmt.Sprintf("%q", name)
	}
	var cmd string
	switch action {
	case "op":
		cmd = "op " + quoted
	case "deop":
		cmd = "deop " + quoted
	case "kick":
		cmd = "kick " + quoted
		if reason != "" {
			cmd += " " + reason
		}
	case "ban":
		if bedrock {
			return ErrActionUnsupported
		}
		cmd = "ban " + name
		if reason != "" {
			cmd += " " + reason
		}
	case "pardon":
		if bedrock {
			return ErrActionUnsupported
		}
		cmd = "pardon " + name
	default:
		return errors.New("unknown action")
	}
	if err := srv.proc.SendCommand(cmd); err != nil {
		return err
	}
	srv.mu.Lock()
	srv.players = nil // force a fresh list on the next poll
	srv.mu.Unlock()
	log.Printf("server %s: player action %q on %q", id, action, name)
	return nil
}
