package mc

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server jar metadata is only ever fetched from these official sources.
const (
	mojangManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	paperAPIBase      = "https://fill.papermc.io/v3/projects/paper"
	userAgent         = "ComputeBox-Craftpanel (+https://computebox.de)"

	TypeVanilla = "vanilla"
	TypePaper   = "paper"
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
	mu    sync.Mutex
	cache map[string]cachedList
}

type cachedList struct {
	list    []VersionInfo
	fetched time.Time
}

func NewVersions() *Versions {
	return &Versions{cache: map[string]cachedList{}}
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
		list, err = listPaper(ctx)
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
// verifying the upstream checksum. progress may be nil.
func (v *Versions) DownloadServerJar(ctx context.Context, typ, version, destPath string, progress func(done, total int64)) error {
	var url, algo, sum string
	var err error
	switch typ {
	case TypeVanilla:
		url, sum, err = resolveVanilla(ctx, version)
		algo = "sha1"
	case TypePaper:
		url, sum, err = resolvePaper(ctx, version)
		algo = "sha256"
	default:
		return fmt.Errorf("unknown server type %q", typ)
	}
	if err != nil {
		return err
	}
	return downloadVerified(ctx, url, destPath, algo, sum, progress)
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

// listPaper uses the PaperMC Fill v3 API. Versions come grouped by minor
// release and include pre-releases, which we filter out.
func listPaper(ctx context.Context) ([]VersionInfo, error) {
	var proj struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := getJSON(ctx, paperAPIBase, &proj); err != nil {
		return nil, fmt.Errorf("paper project info: %w", err)
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

func resolvePaper(ctx context.Context, version string) (url, sha256sum string, err error) {
	var builds []paperBuild
	if err := getJSON(ctx, fmt.Sprintf("%s/versions/%s/builds", paperAPIBase, version), &builds); err != nil {
		return "", "", fmt.Errorf("paper builds for %s: %w", version, err)
	}
	if len(builds) == 0 {
		return "", "", fmt.Errorf("no paper builds available for %s", version)
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
		return "", "", fmt.Errorf("paper build %d for %s has no server download", pick.ID, version)
	}
	return dl.URL, dl.Checksums.SHA256, nil
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
	req.Header.Set("User-Agent", userAgent)
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
			h.Write(buf[:n])
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
	if wantSum != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), wantSum) {
		return fmt.Errorf("checksum mismatch for %s", url)
	}
	return os.Rename(tmpPath, destPath)
}
