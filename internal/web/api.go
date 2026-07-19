package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
)

func (h *Handler) setupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"needsSetup": h.Auth.NeedsSetup(),
		"version":    h.Version,
	})
}

func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	if !h.Auth.NeedsSetup() {
		apiError(w, http.StatusForbidden, "already_setup", "an admin account already exists")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Auth.CreateFirstUser(req.Username, req.Password); err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			apiError(w, http.StatusForbidden, "already_setup", "an admin account already exists")
			return
		}
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	token, err := h.Auth.CreateSession(req.Username)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	h.setSessionCookie(w, r, token, 7*24*3600)
	log.Printf("setup: created admin account %q", req.Username)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	err := h.Auth.Authenticate(h.clientIP(r), req.Username, req.Password, req.Code)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many failed attempts, try again later")
		return
	case errors.Is(err, auth.ErrTOTPRequired):
		apiError(w, http.StatusUnauthorized, "totp_required", "two-factor code required")
		return
	case errors.Is(err, auth.ErrInvalidTOTP):
		apiError(w, http.StatusUnauthorized, "totp_invalid", "wrong two-factor code")
		return
	case err != nil:
		apiError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
		return
	}
	token, err := h.Auth.CreateSession(req.Username)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	h.setSessionCookie(w, r, token, 7*24*3600)
	h.Manager.Audit(req.Username, "login", "", "", h.clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookie); err == nil {
		h.Auth.DestroySession(cookie.Value)
	}
	h.setSessionCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, ok := h.currentUser(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":          u.Username,
		"role":              u.Role,
		"access":            u.Access,
		"admin":             u.IsAdmin(),
		"totp":              h.Auth.TOTPEnabled(u.Username),
		"recoveryRemaining": h.Auth.RecoveryCodesRemaining(u.Username),
	})
}

func (h *Handler) system(w http.ResponseWriter, r *http.Request) {
	latest, available := h.updateInfo()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         h.Version,
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
		"java":            h.javaVersion(),
		"latest":          latest,
		"updateAvailable": available,
		"managed":         h.LockState != nil,
		"lock":            h.lock(),
	})
}

func (h *Handler) javaVersion() javaInfo {
	h.javaMu.Lock()
	defer h.javaMu.Unlock()
	if time.Since(h.javaFetched) < 5*time.Minute {
		return h.javaInfo
	}
	info := javaInfo{}
	if major, version := mc.DetectJava(""); version != "" {
		info = javaInfo{Found: true, Version: strings.TrimSpace(version), Major: major}
	}
	h.javaInfo = info
	h.javaFetched = time.Now()
	return info
}

func (h *Handler) versionList(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	list, err := h.Versions.List(ctx, typ)
	if err != nil {
		apiError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) listServers(w http.ResponseWriter, r *http.Request) {
	all := h.Manager.List()
	u, ok := h.currentUser(r)
	var local []mc.ServerView
	if !ok || u.IsAdmin() {
		local = all
	} else {
		local = make([]mc.ServerView, 0, len(all))
		for _, s := range all {
			if _, allowed := u.AccessFor(s.ID); allowed {
				local = append(local, s)
			}
		}
	}
	// Admins also see remote node servers (composite ids).
	if ok && u.IsAdmin() && h.Nodes != nil {
		out := make([]any, 0, len(local)+8)
		for _, s := range local {
			out = append(out, s)
		}
		for _, rem := range h.Nodes.AllServers() {
			out = append(out, rem)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, local)
}

func (h *Handler) createServer(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	var meta struct {
		NodeID string `json:"nodeId"`
	}
	_ = json.Unmarshal(body, &meta)
	if meta.NodeID != "" {
		if h.Nodes == nil {
			apiError(w, http.StatusBadRequest, "bad_request", "nodes not available")
			return
		}
		h.proxyCreateToNode(w, r, meta.NodeID, body)
		return
	}
	var req mc.CreateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	view, err := h.Manager.Create(req)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	log.Printf("created server %s (%s %s)", view.ID, view.Type, view.Version)
	h.audit(r, "server.create", view.ID, view.Type+" "+view.Version)
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handler) getServer(w http.ResponseWriter, r *http.Request) {
	view, err := h.Manager.View(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) updateServer(w http.ResponseWriter, r *http.Request) {
	var req mc.UpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.Manager.Update(r.PathValue("id"), req)
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Manager.Delete(id); err != nil {
		managerError(w, err)
		return
	}
	log.Printf("deleted server %s", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) startServer(w http.ResponseWriter, r *http.Request) {
	err := h.Manager.Start(r.PathValue("id"))
	if err != nil {
		var tooOld *mc.JavaTooOldError
		switch {
		case errors.As(err, &tooOld):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "java_too_old",
				"message": err.Error(),
				"need":    tooOld.Need,
				"have":    tooOld.Have,
			})
		case strings.Contains(err.Error(), "eula"):
			apiError(w, http.StatusConflict, "eula_required", err.Error())
		default:
			managerError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) stopServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.Manager.View(id); err != nil {
		managerError(w, err)
		return
	}
	go func() {
		if err := h.Manager.Stop(id); err != nil {
			log.Printf("stop %s: %v", id, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) restartServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.Manager.View(id); err != nil {
		managerError(w, err)
		return
	}
	go func() {
		if err := h.Manager.Restart(id); err != nil {
			log.Printf("restart %s: %v", id, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) killServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.Manager.View(id); err != nil {
		managerError(w, err)
		return
	}
	go func() {
		if err := h.Manager.Kill(id); err != nil {
			log.Printf("kill %s: %v", id, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) retryInstall(w http.ResponseWriter, r *http.Request) {
	if err := h.Manager.RetryInstall(r.PathValue("id")); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) command(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manager.SendCommand(r.PathValue("id"), req.Command); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) setEULA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accept bool `json:"accept"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if err := h.Manager.SetEULA(id, req.Accept); err != nil {
		managerError(w, err)
		return
	}
	log.Printf("server %s: eula set to %v", id, req.Accept)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) getProperties(w http.ResponseWriter, r *http.Request) {
	props, err := h.Manager.Properties(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	out := make([]map[string]string, 0, len(props))
	for _, kv := range props {
		out = append(out, map[string]string{"key": kv[0], "value": kv[1]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) setProperties(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req) == 0 {
		apiError(w, http.StatusBadRequest, "bad_request", "no properties given")
		return
	}
	if len(req) > 200 {
		apiError(w, http.StatusBadRequest, "bad_request", "too many properties")
		return
	}
	if err := h.Manager.SetProperties(r.PathValue("id"), req); err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// console streams the ring buffer plus live output as server-sent events.
func (h *Handler) console(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	history, ch, cancel, err := h.Manager.Subscribe(id)
	if err != nil {
		managerError(w, err)
		return
	}
	defer cancel()

	fl, ok := w.(http.Flusher)
	if !ok {
		apiError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if view, err := h.Manager.View(id); err == nil {
		writeSSE(w, mc.Event{Type: "status", Status: view.Status})
	}
	for _, line := range history {
		writeSSE(w, mc.Event{Type: "line", Line: line})
	}
	fl.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			writeSSE(w, ev)
			// Drain whatever queued up before flushing once.
			for i := 0; i < 64; i++ {
				select {
				case more := <-ch:
					writeSSE(w, more)
				default:
					i = 64
				}
			}
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev mc.Event) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
