package web

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxInlineRead = 2 << 20 // editor read limit
	maxTextWrite  = 5 << 20 // editor save limit
	maxUploadSize = 2 << 30 // single file upload limit
)

// openServerRoot returns an os.Root jailed to the server's data directory.
// All file manager operations go through it, which blocks both .. traversal
// and symlink escapes at the kernel path resolution level.
func (h *Handler) openServerRoot(w http.ResponseWriter, r *http.Request) (*os.Root, bool) {
	srv, err := h.Manager.Get(r.PathValue("id"))
	if err != nil {
		managerError(w, err)
		return nil, false
	}
	root, err := os.OpenRoot(srv.DataDir())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", "open data dir: "+err.Error())
		return nil, false
	}
	return root, true
}

// cleanRel normalizes a client-supplied path into a jail-relative fs path.
func cleanRel(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", errors.New("invalid path")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		rel = "."
	}
	return rel, nil
}

func relParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	rel, err := cleanRel(r.URL.Query().Get(name))
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return "", false
	}
	return rel, true
}

type fileEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix milliseconds
}

func (h *Handler) filesList(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	rel, ok := relParam(w, r, "path")
	if !ok {
		return
	}
	entries, err := fs.ReadDir(root.FS(), rel)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "directory not found")
		return
	}
	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		fe := fileEntry{Name: e.Name(), Dir: e.IsDir()}
		if info, err := e.Info(); err == nil {
			fe.Size = info.Size()
			fe.ModTime = info.ModTime().UnixMilli()
		}
		out = append(out, fe)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) fileGet(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	rel, ok := relParam(w, r, "path")
	if !ok {
		return
	}
	f, err := root.Open(rel)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		apiError(w, http.StatusBadRequest, "bad_request", "not a file")
		return
	}

	if r.URL.Query().Get("dl") == "1" {
		name := path.Base(rel)
		w.Header().Set("Content-Type", contentTypeFor(name))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		io.Copy(w, f)
		return
	}

	// Editor read: capped size, always plain text.
	if info.Size() > maxInlineRead {
		apiError(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large to edit, download it instead")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxInlineRead))
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if !utf8.Valid(data) {
		apiError(w, http.StatusUnsupportedMediaType, "binary", "file is not valid text, download it instead")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

func (h *Handler) filePut(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	rel, ok := relParam(w, r, "path")
	if !ok {
		return
	}
	if rel == "." {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid path")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTextWrite))
	if err != nil {
		apiError(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large to save here")
		return
	}
	if err := root.WriteFile(rel, data, 0o644); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) fileDelete(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	rel, ok := relParam(w, r, "path")
	if !ok {
		return
	}
	if rel == "." {
		apiError(w, http.StatusBadRequest, "bad_request", "cannot delete the server root")
		return
	}
	if err := root.RemoveAll(rel); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) fileUpload(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	dirRel, ok := relParam(w, r, "path")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	file, header, err := func() (io.ReadCloser, string, error) {
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, "", err
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				return nil, "", errors.New("no file field in upload")
			}
			if part.FormName() == "file" {
				return part, part.FileName(), nil
			}
			part.Close()
		}
	}()
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	defer file.Close()

	name := path.Base(strings.ReplaceAll(header, "\\", "/"))
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid file name")
		return
	}
	dest := name
	if dirRel != "." {
		dest = dirRel + "/" + name
	}
	out, err := root.Create(dest)
	if err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		root.Remove(dest)
		apiError(w, http.StatusBadRequest, "bad_request", "upload failed: "+err.Error())
		return
	}
	if err := out.Close(); err != nil {
		apiError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (h *Handler) fileMkdir(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	rel, err := cleanRel(req.Path)
	if err != nil || rel == "." {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid path")
		return
	}
	if err := root.MkdirAll(rel, 0o755); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) fileRename(w http.ResponseWriter, r *http.Request) {
	root, ok := h.openServerRoot(w, r)
	if !ok {
		return
	}
	defer root.Close()
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	from, err1 := cleanRel(req.From)
	to, err2 := cleanRel(req.To)
	if err1 != nil || err2 != nil || from == "." || to == "." {
		apiError(w, http.StatusBadRequest, "bad_request", "invalid path")
		return
	}
	if err := root.Rename(from, to); err != nil {
		apiError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func contentTypeFor(name string) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
