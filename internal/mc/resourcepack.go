package mc

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/computenord/craftpanel/internal/fsutil"
)

const resourcePackName = "server-resource-pack.zip"

// ResourcePackInfo describes the hosted pack for a server.
type ResourcePackInfo struct {
	Present  bool   `json:"present"`
	Size     int64  `json:"size,omitempty"`
	SHA1     string `json:"sha1,omitempty"`
	URLPath  string `json:"urlPath,omitempty"` // relative API path clients can use
	Prompt   bool   `json:"prompt"`
	Required bool   `json:"required"`
}

func (m *Manager) resourcePackPath(id string) string {
	srv, err := m.get(id)
	if err != nil {
		return ""
	}
	return filepath.Join(srv.DataDir(), resourcePackName)
}

// ResourcePackInfo returns status and optionally rewrites server.properties
// when applyProps is used via SetResourcePackProps.
func (m *Manager) GetResourcePack(id string) (ResourcePackInfo, error) {
	srv, err := m.get(id)
	if err != nil {
		return ResourcePackInfo{}, err
	}
	path := filepath.Join(srv.DataDir(), resourcePackName)
	info := ResourcePackInfo{URLPath: "/api/servers/" + url.PathEscape(id) + "/resource-pack/download"}
	st, err := os.Stat(path)
	if err != nil {
		return info, nil
	}
	info.Present = true
	info.Size = st.Size()
	sum, err := fileSHA1(path)
	if err == nil {
		info.SHA1 = sum
	}
	// Read require/prompt from properties
	props, _ := os.ReadFile(filepath.Join(srv.DataDir(), "server.properties"))
	for _, line := range strings.Split(string(props), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "require-resource-pack=") {
			info.Required = strings.HasSuffix(line, "=true")
		}
		if strings.HasPrefix(line, "resource-pack-prompt=") {
			info.Prompt = true
		}
	}
	return info, nil
}

// SetResourcePack stores an uploaded zip and optionally points server.properties
// at the panel download URL. publicBase is e.g. https://panel.example.com
func (m *Manager) SetResourcePack(id, zipPath, publicBase string, required bool) (ResourcePackInfo, error) {
	srv, err := m.get(id)
	if err != nil {
		return ResourcePackInfo{}, err
	}
	if srv.meta.Type == TypeVelocity || srv.meta.Type == TypeBedrock {
		return ResourcePackInfo{}, errors.New("resource packs are for Java edition servers")
	}
	dest := filepath.Join(srv.DataDir(), resourcePackName)
	if err := copyFile(zipPath, dest); err != nil {
		return ResourcePackInfo{}, err
	}
	sum, err := fileSHA1(dest)
	if err != nil {
		return ResourcePackInfo{}, err
	}
	publicBase = strings.TrimRight(strings.TrimSpace(publicBase), "/")
	packURL := ""
	if publicBase != "" {
		packURL = publicBase + "/api/servers/" + id + "/resource-pack/download"
	}
	propsPath := filepath.Join(srv.DataDir(), "server.properties")
	data, _ := os.ReadFile(propsPath)
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines)+4)
	seenURL, seenHash, seenReq := false, false, false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "resource-pack="):
			if packURL != "" {
				out = append(out, "resource-pack="+packURL)
			} else {
				out = append(out, line)
			}
			seenURL = true
		case strings.HasPrefix(trim, "resource-pack-sha1="):
			out = append(out, "resource-pack-sha1="+sum)
			seenHash = true
		case strings.HasPrefix(trim, "require-resource-pack="):
			out = append(out, fmt.Sprintf("require-resource-pack=%v", required))
			seenReq = true
		default:
			out = append(out, line)
		}
	}
	if packURL != "" && !seenURL {
		out = append(out, "resource-pack="+packURL)
	}
	if !seenHash {
		out = append(out, "resource-pack-sha1="+sum)
	}
	if !seenReq {
		out = append(out, fmt.Sprintf("require-resource-pack=%v", required))
	}
	if err := fsutil.WriteFileAtomic(propsPath, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return ResourcePackInfo{}, err
	}
	return m.GetResourcePack(id)
}

func (m *Manager) DeleteResourcePack(id string) error {
	srv, err := m.get(id)
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(srv.DataDir(), resourcePackName))
	return nil
}

func (m *Manager) OpenResourcePack(id string) (*os.File, error) {
	srv, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(srv.DataDir(), resourcePackName))
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
