package mc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/computenord/craftpanel/internal/fsutil"
)

var ErrModsUnsupported = errors.New("mods are only available on fabric, forge, neoforge and quilt servers")

const modManifestFile = "mods.json"

// modLoadersFor maps a server type to the Modrinth loader tags it can run.
func modLoadersFor(typ string) []string {
	switch typ {
	case TypeFabric:
		return []string{"fabric"}
	case TypeForge:
		return []string{"forge"}
	case TypeNeoForge:
		return []string{"neoforge"}
	case TypeQuilt:
		return []string{"quilt", "fabric"} // many Fabric mods run on Quilt
	default:
		return nil
	}
}

type modMeta struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
	Title     string `json:"title"`
	Version   string `json:"version"`
}

type ModEntry struct {
	File            string `json:"file"`
	Size            int64  `json:"size"`
	Managed         bool   `json:"managed"`
	Title           string `json:"title,omitempty"`
	Version         string `json:"version,omitempty"`
	ProjectID       string `json:"projectId,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	NewVersion      string `json:"newVersion,omitempty"`
}

type ModSearchHit struct {
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int    `json:"downloads"`
	Installed   bool   `json:"installed"`
}

func (m *Manager) modServer(id string) (*Server, error) {
	srv, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if !IsModded(srv.meta.Type) {
		return nil, ErrModsUnsupported
	}
	return srv, nil
}

func (s *Server) modsDir() string { return filepath.Join(s.DataDir(), "mods") }

func (s *Server) modManifestPath() string { return filepath.Join(s.dir, modManifestFile) }

func (s *Server) readModManifest() map[string]modMeta {
	out := map[string]modMeta{}
	fsutil.ReadJSON(s.modManifestPath(), &out)
	return out
}

// ListMods reports every jar in mods/, enriched with Modrinth metadata for
// the ones the panel installed.
func (m *Manager) ListMods(ctx context.Context, id string, checkUpdates bool) ([]ModEntry, error) {
	srv, err := m.modServer(id)
	if err != nil {
		return nil, err
	}
	manifest := srv.readModManifest()
	entries, _ := os.ReadDir(srv.modsDir())

	out := []ModEntry{}
	changed := false
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		seen[e.Name()] = true
		me := ModEntry{File: e.Name()}
		if info, err := e.Info(); err == nil {
			me.Size = info.Size()
		}
		if meta, ok := manifest[e.Name()]; ok {
			me.Managed = true
			me.Title = meta.Title
			me.Version = meta.Version
			me.ProjectID = meta.ProjectID
		}
		out = append(out, me)
	}
	for file := range manifest {
		if !seen[file] {
			delete(manifest, file)
			changed = true
		}
	}
	if changed {
		fsutil.WriteJSONAtomic(srv.modManifestPath(), manifest)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].File) < strings.ToLower(out[j].File) })

	if checkUpdates {
		srv.mu.Lock()
		mcVersion := srv.meta.Version
		loaders := modLoadersFor(srv.meta.Type)
		srv.mu.Unlock()
		for i := range out {
			if !out[i].Managed || out[i].ProjectID == "" {
				continue
			}
			ver, _, err := resolveModrinthVersion(ctx, out[i].ProjectID, mcVersion, loaders)
			if err != nil {
				continue
			}
			if meta := manifest[out[i].File]; ver.ID != meta.VersionID {
				out[i].UpdateAvailable = true
				out[i].NewVersion = ver.VersionNumber
			}
		}
	}
	return out, nil
}

// SearchMods queries Modrinth for mods compatible with this server's loader.
func (m *Manager) SearchMods(ctx context.Context, id, query, index string) ([]ModSearchHit, error) {
	srv, err := m.modServer(id)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if !searchIndexes[index] {
		index = "relevance"
	}
	if query == "" && index == "relevance" {
		index = "downloads"
	}
	loaderFacet := []string{}
	for _, l := range modLoadersFor(srv.meta.Type) {
		loaderFacet = append(loaderFacet, "categories:"+l)
	}
	facets, _ := json.Marshal([][]string{{"project_type:mod"}, loaderFacet})
	searchURL := fmt.Sprintf("%s/search?limit=20&query=%s&index=%s&facets=%s",
		modrinthBase, url.QueryEscape(query), url.QueryEscape(index), url.QueryEscape(string(facets)))

	var resp struct {
		Hits []struct {
			ProjectID   string `json:"project_id"`
			Slug        string `json:"slug"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Downloads   int    `json:"downloads"`
		} `json:"hits"`
	}
	if err := getJSON(ctx, searchURL, &resp); err != nil {
		return nil, fmt.Errorf("modrinth search: %w", err)
	}

	installed := map[string]bool{}
	for _, meta := range srv.readModManifest() {
		installed[meta.ProjectID] = true
	}
	out := []ModSearchHit{}
	for _, h := range resp.Hits {
		desc := h.Description
		if len(desc) > 160 {
			desc = desc[:160] + "…"
		}
		out = append(out, ModSearchHit{
			ProjectID:   h.ProjectID,
			Slug:        h.Slug,
			Title:       h.Title,
			Description: desc,
			Downloads:   h.Downloads,
			Installed:   installed[h.ProjectID],
		})
	}
	return out, nil
}

