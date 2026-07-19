package mc

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const adoptiumAPI = "https://api.adoptium.net/v3"

// JavaRuntime describes an installed Temurin JDK under <dataDir>/java/.
type JavaRuntime struct {
	Major   int    `json:"major"`
	Path    string `json:"path"` // java executable
	Version string `json:"version,omitempty"`
	Vendor  string `json:"vendor,omitempty"`
}

func (m *Manager) javaRoot() string {
	return filepath.Join(m.dataDir, "java")
}

// ListJavaRuntimes scans installed JDKs managed by the panel.
func (m *Manager) ListJavaRuntimes() []JavaRuntime {
	root := m.javaRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		return []JavaRuntime{}
	}
	out := []JavaRuntime{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		major := 0
		fmt.Sscanf(e.Name(), "%d", &major)
		if major == 0 {
			continue
		}
		javaBin := findJavaBinary(filepath.Join(root, e.Name()))
		if javaBin == "" {
			continue
		}
		maj, ver := DetectJava(javaBin)
		out = append(out, JavaRuntime{Major: major, Path: javaBin, Version: ver, Vendor: "Eclipse Temurin"})
		_ = maj
	}
	return out
}

func findJavaBinary(root string) string {
	candidates := []string{
		filepath.Join(root, "bin", "java"),
		filepath.Join(root, "bin", "java.exe"),
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := strings.ToLower(d.Name())
		if base == "java" || base == "java.exe" {
			if strings.Contains(filepath.ToSlash(p), "/bin/") {
				candidates = append([]string{p}, candidates...)
			}
		}
		return nil
	})
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// InstallJavaRuntime downloads Temurin for the given major version.
func (m *Manager) InstallJavaRuntime(ctx context.Context, major int) (JavaRuntime, error) {
	if major < 8 || major > 25 {
		return JavaRuntime{}, errors.New("java major must be between 8 and 25")
	}
	osName, arch, err := adoptiumPlatform()
	if err != nil {
		return JavaRuntime{}, err
	}
	url := fmt.Sprintf("%s/binary/latest/%d/ga/%s/%s/jdk/hotspot/normal/eclipse?project=jdk",
		adoptiumAPI, major, osName, arch)

	destRoot := filepath.Join(m.javaRoot(), fmt.Sprintf("%d", major))
	tmpZip := destRoot + ".download"
	os.RemoveAll(tmpZip)
	if err := os.MkdirAll(m.javaRoot(), 0o750); err != nil {
		return JavaRuntime{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return JavaRuntime{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	dl := &http.Client{Timeout: 15 * time.Minute}
	resp, err := dl.Do(req)
	if err != nil {
		return JavaRuntime{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return JavaRuntime{}, fmt.Errorf("adoptium: %s", resp.Status)
	}
	f, err := os.Create(tmpZip)
	if err != nil {
		return JavaRuntime{}, err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpZip)
		return JavaRuntime{}, err
	}
	f.Close()

	extractDir := destRoot + ".extract"
	os.RemoveAll(extractDir)
	os.RemoveAll(destRoot)
	ct := resp.Header.Get("Content-Type")
	nameHint := resp.Header.Get("Content-Disposition")
	if strings.Contains(nameHint, ".tar.gz") || strings.Contains(ct, "gzip") || strings.HasSuffix(tmpZip, ".tar.gz") {
		if err := extractTarGz(tmpZip, extractDir); err != nil {
			// Windows often gets .zip
			if err2 := extractZipFlat(tmpZip, extractDir); err2 != nil {
				os.Remove(tmpZip)
				return JavaRuntime{}, err
			}
		}
	} else {
		if err := extractZipFlat(tmpZip, extractDir); err != nil {
			if err2 := extractTarGz(tmpZip, extractDir); err2 != nil {
				os.Remove(tmpZip)
				return JavaRuntime{}, err
			}
		}
	}
	os.Remove(tmpZip)

	// Adoptium archives contain a single top-level folder — hoist it.
	entries, _ := os.ReadDir(extractDir)
	src := extractDir
	if len(entries) == 1 && entries[0].IsDir() {
		src = filepath.Join(extractDir, entries[0].Name())
	}
	if err := os.Rename(src, destRoot); err != nil {
		if err := copyDir(src, destRoot); err != nil {
			os.RemoveAll(extractDir)
			return JavaRuntime{}, err
		}
	}
	os.RemoveAll(extractDir)

	javaBin := findJavaBinary(destRoot)
	if javaBin == "" {
		return JavaRuntime{}, errors.New("java binary not found after extract")
	}
	// Make executable on Unix.
	_ = os.Chmod(javaBin, 0o755)
	maj, ver := DetectJava(javaBin)
	if maj == 0 {
		// Still usable; report requested major.
		maj = major
	}
	return JavaRuntime{Major: maj, Path: javaBin, Version: ver, Vendor: "Eclipse Temurin"}, nil
}

func adoptiumPlatform() (osName, arch string, err error) {
	switch runtime.GOOS {
	case "linux":
		osName = "linux"
	case "darwin":
		osName = "mac"
	case "windows":
		osName = "windows"
	default:
		return "", "", fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "aarch64"
	default:
		return "", "", fmt.Errorf("unsupported arch %s", runtime.GOARCH)
	}
	return osName, arch, nil
}

func extractZipFlat(zipPath, dest string) error {
	return extractZipTo(zipPath, dest)
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, ok := safeZipRel(hdr.Name)
		if !ok {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o755|0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// ResolveJavaForMajor returns a panel-managed java path for major, if installed.
func (m *Manager) ResolveJavaForMajor(major int) string {
	for _, rt := range m.ListJavaRuntimes() {
		if rt.Major == major || (major > 0 && rt.Major >= major) {
			return rt.Path
		}
	}
	return ""
}
