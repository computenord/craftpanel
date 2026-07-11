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

func pluginError(w http.ResponseWriter, err error) {
	if errors.Is(err, mc.ErrPluginsUnsupported) {
		apiError(w, http.StatusBadRequest, "plugins_unsupported", "plugins are only available on paper servers")
		return
	}
	managerError(w, err)
}

func (h *Handler) pluginsList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	checkUpdates := r.URL.Query().Get("check") == "1"
	list, err := h.Manager.ListPlugins(ctx, r.PathValue("id"), checkUpdates)
	if err != nil {
		pluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) pluginsSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := h.Manager.SearchPlugins(ctx, r.PathValue("id"), r.URL.Query().Get("q"))
	if err != nil {
		pluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (h *Handler) pluginInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	id := r.PathValue("id")
	entry, err := h.Manager.InstallPlugin(ctx, id, req.ProjectID)
	if err != nil {
		pluginError(w, err)
		return
	}
	log.Printf("server %s: installed plugin %s %s", id, entry.Title, entry.Version)
	writeJSON(w, http.StatusOK, entry)
}

func (h *Handler) pluginDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file := r.URL.Query().Get("file")
	if err := h.Manager.DeletePlugin(id, file); err != nil {
		pluginError(w, err)
		return
	}
	log.Printf("server %s: deleted plugin %s", id, file)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// pluginUpload accepts a user-provided jar and drops it into plugins/.
func (h *Handler) pluginUpload(w http.ResponseWriter, r *http.Request) {
	dir, err := h.Manager.PluginsDirFor(r.PathValue("id"))
	if err != nil {
		pluginError(w, err)
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
	log.Printf("server %s: uploaded plugin %s", r.PathValue("id"), name)
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}
