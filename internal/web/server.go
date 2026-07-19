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
	"github.com/computenord/craftpanel/internal/node"
	"github.com/computenord/craftpanel/internal/sftpd"
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
	Nodes    *node.Registry
	SFTP     *sftpd.Server

	// TrustProxy makes the login rate limiter read the client address from
	// X-Forwarded-For. Only enable it when a reverse proxy sets that header,
	// otherwise clients can forge their own rate limit bucket.
	TrustProxy bool
	// Restart asks the main loop for a graceful shutdown with a restart exit
	// code. Set by main, used by the self update endpoint.
	Restart func()
	// LockState reports the managed-mode lock level (none|grace|locked|
	// suspended). Nil when the panel is self-hosted.
	LockState func() string
	// SSOKey returns the pinned control-plane Ed25519 public key (PEM) for
	// verifying single-sign-on jumps. Nil when self-hosted.
	SSOKey func() string

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

func New(authStore *auth.Store, manager *mc.Manager, versions *mc.Versions, version string, trustProxy bool, restart func(), lockState func() string, ssoKey func() string, nodes *node.Registry, sftpSrv *sftpd.Server) http.Handler {
	h := &Handler{Auth: authStore, Manager: manager, Versions: versions, Version: version, TrustProxy: trustProxy, Restart: restart, LockState: lockState, SSOKey: ssoKey, Nodes: nodes, SFTP: sftpSrv}

	mux := http.NewServeMux()

	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(static)))
	mux.HandleFunc("GET /sso", h.sso)
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
	mux.HandleFunc("GET /api/loaders", h.loaderVersions)

	mux.HandleFunc("GET /api/servers", h.listServers)
	mux.HandleFunc("POST /api/servers", h.createServer)
	mux.HandleFunc("POST /api/servers/from-backup", h.createFromBackup)
	mux.HandleFunc("POST /api/servers/import", h.importServer)
	mux.HandleFunc("POST /api/servers/from-template", h.createFromTemplate)
	mux.HandleFunc("GET /api/servers/{id}", h.getServer)
	mux.HandleFunc("PATCH /api/servers/{id}", h.updateServer)
	mux.HandleFunc("DELETE /api/servers/{id}", h.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/clone", h.cloneServer)
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
	mux.HandleFunc("POST /api/dns/sync", h.dnsSync)
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
	mux.HandleFunc("POST /api/servers/{id}/discord/test", h.discordTest)
	mux.HandleFunc("GET /api/servers/{id}/network", h.networkInfo)
	mux.HandleFunc("PUT /api/servers/{id}/network", h.networkSet)
	mux.HandleFunc("GET /api/servers/{id}/plugins", h.pluginsList)
	mux.HandleFunc("GET /api/servers/{id}/plugins/search", h.pluginsSearch)
	mux.HandleFunc("POST /api/servers/{id}/plugins/install", h.pluginInstall)
	mux.HandleFunc("DELETE /api/servers/{id}/plugins", h.pluginDelete)
	mux.HandleFunc("POST /api/servers/{id}/plugins/upload", h.pluginUpload)
	mux.HandleFunc("GET /api/servers/{id}/mods", h.modsList)
	mux.HandleFunc("GET /api/servers/{id}/mods/search", h.modsSearch)
	mux.HandleFunc("GET /api/servers/{id}/mods/preview", h.modPreview)
	mux.HandleFunc("GET /api/servers/{id}/mods/updates", h.modsUpdatePreview)
	mux.HandleFunc("POST /api/servers/{id}/mods/update-all", h.modsUpdateAll)
	mux.HandleFunc("POST /api/servers/{id}/mods/install", h.modInstall)
	mux.HandleFunc("POST /api/servers/{id}/mods/enable", h.modEnable)
	mux.HandleFunc("DELETE /api/servers/{id}/mods", h.modDelete)
	mux.HandleFunc("POST /api/servers/{id}/mods/upload", h.modUpload)
	mux.HandleFunc("GET /api/modpacks/search", h.modpacksSearch)
	mux.HandleFunc("GET /api/modpacks/analyze", h.modpackAnalyze)
	mux.HandleFunc("GET /api/modpacks/{project}/versions", h.modpackVersions)
	mux.HandleFunc("POST /api/servers/{id}/modpack/upgrade", h.modpackUpgrade)
	mux.HandleFunc("GET /api/servers/{id}/client-pack", h.clientPackInfo)
	mux.HandleFunc("GET /api/servers/{id}/preflight", h.preflight)
	mux.HandleFunc("GET /api/servers/{id}/datapacks", h.datapacksList)
	mux.HandleFunc("GET /api/servers/{id}/datapacks/search", h.datapacksSearch)
	mux.HandleFunc("POST /api/servers/{id}/datapacks/install", h.datapackInstall)
	mux.HandleFunc("POST /api/servers/{id}/datapacks/enable", h.datapackEnable)
	mux.HandleFunc("DELETE /api/servers/{id}/datapacks", h.datapackDelete)
	mux.HandleFunc("POST /api/servers/{id}/datapacks/upload", h.datapackUpload)
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

	mux.HandleFunc("GET /api/users", h.listUsers)
	mux.HandleFunc("POST /api/users", h.createUser)
	mux.HandleFunc("PATCH /api/users/{username}", h.updateUser)
	mux.HandleFunc("DELETE /api/users/{username}", h.deleteUser)
	mux.HandleFunc("GET /api/tokens", h.listTokens)
	mux.HandleFunc("POST /api/tokens", h.createToken)
	mux.HandleFunc("DELETE /api/tokens/{id}", h.deleteToken)
	mux.HandleFunc("GET /api/audit", h.listAudit)
	mux.HandleFunc("GET /api/java", h.listJava)
	mux.HandleFunc("POST /api/java", h.installJava)
	mux.HandleFunc("GET /api/templates", h.listTemplates)
	mux.HandleFunc("PUT /api/templates", h.saveTemplate)
	mux.HandleFunc("DELETE /api/templates/{id}", h.deleteTemplate)
	mux.HandleFunc("GET /api/servers/{id}/metrics", h.serverMetrics)
	mux.HandleFunc("GET /api/servers/{id}/geyser", h.geyserStatus)
	mux.HandleFunc("POST /api/servers/{id}/geyser", h.geyserInstall)
	mux.HandleFunc("POST /api/servers/{id}/world/import", h.importWorld)
	mux.HandleFunc("GET /api/servers/{id}/resource-pack", h.resourcePackGet)
	mux.HandleFunc("POST /api/servers/{id}/resource-pack", h.resourcePackUpload)
	mux.HandleFunc("DELETE /api/servers/{id}/resource-pack", h.resourcePackDelete)
	mux.HandleFunc("GET /api/servers/{id}/resource-pack/download", h.resourcePackDownload)
	mux.HandleFunc("GET /api/servers/{id}/world/download", h.exportWorld)

	mux.HandleFunc("GET /api/nodes", h.listNodes)
	mux.HandleFunc("POST /api/nodes", h.enrollNode)
	mux.HandleFunc("DELETE /api/nodes/{id}", h.deleteNode)
	mux.HandleFunc("POST /api/nodes/{id}/command", h.nodeCommand)
	mux.HandleFunc("POST /api/nodes/sync", h.nodeSync)
	mux.HandleFunc("GET /api/nodes/bootstrap", h.nodeBootstrap)
	mux.HandleFunc("GET /api/nodes/binary", h.nodeBinary)

	return h.securityHeaders(h.csrfGuard(h.authGate(h.lockGuard(mux))))
}

