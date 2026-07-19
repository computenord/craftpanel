package mc

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server binaries and metadata are only ever fetched from these official sources.
const (
	mojangManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	fillAPIBase       = "https://fill.papermc.io/v3/projects"
	bedrockLinksURL   = "https://net-secondary.web.minecraft-services.net/api/v1.0/download/links"
	userAgent         = "ComputeBox-Craftpanel (+https://computebox.de)"
	// minecraft.net's CDN drops connections from non-browser user agents on
	// the BDS zip downloads, so those requests masquerade as a browser.
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	TypeVanilla  = "vanilla"
	TypePaper    = "paper"
	TypeBedrock  = "bedrock"
	TypeVelocity = "velocity"
	// Modded loader constants live in loaders.go (fabric, forge, neoforge, quilt).
)

var metaClient = &http.Client{Timeout: 30 * time.Second}

type VersionInfo struct {
	ID          string `json:"id"`
	ReleaseTime string `json:"releaseTime,omitempty"`
	Latest      bool   `json:"latest,omitempty"`
}

// Versions lists available versions and resolves jar downloads. Results are
// cached for a few minutes to keep the UI snappy and the upstream APIs happy.
type Versions struct {
	mu        sync.Mutex
	cache     map[string]cachedList
	javaCache map[string]int
}

type cachedList struct {
	list    []VersionInfo
	fetched time.Time
}

func NewVersions() *Versions {
	return &Versions{cache: map[string]cachedList{}, javaCache: map[string]int{}}
}

// JavaMajor reports the Java major version Mojang declares for a Minecraft
// release, e.g. 21 for 1.21.x and 25 for 26.1 and newer. Paper builds track the
// same requirement, so the lookup works for both server types. It returns 0
// when the version is unknown upstream.
func (v *Versions) JavaMajor(ctx context.Context, version string) (int, error) {
	v.mu.Lock()
	if major, ok := v.javaCache[version]; ok {
		v.mu.Unlock()
		return major, nil
	}
	v.mu.Unlock()

	var m mojangManifest
	if err := getJSON(ctx, mojangManifestURL, &m); err != nil {
		return 0, fmt.Errorf("mojang version manifest: %w", err)
	}
	detailURL := ""
	for _, mv := range m.Versions {
		if mv.ID == version {
			detailURL = mv.URL
			break
		}
	}
	if detailURL == "" {
		return 0, nil // not a Mojang release id, treat the requirement as unknown
	}
	var detail struct {
		JavaVersion struct {
			MajorVersion int `json:"majorVersion"`
		} `json:"javaVersion"`
	}
	if err := getJSON(ctx, detailURL, &detail); err != nil {
		return 0, fmt.Errorf("mojang version detail: %w", err)
	}
	major := detail.JavaVersion.MajorVersion
	v.mu.Lock()
	v.javaCache[version] = major
	v.mu.Unlock()
	return major, nil
}

func (v *Versions) List(ctx context.Context, typ string) ([]VersionInfo, error) {
	v.mu.Lock()
	if c, ok := v.cache[typ]; ok && time.Since(c.fetched) < 10*time.Minute {
		v.mu.Unlock()
		return c.list, nil
	}
	v.mu.Unlock()

	var list []VersionInfo
	var err error
	switch typ {
	case TypeVanilla:
		list, err = listVanilla(ctx)
	case TypePaper:
		list, err = listFill(ctx, "paper")
	case TypeFolia:
		list, err = listFill(ctx, "folia")
	case TypeVelocity:
		list, err = listFill(ctx, "velocity")
	case TypeWaterfall:
		list, err = listFill(ctx, "waterfall")
	case TypePurpur:
		list, err = listPurpur(ctx)
	case TypeBedrock:
		list, err = listBedrock(ctx)
	case TypeFabric:
		list, err = listFabricLike(ctx, fabricMetaBase)
	case TypeQuilt:
		list, err = listFabricLike(ctx, quiltMetaBase)
	case TypeForge:
		list, err = listForge(ctx)
	case TypeNeoForge:
		list, err = listNeoForge(ctx)
	default:
		return nil, fmt.Errorf("unknown server type %q", typ)
	}
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	v.cache[typ] = cachedList{list: list, fetched: time.Now()}
	v.mu.Unlock()
	return list, nil
}

