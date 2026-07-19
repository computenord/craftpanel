package mc

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const (
	curseforgeAPIBase = "https://api.curseforge.com/v1"
	cfMinecraftGameID = 432
	cfModpackClassID  = 4471
	SourceModrinth    = "modrinth"
	SourceCurseForge  = "curseforge"
)

type cfManifest struct {
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID        string `json:"id"`
			Primary   bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	Name   string `json:"name"`
	Files  []struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	} `json:"files"`
	Overrides string `json:"overrides"`
}

func (m *Manager) curseForgeKey() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.TrimSpace(m.settings.CurseForgeKey)
}

// SetCurseForgeKey stores or clears the CurseForge API key (write-only to the UI).
func (m *Manager) SetCurseForgeKey(key *string) error {
	if key == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings.CurseForgeKey = strings.TrimSpace(*key)
	return fsutil.WriteJSONAtomic(m.settingsPath, m.settings)
}

// searchCurseForgeModpacks lists Minecraft modpacks from CurseForge.
func searchCurseForgeModpacks(ctx context.Context, apiKey, query string) ([]ModpackSearchHit, error) {
	if apiKey == "" {
		return nil, errors.New("curseforge API key is not configured in panel settings")
	}
	q := url.Values{}
	q.Set("gameId", strconv.Itoa(cfMinecraftGameID))
	q.Set("classId", strconv.Itoa(cfModpackClassID))
	q.Set("pageSize", "20")
	q.Set("sortField", "2") // Popularity
	q.Set("sortOrder", "desc")
	if query != "" {
		q.Set("searchFilter", query)
	}
	var resp struct {
		Data []struct {
			ID           int      `json:"id"`
			Name         string   `json:"name"`
			Slug         string   `json:"slug"`
			Summary      string   `json:"summary"`
			DownloadCount float64 `json:"downloadCount"`
			LatestFilesIndexes []struct {
				GameVersion string `json:"gameVersion"`
				ModLoader   int    `json:"modLoader"`
			} `json:"latestFilesIndexes"`
		} `json:"data"`
	}
	if err := cfGetJSON(ctx, apiKey, "/mods/search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	out := make([]ModpackSearchHit, 0, len(resp.Data))
	for _, h := range resp.Data {
		desc := h.Summary
		if len(desc) > 160 {
			desc = desc[:160] + "…"
		}
		loaders := map[string]bool{}
		versions := []string{}
		seenVer := map[string]bool{}
		for _, idx := range h.LatestFilesIndexes {
			if l := cfLoaderName(idx.ModLoader); l != "" {
				loaders[l] = true
			}
			if idx.GameVersion != "" && !seenVer[idx.GameVersion] {
				seenVer[idx.GameVersion] = true
				versions = append(versions, idx.GameVersion)
			}
		}
		loaderList := []string{}
		for l := range loaders {
			loaderList = append(loaderList, l)
		}
		out = append(out, ModpackSearchHit{
			ProjectID:   strconv.Itoa(h.ID),
			Slug:        h.Slug,
			Title:       h.Name,
			Description: desc,
			Downloads:   int(h.DownloadCount),
			Loaders:     loaderList,
			Versions:    versions,
			Source:      SourceCurseForge,
		})
	}
	return out, nil
}

func cfLoaderName(id int) string {
	// CurseForge modLoaderType: 1=Forge 4=Fabric 5=Quilt 6=NeoForge
	switch id {
	case 1:
		return TypeForge
	case 4:
		return TypeFabric
	case 5:
		return TypeQuilt
	case 6:
		return TypeNeoForge
	default:
		return ""
	}
}

func listCurseForgeModpackVersions(ctx context.Context, apiKey, project string) ([]ModpackVersionInfo, error) {
	if apiKey == "" {
		return nil, errors.New("curseforge API key is not configured in panel settings")
	}
	modID, err := strconv.Atoi(strings.TrimSpace(project))
	if err != nil || modID <= 0 {
		return nil, errors.New("invalid curseforge project id")
	}
	var resp struct {
		Data []struct {
			ID              int      `json:"id"`
			DisplayName     string   `json:"displayName"`
			FileName        string   `json:"fileName"`
			FileDate        string   `json:"fileDate"`
			GameVersions    []string `json:"gameVersions"`
			ReleaseType     int      `json:"releaseType"`
		} `json:"data"`
	}
	u := fmt.Sprintf("/mods/%d/files?pageSize=50", modID)
	if err := cfGetJSON(ctx, apiKey, u, &resp); err != nil {
		return nil, err
	}
	out := make([]ModpackVersionInfo, 0, len(resp.Data))
	for _, f := range resp.Data {
		loaders := []string{}
		gameVers := []string{}
		for _, gv := range f.GameVersions {
			low := strings.ToLower(gv)
			switch {
			case low == "forge" || strings.HasPrefix(low, "forge-"):
				loaders = appendUnique(loaders, TypeForge)
			case low == "fabric" || strings.HasPrefix(low, "fabric-"):
				loaders = appendUnique(loaders, TypeFabric)
			case low == "quilt" || strings.HasPrefix(low, "quilt-"):
				loaders = appendUnique(loaders, TypeQuilt)
			case low == "neoforge" || strings.HasPrefix(low, "neoforge-"):
				loaders = appendUnique(loaders, TypeNeoForge)
			case looksLikeMCVersion(gv):
				gameVers = appendUnique(gameVers, gv)
			}
		}
		name := f.DisplayName
		if name == "" {
			name = f.FileName
		}
		out = append(out, ModpackVersionInfo{
			ID:            strconv.Itoa(f.ID),
			VersionNumber: f.FileName,
			Name:          name,
			GameVersions:  gameVers,
			Loaders:       loaders,
			DatePublished: f.FileDate,
			Source:        SourceCurseForge,
		})
	}
	return out, nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func looksLikeMCVersion(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "forge") || strings.Contains(low, "fabric") ||
		strings.Contains(low, "quilt") || strings.Contains(low, "java") {
		return false
	}
	if strings.Contains(low, "snapshot") {
		return true
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

func resolveCurseForgeModpack(ctx context.Context, apiKey, project, fileID string) (resolvedModpack, error) {
	if apiKey == "" {
		return resolvedModpack{}, errors.New("curseforge API key is not configured in panel settings")
	}
	modID, err := strconv.Atoi(strings.TrimSpace(project))
	if err != nil || modID <= 0 {
		return resolvedModpack{}, errors.New("invalid curseforge project id")
	}
	var mod struct {
		Data struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := cfGetJSON(ctx, apiKey, fmt.Sprintf("/mods/%d", modID), &mod); err != nil {
		return resolvedModpack{}, err
	}
	if fileID == "" {
		vers, err := listCurseForgeModpackVersions(ctx, apiKey, project)
		if err != nil {
			return resolvedModpack{}, err
		}
		if len(vers) == 0 {
			return resolvedModpack{}, errors.New("modpack has no files")
		}
		fileID = vers[0].ID
	}
	fid, err := strconv.Atoi(fileID)
	if err != nil || fid <= 0 {
		return resolvedModpack{}, errors.New("invalid curseforge file id")
	}
	var file struct {
		Data struct {
			ID           int      `json:"id"`
			DisplayName  string   `json:"displayName"`
			FileName     string   `json:"fileName"`
			DownloadURL  string   `json:"downloadUrl"`
			GameVersions []string `json:"gameVersions"`
		} `json:"data"`
	}
	if err := cfGetJSON(ctx, apiKey, fmt.Sprintf("/mods/%d/files/%d", modID, fid), &file); err != nil {
		return resolvedModpack{}, err
	}
	dl := file.Data.DownloadURL
	if dl == "" {
		var urlResp struct {
			Data string `json:"data"`
		}
		if err := cfGetJSON(ctx, apiKey, fmt.Sprintf("/mods/%d/files/%d/download-url", modID, fid), &urlResp); err != nil {
			return resolvedModpack{}, err
		}
		dl = urlResp.Data
	}
	if dl == "" {
		return resolvedModpack{}, errors.New("curseforge file has no download URL")
	}

	loaderType := ""
	mcVersion := ""
	for _, gv := range file.Data.GameVersions {
		low := strings.ToLower(gv)
		switch {
		case low == "forge" || strings.HasPrefix(low, "forge-"):
			if loaderType == "" {
				loaderType = TypeForge
			}
		case low == "fabric" || strings.HasPrefix(low, "fabric-"):
			if loaderType == "" {
				loaderType = TypeFabric
			}
		case low == "quilt" || strings.HasPrefix(low, "quilt-"):
			if loaderType == "" {
				loaderType = TypeQuilt
			}
		case low == "neoforge" || strings.HasPrefix(low, "neoforge-"):
			if loaderType == "" {
				loaderType = TypeNeoForge
			}
		case looksLikeMCVersion(gv) && mcVersion == "":
			mcVersion = gv
		}
	}
	if loaderType == "" {
		loaderType = TypeForge // common default for older CF packs; refined from manifest later
	}

	return resolvedModpack{
		Ref: ModpackRef{
			ProjectID: strconv.Itoa(mod.Data.ID),
			VersionID: strconv.Itoa(file.Data.ID),
			Slug:      mod.Data.Slug,
			Title:     mod.Data.Name,
			Version:   file.Data.DisplayName,
			Source:    SourceCurseForge,
		},
		LoaderType:  loaderType,
		Minecraft:   mcVersion,
		MrpackURL:   dl,
		MrpackName:  file.Data.FileName,
		Source:      SourceCurseForge,
		CurseForge:  true,
	}, nil
}

// installCurseForgeModpack downloads a CF zip, installs the loader from the
// manifest, fetches each required mod file, and applies overrides.
func (m *Manager) installCurseForgeModpack(ctx context.Context, srv *Server, pack resolvedModpack, apiKey, javaPath string, progress func(done, total int64)) (mcVersion, loaderType, loaderVersion string, err error) {
	dataDir := srv.DataDir()
	zipPath := filepath.Join(dataDir, ".modpack-cf.zip")
	defer os.Remove(zipPath)
	stage := stagedProgress{report: progress}

	if err := downloadVerified(ctx, pack.MrpackURL, zipPath, "", "", stage.span(0, 0.12)); err != nil {
		return "", "", "", fmt.Errorf("download curseforge pack: %w", err)
	}
	manifest, err := readCFManifest(zipPath)
	if err != nil {
		return "", "", "", err
	}
	mcVersion = manifest.Minecraft.Version
	if mcVersion == "" {
		mcVersion = pack.Minecraft
	}
	loaderType, loaderVersion = parseCFModLoader(manifest)
	if loaderType == "" {
		loaderType = pack.LoaderType
	}
	if loaderType == "" || mcVersion == "" {
		return "", "", "", errors.New("curseforge manifest is missing minecraft/loader info")
	}

	if len(manifest.Files) == 0 && !cfZipHasOverrides(zipPath, manifest.Overrides) {
		return "", "", "", errors.New("this curseforge pack has no mods or overrides (likely client-only)")
	}

	if err := resetModpackMods(srv); err != nil {
		return "", "", "", err
	}
	res, err := m.versions.InstallLoader(ctx, loaderType, mcVersion, loaderVersion, javaPath, dataDir, stage.span(0.12, 0.30))
	if err != nil {
		return "", "", "", err
	}
	if res.LoaderVersion != "" {
		loaderVersion = res.LoaderVersion
	}

	files := manifest.Files
	total := int64(len(files))
	var done int64
	for _, f := range files {
		if f.ProjectID == 0 || f.FileID == 0 {
			continue
		}
		dlURL, fileName, err := cfFileDownload(ctx, apiKey, f.ProjectID, f.FileID)
		if err != nil {
			if !f.Required {
				done++
				if progress != nil && total > 0 {
					stage.set(0.30 + 0.60*float64(done)/float64(total))
				}
				continue
			}
			return "", "", "", fmt.Errorf("curseforge file %d/%d: %w", f.ProjectID, f.FileID, err)
		}
		name := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
		if name == "" || name == "." || !strings.HasSuffix(strings.ToLower(name), ".jar") {
			name = fmt.Sprintf("%d-%d.jar", f.ProjectID, f.FileID)
		}
		dest := filepath.Join(dataDir, "mods", name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return "", "", "", err
		}
		if err := downloadVerified(ctx, dlURL, dest, "", "", nil); err != nil {
			if !f.Required {
				done++
				continue
			}
			return "", "", "", fmt.Errorf("download %s: %w", name, err)
		}
		done++
		if progress != nil && total > 0 {
			stage.set(0.30 + 0.60*float64(done)/float64(total))
		}
	}

	overrideDir := manifest.Overrides
	if overrideDir == "" {
		overrideDir = "overrides"
	}
	if err := extractCFOverrides(zipPath, dataDir, overrideDir); err != nil {
		return "", "", "", err
	}
	stage.set(1)
	return mcVersion, loaderType, loaderVersion, nil
}

func parseCFModLoader(manifest cfManifest) (loaderType, loaderVersion string) {
	pick := ""
	for _, l := range manifest.Minecraft.ModLoaders {
		if l.Primary || pick == "" {
			pick = l.ID
		}
	}
	// ids look like "forge-47.2.0", "fabric-0.15.0", "neoforge-21.1.77"
	low := strings.ToLower(pick)
	idx := strings.Index(low, "-")
	if idx <= 0 {
		return "", ""
	}
	kind, ver := low[:idx], pick[idx+1:]
	switch kind {
	case "neoforge":
		return TypeNeoForge, ver
	case "forge":
		return TypeForge, ver
	case "fabric":
		return TypeFabric, ver
	case "quilt":
		return TypeQuilt, ver
	default:
		return kind, ver
	}
}

func readCFManifest(zipPath string) (cfManifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return cfManifest{}, err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if path.Base(name) != "manifest.json" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return cfManifest{}, err
		}
		defer rc.Close()
		var man cfManifest
		if err := json.NewDecoder(io.LimitReader(rc, 8<<20)).Decode(&man); err != nil {
			return cfManifest{}, fmt.Errorf("manifest.json: %w", err)
		}
		return man, nil
	}
	return cfManifest{}, errors.New("curseforge pack is missing manifest.json")
}

func cfZipHasOverrides(zipPath, overrideDir string) bool {
	if overrideDir == "" {
		overrideDir = "overrides"
	}
	prefix := strings.TrimSuffix(overrideDir, "/") + "/"
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		if strings.HasPrefix(name, prefix) && name != strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}

func extractCFOverrides(zipPath, dataDir, overrideDir string) error {
	if overrideDir == "" {
		overrideDir = "overrides"
	}
	prefix := strings.TrimSuffix(strings.ReplaceAll(overrideDir, "\\", "/"), "/") + "/"
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(zf.Name, "\\", "/"))
		name = strings.TrimPrefix(name, "/")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		safe, ok := safeRelPath(rel)
		if !ok || rel == "" || rel == "." {
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

func cfFileDownload(ctx context.Context, apiKey string, projectID, fileID int) (dlURL, fileName string, err error) {
	var file struct {
		Data struct {
			FileName    string `json:"fileName"`
			DownloadURL string `json:"downloadUrl"`
		} `json:"data"`
	}
	if err := cfGetJSON(ctx, apiKey, fmt.Sprintf("/mods/%d/files/%d", projectID, fileID), &file); err != nil {
		return "", "", err
	}
	dlURL = file.Data.DownloadURL
	if dlURL == "" {
		var urlResp struct {
			Data string `json:"data"`
		}
		if err := cfGetJSON(ctx, apiKey, fmt.Sprintf("/mods/%d/files/%d/download-url", projectID, fileID), &urlResp); err != nil {
			return "", "", err
		}
		dlURL = urlResp.Data
	}
	if dlURL == "" {
		return "", "", errors.New("no download url")
	}
	return dlURL, file.Data.FileName, nil
}

func cfGetJSON(ctx context.Context, apiKey, pathQuery string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, curseforgeAPIBase+pathQuery, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", apiKey)
	resp, err := metaClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("curseforge %s: %s", pathQuery, msg)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(v)
}
