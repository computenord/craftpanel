package mc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DiscordConfig is the per-server notification setup.
type DiscordConfig struct {
	Webhook string `json:"webhook,omitempty"`
	Lang    string `json:"lang,omitempty"` // "de" or "en"
	Status  bool   `json:"status"`
	Backups bool   `json:"backups"`
	Players bool   `json:"players"`
	Chat    bool   `json:"chat"`
}

const (
	notifyStatus = "status"
	notifyBackup = "backup"
	notifyPlayer = "player"
	notifyChat   = "chat"
)

// Only real Discord webhook endpoints are accepted, the panel must not be
// usable as a generic request proxy.
func validDiscordWebhook(url string) bool {
	return strings.HasPrefix(url, "https://discord.com/api/webhooks/") ||
		strings.HasPrefix(url, "https://discordapp.com/api/webhooks/") ||
		strings.HasPrefix(url, "https://ptb.discord.com/api/webhooks/") ||
		strings.HasPrefix(url, "https://canary.discord.com/api/webhooks/")
}

var discordClient = &http.Client{Timeout: 10 * time.Second}

// postDiscord delivers one message. Mentions are disabled so nothing coming
// out of a game chat can ping people.
func postDiscord(webhook, content string) error {
	if !validDiscordWebhook(webhook) {
		return errors.New("not a discord webhook URL")
	}
	if len(content) > 1900 {
		content = content[:1900] + "…"
	}
	payload, err := json.Marshal(map[string]any{
		"content":          content,
		"username":         "ComputeBox Craftpanel",
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		return err
	}
	resp, err := discordClient.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord answered %s", resp.Status)
	}
	return nil
}

/* ---------- message catalog (customer facing, no emojis) ---------- */

var discordTexts = map[string]map[string]string{
	"en": {
		"online":       "**%s** is online",
		"stopped":      "**%s** was stopped",
		"crashed":      "**%s** crashed",
		"crashRestart": "**%s** crashed, automatic restart in %s (attempt %d)",
		"backupOk":     "**%s**: backup created (%s)",
		"backupFail":   "**%s**: backup failed: %s",
		"join":         "**%s**: %s joined the server",
		"leave":        "**%s**: %s left the server",
		"test":         "Test message from ComputeBox Craftpanel for **%s**",
	},
	"de": {
		"online":       "**%s** ist jetzt online",
		"stopped":      "**%s** wurde gestoppt",
		"crashed":      "**%s** ist abgestürzt",
		"crashRestart": "**%s** ist abgestürzt, automatischer Neustart in %s (Versuch %d)",
		"backupOk":     "**%s**: Backup erstellt (%s)",
		"backupFail":   "**%s**: Backup fehlgeschlagen: %s",
		"join":         "**%s**: %s ist dem Server beigetreten",
		"leave":        "**%s**: %s hat den Server verlassen",
		"test":         "Testnachricht von ComputeBox Craftpanel für **%s**",
	},
}

func discordText(lang, key string, args ...any) string {
	table, ok := discordTexts[lang]
	if !ok {
		table = discordTexts["en"]
	}
	return fmt.Sprintf(table[key], args...)
}

/* ---------- event wiring ---------- */

// notify formats and enqueues one notification if the server has a webhook
// and the event kind enabled. Never blocks.
func (m *Manager) notify(srv *Server, kind, key string, extra ...any) {
	srv.mu.Lock()
	cfg := srv.meta.Discord
	name := srv.meta.Name
	ch := srv.notifyCh
	srv.mu.Unlock()
	if ch == nil || cfg.Webhook == "" {
		return
	}
	enabled := false
	switch kind {
	case notifyStatus:
		enabled = cfg.Status
	case notifyBackup:
		enabled = cfg.Backups
	case notifyPlayer:
		enabled = cfg.Players
	case notifyChat:
		enabled = cfg.Chat
	}
	if !enabled {
		return
	}
	var msg string
	if kind == notifyChat {
		// extra = player, text; language independent
		msg = fmt.Sprintf("[%s] <%s> %s", name, extra[0], extra[1])
	} else {
		msg = discordText(cfg.Lang, key, append([]any{name}, extra...)...)
	}
	select {
	case ch <- msg:
	default:
		// Queue full, drop rather than stall anything.
	}
}

