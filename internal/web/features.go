package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
	"github.com/computenord/craftpanel/internal/selfupdate"
)

// username resolves the signed-in user. Handlers behind authGate can rely on
// a valid session cookie being present.
func (h *Handler) username(r *http.Request) string {
	cookie, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return ""
	}
	name, _ := h.Auth.ValidateSession(cookie.Value)
	return name
}

/* ---------- panel settings ---------- */

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Manager.Settings())
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupDir string `json:"backupDir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manager.SetBackupDir(req.BackupDir); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	log.Printf("settings: backup dir set to %q", req.BackupDir)
	writeJSON(w, http.StatusOK, h.Manager.Settings())
}

/* ---------- two-factor auth ---------- */

func (h *Handler) totpInit(w http.ResponseWriter, r *http.Request) {
	user := h.username(r)
	secret, url, err := h.Auth.InitTOTP(user)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "url": url})
}

func (h *Handler) totpEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	user := h.username(r)
	if err := h.Auth.EnableTOTP(user, req.Code); err != nil {
		if errors.Is(err, auth.ErrInvalidTOTP) {
			apiError(w, http.StatusBadRequest, "totp_invalid", "wrong code")
			return
		}
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	log.Printf("totp enabled for %q", user)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) totpDisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	user := h.username(r)
	if err := h.Auth.DisableTOTP(user, req.Code); err != nil {
		if errors.Is(err, auth.ErrInvalidTOTP) {
			apiError(w, http.StatusBadRequest, "totp_invalid", "wrong code")
			return
		}
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	log.Printf("totp disabled for %q", user)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* ---------- backups ---------- */

func (h *Handler) backupList(w http.ResponseWriter, r *http.Request) {
	list, err := h.Manager.ListBackups(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) backupCreate(w http.ResponseWriter, r *http.Request) {
	if err := h.Manager.StartBackup(r.PathValue("id"), "manual"); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) backupRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manager.RestoreBackup(r.PathValue("id"), req.Name); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) backupDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.Manager.DeleteBackup(r.PathValue("id"), r.URL.Query().Get("name")); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) backupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	f, size, err := h.Manager.OpenBackup(r.PathValue("id"), name)
	if err != nil {
		managerError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	io.Copy(w, f)
}

/* ---------- version upgrade ---------- */

func (h *Handler) upgrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if err := h.Manager.Upgrade(id, req.Version); err != nil {
		managerError(w, err)
		return
	}
	log.Printf("server %s: upgrading to %s", id, req.Version)
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

/* ---------- discord ---------- */

func (h *Handler) discordTest(w http.ResponseWriter, r *http.Request) {
	if err := h.Manager.DiscordTest(r.PathValue("id")); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* ---------- velocity network ---------- */

func (h *Handler) networkInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.Manager.Network(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, mc.ErrNotVelocity) {
			apiError(w, http.StatusBadRequest, "not_velocity", "this server is not a velocity proxy")
			return
		}
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) networkSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Servers []string `json:"servers"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	warnings, err := h.Manager.SetNetwork(id, req.Servers)
	if err != nil {
		if errors.Is(err, mc.ErrNotVelocity) {
			apiError(w, http.StatusBadRequest, "not_velocity", "this server is not a velocity proxy")
			return
		}
		managerError(w, err)
		return
	}
	log.Printf("server %s: network set to %v (%d warnings)", id, req.Servers, len(warnings))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "warnings": warnings})
}

/* ---------- live players ---------- */

