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

var ErrDatapacksUnsupported = errors.New("datapacks are only available on Java world servers")

const datapackManifestFile = "datapacks.json"

type datapackMeta struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
	Title     string `json:"title"`
	Version   string `json:"version"`
}

type DatapackEntry struct {
	File            string `json:"file"`
	Size            int64  `json:"size"`
	Managed         bool   `json:"managed"`
	Title           string `json:"title,omitempty"`
	Version         string `json:"version,omitempty"`
	ProjectID       string `json:"projectId,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	NewVersion      string `json:"newVersion,omitempty"`
	Disabled        bool   `json:"disabled,omitempty"`
}

type DatapackSearchHit struct {
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int    `json:"downloads"`
	Installed   bool   `json:"installed"`
}

func (m *Manager) datapackServer(id string) (*Server, error) {
	srv, err := m.get(id)
	if err != nil {
		return nil, err
	}
	switch srv.meta.Type {
	case TypeBedrock, TypeVelocity:
		return nil, ErrDatapacksUnsupported
	}
	return srv, nil
}

func (s *Server) worldName() string {
	props, err := os.ReadFile(filepath.Join(s.DataDir(), "server.properties"))
	if err != nil {
		return "world"
	}
	for _, line := range strings.Split(string(props), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "level-name=") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "level-name="))
			if name != "" && !strings.Contains(name, "..") && !strings.ContainsAny(name, `/\:`) {
				return name
			}
		}
	}
	return "world"
}

func (s *Server) datapacksDir() string {
	return filepath.Join(s.DataDir(), s.worldName(), "datapacks")
}

func (s *Server) datapackManifestPath() string {
	return filepath.Join(s.dir, datapackManifestFile)
}

func (s *Server) readDatapackManifest() map[string]datapackMeta {
	out := map[string]datapackMeta{}
	fsutil.ReadJSON(s.datapackManifestPath(), &out)
	return out
}

func (m *Manager) ListDatapacks(ctx context.Context, id string, checkUpdates bool) ([]DatapackEntry, error) {
	srv, err := m.datapackServer(id)
	if err != nil {
		return nil, err
	}
	dir := srv.datapacksDir()
	_ = os.MkdirAll(dir, 0o750)
	manifest := srv.readDatapackManifest()
	entries, _ := os.ReadDir(dir)
	out := []DatapackEntry{}
	seen := map[string]bool{}
	changed := false
	for _, e := range entries {
		name := e.Name()
		low := strings.ToLower(name)
		disabled := strings.HasSuffix(low, ".zip.disabled")
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(low, ".zip") && !disabled {
			continue
		}
		display := name
		if disabled {
			display = strings.TrimSuffix(name, ".disabled")
			if !strings.HasSuffix(strings.ToLower(display), ".zip") {
				continue
			}
		}
		seen[display] = true
		de := DatapackEntry{File: display, Disabled: disabled}
		if info, err := e.Info(); err == nil {
			de.Size = info.Size()
		}
		key := display
		if meta, ok := manifest[key]; ok {
			de.Managed = true
			de.Title = meta.Title
			de.Version = meta.Version
			de.ProjectID = meta.ProjectID
		}
		out = append(out, de)
	}
	for file := range manifest {
		if !seen[file] {
			delete(manifest, file)
			changed = true
		}
	}
	if changed {
		fsutil.WriteJSONAtomic(srv.datapackManifestPath(), manifest)
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
			ver, _, err := resolveModrinthVersion(ctx, out[i].ProjectID, mcVersion, []string{"datapack"})
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

func (m *Manager) SearchDatapacks(ctx context.Context, id, query, index string) ([]DatapackSearchHit, error) {
	srv, err := m.datapackServer(id)
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
	facets, _ := json.Marshal([][]string{{"project_type:datapack"}})
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
		return nil, err
	}
	installed := map[string]bool{}
	for _, meta := range srv.readDatapackManifest() {
		installed[meta.ProjectID] = true
	}
	out := []DatapackSearchHit{}
	for _, h := range resp.Hits {
		desc := h.Description
		if len(desc) > 160 {
			desc = desc[:160] + "…"
		}
		out = append(out, DatapackSearchHit{
			ProjectID: h.ProjectID, Slug: h.Slug, Title: h.Title,
			Description: desc, Downloads: h.Downloads, Installed: installed[h.ProjectID],
		})
	}
	return out, nil
}

func (m *Manager) InstallDatapack(ctx context.Context, id, projectID string) (DatapackEntry, error) {
	srv, err := m.datapackServer(id)
	if err != nil {
		return DatapackEntry{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.ContainsAny(projectID, "/\\ ") {
		return DatapackEntry{}, errors.New("invalid project id")
	}
	srv.mu.Lock()
	mcVersion := srv.meta.Version
	srv.mu.Unlock()
	var project struct {
		Title string `json:"title"`
	}
	if err := getJSON(ctx, modrinthBase+"/project/"+url.PathEscape(projectID), &project); err != nil {
		return DatapackEntry{}, err
	}
	ver, file, err := resolveDatapackFile(ctx, projectID, mcVersion)
	if err != nil {
		return DatapackEntry{}, err
	}
	dir := srv.datapacksDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return DatapackEntry{}, err
	}
	dest := filepath.Join(dir, file.Filename)
	if err := downloadVerified(ctx, file.URL, dest, "sha512", file.SHA512, nil); err != nil {
		return DatapackEntry{}, err
	}
	manifest := srv.readDatapackManifest()
	for oldFile, meta := range manifest {
		if meta.ProjectID == projectID && oldFile != file.Filename {
			os.Remove(filepath.Join(dir, oldFile))
			os.Remove(filepath.Join(dir, oldFile+".disabled"))
			delete(manifest, oldFile)
		}
	}
	manifest[file.Filename] = datapackMeta{
		ProjectID: projectID, VersionID: ver.ID, Title: project.Title, Version: ver.VersionNumber,
	}
	if err := fsutil.WriteJSONAtomic(srv.datapackManifestPath(), manifest); err != nil {
		return DatapackEntry{}, err
	}
	entry := DatapackEntry{
		File: file.Filename, Managed: true,
		Title: project.Title, Version: ver.VersionNumber, ProjectID: projectID,
	}
	if fi, err := os.Stat(dest); err == nil {
		entry.Size = fi.Size()
	}
	return entry, nil
}

func resolveDatapackFile(ctx context.Context, projectID, mcVersion string) (modrinthVersion, modrinthFile, error) {
	// Prefer game-version match; datapacks are not loader-scoped the same way.
	loaders, _ := json.Marshal([]string{"datapack"})
	fetch := func(withGV bool) ([]modrinthVersion, error) {
		u := fmt.Sprintf("%s/project/%s/version?loaders=%s",
			modrinthBase, url.PathEscape(projectID), url.QueryEscape(string(loaders)))
		if withGV {
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
		return modrinthVersion{}, modrinthFile{}, err
	}
	if len(versions) == 0 {
		versions, err = fetch(false)
		if err != nil {
			return modrinthVersion{}, modrinthFile{}, err
		}
	}
	if len(versions) == 0 {
		return modrinthVersion{}, modrinthFile{}, errors.New("no compatible datapack version found")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].DatePublished > versions[j].DatePublished })
	v := versions[0]
	for _, f := range v.Files {
		name := path.Base(strings.ReplaceAll(f.Filename, "\\", "/"))
		if strings.HasSuffix(strings.ToLower(name), ".zip") && !strings.Contains(name, "..") {
			return v, modrinthFile{URL: f.URL, Filename: name, SHA512: f.Hashes.SHA512}, nil
		}
	}
	return modrinthVersion{}, modrinthFile{}, errors.New("datapack version has no zip file")
}

func (m *Manager) DeleteDatapack(id, file string) error {
	srv, err := m.datapackServer(id)
	if err != nil {
		return err
	}
	name := path.Base(strings.ReplaceAll(file, "\\", "/"))
	if name == "" || name == "." {
		return errors.New("invalid datapack file name")
	}
	dir := srv.datapacksDir()
	_ = os.Remove(filepath.Join(dir, name))
	_ = os.Remove(filepath.Join(dir, name+".disabled"))
	manifest := srv.readDatapackManifest()
	if _, ok := manifest[name]; ok {
		delete(manifest, name)
		fsutil.WriteJSONAtomic(srv.datapackManifestPath(), manifest)
	}
	return nil
}

func (m *Manager) SetDatapackEnabled(id, file string, enabled bool) error {
	srv, err := m.datapackServer(id)
	if err != nil {
		return err
	}
	name := path.Base(strings.ReplaceAll(file, "\\", "/"))
	if !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return errors.New("invalid datapack file name")
	}
	dir := srv.datapacksDir()
	active := filepath.Join(dir, name)
	disabled := active + ".disabled"
	if enabled {
		if _, err := os.Stat(active); err == nil {
			return nil
		}
		return os.Rename(disabled, active)
	}
	if _, err := os.Stat(disabled); err == nil {
		return nil
	}
	return os.Rename(active, disabled)
}

func (m *Manager) DatapacksDirFor(id string) (string, error) {
	srv, err := m.datapackServer(id)
	if err != nil {
		return "", err
	}
	dir := srv.datapacksDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}
