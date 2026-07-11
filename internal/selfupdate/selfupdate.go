// Package selfupdate replaces the running panel binary with a verified release
// build from GitHub. It is used both by the web self-update endpoint (update to
// latest) and by the managed-mode agent (update to a control-plane-pinned
// version). The caller triggers the actual process restart after Apply returns.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repo = "computenord/craftpanel"

var ErrUnsupported = errors.New("self update only works on Linux installs")

// ErrNotWritable means the binary's directory cannot be written, so the atomic
// swap would fail. Old installs (root-owned /usr/local/bin) hit this until the
// installer is rerun.
var ErrNotWritable = errors.New("the panel cannot replace its own binary on this install, run the install command from the README once")

func downloadBase(version string) string {
	if version == "" || version == "latest" {
		return "https://github.com/" + repo + "/releases/latest/download/"
	}
	tag := version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return "https://github.com/" + repo + "/releases/download/" + tag + "/"
}

// ExecPath resolves the running executable through symlinks.
func ExecPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// EnsureWritable checks that the binary can be replaced in place, returning the
// resolved executable path.
func EnsureWritable() (string, error) {
	if runtime.GOOS != "linux" {
		return "", ErrUnsupported
	}
	exe, err := ExecPath()
	if err != nil {
		return "", err
	}
	probe, err := os.CreateTemp(filepath.Dir(exe), ".craftpanel-update-*")
	if err != nil {
		return "", ErrNotWritable
	}
	probe.Close()
	os.Remove(probe.Name())
	return exe, nil
}

// Apply downloads the release binary for version (empty = latest), verifies it
// against the release's SHA256SUMS, and atomically swaps the running
// executable. It does not restart the process — the caller does that once Apply
// succeeds.
func Apply(ctx context.Context, version string) error {
	exe, err := EnsureWritable()
	if err != nil {
		return err
	}
	base := downloadBase(version)
	asset := "craftpanel-linux-" + runtime.GOARCH

	sums, err := fetchText(ctx, base+"SHA256SUMS", 1<<20)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	wantSum := ""
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == asset {
			wantSum = fields[0]
			break
		}
	}
	if wantSum == "" {
		return fmt.Errorf("release has no checksum for %s", asset)
	}

	tmp := exe + ".new"
	if err := downloadChecked(ctx, base+asset, tmp, wantSum); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("download binary: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func fetchText(ctx context.Context, url string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ComputeBox-Craftpanel")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return string(data), err
}

func downloadChecked(ctx context.Context, url, dest, wantSum string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ComputeBox-Craftpanel")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), wantSum) {
		return errors.New("checksum mismatch")
	}
	return nil
}
