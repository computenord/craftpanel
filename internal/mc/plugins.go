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

// Plugins are fetched from Modrinth's open API.
const modrinthBase = "https://api.modrinth.com/v2"

// pluginLoaders are the Modrinth loader tags a Paper server can run.
var pluginLoaders = []string{"paper", "spigot", "bukkit", "folia"}

var ErrPluginsUnsupported = errors.New("plugins are only available on paper servers")

const pluginManifestFile = "plugins.json"

// pluginMeta is what the panel remembers about a Modrinth-installed plugin,
// keyed by jar filename in the manifest.
type pluginMeta struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
	Title     string `json:"title"`
	Version   string `json:"version"`
}

type PluginEntry struct {
	File            string `json:"file"`
	Size            int64  `json:"size"`
	Managed         bool   `json:"managed"`
	Title           string `json:"title,omitempty"`
	Version         string `json:"version,omitempty"`
	ProjectID       string `json:"projectId,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	NewVersion      string `json:"newVersion,omitempty"`
}

type PluginSearchHit struct {
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int    `json:"downloads"`
	Installed   bool   `json:"installed"`
}

func (m *Manager) pluginServer(id string) (*Server, error) {
	srv, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if srv.meta.Type != TypePaper {
		return nil, ErrPluginsUnsupported
	}
	return srv, nil
}

func (s *Server) pluginsDir() string { return filepath.Join(s.DataDir(), "plugins") }

func (s *Server) manifestPath() string { return filepath.Join(s.dir, pluginManifestFile) }

func (s *Server) readManifest() map[string]pluginMeta {
	out := map[string]pluginMeta{}
	fsutil.ReadJSON(s.manifestPath(), &out)
	return out
}

// ListPlugins reports every jar in plugins/, enriched with Modrinth metadata
// for the ones the panel installed. With checkUpdates it also asks Modrinth
// for newer compatible versions.
func (m *Manager) ListPlugins(ctx context.Context, id string, checkUpdates bool) ([]PluginEntry, error) {
	srv, err := m.pluginServer(id)
	if err != nil {
		return nil, err
	}
	manifest := srv.readManifest()
	entries, _ := os.ReadDir(srv.pluginsDir())

	out := []PluginEntry{}
	changed := false
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		seen[e.Name()] = true
		pe := PluginEntry{File: e.Name()}
		if info, err := e.Info(); err == nil {
			pe.Size = info.Size()
		}
		if meta, ok := manifest[e.Name()]; ok {
			pe.Managed = true
			pe.Title = meta.Title
			pe.Version = meta.Version
			pe.ProjectID = meta.ProjectID
		}
		out = append(out, pe)
	}
	// Forget manifest entries whose jar was deleted through the file manager.
	for file := range manifest {
		if !seen[file] {
			delete(manifest, file)
			changed = true
		}
	}
	if changed {
		fsutil.WriteJSONAtomic(srv.manifestPath(), manifest)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].File) < strings.ToLower(out[j].File) })

	if checkUpdates {
		srv.mu.Lock()
		mcVersion := srv.meta.Version
		srv.mu.Unlock()
		for i := range out {
			if !out[i].Managed || out[i].ProjectID == "" {
				continue
			}
			ver, _, err := resolvePluginVersion(ctx, out[i].ProjectID, mcVersion)
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

// SearchPlugins queries Modrinth for Paper-compatible plugins.
func (m *Manager) SearchPlugins(ctx context.Context, id, query string) ([]PluginSearchHit, error) {
	srv, err := m.pluginServer(id)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []PluginSearchHit{}, nil
	}
	loaderFacet := []string{}
	for _, l := range pluginLoaders {
		loaderFacet = append(loaderFacet, "categories:"+l)
	}
	facets, _ := json.Marshal([][]string{{"project_type:plugin"}, loaderFacet})
	searchURL := fmt.Sprintf("%s/search?limit=20&query=%s&facets=%s",
		modrinthBase, url.QueryEscape(query), url.QueryEscape(string(facets)))

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
	for _, meta := range srv.readManifest() {
		installed[meta.ProjectID] = true
	}
	out := []PluginSearchHit{}
	for _, h := range resp.Hits {
		desc := h.Description
		if len(desc) > 160 {
			desc = desc[:160] + "…"
		}
		out = append(out, PluginSearchHit{
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

type modrinthVersion struct {
	ID            string `json:"id"`
	VersionNumber string `json:"version_number"`
	DatePublished string `json:"date_published"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
		Hashes   struct {
			SHA512 string `json:"sha512"`
		} `json:"hashes"`
	} `json:"files"`
}

type modrinthFile struct {
	URL      string
	Filename string
	SHA512   string
}