// runNotifier batches queued messages so chat mirroring stays far away from
// Discord's webhook rate limits.
func (m *Manager) runNotifier(srv *Server) {
	var batch []string
	var flushTimer <-chan time.Time
	flush := func() {
		if len(batch) == 0 {
			return
		}
		content := strings.Join(batch, "\n")
		batch = nil
		srv.mu.Lock()
		webhook := srv.meta.Discord.Webhook
		id := srv.meta.ID
		srv.mu.Unlock()
		if webhook == "" {
			return
		}
		go func() {
			if err := postDiscord(webhook, content); err != nil {
				log.Printf("discord %s: %v", id, err)
			}
		}()
	}
	for {
		select {
		case msg := <-srv.notifyCh:
			batch = append(batch, msg)
			if len(batch) == 1 {
				flushTimer = time.After(2 * time.Second)
			}
			if len(batch) >= 20 {
				flush()
				flushTimer = nil
			}
		case <-flushTimer:
			flush()
			flushTimer = nil
		case <-srv.watchQuit:
			flush()
			return
		}
	}
}

var (
	javaChatLineRe  = regexp.MustCompile(`\]: (?:\[Not Secure\] )?<([^>]{1,32})> (.*)$`)
	javaJoinLineRe  = regexp.MustCompile(`\]: ([A-Za-z0-9_]{2,16}) joined the game$`)
	javaLeaveLineRe = regexp.MustCompile(`\]: ([A-Za-z0-9_]{2,16}) left the game$`)
	bedrockJoinRe   = regexp.MustCompile(`Player connected: ([^,]+),`)
	bedrockLeaveRe  = regexp.MustCompile(`Player disconnected: ([^,]+),`)
	velocityConnRe  = regexp.MustCompile(`\[connected player\] (\S+) .*has (connected|disconnected)`)
)

// watchServer follows the console stream of one server for its whole life
// and turns state changes and log lines into notifications.
func (m *Manager) watchServer(srv *Server) {
	_, ch, cancel := srv.proc.Subscribe()
	defer cancel()
	for {
		select {
		case <-srv.watchQuit:
			return
		case ev := <-ch:
			switch ev.Type {
			case "status":
				if ev.Status == StateRunning {
					m.notify(srv, notifyStatus, "online")
				}
			case "line":
				m.scanConsoleLine(srv, ev.Line)
			}
		}
	}
}

func (m *Manager) scanConsoleLine(srv *Server, line string) {
	srv.mu.Lock()
	typ := srv.meta.Type
	bedrock := typ == TypeBedrock
	cfg := srv.meta.Discord
	srv.mu.Unlock()
	if cfg.Webhook == "" || (!cfg.Players && !cfg.Chat) {
		return
	}
	if typ == TypeVelocity {
		if mres := velocityConnRe.FindStringSubmatch(line); mres != nil {
			key := "join"
			if mres[2] == "disconnected" {
				key = "leave"
			}
			m.notify(srv, notifyPlayer, key, mres[1])
		}
		return
	}
	if bedrock {
		if mres := bedrockJoinRe.FindStringSubmatch(line); mres != nil {
			m.notify(srv, notifyPlayer, "join", strings.TrimSpace(mres[1]))
		} else if mres := bedrockLeaveRe.FindStringSubmatch(line); mres != nil {
			m.notify(srv, notifyPlayer, "leave", strings.TrimSpace(mres[1]))
		}
		return
	}
	// Chat first: a chat message could contain "joined the game".
	if mres := javaChatLineRe.FindStringSubmatch(line); mres != nil {
		m.notify(srv, notifyChat, "chat", mres[1], mres[2])
		return
	}
	if mres := javaJoinLineRe.FindStringSubmatch(line); mres != nil {
		m.notify(srv, notifyPlayer, "join", mres[1])
		return
	}
	if mres := javaLeaveLineRe.FindStringSubmatch(line); mres != nil {
		m.notify(srv, notifyPlayer, "leave", mres[1])
	}
}

// DiscordTest sends a test message through the configured webhook.
func (m *Manager) DiscordTest(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	srv.mu.Lock()
	cfg := srv.meta.Discord
	name := srv.meta.Name
	srv.mu.Unlock()
	if cfg.Webhook == "" {
		return errors.New("no webhook configured, save one first")
	}
	return postDiscord(cfg.Webhook, discordText(cfg.Lang, "test", name))
}
