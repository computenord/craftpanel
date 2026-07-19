package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/auth"
	"github.com/computenord/craftpanel/internal/mc"
)

/* ---------- users (admin) ---------- */

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.Auth.ListUsers())
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		Username string                      `json:"username"`
		Password string                      `json:"password"`
		Role     string                      `json:"role"`
		Access   map[string]auth.ServerAccess `json:"access"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Auth.CreateUserWithRole(req.Username, req.Password, req.Role, req.Access); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "user.create", "", req.Username)
	u, _ := h.Auth.GetUser(req.Username)
	writeJSON(w, http.StatusCreated, u)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	username := r.PathValue("username")
	var req struct {
		Role   string                       `json:"role"`
		Access map[string]auth.ServerAccess `json:"access"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Auth.UpdateUserRoleAccess(username, req.Role, req.Access); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "user.update", "", username)
	u, _ := h.Auth.GetUser(username)
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	username := r.PathValue("username")
	if err := h.Auth.DeleteUser(username); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "user.delete", "", username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* ---------- API tokens ---------- */

func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) {
	u, ok := h.currentUser(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	if u.IsAdmin() && r.URL.Query().Get("all") == "1" {
		writeJSON(w, http.StatusOK, h.Auth.ListAPITokens(""))
		return
	}
	writeJSON(w, http.StatusOK, h.Auth.ListAPITokens(u.Username))
}

func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) {
	u, ok := h.currentUser(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tok, err := h.Auth.CreateAPIToken(u.Username, req.Name)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "token.create", "", tok.Name)
	writeJSON(w, http.StatusCreated, tok)
}

func (h *Handler) deleteToken(w http.ResponseWriter, r *http.Request) {
	u, ok := h.currentUser(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "unauthorized", "not signed in")
		return
	}
	if err := h.Auth.DeleteAPIToken(r.PathValue("id"), u.Username, u.IsAdmin()); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "token.delete", "", r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* ---------- audit ---------- */

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := h.Manager.ListAudit(limit)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

/* ---------- clone / from-backup / import ---------- */

func (h *Handler) cloneServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermSettings); !ok {
		return
	}
	u, _ := h.currentUser(r)
	if !u.IsAdmin() {
		apiError(w, http.StatusForbidden, "forbidden", "only admins can clone servers")
		return
	}
	var req mc.CloneRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.Manager.Clone(id, req)
	if err != nil {
		managerError(w, err)
		return
	}
	h.audit(r, "server.clone", id, view.ID)
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handler) createFromBackup(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var req mc.CreateFromBackupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.Manager.CreateFromBackup(req)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "server.from_backup", view.ID, req.BackupName)
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handler) importServer(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if err := r.ParseMultipartForm(2 << 30); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "file is required")
		return
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "craftpanel-import-*.zip")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tmp.Close()

	mem, _ := strconv.Atoi(r.FormValue("memoryMB"))
	port, _ := strconv.Atoi(r.FormValue("port"))
	view, err := h.Manager.ImportServer(mc.ImportServerRequest{
		Name: r.FormValue("name"), Type: r.FormValue("type"), Version: r.FormValue("version"),
		MemoryMB: mem, Port: port, ZipPath: tmpPath,
	})
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "server.import", view.ID, "")
	writeJSON(w, http.StatusCreated, view)
}

/* ---------- world import ---------- */

func (h *Handler) importWorld(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermFiles); !ok {
		return
	}
	if err := r.ParseMultipartForm(2 << 30); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "file is required")
		return
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "craftpanel-world-*.zip")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tmp.Close()
	if err := h.Manager.ImportWorld(id, tmpPath); err != nil {
		managerError(w, err)
		return
	}
	h.audit(r, "world.import", id, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

/* ---------- Java runtimes ---------- */

func (h *Handler) listJava(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.Manager.ListJavaRuntimes())
}