// lockGuard blocks state-changing API calls while the instance is locked or
// suspended (managed mode, e.g. payment overdue). Reads stay allowed so the
// customer can still see their servers; the UI shows a lock banner.
func (h *Handler) lockGuard(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"/api/login":  true,
		"/api/logout": true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.LockState != nil && strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && !allowed[r.URL.Path] {
			switch h.LockState() {
			case "locked", "suspended":
				apiError(w, http.StatusLocked, "locked", "this instance is locked, please check your ComputeBox account")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) lock() string {
	if h.LockState == nil {
		return ""
	}
	return h.LockState()
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
				// Bearer API tokens are not cookie sessions; skip CSRF.
				if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
					next.ServeHTTP(w, r)
					return
				}
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
		"POST /api/login":         true,
		"GET /api/setup-status":   true,
		"POST /api/setup":         true,
		"POST /api/nodes/sync":    true, // remote agents authenticate with node tokens
		"GET /api/nodes/bootstrap": true,
		"GET /api/nodes/binary":   true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || open[r.Method+" "+r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		var user auth.User
		var ok bool
		if authz := r.Header.Get("Authorization"); strings.HasPrefix(authz, "Bearer ") {
			raw := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
			if strings.HasPrefix(raw, "node_") {
				apiError(w, http.StatusUnauthorized, "unauthorized", "node tokens only work on /api/nodes/sync")
				return
			}
			user, ok = h.Auth.ValidateAPIToken(raw)
			if !ok {
				apiError(w, http.StatusUnauthorized, "unauthorized", "invalid API token")
				return
			}
		} else {
			cookie, err := r.Cookie(auth.SessionCookie)
			if err != nil {
				apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
				return
			}
			username, sessOK := h.Auth.ValidateSession(cookie.Value)
			if !sessOK {
				apiError(w, http.StatusUnauthorized, "unauthorized", "session expired")
				return
			}
			user, ok = h.Auth.GetUser(username)
			if !ok {
				apiError(w, http.StatusUnauthorized, "unauthorized", "user not found")
				return
			}
		}
		r = r.WithContext(auth.WithUser(r.Context(), user))

		// Per-server permission gate for /api/servers/{id}/...
		if strings.HasPrefix(r.URL.Path, "/api/servers/") {
			rest := strings.TrimPrefix(r.URL.Path, "/api/servers/")
			id := strings.SplitN(rest, "/", 2)[0]
			if id != "" && id != "from-backup" && id != "import" && id != "from-template" {
				perm := serverPermForRoute(r.Method, r.URL.Path)
				if perm != "" {
					access, allowed := user.AccessFor(id)
					if !allowed || !access.Allows(perm) {
						apiError(w, http.StatusForbidden, "forbidden", "missing permission: "+perm)
						return
					}
				}
			}
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
