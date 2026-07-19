package mc

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// ModpackRef is persisted on an Instance created from a modpack.
type ModpackRef struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
	Slug      string `json:"slug,omitempty"`
	Title     string `json:"title,omitempty"`
	Version   string `json:"version,omitempty"`
	Source    string `json:"source,omitempty"` // modrinth | curseforge
}

type ModpackSearchHit struct {
	ProjectID   string   `json:"projectId"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Downloads   int      `json:"downloads"`
	Loaders     []string `json:"loaders"`
	Versions    []string `json:"gameVersions"`
	Source      string   `json:"source"`
}

type ModpackVersionInfo struct {
	ID            string   `json:"id"`
	VersionNumber string   `json:"versionNumber"`
	Name          string   `json:"name"`
	GameVersions  []string `json:"gameVersions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"datePublished"`
	Source        string   `json:"source,omitempty"`
}

// ModpackAnalysis summarizes whether a pack is useful on a dedicated server.
type ModpackAnalysis struct {
	Source           string `json:"source"`
	ServerFiles      int    `json:"serverFiles"`
	ClientOnlyFiles  int    `json:"clientOnlyFiles"`
	HasOverrides     bool   `json:"hasOverrides"`
	Suitability      string `json:"suitability"` // good | mixed | client
	SuggestedMemoryMB int   `json:"suggestedMemoryMB"`
	SuggestedJavaMajor int  `json:"suggestedJavaMajor,omitempty"`
	Minecraft        string `json:"minecraft,omitempty"`
	LoaderType       string `json:"loaderType,omitempty"`
	LoaderVersion    string `json:"loaderVersion,omitempty"`
	Message          string `json:"message"`
}

type resolvedModpack struct {
	Ref          ModpackRef
	LoaderType   string // fabric | forge | neoforge | quilt
	Minecraft    string
	MrpackURL    string
	MrpackSHA512 string
	MrpackName   string
	Source       string
	CurseForge   bool
}