// DownloadServerJar fetches the server jar for typ/version to destPath,
// verifying the upstream checksum. It returns the upstream build identifier
// for types with a build channel (Paper family, Purpur), "" otherwise.
// progress may be nil.
func (v *Versions) DownloadServerJar(ctx context.Context, typ, version, destPath string, progress func(done, total int64)) (string, error) {
	var url, algo, sum, build string
	var err error
	switch typ {
	case TypeVanilla:
		url, sum, err = resolveVanilla(ctx, version)
		algo = "sha1"
	case TypePaper:
		url, sum, build, err = resolveFill(ctx, "paper", version)
		algo = "sha256"
	case TypeFolia:
		url, sum, build, err = resolveFill(ctx, "folia", version)
		algo = "sha256"
	case TypeVelocity:
		url, sum, build, err = resolveFill(ctx, "velocity", version)
		algo = "sha256"
	case TypeWaterfall:
		url, sum, build, err = resolveFill(ctx, "waterfall", version)
		algo = "sha256"
	case TypePurpur:
		url, sum, build, err = resolvePurpur(ctx, version)
		algo = "" // Purpur publishes md5 only; rely on TLS
		sum = ""
	default:
		return "", fmt.Errorf("unknown server type %q", typ)
	}
	if err != nil {
		return "", err
	}
	return build, downloadVerified(ctx, url, destPath, algo, sum, progress)
}

// HasBuilds reports whether typ publishes numbered builds within one
// Minecraft version, so servers can be updated in place.
func HasBuilds(typ string) bool {
	switch typ {
	case TypePaper, TypeFolia, TypeVelocity, TypeWaterfall, TypePurpur:
		return true
	}
	return false
}

// LatestBuild returns the newest available build identifier for typ/version.
func (v *Versions) LatestBuild(ctx context.Context, typ, version string) (string, error) {
	switch typ {
	case TypePaper:
		_, _, b, err := resolveFill(ctx, "paper", version)
		return b, err
	case TypeFolia:
		_, _, b, err := resolveFill(ctx, "folia", version)
		return b, err
	case TypeVelocity:
		_, _, b, err := resolveFill(ctx, "velocity", version)
		return b, err
	case TypeWaterfall:
		_, _, b, err := resolveFill(ctx, "waterfall", version)
		return b, err
	case TypePurpur:
		_, _, b, err := resolvePurpur(ctx, version)
		return b, err
	}
	return "", fmt.Errorf("no build channel for %q", typ)
}

type mojangManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		URL         string `json:"url"`
		ReleaseTime string `json:"releaseTime"`
	} `json:"versions"`
}

func listVanilla(ctx context.Context) ([]VersionInfo, error) {
	var m mojangManifest
	if err := getJSON(ctx, mojangManifestURL, &m); err != nil {
		return nil, fmt.Errorf("mojang version manifest: %w", err)
	}
	out := make([]VersionInfo, 0, len(m.Versions))
	for _, mv := range m.Versions {
		if mv.Type != "release" {
			continue
		}
		out = append(out, VersionInfo{
			ID:          mv.ID,
			ReleaseTime: mv.ReleaseTime,
			Latest:      mv.ID == m.Latest.Release,
		})
	}
	return out, nil
}

