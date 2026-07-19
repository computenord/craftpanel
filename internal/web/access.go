package web

import (
	"net/http"
	"strings"

	"github.com/computenord/craftpanel/internal/auth"
)

func (h *Handler) currentUser(r *http.Request) (auth.User, bool) {
	if u, ok := auth.UserFromRequest(r); ok {
		return u, true
	}
	// Fallback for handlers that somehow skipped context injection.
	name := h.username(r)
	if name == "" {
		return auth.User{}, false
	}
	return h.Auth.GetUser(name)
}

func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	u, ok := h.currentUser(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return auth.User{}, false
	}
	if !u.IsAdmin() {
		apiError(w, http.StatusForbidden, "forbidden", "admin only")
		return auth.User{}, false
	}
	return u, true
}

// requireServerPerm checks per-server access. Admins always pass.
func (h *Handler) requireServerPerm(w http.ResponseWriter, r *http.Request, serverID, perm string) (auth.User, bool) {
	u, ok := h.currentUser(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return auth.User{}, false
	}
	access, ok := u.AccessFor(serverID)
	if !ok || !access.Allows(perm) {
		apiError(w, http.StatusForbidden, "forbidden", "missing permission: "+perm)
		return auth.User{}, false
	}
	return u, true
}

func (h *Handler) audit(r *http.Request, action, serverID, detail string) {
	u, _ := h.currentUser(r)
	actor := u.Username
	if actor == "" {
		actor = "?"
	}
	h.Manager.Audit(actor, action, serverID, detail, h.clientIP(r))
}

// serverPermForRoute maps common path+method patterns to a required permission.
func serverPermForRoute(method, path string) string {
	// path like /api/servers/{id}/...
	if !strings.HasPrefix(path, "/api/servers/") {
		return ""
	}
	rest := strings.TrimPrefix(path, "/api/servers/")
	parts := strings.Split(rest, "/")
	if len(parts) < 1 || parts[0] == "" {
		return ""
	}
	if len(parts) == 1 {
		switch method {
		case http.MethodGet:
			return auth.PermView
		case http.MethodPatch:
			return auth.PermSettings
		case http.MethodDelete:
			return auth.PermDelete
		}
		return auth.PermView
	}
	action := parts[1]
	switch {
	case action == "start", action == "stop", action == "restart", action == "kill", action == "command":
		return auth.PermControl
	case action == "console":
		return auth.PermConsole
	case action == "files", action == "file":
		return auth.PermFiles
	case action == "backups":
		return auth.PermBackups
	case action == "eula", action == "properties", action == "upgrade", action == "discord",
		action == "network", action == "modpack", action == "retry-install":
		return auth.PermSettings
	case action == "plugins", action == "mods", action == "datapacks", action == "players",
		action == "access", action == "geyser", action == "world", action == "resource-pack",
		action == "metrics", action == "preflight", action == "client-pack":
		if method == http.MethodGet {
			return auth.PermView
		}
		if action == "players" {
			return auth.PermControl
		}
		return auth.PermSettings
	case action == "clone":
		return auth.PermSettings
	default:
		return auth.PermView
	}
}
