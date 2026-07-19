package auth

import (
	"context"
	"net/http"
)

type ctxKey int

const userCtxKey ctxKey = 1

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	PermView     = "view"
	PermConsole  = "console"
	PermFiles    = "files"
	PermControl  = "control"
	PermSettings = "settings"
	PermBackups  = "backups"
	PermDelete   = "delete"
)

// ServerAccess is the permission set a non-admin user has on one server.
type ServerAccess struct {
	View     bool `json:"view"`
	Console  bool `json:"console"`
	Files    bool `json:"files"`
	Control  bool `json:"control"`  // start/stop/restart/kill/command
	Settings bool `json:"settings"` // patch settings, properties, eula, upgrade
	Backups  bool `json:"backups"`
	Delete   bool `json:"delete"`
}

// FullAccess is what admins effectively have on every server.
func FullAccess() ServerAccess {
	return ServerAccess{
		View: true, Console: true, Files: true, Control: true,
		Settings: true, Backups: true, Delete: true,
	}
}

func (u User) IsAdmin() bool {
	return u.Role == "" || u.Role == RoleAdmin
}

func (u User) AccessFor(serverID string) (ServerAccess, bool) {
	if u.IsAdmin() {
		return FullAccess(), true
	}
	if u.Access == nil {
		return ServerAccess{}, false
	}
	a, ok := u.Access[serverID]
	if !ok || !a.View {
		return ServerAccess{}, false
	}
	return a, true
}

func (a ServerAccess) Allows(perm string) bool {
	switch perm {
	case PermView:
		return a.View
	case PermConsole:
		return a.Console
	case PermFiles:
		return a.Files
	case PermControl:
		return a.Control
	case PermSettings:
		return a.Settings
	case PermBackups:
		return a.Backups
	case PermDelete:
		return a.Delete
	default:
		return false
	}
}

// WithUser stores the authenticated user on the request context.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// UserFromContext returns the authenticated user, if any.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userCtxKey).(User)
	return u, ok
}

// UserFromRequest is a convenience wrapper.
func UserFromRequest(r *http.Request) (User, bool) {
	return UserFromContext(r.Context())
}
