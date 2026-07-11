package mc

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// runScheduler drives all time based per-server jobs: daily backups and
// scheduled restarts with player warnings.
func (m *Manager) runScheduler() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		hhmm := now.Format("15:04")
		day := now.Format("20060102")

		m.mu.Lock()
		servers := make([]*Server, 0, len(m.items))
		for _, s := range m.items {
			servers = append(servers, s)
		}
		m.mu.Unlock()

		for _, srv := range servers {
			srv.mu.Lock()
			backupDue := srv.meta.BackupAuto && srv.meta.BackupTime == hhmm &&
				srv.lastAutoBackup != day && !srv.backupBusy && !srv.installing && !srv.deleting
			if backupDue {
				srv.lastAutoBackup = day
			}
			restartDue := srv.meta.RestartAuto && srv.meta.RestartTime != "" &&
				hhmm == warnStartHHMM(srv.meta.RestartTime, srv.meta.RestartWarn) &&
				srv.lastAutoRestart != day && !srv.deleting &&
				srv.proc.State() == StateRunning
			if restartDue {
				srv.lastAutoRestart = day
			}
			id := srv.meta.ID
			srv.mu.Unlock()

			if backupDue {
				if err := m.StartBackup(id, "auto"); err != nil {
					log.Printf("auto backup %s: %v", id, err)
				}
			}
			if restartDue {
				go m.warnedRestart(srv)
			}
		}
	}
}

// warnStartHHMM shifts the configured restart time back by the warning lead,
// because that is the moment the scheduler has to act.
func warnStartHHMM(restartTime string, warnMinutes int) string {
	parts := strings.SplitN(restartTime, ":", 2)
	if len(parts) != 2 {
		return restartTime
	}
	h, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return restartTime
	}
	total := (h*60 + min - warnMinutes) % 1440
	if total < 0 {
		total += 1440
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

var restartSayTexts = map[string]map[string]string{
	"en": {
		"minutes": "Server restarts in %d minutes",
		"minute":  "Server restarts in 1 minute",
		"seconds": "Server restarts in 10 seconds",
	},
	"de": {
		"minutes": "Der Server startet in %d Minuten neu",
		"minute":  "Der Server startet in 1 Minute neu",
		"seconds": "Der Server startet in 10 Sekunden neu",
	},
}

func restartSay(lang, key string, args ...any) string {
	table, ok := restartSayTexts[lang]
	if !ok {
		table = restartSayTexts["en"]
	}
	return fmt.Sprintf(table[key], args...)
}

// warnedRestart announces the upcoming restart to the players, waits out the
// lead time and then restarts the server. It aborts silently when the server
// stops for any other reason in the meantime.
func (m *Manager) warnedRestart(srv *Server) {
	srv.mu.Lock()
	id := srv.meta.ID
	warn := srv.meta.RestartWarn
	lang := srv.meta.Discord.Lang
	// Velocity has no say command, restart without in-game warnings.
	if srv.meta.Type == TypeVelocity {
		warn = 0
	}
	srv.mu.Unlock()

	stillRunning := func() bool { return srv.proc.State() == StateRunning }
	announce := func(msg string) {
		if err := srv.proc.SendCommand("say " + msg); err != nil {
			log.Printf("scheduled restart %s: announce: %v", id, err)
		}
	}

	srv.proc.Note("Scheduled restart")
	if warn > 0 {
		if warn == 1 {
			announce(restartSay(lang, "minute"))
		} else {
			announce(restartSay(lang, "minutes", warn))
		}
		// Sleep in one minute steps so a manually stopped server aborts the
		// countdown instead of being restarted unexpectedly.
		for left := warn; left > 1; left-- {
			time.Sleep(time.Minute)
			if !stillRunning() {
				return
			}
			if left-1 == 1 {
				announce(restartSay(lang, "minute"))
			}
		}
		time.Sleep(50 * time.Second)
		if !stillRunning() {
			return
		}
		announce(restartSay(lang, "seconds"))
		time.Sleep(10 * time.Second)
	}
	if !stillRunning() {
		return
	}
	log.Printf("scheduled restart %s", id)
	if err := m.Restart(id); err != nil {
		log.Printf("scheduled restart %s: %v", id, err)
		srv.proc.Note("Scheduled restart failed: " + err.Error())
	}
}