func (h *Handler) playersList(w http.ResponseWriter, r *http.Request) {
	info, err := h.Manager.OnlinePlayers(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) playerAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	err := h.Manager.PlayerAction(r.PathValue("id"), req.Action, req.Name, req.Reason)
	switch {
	case errors.Is(err, mc.ErrActionUnsupported):
		apiError(w, http.StatusBadRequest, "action_unsupported", "this action is not available for this server type")
	case errors.Is(err, mc.ErrBadPlayerName):
		apiError(w, http.StatusBadRequest, "bad_name", "invalid player name")
	case err != nil:
		managerError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

/* ---------- whitelist and ops ---------- */

func validAccessList(list string) bool {
	return list == "whitelist" || list == "ops"
}

func (h *Handler) accessInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.Manager.AccessInfo(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func accessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mc.ErrBadPlayerName):
		apiError(w, http.StatusBadRequest, "bad_name", "invalid player name")
	case errors.Is(err, mc.ErrUnknownPlayer):
		apiError(w, http.StatusBadRequest, "invalid_player", "this player name does not exist")
	default:
		managerError(w, err)
	}
}

func (h *Handler) accessAdd(w http.ResponseWriter, r *http.Request) {
	list := r.PathValue("list")
	if !validAccessList(list) {
		apiError(w, http.StatusNotFound, "not_found", "unknown list")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.Manager.AccessAdd(ctx, r.PathValue("id"), list, req.Name); err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) accessRemove(w http.ResponseWriter, r *http.Request) {
	list := r.PathValue("list")
	if !validAccessList(list) {
		apiError(w, http.StatusNotFound, "not_found", "unknown list")
		return
	}
	if err := h.Manager.AccessRemove(r.PathValue("id"), list, r.URL.Query().Get("name")); err != nil {
		accessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) whitelistMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manager.SetWhitelistEnforced(r.PathValue("id"), req.Enabled); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* ---------- panel update check ---------- */

// latestTag asks GitHub for the newest release tag, cached for half a day.
// Failures (offline host, private repo) just mean no update hint.
func (h *Handler) latestTag() string {
	h.updMu.Lock()
	defer h.updMu.Unlock()
	if time.Since(h.updAt) < 12*time.Hour {
		return h.updTag
	}
	h.updAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/computenord/craftpanel/releases/latest", nil)
	if err != nil {
		return h.updTag
	}
	req.Header.Set("User-Agent", "ComputeBox-Craftpanel")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return h.updTag
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h.updTag
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err == nil {
		h.updTag = rel.TagName
	}
	return h.updTag
}

func (h *Handler) updateInfo() (latest string, available bool) {
	latest = strings.TrimPrefix(h.latestTag(), "v")
	if latest == "" || h.Version == "dev" {
		return latest, false
	}
	return latest, latest != strings.TrimPrefix(h.Version, "v")
}

// checkUpdate bypasses the 12 hour cache and asks GitHub right now.
func (h *Handler) checkUpdate(w http.ResponseWriter, r *http.Request) {
	h.updMu.Lock()
	h.updAt = time.Time{}
	h.updMu.Unlock()
	latest, available := h.updateInfo()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         h.Version,
		"latest":          latest,
		"updateAvailable": available,
	})
}

/* ---------- self update ---------- */

// systemUpdate downloads the latest release binary, verifies it against the
// release's SHA256SUMS, atomically swaps the running executable and asks the
// main loop for a graceful restart. Requires the install layout where the
// binary is owned and writable by the service user.
func (h *Handler) systemUpdate(w http.ResponseWriter, r *http.Request) {
	latest, available := h.updateInfo()
	if !available {
		apiError(w, http.StatusBadRequest, "no_update", "already up to date")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	switch err := selfupdate.Apply(ctx, ""); {
	case errors.Is(err, selfupdate.ErrUnsupported):
		apiError(w, http.StatusBadRequest, "self_update_unsupported", "self update only works on Linux installs")
		return
	case errors.Is(err, selfupdate.ErrNotWritable):
		apiError(w, http.StatusBadRequest, "self_update_unsupported", err.Error())
		return
	case err != nil:
		apiError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}

	log.Printf("self update: installed %s, restarting", latest)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": latest})
	if h.Restart != nil {
		go func() {
			time.Sleep(500 * time.Millisecond) // let the response reach the client
			h.Restart()
		}()
	}
}