func resolveVanilla(ctx context.Context, version string) (url, sha1sum string, err error) {
	var m mojangManifest
	if err := getJSON(ctx, mojangManifestURL, &m); err != nil {
		return "", "", fmt.Errorf("mojang version manifest: %w", err)
	}
	detailURL := ""
	for _, mv := range m.Versions {
		if mv.ID == version {
			detailURL = mv.URL
			break
		}
	}
	if detailURL == "" {
		return "", "", fmt.Errorf("unknown vanilla version %q", version)
	}
	var detail struct {
		Downloads struct {
			Server struct {
				URL  string `json:"url"`
				SHA1 string `json:"sha1"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := getJSON(ctx, detailURL, &detail); err != nil {
		return "", "", fmt.Errorf("mojang version detail: %w", err)
	}
	if detail.Downloads.Server.URL == "" {
		return "", "", fmt.Errorf("version %s has no server download", version)
	}
	return detail.Downloads.Server.URL, detail.Downloads.Server.SHA1, nil
}

// listFill lists releases of a PaperMC project (paper, velocity) via the
// Fill v3 API. Versions come grouped by minor release and include
// pre-releases and snapshots, which we filter out.
func listFill(ctx context.Context, project string) ([]VersionInfo, error) {
	var proj struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := getJSON(ctx, fillAPIBase+"/"+project, &proj); err != nil {
		return nil, fmt.Errorf("%s project info: %w", project, err)
	}
	var out []VersionInfo
	for _, group := range proj.Versions {
		for _, v := range group {
			if strings.Contains(v, "-") { // skip -rc and -pre builds
				continue
			}
			out = append(out, VersionInfo{ID: v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return cmpVersion(out[i].ID, out[j].ID) > 0 })
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

type paperBuild struct {
	ID        int    `json:"id"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	} `json:"downloads"`
}

func resolveFill(ctx context.Context, project, version string) (url, sha256sum, build string, err error) {
	var builds []paperBuild
	if err := getJSON(ctx, fmt.Sprintf("%s/%s/versions/%s/builds", fillAPIBase, project, version), &builds); err != nil {
		return "", "", "", fmt.Errorf("%s builds for %s: %w", project, version, err)
	}
	if len(builds) == 0 {
		return "", "", "", fmt.Errorf("no %s builds available for %s", project, version)
	}
	// The API lists newest builds first. Prefer the newest stable build.
	pick := builds[0]
	for _, b := range builds {
		if strings.EqualFold(b.Channel, "STABLE") {
			pick = b
			break
		}
	}
	dl, ok := pick.Downloads["server:default"]
	if !ok || dl.URL == "" {
		return "", "", "", fmt.Errorf("%s build %d for %s has no server download", project, pick.ID, version)
	}
	return dl.URL, dl.Checksums.SHA256, strconv.Itoa(pick.ID), nil
}

/* ---------- bedrock ---------- */

var bedrockVersionRe = regexp.MustCompile(`bedrock-server-([0-9][0-9.]*)\.zip`)

// bedrockDownloadInfo returns the download URL and version of the current
// Bedrock Dedicated Server for this OS. Mojang only distributes the latest
// release, so this is always a single version.
func bedrockDownloadInfo(ctx context.Context) (url, version string, err error) {
	var resp struct {
		Result struct {
			Links []struct {
				DownloadType string `json:"downloadType"`
				DownloadURL  string `json:"downloadUrl"`
			} `json:"links"`
		} `json:"result"`
	}
	if err := getJSON(ctx, bedrockLinksURL, &resp); err != nil {
		return "", "", fmt.Errorf("bedrock download links: %w", err)
	}
	want := "serverBedrockLinux"
	if runtime.GOOS == "windows" {
		want = "serverBedrockWindows"
	}
	for _, l := range resp.Result.Links {
		if l.DownloadType == want {
			m := bedrockVersionRe.FindStringSubmatch(l.DownloadURL)
			if m == nil {
				return l.DownloadURL, "latest", nil
			}
			return l.DownloadURL, m[1], nil
		}
	}
	return "", "", errors.New("no bedrock server download available for this platform")
}

func listBedrock(ctx context.Context) ([]VersionInfo, error) {
	_, version, err := bedrockDownloadInfo(ctx)
	if err != nil {
		return nil, err
	}
	return []VersionInfo{{ID: version, Latest: true}}, nil
}

// BedrockBinaryName is the server executable inside a BDS distribution.
func BedrockBinaryName() string {
	if runtime.GOOS == "windows" {
		return "bedrock_server.exe"
	}
	return "bedrock_server"
}

// bedrockPreserve are files the server operator owns; upgrades must not
// overwrite them once they exist.
var bedrockPreserve = map[string]bool{
	"server.properties": true,
	"allowlist.json":    true,
	"permissions.json":  true,
}

// InstallBedrock downloads the current BDS zip and unpacks it into dataDir,
// preserving world data and operator-owned config on upgrades. Returns the
// installed version.
func (v *Versions) InstallBedrock(ctx context.Context, dataDir string, progress func(done, total int64)) (string, error) {
	url, version, err := bedrockDownloadInfo(ctx)
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(dataDir, ".bedrock-download.zip")
	defer os.Remove(zipPath)
	// Mojang publishes no checksums for BDS, so this is TLS-trust only.
	if err := downloadVerified(ctx, url, zipPath, "", "", progress); err != nil {
		return "", err
	}
	if err := extractBedrock(zipPath, dataDir); err != nil {
		return "", err
	}
	return version, nil
}

func extractBedrock(zipPath, dataDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		rel := path.Clean(zf.Name)
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) || strings.Contains(rel, "\\") {
			continue
		}
		target := filepath.Join(dataDir, filepath.FromSlash(rel))
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
			continue
		}
		if bedrockPreserve[rel] || strings.HasPrefix(rel, "worlds/") {
			if _, err := os.Stat(target); err == nil {
				continue
			}
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
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			in.Close()
			return err
		}
		out.Close()
		in.Close()
	}
	return os.Chmod(filepath.Join(dataDir, BedrockBinaryName()), 0o755)
}

