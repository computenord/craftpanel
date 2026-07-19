package web

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/mc"
)

func modError(w http.ResponseWriter, err error) {
	if errors.Is(err, mc.ErrModsUnsupported) {
		apiError(w, http.StatusBadRequest, "mods_unsupported", "mods are only available on fabric, forge, neoforge and quilt servers")
		return
	}
	managerError(w, err)
}

func datapackError(w http.ResponseWriter, err error) {
	if errors.Is(err, mc.ErrDatapacksUnsupported) {
		apiError(w, http.StatusBadRequest, "datapacks_unsupported", "datapacks are not available on this server type")
		return
	}
	managerError(w, err)
}

func (h *Handler) modsList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	checkUpdates := r.URL.Query().Get("check") == "1"
	list, err := h.Manager.ListMods(ctx, r.PathValue("id"), checkUpdates)
	if err != nil {
		modError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) modsSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := h.Manager.SearchMods(ctx, r.PathValue("id"), r.URL.Query().Get("q"), r.URL.Query().Get("sort"))
	if err != nil {
		modError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (h *Handler) modInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	id := r.PathValue("id")
	entry, err := h.Manager.InstallMod(ctx, id, req.ProjectID)
	if err != nil {
		modError(w, err)
		return
	}
	log.Printf("server %s: installed mod %s %s", id, entry.Title, entry.Version)
	writeJSON(w, http.StatusOK, entry)
}

func (h *Handler) modPreview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	preview, err := h.Manager.PreviewModInstall(ctx, r.PathValue("id"), r.URL.Query().Get("projectId"))
	if err != nil {
		modError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) modDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file := r.URL.Query().Get("file")
	if err := h.Manager.DeleteMod(id, file); err != nil {
		modError(w, err)
		return
	}
	log.Printf("server %s: deleted mod %s", id, file)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) modEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File    string `json:"file"`
		Enabled bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manager.SetModEnabled(r.PathValue("id"), req.File, req.Enabled); err != nil {
		modError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) modsUpdatePreview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	preview, err := h.Manager.PreviewModUpdates(ctx, r.PathValue("id"))
	if err != nil {
		modError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) modsUpdateAll(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	n, err := h.Manager.UpdateAllMods(ctx, r.PathValue("id"))
	if err != nil {
		modError(w, err)
		return
	}
	log.Printf("server %s: bulk-updated %d mods", r.PathValue("id"), n)
	writeJSON(w, http.StatusOK, map[string]int{"updated": n})
}

func (h *Handler) modUpload(w http.ResponseWriter, r *http.Request) {
	dir, err := h.Manager.ModsDirFor(r.PathValue("id"))
	if err != nil {
		modError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var part io.ReadCloser
	var filename string
	for {
		p, err := mr.NextPart()
		if err != nil {
			apiError(w, http.StatusBadRequest, "bad_request", "no file field in upload")
			return
		}
		if p.FormName() == "file" {
			part = p
			filename = p.FileName()
			break
		}
		p.Close()
	}
	defer part.Close()

	name := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if name == "" || name == "." || strings.Contains(name, "..") || !strings.HasSuffix(strings.ToLower(name), ".jar") {
		apiError(w, http.StatusBadRequest, "bad_request", "the file must be a .jar")
		return
	}
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if _, err := io.Copy(out, part); err != nil {
		out.Close()
		os.Remove(filepath.Join(dir, name))
		apiError(w, http.StatusBadRequest, "bad_request", "upload failed: "+err.Error())
		return
	}
	if err := out.Close(); err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	log.Printf("server %s: uploaded mod %s", r.PathValue("id"), name)
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (h *Handler) modpacksSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	hits, err := h.Manager.SearchModpacks(ctx, r.URL.Query().Get("q"), r.URL.Query().Get("sort"), r.URL.Query().Get("source"))
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (h *Handler) modpackVersions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list, err := h.Manager.ListModpackVersions(ctx, r.PathValue("project"), r.URL.Query().Get("source"))
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) modpackAnalyze(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	a, err := h.Manager.AnalyzeModpack(ctx, r.URL.Query().Get("source"), r.URL.Query().Get("project"), r.URL.Query().Get("version"))
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) modpackUpgrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VersionID string `json:"versionId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if err := h.Manager.UpgradeModpack(id, req.VersionID); err != nil {
		managerError(w, err)
		return
	}
	log.Printf("server %s: upgrading modpack to %s", id, req.VersionID)
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (h *Handler) clientPackInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.Manager.ClientPackInfoFor(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) preflight(w http.ResponseWriter, r *http.Request) {
	res, err := h.Manager.Preflight(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) loaderVersions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list, err := h.Versions.ListLoaderVersions(ctx, r.URL.Query().Get("type"), r.URL.Query().Get("version"))
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

/* ---------- datapacks ---------- */

func (h *Handler) datapacksList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list, err := h.Manager.ListDatapacks(ctx, r.PathValue("id"), r.URL.Query().Get("check") == "1")
	if err != nil {
		datapackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) datapacksSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := h.Manager.SearchDatapacks(ctx, r.PathValue("id"), r.URL.Query().Get("q"), r.URL.Query().Get("sort"))
	if err != nil {
		datapackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (h *Handler) datapackInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	entry, err := h.Manager.InstallDatapack(ctx, r.PathValue("id"), req.ProjectID)
	if err != nil {
		datapackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (h *Handler) datapackDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.Manager.DeleteDatapack(r.PathValue("id"), r.URL.Query().Get("file")); err != nil {
		datapackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) datapackEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File    string `json:"file"`
		Enabled bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manager.SetDatapackEnabled(r.PathValue("id"), req.File, req.Enabled); err != nil {
		datapackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) datapackUpload(w http.ResponseWriter, r *http.Request) {
	dir, err := h.Manager.DatapacksDirFor(r.PathValue("id"))
	if err != nil {
		datapackError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var part io.ReadCloser
	var filename string
	for {
		p, err := mr.NextPart()
		if err != nil {
			apiError(w, http.StatusBadRequest, "bad_request", "no file field in upload")
			return
		}
		if p.FormName() == "file" {
			part = p
			filename = p.FileName()
			break
		}
		p.Close()
	}
	defer part.Close()
	name := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if name == "" || name == "." || strings.Contains(name, "..") || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		apiError(w, http.StatusBadRequest, "bad_request", "the file must be a .zip")
		return
	}
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if _, err := io.Copy(out, part); err != nil {
		out.Close()
		os.Remove(filepath.Join(dir, name))
		apiError(w, http.StatusBadRequest, "bad_request", "upload failed: "+err.Error())
		return
	}
	if err := out.Close(); err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}
