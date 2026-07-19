package web

import (
	"net/http"
	"strings"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
)

// NodeLocalAPI serves the control plane on a remote agent. Authenticated with
// the node enroll token; every request runs as an admin.
func NodeLocalAPI(manager *mc.Manager, versions *mc.Versions, token, version string) http.Handler {
	h := &Handler{Manager: manager, Versions: versions, Version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "role": "node", "version": version})
	})
	h.mountNodeRoutes(mux)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			apiError(w, http.StatusUnauthorized, "unauthorized", "missing node token")
			return
		}
		got := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		if got == "" || got != token {
			apiError(w, http.StatusUnauthorized, "unauthorized", "invalid node token")
			return
		}
		admin := auth.User{Username: "node", Role: "admin"}
		r = r.WithContext(auth.WithUser(r.Context(), admin))
		mux.ServeHTTP(w, r)
	})
}

// mountNodeRoutes registers the subset of panel APIs a remote node must expose
// so the panel can fully operate servers on that host.
func (h *Handler) mountNodeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/versions", h.versionList)
	mux.HandleFunc("GET /api/loaders", h.loaderVersions)
	mux.HandleFunc("GET /api/modpacks/search", h.modpacksSearch)
	mux.HandleFunc("GET /api/modpacks/analyze", h.modpackAnalyze)
	mux.HandleFunc("GET /api/modpacks/{project}/versions", h.modpackVersions)

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

	mux.HandleFunc("GET /api/servers/{id}/backups", h.backupList)
	mux.HandleFunc("POST /api/servers/{id}/backups", h.backupCreate)
	mux.HandleFunc("POST /api/servers/{id}/backups/restore", h.backupRestore)
	mux.HandleFunc("DELETE /api/servers/{id}/backups", h.backupDelete)
	mux.HandleFunc("GET /api/servers/{id}/backups/download", h.backupDownload)
	mux.HandleFunc("POST /api/servers/{id}/upgrade", h.upgrade)
	mux.HandleFunc("GET /api/servers/{id}/build-update", h.buildUpdate)
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

	mux.HandleFunc("GET /api/servers/{id}/metrics", h.serverMetrics)
	mux.HandleFunc("GET /api/servers/{id}/geyser", h.geyserStatus)
	mux.HandleFunc("POST /api/servers/{id}/geyser", h.geyserInstall)
	mux.HandleFunc("POST /api/servers/{id}/world/import", h.importWorld)
	mux.HandleFunc("GET /api/servers/{id}/resource-pack", h.resourcePackGet)
	mux.HandleFunc("POST /api/servers/{id}/resource-pack", h.resourcePackUpload)
	mux.HandleFunc("DELETE /api/servers/{id}/resource-pack", h.resourcePackDelete)
	mux.HandleFunc("GET /api/servers/{id}/resource-pack/download", h.resourcePackDownload)
	mux.HandleFunc("GET /api/servers/{id}/world/download", h.exportWorld)

	mux.HandleFunc("GET /api/templates", h.listTemplates)
	mux.HandleFunc("PUT /api/templates", h.saveTemplate)
	mux.HandleFunc("DELETE /api/templates/{id}", h.deleteTemplate)
	mux.HandleFunc("GET /api/java", h.listJava)
	mux.HandleFunc("POST /api/java", h.installJava)
}