// cmpVersion compares dotted numeric versions like "1.21.4" and "26.2".
func cmpVersion(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var ai, bi int
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}

func getJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := metaClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(v)
}

func downloadVerified(ctx context.Context, url, destPath, algo, wantSum string, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	ua := userAgent
	if strings.Contains(url, "minecraft.net/bedrockdedicatedserver") {
		ua = browserUA
	}
	req.Header.Set("User-Agent", ua)
	// No client timeout here: jar downloads can legitimately take minutes on
	// slow links. Cancellation happens through ctx.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	tmpPath := destPath + ".part"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	var h hash.Hash
	switch algo {
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	case "":
		// No checksum published upstream (Bedrock); TLS is the only integrity.
	default:
		f.Close()
		return fmt.Errorf("unknown checksum algorithm %q", algo)
	}

	total := resp.ContentLength
	var done int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			if h != nil {
				h.Write(buf[:n])
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if h != nil && wantSum != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSum) {
		return fmt.Errorf("checksum mismatch for %s", url)
	}
	return os.Rename(tmpPath, destPath)
}

const purpurAPIBase = "https://api.purpurmc.org/v2/purpur"

func listPurpur(ctx context.Context) ([]VersionInfo, error) {
	var resp struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON(ctx, purpurAPIBase, &resp); err != nil {
		return nil, fmt.Errorf("purpur versions: %w", err)
	}
	out := make([]VersionInfo, 0, len(resp.Versions))
	for i := len(resp.Versions) - 1; i >= 0; i-- {
		v := resp.Versions[i]
		if strings.Contains(v, "-") {
			continue
		}
		out = append(out, VersionInfo{ID: v})
	}
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

func resolvePurpur(ctx context.Context, version string) (url, checksum, build string, err error) {
	var resp struct {
		Builds struct {
			Latest string `json:"latest"`
		} `json:"builds"`
	}
	if err := getJSON(ctx, purpurAPIBase+"/"+version, &resp); err != nil {
		return "", "", "", fmt.Errorf("purpur %s: %w", version, err)
	}
	if resp.Builds.Latest == "" {
		return "", "", "", fmt.Errorf("no purpur builds for %s", version)
	}
	// Direct download URL; md5 available at .../builds/{build} but unused.
	u := fmt.Sprintf("%s/%s/%s/download", purpurAPIBase, version, resp.Builds.Latest)
	return u, "", resp.Builds.Latest, nil
}