// resolvePluginVersion picks the newest plugin version for the server's
// Minecraft version, falling back to the newest Paper-compatible version of
// any game version when nothing matches exactly.
func resolvePluginVersion(ctx context.Context, projectID, mcVersion string) (modrinthVersion, modrinthFile, error) {
	loaders, _ := json.Marshal(pluginLoaders)
	fetch := func(withGameVersion bool) ([]modrinthVersion, error) {
		u := fmt.Sprintf("%s/project/%s/version?loaders=%s",
			modrinthBase, url.PathEscape(projectID), url.QueryEscape(string(loaders)))
		if withGameVersion {
			gv, _ := json.Marshal([]string{mcVersion})
			u += "&game_versions=" + url.QueryEscape(string(gv))
		}
		var versions []modrinthVersion
		if err := getJSON(ctx, u, &versions); err != nil {
			return nil, err
		}
		return versions, nil
	}
	versions, err := fetch(true)
	if err != nil {
		return modrinthVersion{}, modrinthFile{}, fmt.Errorf("modrinth versions: %w", err)
	}
	if len(versions) == 0 {
		if versions, err = fetch(false); err != nil {
			return modrinthVersion{}, modrinthFile{}, fmt.Errorf("modrinth versions: %w", err)
		}
	}
	if len(versions) == 0 {
		return modrinthVersion{}, modrinthFile{}, errors.New("no compatible plugin version found")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].DatePublished > versions[j].DatePublished })
	v := versions[0]
	for _, f := range v.Files {
		if !f.Primary && len(v.Files) > 1 {
			continue
		}
		name := path.Base(strings.ReplaceAll(f.Filename, "\\", "/"))
		if !strings.HasSuffix(strings.ToLower(name), ".jar") || strings.Contains(name, "..") {
			continue
		}
		return v, modrinthFile{URL: f.URL, Filename: name, SHA512: f.Hashes.SHA512}, nil
	}
	// No primary flagged, take the first jar.
	for _, f := range v.Files {
		name := path.Base(strings.ReplaceAll(f.Filename, "\\", "/"))
		if strings.HasSuffix(strings.ToLower(name), ".jar") && !strings.Contains(name, "..") {
			return v, modrinthFile{URL: f.URL, Filename: name, SHA512: f.Hashes.SHA512}, nil
		}
	}
	return modrinthVersion{}, modrinthFile{}, errors.New("plugin version has no jar file")
}

// InstallPlugin downloads the best matching version of a Modrinth project
// into plugins/. Installing an already managed project acts as an update:
// the old jar is replaced.
func (m *Manager) InstallPlugin(ctx context.Context, id, projectID string) (PluginEntry, error) {
	srv, err := m.pluginServer(id)
	if err != nil {
		return PluginEntry{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.ContainsAny(projectID, "/\\ ") {
		return PluginEntry{}, errors.New("invalid project id")
	}
	srv.mu.Lock()
	mcVersion := srv.meta.Version
	srv.mu.Unlock()

	var project struct {
		Title string `json:"title"`
	}
	if err := getJSON(ctx, modrinthBase+"/project/"+url.PathEscape(projectID), &project); err != nil {
		return PluginEntry{}, fmt.Errorf("modrinth project: %w", err)
	}
	ver, file, err := resolvePluginVersion(ctx, projectID, mcVersion)
	if err != nil {
		return PluginEntry{}, err
	}

	dir := srv.pluginsDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return PluginEntry{}, err
	}
	dest := filepath.Join(dir, file.Filename)
	if err := downloadVerified(ctx, file.URL, dest, "sha512", file.SHA512, nil); err != nil {
		return PluginEntry{}, fmt.Errorf("download plugin: %w", err)
	}

	// One project, one jar: drop older files of the same project.
	manifest := srv.readManifest()
	for oldFile, meta := range manifest {
		if meta.ProjectID == projectID && oldFile != file.Filename {
			os.Remove(filepath.Join(dir, oldFile))
			delete(manifest, oldFile)
		}
	}
	manifest[file.Filename] = pluginMeta{
		ProjectID: projectID,
		VersionID: ver.ID,
		Title:     project.Title,
		Version:   ver.VersionNumber,
	}
	if err := fsutil.WriteJSONAtomic(srv.manifestPath(), manifest); err != nil {
		return PluginEntry{}, err
	}
	entry := PluginEntry{
		File: file.Filename, Managed: true,
		Title: project.Title, Version: ver.VersionNumber, ProjectID: projectID,
	}
	if fi, err := os.Stat(dest); err == nil {
		entry.Size = fi.Size()
	}
	return entry, nil
}

// DeletePlugin removes a jar from plugins/ and forgets its manifest entry.
func (m *Manager) DeletePlugin(id, file string) error {
	srv, err := m.pluginServer(id)
	if err != nil {
		return err
	}
	name := path.Base(strings.ReplaceAll(file, "\\", "/"))
	if name == "" || name == "." || !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return errors.New("invalid plugin file name")
	}
	if err := os.Remove(filepath.Join(srv.pluginsDir(), name)); err != nil {
		return err
	}
	manifest := srv.readManifest()
	if _, ok := manifest[name]; ok {
		delete(manifest, name)
		fsutil.WriteJSONAtomic(srv.manifestPath(), manifest)
	}
	return nil
}

// PluginsDirFor exposes the plugins directory for the upload handler and
// makes sure it exists.
func (m *Manager) PluginsDirFor(id string) (string, error) {
	srv, err := m.pluginServer(id)
	if err != nil {
		return "", err
	}
	dir := srv.pluginsDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}