// InstallMod downloads the best matching version of a Modrinth mod into mods/.
func (m *Manager) InstallMod(ctx context.Context, id, projectID string) (ModEntry, error) {
	srv, err := m.modServer(id)
	if err != nil {
		return ModEntry{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.ContainsAny(projectID, "/\\ ") {
		return ModEntry{}, errors.New("invalid project id")
	}
	srv.mu.Lock()
	mcVersion := srv.meta.Version
	loaders := modLoadersFor(srv.meta.Type)
	srv.mu.Unlock()

	var project struct {
		Title string `json:"title"`
	}
	if err := getJSON(ctx, modrinthBase+"/project/"+url.PathEscape(projectID), &project); err != nil {
		return ModEntry{}, fmt.Errorf("modrinth project: %w", err)
	}
	ver, file, err := resolveModrinthVersion(ctx, projectID, mcVersion, loaders)
	if err != nil {
		return ModEntry{}, err
	}

	dir := srv.modsDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return ModEntry{}, err
	}
	dest := filepath.Join(dir, file.Filename)
	if err := downloadVerified(ctx, file.URL, dest, "sha512", file.SHA512, nil); err != nil {
		return ModEntry{}, fmt.Errorf("download mod: %w", err)
	}

	manifest := srv.readModManifest()
	for oldFile, meta := range manifest {
		if meta.ProjectID == projectID && oldFile != file.Filename {
			os.Remove(filepath.Join(dir, oldFile))
			delete(manifest, oldFile)
		}
	}
	manifest[file.Filename] = modMeta{
		ProjectID: projectID,
		VersionID: ver.ID,
		Title:     project.Title,
		Version:   ver.VersionNumber,
	}
	if err := fsutil.WriteJSONAtomic(srv.modManifestPath(), manifest); err != nil {
		return ModEntry{}, err
	}
	entry := ModEntry{
		File: file.Filename, Managed: true,
		Title: project.Title, Version: ver.VersionNumber, ProjectID: projectID,
	}
	if fi, err := os.Stat(dest); err == nil {
		entry.Size = fi.Size()
	}
	return entry, nil
}

// DeleteMod removes a jar from mods/ and forgets its manifest entry.
func (m *Manager) DeleteMod(id, file string) error {
	srv, err := m.modServer(id)
	if err != nil {
		return err
	}
	name := path.Base(strings.ReplaceAll(file, "\\", "/"))
	if name == "" || name == "." || !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return errors.New("invalid mod file name")
	}
	if err := os.Remove(filepath.Join(srv.modsDir(), name)); err != nil {
		return err
	}
	manifest := srv.readModManifest()
	if _, ok := manifest[name]; ok {
		delete(manifest, name)
		fsutil.WriteJSONAtomic(srv.modManifestPath(), manifest)
	}
	return nil
}

// ModsDirFor exposes the mods directory for the upload handler.
func (m *Manager) ModsDirFor(id string) (string, error) {
	srv, err := m.modServer(id)
	if err != nil {
		return "", err
	}
	dir := srv.modsDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}

// resolveModrinthVersion picks the newest project version for the server's
// Minecraft version and loaders (works for mods and is shared with plugins).
func resolveModrinthVersion(ctx context.Context, projectID, mcVersion string, loaderList []string) (modrinthVersion, modrinthFile, error) {
	return resolvePluginVersion(ctx, projectID, mcVersion, loaderList)
}
