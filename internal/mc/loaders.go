package mc

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	TypeFabric    = "fabric"
	TypeForge     = "forge"
	TypeNeoForge  = "neoforge"
	TypeQuilt     = "quilt"
	TypeModpack   = "modpack" // create-time only; persisted as the resolved loader
	TypePurpur    = "purpur"
	TypeFolia     = "folia"
	TypeWaterfall = "waterfall"

	fabricMetaBase = "https://meta.fabricmc.net/v2"
	quiltMetaBase  = "https://meta.quiltmc.org/v3"
	forgeMavenBase = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	neoMavenBase   = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
	quiltMavenBase = "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer"
)

// IsModded reports whether typ is a mod-loader server (has a mods/ folder).
func IsModded(typ string) bool {
	switch typ {
	case TypeFabric, TypeForge, TypeNeoForge, TypeQuilt:
		return true
	default:
		return false
	}
}

func validServerType(typ string) bool {
	switch typ {
	case TypeVanilla, TypePaper, TypePurpur, TypeFolia, TypeBedrock, TypeVelocity, TypeWaterfall,
		TypeFabric, TypeForge, TypeNeoForge, TypeQuilt, TypeModpack:
		return true
	default:
		return false
	}
}

// IsPluginServer reports whether the type uses a plugins/ folder (Paper family / proxies).
func IsPluginServer(typ string) bool {
	switch typ {
	case TypePaper, TypePurpur, TypeFolia, TypeVelocity, TypeWaterfall:
		return true
	default:
		return false
	}
}

// LoaderVersionInfo is one Fabric/Forge/NeoForge/Quilt loader build for a MC release.
type LoaderVersionInfo struct {
	ID     string `json:"id"`
	Latest bool   `json:"latest,omitempty"`
}

// ListLoaderVersions lists available loader builds for typ + Minecraft version.
func (v *Versions) ListLoaderVersions(ctx context.Context, typ, mcVersion string) ([]LoaderVersionInfo, error) {
	mcVersion = strings.TrimSpace(mcVersion)
	if mcVersion == "" {
		return nil, errors.New("minecraft version is required")
	}
	switch typ {
	case TypeFabric:
		return listFabricLoaderVersions(ctx, fabricMetaBase, mcVersion)
	case TypeQuilt:
		return listQuiltLoaderVersions(ctx, mcVersion)
	case TypeForge:
		return listForgeLoaderVersions(ctx, mcVersion)
	case TypeNeoForge:
		return listNeoForgeLoaderVersions(ctx, mcVersion)
	default:
		return nil, fmt.Errorf("loader versions not available for %s", typ)
	}
}