func (h *Handler) installJava(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		Major int `json:"major"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	rt, err := h.Manager.InstallJavaRuntime(ctx, req.Major)
	if err != nil {
		apiError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	h.audit(r, "java.install", "", strconv.Itoa(req.Major))
	writeJSON(w, http.StatusOK, rt)
}

/* ---------- Geyser ---------- */

func (h *Handler) geyserStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermView); !ok {
		return
	}
	st, err := h.Manager.GeyserStatus(id)
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *Handler) geyserInstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermSettings); !ok {
		return
	}
	var req struct {
		Floodgate *bool `json:"floodgate"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	withFG := true
	if req.Floodgate != nil {
		withFG = *req.Floodgate
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := h.Manager.InstallGeyser(ctx, id, withFG); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "geyser.install", id, "")
	st, _ := h.Manager.GeyserStatus(id)
	writeJSON(w, http.StatusOK, st)
}

/* ---------- jar build updates ---------- */

// buildUpdate reports whether a newer jar build of the installed Minecraft
// version is available upstream (Paper family, Purpur). Applying it is a
// plain upgrade to the same version.
func (h *Handler) buildUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermView); !ok {
		return
	}
	info, err := h.Manager.BuildUpdate(r.Context(), id)
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

/* ---------- metrics ---------- */

func (h *Handler) serverMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermView); !ok {
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	list, err := h.Manager.ListMetrics(id, since)
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

/* ---------- templates ---------- */

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.Manager.ListTemplates())
}

func (h *Handler) saveTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var t mc.ServerTemplate
	if !decodeJSON(w, r, &t) {
		return
	}
	out, err := h.Manager.SaveTemplate(t)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "template.save", "", out.ID)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if err := h.Manager.DeleteTemplate(r.PathValue("id")); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "template.delete", "", r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) createFromTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		TemplateID string `json:"templateId"`
		Name       string `json:"name"`
		Port       int    `json:"port"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := h.Manager.CreateFromTemplate(req.TemplateID, req.Name, req.Port)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "server.from_template", view.ID, req.TemplateID)
	writeJSON(w, http.StatusCreated, view)
}

/* ---------- resource pack ---------- */

func (h *Handler) resourcePackGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermView); !ok {
		return
	}
	info, err := h.Manager.GetResourcePack(id)
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) resourcePackUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermSettings); !ok {
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", "file is required")
		return
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "craftpanel-rp-*.zip")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	tmp.Close()
	required := r.FormValue("required") == "true" || r.FormValue("required") == "1"
	base := strings.TrimSpace(r.FormValue("publicBase"))
	if base == "" {
		base = publicBaseFromRequest(r)
	}
	info, err := h.Manager.SetResourcePack(id, tmpPath, base, required)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.audit(r, "resourcepack.set", id, "")
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) resourcePackDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermSettings); !ok {
		return
	}
	if err := h.Manager.DeleteResourcePack(id); err != nil {
		managerError(w, err)
		return
	}
	h.audit(r, "resourcepack.delete", id, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) resourcePackDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Authenticated download so packs are not world-readable without a session/token.
	if _, ok := h.requireServerPerm(w, r, id, auth.PermView); !ok {
		return
	}
	f, err := h.Manager.OpenResourcePack(id)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "no resource pack")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(f.Name())+`"`)
	http.ServeContent(w, r, "server-resource-pack.zip", time.Now(), f)
}

func (h *Handler) exportWorld(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.requireServerPerm(w, r, id, auth.PermBackups); !ok {
		return
	}
	path, filename, err := h.Manager.ExportWorld(id)
	if err != nil {
		managerError(w, err)
		return
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeContent(w, r, filename, time.Now(), f)
	h.audit(r, "world.export", id, filename)
}

func publicBaseFromRequest(r *http.Request) string {
	proto := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		proto = "https"
	}
	host := r.Host
	if host == "" {
		return ""
	}
	return proto + "://" + host
}
