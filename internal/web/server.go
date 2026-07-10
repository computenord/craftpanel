// Package web serves the embedded single page UI and the JSON API.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
)

//go:embed static
var staticFiles embed.FS

// csrfHeader must be present on every mutating API request. Browsers do not
// attach custom headers to cross-site form or image requests, so together
// with SameSite=Strict cookies this blocks CSRF.
const csrfHeader = "X-Craftpanel"

type Handler struct {
	Auth     *auth.Store
	Manager  *mc.Manager
	Versions *mc.Versions
	Version  string

	// TrustProxy makes the login rate limiter read the client address from
	// X-Forwarded-For. Only enable it when a reverse proxy sets that header,
	// otherwise clients can forge their own rate limit bucket.
	TrustProxy bool
	// Restart asks the main loop for a graceful shutdown with a restart exit
	// code. Set by main, used by the self update endpoint.
	Restart func()

	javaMu      sync.Mutex
	javaInfo    javaInfo
	javaFetched time.Time

	updMu  sync.Mutex
	updTag string
	updAt  time.Time
}

type javaInfo struct {
	Found   bool   `json:"found"`
	Version string `json:"version,omitempty"`
	Major   int    `json:"major,omitempty"`
}

func New(authStore *auth.Store, manager *mc.Manager, versions *mc.Versions, version string, trustProxy bool, restart func()) http.Handler {
	h := &Handler{Auth: authStore, Manager: manager, Versions: versions, Version: version, TrustProxy: trustProxy, Restart: restart}

	mux := http.NewServeMux()

	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(static)))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, "missing UI", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	})

	mux.HandleFunc("GET /api/setup-status", h.setupStatus)
	mux.HandleFunc("POST /api/setup", h.setup)
	mux.HandleFunc("POST /api/login", h.login)
	mux.HandleFunc("POST /api/logout", h.logout)
	mux.HandleFunc("GET /api/me", h.me)
	mux.HandleFunc("GET /api/system", h.system)
	mux.HandleFunc("GET /api/versions", h.versionList)

	mux.HandleFunc("GET /api/servers", h.listServers)
	mux.HandleFunc("POST /api/servers", h.createServer)
	mux.HandleFunc("GET /api/servers/{id}", h.getServer)
	mux.HandleFunc("PATCH /api/servers/{id}", h.updateServer)
	mux.HandleFunc("DELETE /api/servers/{id}", h.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/start", h.startServer)
	mux.HandleFunc("POST /api/servers/{id}/stop", h.stopServer)
	mux.HandleFunc("POST /api/servers/{id}/restart", h.restartServer)
	mux.HandleFunc("POST /api/servers/{id}/kill", h.killServer)
	mux.HandleFunc("POST /api/servers/{id}/retry-install", h.retryInstall)
	mux.HandleFunc("POST /api/servers/{id}/command", h.command)
	mux.HandleFunc("GET /api/servers/{id}/console", h.console)
	mux.HandleFunc("POST /api/servers/{id}/eula", h.setEULA)
	mux.HandleFunc("GET /api/servers/{id}/properties", h.getProperties)
	mux.HandleFunc("PUT /api/servers/{id}/properties", h.setProperties)

	mux.HandleFunc("GET /api/settings", h.getSettings)
	mux.HandleFunc("PUT /api/settings", h.putSettings)
	mux.HandleFunc("POST /api/system/update", h.systemUpdate)
	mux.HandleFunc("POST /api/system/check-update", h.checkUpdate)
	mux.HandleFunc("POST /api/account/totp/init", h.totpInit)
	mux.HandleFunc("POST /api/account/totp/enable", h.totpEnable)
	mux.HandleFunc("POST /api/account/totp/disable", h.totpDisable)

	mux.HandleFunc("GET /api/servers/{id}/backups", h.backupList)
	mux.HandleFunc("POST /api/servers/{id}/backups", h.backupCreate)
	mux.HandleFunc("POST /api/servers/{id}/backups/restore", h.backupRestore)
	mux.HandleFunc("DELETE /api/servers/{id}/backups", h.backupDelete)
	mux.HandleFunc("GET /api/servers/{id}/backups/download", h.backupDownload)
	mux.HandleFunc("POST /api/servers/{id}/upgrade", h.upgrade)
	mux.HandleFunc("GET /api/servers/{id}/players", h.playersList)
	mux.HandleFunc("POST /api/servers/{id}/players/action", h.playerAction)
	mux.HandleFunc("GET /api/servers/{id}/access", h.accessInfo)
	mux.HandleFunc("POST /api/servers/{id}/access/{list}", h.accessAdd)
	mux.HandleFunc("DELETE /api/servers/{id}/access/{list}", h.accessRemove)
	mux.HandleFunc("PUT /api/servers/{id}/access/whitelist-mode", h.whitelistMode)

	mux.HandleFunc("GET /api/servers/{id}/files", h.filesList)
	mux.HandleFunc("GET /api/servers/{id}/file", h.fileGet)
	mux.HandleFunc("PUT /api/servers/{id}/file", h.filePut)
	mux.HandleFunc("DELETE /api/servers/{id}/file", h.fileDelete)
	mux.HandleFunc("POST /api/servers/{id}/files/upload", h.fileUpload)
	mux.HandleFunc("POST /api/servers/{id}/files/mkdir", h.fileMkdir)
	mux.HandleFunc("POST /api/servers/{id}/files/rename", h.fileRename)

	return h.securityHeaders(h.csrfGuard(h.authGate(mux)))
}

func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		hdr.Set("X-Content-Type-Options", "nosniff")
		hdr.Set("X-Frame-Options", "DENY")
		hdr.Set("Referrer-Policy", "no-referrer")
		hdr.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if r.Header.Get(csrfHeader) == "" {
					apiError(w, http.StatusForbidden, "csrf", "missing "+csrfHeader+" header")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authGate protects every /api route except login and first-run setup.
func (h *Handler) authGate(next http.Handler) http.Handler {
	open := map[string]bool{
		"POST /api/login":       true,
		"GET /api/setup-status": true,
		"POST /api/setup":       true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || open[r.Method+" "+r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(auth.SessionCookie)
		if err != nil {
			apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
			return
		}
		if _, ok := h.Auth.ValidateSession(cookie.Value); !ok {
			apiError(w, http.StatusUnauthorized, "unauthorized", "session expired")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func apiError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

// decodeJSON reads a small JSON body into v, replying with 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (h *Handler) clientIP(r *http.Request) string {
	if h.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if first = strings.TrimSpace(first); first != "" {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// managerError maps well-known manager errors onto HTTP responses.
func managerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mc.ErrNotFound):
		apiError(w, http.StatusNotFound, "not_found", "server not found")
	case errors.Is(err, mc.ErrNotStopped):
		apiError(w, http.StatusConflict, "not_stopped", "server must be stopped first")
	default:
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
	}
}