func listFabricLoaderVersions(ctx context.Context, metaBase, mcVersion string) ([]LoaderVersionInfo, error) {
	var loaders []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	if err := getJSON(ctx, metaBase+"/versions/loader/"+url.PathEscape(mcVersion), &loaders); err != nil {
		return nil, err
	}
	out := []LoaderVersionInfo{}
	for _, l := range loaders {
		if !l.Loader.Stable {
			continue
		}
		out = append(out, LoaderVersionInfo{ID: l.Loader.Version})
	}
	if len(out) == 0 {
		for _, l := range loaders {
			out = append(out, LoaderVersionInfo{ID: l.Loader.Version})
			if len(out) >= 20 {
				break
			}
		}
	}
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

func listQuiltLoaderVersions(ctx context.Context, mcVersion string) ([]LoaderVersionInfo, error) {
	var loaders []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := getJSON(ctx, quiltMetaBase+"/versions/loader/"+url.PathEscape(mcVersion), &loaders); err != nil {
		return nil, err
	}
	out := make([]LoaderVersionInfo, 0, len(loaders))
	for i, l := range loaders {
		if i >= 30 {
			break
		}
		out = append(out, LoaderVersionInfo{ID: l.Loader.Version, Latest: i == 0})
	}
	return out, nil
}

func listForgeLoaderVersions(ctx context.Context, mcVersion string) ([]LoaderVersionInfo, error) {
	versions, err := fetchMavenVersions(ctx, forgeMavenBase+"/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	out := []LoaderVersionInfo{}
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.Contains(v, "beta") || strings.Contains(v, "pre") {
			continue
		}
		mc, forge, ok := splitForgeCoord(v)
		if !ok || mc != mcVersion {
			continue
		}
		out = append(out, LoaderVersionInfo{ID: forge})
		if len(out) >= 30 {
			break
		}
	}
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

func listNeoForgeLoaderVersions(ctx context.Context, mcVersion string) ([]LoaderVersionInfo, error) {
	versions, err := fetchMavenVersions(ctx, neoMavenBase+"/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	out := []LoaderVersionInfo{}
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.Contains(v, "beta") || strings.Contains(v, "alpha") {
			continue
		}
		if neoForgeToMinecraft(v) != mcVersion {
			continue
		}
		out = append(out, LoaderVersionInfo{ID: v})
		if len(out) >= 30 {
			break
		}
	}
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

/* ---------- listing ---------- */

func listFabricLike(ctx context.Context, metaBase string) ([]VersionInfo, error) {
	var games []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := getJSON(ctx, metaBase+"/versions/game", &games); err != nil {
		return nil, fmt.Errorf("game versions: %w", err)
	}
	out := make([]VersionInfo, 0, len(games))
	for _, g := range games {
		if !g.Stable {
			continue
		}
		out = append(out, VersionInfo{ID: g.Version})
	}
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

func listForge(ctx context.Context) ([]VersionInfo, error) {
	versions, err := fetchMavenVersions(ctx, forgeMavenBase+"/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	// Newest forge build per Minecraft version (metadata is oldest-first).
	latest := map[string]string{}
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.Contains(v, "beta") || strings.Contains(v, "pre") {
			continue
		}
		mc, _, ok := splitForgeCoord(v)
		if !ok {
			continue
		}
		if _, seen := latest[mc]; seen {
			continue
		}
		latest[mc] = v
	}
	out := make([]VersionInfo, 0, len(latest))
	for mc := range latest {
		out = append(out, VersionInfo{ID: mc})
	}
	sort.SliceStable(out, func(i, j int) bool { return cmpVersion(out[i].ID, out[j].ID) > 0 })
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

func listNeoForge(ctx context.Context) ([]VersionInfo, error) {
	versions, err := fetchMavenVersions(ctx, neoMavenBase+"/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	latest := map[string]string{}
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.Contains(v, "beta") || strings.Contains(v, "alpha") {
			continue
		}
		mc := neoForgeToMinecraft(v)
		if mc == "" {
			continue
		}
		if _, seen := latest[mc]; seen {
			continue
		}
		latest[mc] = v
	}
	out := make([]VersionInfo, 0, len(latest))
	for mc := range latest {
		out = append(out, VersionInfo{ID: mc})
	}
	sort.SliceStable(out, func(i, j int) bool { return cmpVersion(out[i].ID, out[j].ID) > 0 })
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out, nil
}

type mavenMetadata struct {
	Versioning struct {
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

func fetchMavenVersions(ctx context.Context, metaURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := metaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", metaURL, resp.Status)
	}
	var meta mavenMetadata
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&meta); err != nil {
		return nil, fmt.Errorf("maven metadata: %w", err)
	}
	return meta.Versioning.Versions, nil
}

// splitForgeCoord splits "1.20.1-47.2.0" or "26.2-65.0.5" into MC + Forge.
func splitForgeCoord(v string) (mc, forge string, ok bool) {
	idx := strings.LastIndex(v, "-")
	if idx <= 0 || idx == len(v)-1 {
		return "", "", false
	}
	forge = v[idx+1:]
	parts := strings.Split(forge, ".")
	if len(parts) < 2 {
		return "", "", false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", "", false
		}
	}
	return v[:idx], forge, true
}

// neoForgeToMinecraft maps a NeoForge version like "21.1.77" or "20.4.237-beta"
// onto the Minecraft release id ("1.21.1", "1.20.4"). From Minecraft 26 onward
// NeoForge drops the leading "1." (e.g. "26.1.3" → "26.1").
func neoForgeToMinecraft(neo string) string {
	base, _, _ := strings.Cut(neo, "-")
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return ""
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return ""
	}
	if major >= 26 {
		return parts[0] + "." + parts[1]
	}
	return "1." + parts[0] + "." + parts[1]
}

/* ---------- install ---------- */

// InstallLoaderResult is what a loader install reports back for persistence.
type InstallLoaderResult struct {
	LoaderVersion string
}

// InstallLoader installs fabric/quilt/forge/neoforge for the given Minecraft
// version into dataDir. loaderVersion may be empty to pick the newest stable.
func (v *Versions) InstallLoader(ctx context.Context, typ, mcVersion, loaderVersion, javaPath, dataDir string, progress func(done, total int64)) (InstallLoaderResult, error) {
	if javaPath == "" {
		javaPath = "java"
	}
	switch typ {
	case TypeFabric:
		lv, err := installFabric(ctx, fabricMetaBase, mcVersion, loaderVersion, dataDir, progress)
		return InstallLoaderResult{LoaderVersion: lv}, err
	case TypeQuilt:
		lv, err := installQuilt(ctx, mcVersion, loaderVersion, javaPath, dataDir, progress)
		return InstallLoaderResult{LoaderVersion: lv}, err
	case TypeForge:
		lv, err := installForge(ctx, mcVersion, loaderVersion, javaPath, dataDir, progress)
		return InstallLoaderResult{LoaderVersion: lv}, err
	case TypeNeoForge:
		lv, err := installNeoForge(ctx, mcVersion, loaderVersion, javaPath, dataDir, progress)
		return InstallLoaderResult{LoaderVersion: lv}, err
	default:
		return InstallLoaderResult{}, fmt.Errorf("not a mod loader: %s", typ)
	}
}

func installFabric(ctx context.Context, metaBase, mcVersion, loaderVersion, dataDir string, progress func(done, total int64)) (string, error) {
	loader, err := resolveFabricLoader(ctx, metaBase, mcVersion, loaderVersion)
	if err != nil {
		return "", err
	}
	installer, err := resolveFabricInstaller(ctx, metaBase)
	if err != nil {
		return "", err
	}
	jarURL := fmt.Sprintf("%s/versions/loader/%s/%s/%s/server/jar",
		metaBase, url.PathEscape(mcVersion), url.PathEscape(loader), url.PathEscape(installer))
	dest := filepath.Join(dataDir, jarName)
	if err := downloadVerified(ctx, jarURL, dest, "", "", progress); err != nil {
		return "", fmt.Errorf("fabric server jar: %w", err)
	}
	return loader, nil
}

func resolveFabricLoader(ctx context.Context, metaBase, mcVersion, want string) (string, error) {
	var loaders []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	u := metaBase + "/versions/loader/" + url.PathEscape(mcVersion)
	if err := getJSON(ctx, u, &loaders); err != nil {
		return "", fmt.Errorf("fabric loaders for %s: %w", mcVersion, err)
	}
	if len(loaders) == 0 {
		return "", fmt.Errorf("no fabric loader for Minecraft %s", mcVersion)
	}
	if want != "" {
		for _, l := range loaders {
			if l.Loader.Version == want {
				return want, nil
			}
		}
		return "", fmt.Errorf("fabric loader %s not available for %s", want, mcVersion)
	}
	for _, l := range loaders {
		if l.Loader.Stable {
			return l.Loader.Version, nil
		}
	}
	return loaders[0].Loader.Version, nil
}

func resolveFabricInstaller(ctx context.Context, metaBase string) (string, error) {
	var installers []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := getJSON(ctx, metaBase+"/versions/installer", &installers); err != nil {
		return "", fmt.Errorf("fabric installer list: %w", err)
	}
	for _, i := range installers {
		if i.Stable {
			return i.Version, nil
		}
	}
	if len(installers) == 0 {
		return "", errors.New("no fabric installer versions available")
	}
	return installers[0].Version, nil
}

func installQuilt(ctx context.Context, mcVersion, loaderVersion, javaPath, dataDir string, progress func(done, total int64)) (string, error) {
	loader, err := resolveQuiltLoader(ctx, mcVersion, loaderVersion)
	if err != nil {
		return "", err
	}
	installerVer, installerURL, err := resolveQuiltInstaller(ctx)
	if err != nil {
		return "", err
	}
	_ = installerVer
	installerPath := filepath.Join(dataDir, ".quilt-installer.jar")
	defer os.Remove(installerPath)
	if err := downloadVerified(ctx, installerURL, installerPath, "", "", progress); err != nil {
		return "", fmt.Errorf("quilt installer: %w", err)
	}
	args := []string{"-jar", installerPath, "install", "server", mcVersion, loader,
		"--download-server", "--install-dir=" + dataDir}
	if err := runJavaInstaller(ctx, javaPath, dataDir, args); err != nil {
		return "", fmt.Errorf("quilt install: %w", err)
	}
	// Quilt writes quilt-server-launch.jar; expose it as server.jar for a
	// uniform launch path with Fabric/Vanilla.
	launch := filepath.Join(dataDir, "quilt-server-launch.jar")
	dest := filepath.Join(dataDir, jarName)
	if _, err := os.Stat(launch); err == nil {
		_ = os.Remove(dest)
		if err := os.Rename(launch, dest); err != nil {
			// Cross-device rename can fail; fall back to copy.
			if cerr := copyFile(launch, dest); cerr != nil {
				return "", cerr
			}
			os.Remove(launch)
		}
	} else if _, err := os.Stat(dest); err != nil {
		return "", errors.New("quilt installer did not produce a server jar")
	}
	return loader, nil
}

func resolveQuiltLoader(ctx context.Context, mcVersion, want string) (string, error) {
	var loaders []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	u := quiltMetaBase + "/versions/loader/" + url.PathEscape(mcVersion)
	if err := getJSON(ctx, u, &loaders); err != nil {
		return "", fmt.Errorf("quilt loaders for %s: %w", mcVersion, err)
	}
	if len(loaders) == 0 {
		return "", fmt.Errorf("no quilt loader for Minecraft %s", mcVersion)
	}
	if want != "" {
		for _, l := range loaders {
			if l.Loader.Version == want {
				return want, nil
			}
		}
		return "", fmt.Errorf("quilt loader %s not available for %s", want, mcVersion)
	}
	return loaders[0].Loader.Version, nil
}

func resolveQuiltInstaller(ctx context.Context) (version, jarURL string, err error) {
	versions, err := fetchMavenVersions(ctx, quiltMavenBase+"/maven-metadata.xml")
	if err != nil {
		return "", "", err
	}
	if len(versions) == 0 {
		return "", "", errors.New("no quilt installer versions available")
	}
	// Prefer a non-beta release from the end of the list.
	pick := versions[len(versions)-1]
	for i := len(versions) - 1; i >= 0; i-- {
		if !strings.Contains(versions[i], "beta") && !strings.Contains(versions[i], "alpha") {
			pick = versions[i]
			break
		}
	}
	jarURL = fmt.Sprintf("%s/%s/quilt-installer-%s.jar", quiltMavenBase, url.PathEscape(pick), pick)
	return pick, jarURL, nil
}

func installForge(ctx context.Context, mcVersion, loaderVersion, javaPath, dataDir string, progress func(done, total int64)) (string, error) {
	coord, forgeVer, err := resolveForgeCoord(ctx, mcVersion, loaderVersion)
	if err != nil {
		return "", err
	}
	jarURL := fmt.Sprintf("%s/%s/forge-%s-installer.jar", forgeMavenBase, url.PathEscape(coord), coord)
	installerPath := filepath.Join(dataDir, ".forge-installer.jar")
	defer os.Remove(installerPath)
	defer os.Remove(filepath.Join(dataDir, "installer.log"))
	defer os.Remove(installerPath + ".log")
	if err := downloadVerified(ctx, jarURL, installerPath, "", "", progress); err != nil {
		return "", fmt.Errorf("forge installer: %w", err)
	}
	if err := runJavaInstaller(ctx, javaPath, dataDir, []string{"-jar", installerPath, "--installServer"}); err != nil {
		return "", fmt.Errorf("forge install: %w", err)
	}
	// Older forge dropped a *-installer.jar remnant next to libraries.
	os.Remove(installerPath)
	return forgeVer, nil
}

func resolveForgeCoord(ctx context.Context, mcVersion, wantForge string) (coord, forgeVer string, err error) {
	versions, err := fetchMavenVersions(ctx, forgeMavenBase+"/maven-metadata.xml")
	if err != nil {
		return "", "", err
	}
	if wantForge != "" {
		coord = mcVersion + "-" + wantForge
		for _, v := range versions {
			if v == coord {
				return coord, wantForge, nil
			}
		}
		return "", "", fmt.Errorf("forge %s not available for %s", wantForge, mcVersion)
	}
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.Contains(v, "beta") || strings.Contains(v, "pre") {
			continue
		}
		mc, forge, ok := splitForgeCoord(v)
		if ok && mc == mcVersion {
			return v, forge, nil
		}
	}
	return "", "", fmt.Errorf("no forge build for Minecraft %s", mcVersion)
}

func installNeoForge(ctx context.Context, mcVersion, loaderVersion, javaPath, dataDir string, progress func(done, total int64)) (string, error) {
	neoVer, err := resolveNeoForgeVersion(ctx, mcVersion, loaderVersion)
	if err != nil {
		return "", err
	}
	jarURL := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", neoMavenBase, url.PathEscape(neoVer), neoVer)
	installerPath := filepath.Join(dataDir, ".neoforge-installer.jar")
	defer os.Remove(installerPath)
	defer os.Remove(filepath.Join(dataDir, "installer.log"))
	if err := downloadVerified(ctx, jarURL, installerPath, "", "", progress); err != nil {
		return "", fmt.Errorf("neoforge installer: %w", err)
	}
	if err := runJavaInstaller(ctx, javaPath, dataDir, []string{"-jar", installerPath, "--installServer"}); err != nil {
		return "", fmt.Errorf("neoforge install: %w", err)
	}
	os.Remove(installerPath)
	return neoVer, nil
}

func resolveNeoForgeVersion(ctx context.Context, mcVersion, want string) (string, error) {
	versions, err := fetchMavenVersions(ctx, neoMavenBase+"/maven-metadata.xml")
	if err != nil {
		return "", err
	}
	if want != "" {
		for _, v := range versions {
			if v == want {
				return want, nil
			}
		}
		return "", fmt.Errorf("neoforge %s not available", want)
	}
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.Contains(v, "beta") || strings.Contains(v, "alpha") {
			continue
		}
		if neoForgeToMinecraft(v) == mcVersion {
			return v, nil
		}
	}
	return "", fmt.Errorf("no neoforge build for Minecraft %s", mcVersion)
}

func runJavaInstaller(ctx context.Context, javaPath, dir string, args []string) error {
	if _, err := exec.LookPath(javaPath); err != nil {
		return fmt.Errorf("java not found (%s)", javaPath)
	}
	cmd := exec.CommandContext(ctx, javaPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 800 {
			msg = msg[len(msg)-800:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

/* ---------- launch helpers ---------- */

// loaderLaunchArgs returns JVM args after memory flags for starting a modded
// server (e.g. ["-jar","server.jar","nogui"] or ["@libraries/.../unix_args.txt","nogui"]).
func loaderLaunchArgs(dataDir, typ, mcVersion, loaderVersion string) ([]string, error) {
	switch typ {
	case TypeFabric, TypeQuilt:
		if _, err := os.Stat(filepath.Join(dataDir, jarName)); err != nil {
			return nil, errors.New("server jar is missing")
		}
		return []string{"-jar", jarName, "nogui"}, nil
	case TypeForge:
		return forgeFamilyArgs(dataDir, "net/minecraftforge/forge", mcVersion+"-"+loaderVersion)
	case TypeNeoForge:
		return forgeFamilyArgs(dataDir, "net/neoforged/neoforge", loaderVersion)
	default:
		return nil, fmt.Errorf("not a mod loader: %s", typ)
	}
}

func forgeFamilyArgs(dataDir, mavenPath, version string) ([]string, error) {
	if version == "" {
		return nil, errors.New("loader version unknown; retry the installation")
	}
	argsName := "unix_args.txt"
	if runtime.GOOS == "windows" {
		argsName = "win_args.txt"
	}
	rel := filepath.ToSlash(filepath.Join("libraries", filepath.FromSlash(mavenPath), version, argsName))
	abs := filepath.Join(dataDir, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil && runtime.GOOS == "windows" {
		// win_args can be missing on some builds; unix_args still works.
		rel = filepath.ToSlash(filepath.Join("libraries", filepath.FromSlash(mavenPath), version, "unix_args.txt"))
		abs = filepath.Join(dataDir, filepath.FromSlash(rel))
	}
	if _, err := os.Stat(abs); err == nil {
		return []string{"@" + rel, "nogui"}, nil
	}
	// Legacy Forge: a forge-*.jar next to libraries/.
	entries, _ := os.ReadDir(dataDir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "installer") {
			continue
		}
		if strings.HasPrefix(lower, "forge-") || strings.HasPrefix(lower, "neoforge-") {
			return []string{"-jar", name, "nogui"}, nil
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, jarName)); err == nil {
		return []string{"-jar", jarName, "nogui"}, nil
	}
	return nil, errors.New("forge/neoforge launch files missing; retry the installation")
}

// loaderBinaryExists reports whether a modded server looks installed enough to start.
func loaderBinaryExists(dataDir, typ, mcVersion, loaderVersion string) bool {
	_, err := loaderLaunchArgs(dataDir, typ, mcVersion, loaderVersion)
	return err == nil
}