type mrpackIndex struct {
	FormatVersion int    `json:"formatVersion"`
	Game          string `json:"game"`
	VersionID     string `json:"versionId"`
	Name          string `json:"name"`
	Files         []struct {
		Path    string            `json:"path"`
		Hashes  map[string]string `json:"hashes"`
		Env     *struct {
			Client string `json:"client"`
			Server string `json:"server"`
		} `json:"env"`
		Downloads []string `json:"downloads"`
	} `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
}

// SearchModpacks queries Modrinth and/or CurseForge for modpacks.
// source: ""|"all"|"modrinth"|"curseforge"
func (m *Manager) SearchModpacks(ctx context.Context, query, index, source string) ([]ModpackSearchHit, error) {
	query = strings.TrimSpace(query)
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		source = "all"
	}
	var out []ModpackSearchHit
	if source == "all" || source == SourceModrinth {
		hits, err := searchModrinthModpacks(ctx, query, index)
		if err != nil {
			return nil, err
		}
		out = append(out, hits...)
	}
	if source == "all" || source == SourceCurseForge {
		if key := m.curseForgeKey(); key != "" {
			hits, err := searchCurseForgeModpacks(ctx, key, query)
			if err != nil && source == SourceCurseForge {
				return nil, err
			}
			if err == nil {
				out = append(out, hits...)
			}
		} else if source == SourceCurseForge {
			return nil, errors.New("curseforge API key is not configured in panel settings")
		}
	}
	return out, nil
}

func searchModrinthModpacks(ctx context.Context, query, index string) ([]ModpackSearchHit, error) {
	if !searchIndexes[index] {
		index = "relevance"
	}
	if query == "" && index == "relevance" {
		index = "downloads"
	}
	// Restrict to packs that declare a supported loader category so pure
	// resource / shader packs stay out of the default results.
	facets, _ := json.Marshal([][]string{
		{"project_type:modpack"},
		{"categories:fabric", "categories:forge", "categories:neoforge", "categories:quilt"},
	})
	searchURL := fmt.Sprintf("%s/search?limit=20&query=%s&index=%s&facets=%s",
		modrinthBase, url.QueryEscape(query), url.QueryEscape(index), url.QueryEscape(string(facets)))

	var resp struct {
		Hits []struct {
			ProjectID   string   `json:"project_id"`
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Downloads   int      `json:"downloads"`
			Categories  []string `json:"categories"`
			Versions    []string `json:"versions"`
			DisplayCats []string `json:"display_categories"`
		} `json:"hits"`
	}
	if err := getJSON(ctx, searchURL, &resp); err != nil {
		return nil, fmt.Errorf("modrinth search: %w", err)
	}
	out := []ModpackSearchHit{}
	for _, h := range resp.Hits {
		desc := h.Description
		if len(desc) > 160 {
			desc = desc[:160] + "…"
		}
		loaders := filterLoaderTags(h.Categories)
		if len(loaders) == 0 {
			loaders = filterLoaderTags(h.DisplayCats)
		}
		out = append(out, ModpackSearchHit{
			ProjectID:   h.ProjectID,
			Slug:        h.Slug,
			Title:       h.Title,
			Description: desc,
			Downloads:   h.Downloads,
			Loaders:     loaders,
			Versions:    h.Versions,
			Source:      SourceModrinth,
		})
	}
	return out, nil
}

func filterLoaderTags(cats []string) []string {
	want := map[string]bool{"fabric": true, "forge": true, "neoforge": true, "quilt": true}
	var out []string
	seen := map[string]bool{}
	for _, c := range cats {
		c = strings.ToLower(c)
		if want[c] && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// ListModpackVersions lists published versions of a modpack project.
func (m *Manager) ListModpackVersions(ctx context.Context, project, source string) ([]ModpackVersionInfo, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == SourceCurseForge {
		return listCurseForgeModpackVersions(ctx, m.curseForgeKey(), project)
	}
	return listModrinthModpackVersions(ctx, project)
}

func listModrinthModpackVersions(ctx context.Context, project string) ([]ModpackVersionInfo, error) {
	project = strings.TrimSpace(project)
	if project == "" || strings.ContainsAny(project, "/\\ ") {
		return nil, errors.New("invalid project id")
	}
	var rich []struct {
		ID            string   `json:"id"`
		VersionNumber string   `json:"version_number"`
		Name          string   `json:"name"`
		GameVersions  []string `json:"game_versions"`
		Loaders       []string `json:"loaders"`
		DatePublished string   `json:"date_published"`
	}
	u := modrinthBase + "/project/" + url.PathEscape(project) + "/version"
	if err := getJSON(ctx, u, &rich); err != nil {
		return nil, fmt.Errorf("modrinth versions: %w", err)
	}
	out := make([]ModpackVersionInfo, 0, len(rich))
	for _, v := range rich {
		name := v.Name
		if name == "" {
			name = v.VersionNumber
		}
		out = append(out, ModpackVersionInfo{
			ID:            v.ID,
			VersionNumber: v.VersionNumber,
			Name:          name,
			GameVersions:  v.GameVersions,
			Loaders:       v.Loaders,
			DatePublished: v.DatePublished,
			Source:        SourceModrinth,
		})
	}
	return out, nil
}

// AnalyzeModpack downloads pack metadata and reports server suitability + resource hints.
func (m *Manager) AnalyzeModpack(ctx context.Context, source, project, versionID string) (ModpackAnalysis, error) {
	pack, err := m.resolveModpack(ctx, source, project, versionID)
	if err != nil {
		return ModpackAnalysis{}, err
	}
	a := ModpackAnalysis{
		Source:            pack.Source,
		Minecraft:         pack.Minecraft,
		LoaderType:        pack.LoaderType,
		SuggestedMemoryMB: 6144,
	}
	if major, err := m.versions.JavaMajor(ctx, pack.Minecraft); err == nil && major > 0 {
		a.SuggestedJavaMajor = major
	}
	if pack.CurseForge {
		// Lightweight: CF packs that resolve are usually server-installable once
		// the manifest has mods; full zip analysis happens at install time.
		a.Suitability = "good"
		a.Message = "CurseForge pack looks installable. Large packs need more RAM."
		if a.SuggestedJavaMajor == 0 {
			a.SuggestedJavaMajor = 21
		}
		return a, nil
	}

	tmp, err := os.CreateTemp("", "craftpanel-mrpack-*")
	if err != nil {
		return ModpackAnalysis{}, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if err := downloadVerified(ctx, pack.MrpackURL, tmpPath, "sha512", pack.MrpackSHA512, nil); err != nil {
		return ModpackAnalysis{}, err
	}
	index, err := readMrpackIndex(tmpPath)
	if err != nil {
		return ModpackAnalysis{}, err
	}
	serverFiles := filterServerMrpackFiles(index)
	clientOnly := 0
	for _, f := range index.Files {
		if f.Env != nil && strings.EqualFold(f.Env.Server, "unsupported") {
			clientOnly++
		}
	}
	a.ServerFiles = len(serverFiles)
	a.ClientOnlyFiles = clientOnly
	a.HasOverrides = mrpackHasOverrides(tmpPath)
	a.LoaderVersion = loaderDepVersion(index.Dependencies, pack.LoaderType)
	if mc := index.Dependencies["minecraft"]; mc != "" {
		a.Minecraft = mc
	}
	switch {
	case a.ServerFiles == 0 && !a.HasOverrides:
		a.Suitability = "client"
		a.Message = "This pack has no server-side mods or overrides (client-only)."
		a.SuggestedMemoryMB = 2048
	case a.ClientOnlyFiles > a.ServerFiles*2 && a.ServerFiles < 5:
		a.Suitability = "mixed"
		a.Message = "Most files are client-only; only a small server footprint will be installed."
		a.SuggestedMemoryMB = 4096
	default:
		a.Suitability = "good"
		a.Message = "Pack looks suitable for a dedicated server."
		if a.ServerFiles > 80 {
			a.SuggestedMemoryMB = 8192
		} else if a.ServerFiles > 40 {
			a.SuggestedMemoryMB = 6144
		} else {
			a.SuggestedMemoryMB = 4096
		}
	}
	return a, nil
}

// resolveModpack picks a modpack version and its download.
func (m *Manager) resolveModpack(ctx context.Context, source, project, versionID string) (resolvedModpack, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == SourceCurseForge {
		return resolveCurseForgeModpack(ctx, m.curseForgeKey(), project, versionID)
	}
	return resolveModrinthModpack(ctx, project, versionID)
}

func resolveModrinthModpack(ctx context.Context, project, versionID string) (resolvedModpack, error) {
	project = strings.TrimSpace(project)
	versionID = strings.TrimSpace(versionID)
	if project == "" || strings.ContainsAny(project, "/\\ ") {
		return resolvedModpack{}, errors.New("invalid modpack project")
	}

	var proj struct {
		ID    string `json:"id"`
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	if err := getJSON(ctx, modrinthBase+"/project/"+url.PathEscape(project), &proj); err != nil {
		return resolvedModpack{}, fmt.Errorf("modrinth project: %w", err)
	}

	var ver struct {
		ID            string   `json:"id"`
		VersionNumber string   `json:"version_number"`
		GameVersions  []string `json:"game_versions"`
		Loaders       []string `json:"loaders"`
		Files         []struct {
			URL      string `json:"url"`
			Filename string `json:"filename"`
			Primary  bool   `json:"primary"`
			Hashes   struct {
				SHA512 string `json:"sha512"`
			} `json:"hashes"`
		} `json:"files"`
	}
	if versionID == "" {
		versions, err := listModrinthModpackVersions(ctx, proj.ID)
		if err != nil {
			return resolvedModpack{}, err
		}
		if len(versions) == 0 {
			return resolvedModpack{}, errors.New("modpack has no versions")
		}
		versionID = versions[0].ID
	}
	if err := getJSON(ctx, modrinthBase+"/version/"+url.PathEscape(versionID), &ver); err != nil {
		return resolvedModpack{}, fmt.Errorf("modrinth version: %w", err)
	}

	loaderType := ""
	for _, l := range ver.Loaders {
		switch strings.ToLower(l) {
		case TypeFabric, TypeForge, TypeNeoForge, TypeQuilt:
			loaderType = strings.ToLower(l)
		}
		if loaderType != "" {
			break
		}
	}
	if loaderType == "" {
		return resolvedModpack{}, errors.New("modpack does not use a supported loader (fabric, forge, neoforge, quilt)")
	}
	if len(ver.GameVersions) == 0 {
		return resolvedModpack{}, errors.New("modpack version has no Minecraft version")
	}

	var fileURL, fileName, sha512 string
	for _, f := range ver.Files {
		name := path.Base(strings.ReplaceAll(f.Filename, "\\", "/"))
		if !strings.HasSuffix(strings.ToLower(name), ".mrpack") {
			continue
		}
		if f.Primary || fileURL == "" {
			fileURL, fileName, sha512 = f.URL, name, f.Hashes.SHA512
			if f.Primary {
				break
			}
		}
	}
	if fileURL == "" {
		return resolvedModpack{}, errors.New("modpack version has no .mrpack file")
	}

	return resolvedModpack{
		Ref: ModpackRef{
			ProjectID: proj.ID,
			VersionID: ver.ID,
			Slug:      proj.Slug,
			Title:     proj.Title,
			Version:   ver.VersionNumber,
			Source:    SourceModrinth,
		},
		LoaderType:   loaderType,
		Minecraft:    ver.GameVersions[0],
		MrpackURL:    fileURL,
		MrpackSHA512: sha512,
		MrpackName:   fileName,
		Source:       SourceModrinth,
	}, nil
}

// installModpack installs a Modrinth .mrpack or CurseForge zip pack.
// World data is kept; mods/ and the panel mod manifest are replaced.
func (m *Manager) installModpack(ctx context.Context, srv *Server, pack resolvedModpack, javaPath string, progress func(done, total int64)) (mcVersion, loaderType, loaderVersion string, err error) {
	if pack.CurseForge || pack.Source == SourceCurseForge {
		return m.installCurseForgeModpack(ctx, srv, pack, m.curseForgeKey(), javaPath, progress)
	}
	loaderType = pack.LoaderType
	dataDir := srv.DataDir()
	mrpackPath := filepath.Join(dataDir, ".modpack.mrpack")
	defer os.Remove(mrpackPath)

	stage := stagedProgress{report: progress}
	if err := downloadVerified(ctx, pack.MrpackURL, mrpackPath, "sha512", pack.MrpackSHA512, stage.span(0, 0.15)); err != nil {
		return "", "", "", fmt.Errorf("download modpack: %w", err)
	}

	index, err := readMrpackIndex(mrpackPath)
	if err != nil {
		return "", "", "", err
	}
	mcVersion = index.Dependencies["minecraft"]
	if mcVersion == "" {
		mcVersion = pack.Minecraft
	}
	loaderVersion = loaderDepVersion(index.Dependencies, loaderType)
	if mcVersion == "" {
		return "", "", "", errors.New("modpack index is missing a minecraft dependency")
	}

	files := filterServerMrpackFiles(index)
	hasOverrides := mrpackHasOverrides(mrpackPath)
	if len(files) == 0 && !hasOverrides {
		if len(index.Files) > 0 {
			return "", "", "", errors.New("this modpack has no server-side content (client-only pack)")
		}
		return "", "", "", errors.New("modpack contains no installable files")
	}

	// Drop previous pack mods so upgrades do not leave stale jars behind.
	if err := resetModpackMods(srv); err != nil {
		return "", "", "", err
	}

	res, err := m.versions.InstallLoader(ctx, loaderType, mcVersion, loaderVersion, javaPath, dataDir, stage.span(0.15, 0.35))
	if err != nil {
		return "", "", "", err
	}
	if res.LoaderVersion != "" {
		loaderVersion = res.LoaderVersion
	}

	if err := downloadMrpackFiles(ctx, dataDir, files, stage.span(0.35, 0.92)); err != nil {
		return "", "", "", err
	}

	if err := extractMrpackOverrides(mrpackPath, dataDir); err != nil {
		return "", "", "", err
	}
	stage.set(1)
	return mcVersion, loaderType, loaderVersion, nil
}

// stagedProgress maps sub-step progress onto a single 0..1 overall bar.
type stagedProgress struct {
	report func(done, total int64)
}

func (s stagedProgress) set(frac float64) {
	if s.report == nil {
		return
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	s.report(int64(frac*1000), 1000)
}

func (s stagedProgress) span(start, end float64) func(done, total int64) {
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		frac := float64(done) / float64(total)
		if frac > 1 {
			frac = 1
		}
		s.set(start + (end-start)*frac)
	}
}

func resetModpackMods(srv *Server) error {
	mods := filepath.Join(srv.DataDir(), "mods")
	if err := os.RemoveAll(mods); err != nil {
		return err
	}
	if err := os.MkdirAll(mods, 0o750); err != nil {
		return err
	}
	_ = os.Remove(srv.modManifestPath())
	return fsutil.WriteJSONAtomic(srv.modManifestPath(), map[string]modMeta{})
}

const modpackDownloadWorkers = 4

func downloadMrpackFiles(ctx context.Context, dataDir string, files []mrpackFile, progress func(done, total int64)) error {
	if len(files) == 0 {
		if progress != nil {
			progress(1, 1)
		}
		return nil
	}
	type job struct {
		file mrpackFile
		rel  string
	}
	jobs := make([]job, 0, len(files))
	for _, f := range files {
		rel, ok := safeRelPath(f.Path)
		if !ok {
			return fmt.Errorf("unsafe modpack path %q", f.Path)
		}
		if len(f.Downloads) == 0 {
			return fmt.Errorf("modpack file %s has no download URL", rel)
		}
		jobs = append(jobs, job{file: f, rel: rel})
	}

	total := int64(len(jobs))
	var done atomic.Int64
	var firstErr error
	var errMu sync.Mutex
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan job)
	var wg sync.WaitGroup
	workers := modpackDownloadWorkers
	if workers > len(jobs) {
		workers = len(jobs)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				if ctx.Err() != nil {
					return
				}
				dest := filepath.Join(dataDir, filepath.FromSlash(j.rel))
				if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errMu.Unlock()
					return
				}
				algo, sum := "sha512", j.file.Hashes["sha512"]
				if sum == "" {
					algo, sum = "sha1", j.file.Hashes["sha1"]
				}
				var lastErr error
				for _, u := range j.file.Downloads {
					lastErr = downloadVerified(ctx, u, dest, algo, sum, nil)
					if lastErr == nil {
						break
					}
					if ctx.Err() != nil {
						return
					}
				}
				if lastErr != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("download %s: %w", j.rel, lastErr)
						cancel()
					}
					errMu.Unlock()
					return
				}
				n := done.Add(1)
				if progress != nil {
					progress(n, total)
				}
			}
		}()
	}
	go func() {
		defer close(ch)
		for _, j := range jobs {
			select {
			case <-ctx.Done():
				return
			case ch <- j:
			}
		}
	}()
	wg.Wait()
	return firstErr
}

func mrpackHasOverrides(mrpackPath string) bool {
	zr, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return false
	}
	defer zr.Close()
	prefixes := []string{"overrides/", "server-overrides/", "overrides-server/"}
	for _, zf := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(zf.Name, "\\", "/"))
		name = strings.TrimPrefix(name, "/")
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) && name != strings.TrimSuffix(p, "/") {
				return true
			}
		}
	}
	return false
}

func loaderDepVersion(deps map[string]string, loaderType string) string {
	keys := map[string]string{
		TypeFabric:   "fabric-loader",
		TypeForge:    "forge",
		TypeNeoForge: "neoforge",
		TypeQuilt:    "quilt-loader",
	}
	return deps[keys[loaderType]]
}

type mrpackFile struct {
	Path      string
	Hashes    map[string]string
	Downloads []string
}

func filterServerMrpackFiles(index mrpackIndex) []mrpackFile {
	out := make([]mrpackFile, 0, len(index.Files))
	for _, f := range index.Files {
		if f.Env != nil && strings.EqualFold(f.Env.Server, "unsupported") {
			continue
		}
		out = append(out, mrpackFile{Path: f.Path, Hashes: f.Hashes, Downloads: f.Downloads})
	}
	return out
}

func readMrpackIndex(mrpackPath string) (mrpackIndex, error) {
	zr, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return mrpackIndex{}, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != "modrinth.index.json" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return mrpackIndex{}, err
		}
		defer rc.Close()
		var idx mrpackIndex
		if err := json.NewDecoder(io.LimitReader(rc, 32<<20)).Decode(&idx); err != nil {
			return mrpackIndex{}, fmt.Errorf("modrinth.index.json: %w", err)
		}
		if idx.Game != "" && idx.Game != "minecraft" {
			return mrpackIndex{}, fmt.Errorf("unsupported modpack game %q", idx.Game)
		}
		return idx, nil
	}
	return mrpackIndex{}, errors.New("modpack is missing modrinth.index.json")
}

func extractMrpackOverrides(mrpackPath, dataDir string) error {
	zr, err := zip.OpenReader(mrpackPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	prefixes := []string{"overrides/", "server-overrides/", "overrides-server/"}
	for _, zf := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(zf.Name, "\\", "/"))
		name = strings.TrimPrefix(name, "/")
		var rel string
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				rel = strings.TrimPrefix(name, p)
				break
			}
		}
		if rel == "" || rel == "." {
			continue
		}
		safe, ok := safeRelPath(rel)
		if !ok {
			continue
		}
		target := filepath.Join(dataDir, filepath.FromSlash(safe))
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		in, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		out.Close()
		in.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func safeRelPath(p string) (string, bool) {
	p = path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
		return "", false
	}
	if filepath.IsAbs(p) {
		return "", false
	}
	return p, true
}
