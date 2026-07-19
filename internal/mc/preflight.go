package mc

import (
	"errors"
	"fmt"
	"os/exec"
)

// PreflightCheck is one readiness check before starting a server.
type PreflightCheck struct {
	Code    string `json:"code"`
	OK      bool   `json:"ok"`
	Level   string `json:"level"` // error | warn | info
	Message string `json:"message"`
}

// PreflightResult aggregates start readiness.
type PreflightResult struct {
	OK     bool             `json:"ok"`
	Checks []PreflightCheck `json:"checks"`
}

// Preflight evaluates whether a server can be started cleanly.
func (m *Manager) Preflight(id string) (PreflightResult, error) {
	srv, err := m.get(id)
	if err != nil {
		return PreflightResult{}, err
	}
	srv.mu.Lock()
	meta := srv.meta
	installing := srv.installing
	installErr := srv.installErr
	binaryOK := srv.binaryExists()
	eulaOK := srv.eulaAccepted()
	srv.mu.Unlock()

	var checks []PreflightCheck
	add := func(code, level, msg string, ok bool) {
		checks = append(checks, PreflightCheck{Code: code, OK: ok, Level: level, Message: msg})
	}

	if installing {
		add("installing", "error", "installation is still in progress", false)
	} else {
		add("installing", "info", "not installing", true)
	}
	if installErr != "" {
		add("install_failed", "error", "installation failed: "+installErr, false)
	} else {
		add("install_failed", "info", "install looks complete", true)
	}
	if !binaryOK {
		add("binary", "error", "server files are missing; retry the installation", false)
	} else {
		add("binary", "info", "server binary is present", true)
	}
	if meta.Type != TypeVelocity {
		if !eulaOK {
			add("eula", "error", "Minecraft EULA is not accepted", false)
		} else {
			add("eula", "info", "EULA accepted", true)
		}
	}

	if meta.Type != TypeBedrock {
		javaPath := meta.JavaPath
		if javaPath == "" {
			javaPath = "java"
		}
		if _, err := exec.LookPath(javaPath); err != nil {
			add("java_found", "error", "Java runtime not found ("+javaPath+")", false)
		} else {
			add("java_found", "info", "Java runtime found", true)
			have, _ := DetectJava(javaPath)
			if meta.JavaMajor > 0 && have > 0 && have < meta.JavaMajor {
				add("java_version", "error",
					fmt.Sprintf("needs Java %d or newer, host has Java %d", meta.JavaMajor, have), false)
			} else if meta.JavaMajor > 0 && have > 0 {
				add("java_version", "info",
					fmt.Sprintf("Java %d meets requirement (%d+)", have, meta.JavaMajor), true)
			} else if have > 0 {
				add("java_version", "info", fmt.Sprintf("Java %d detected", have), true)
			}
		}
	}

	if IsModded(meta.Type) {
		if meta.MemoryMB > 0 && meta.MemoryMB < 2048 {
			add("memory", "warn",
				fmt.Sprintf("only %d MB RAM assigned; modded servers usually need 2048 MB+", meta.MemoryMB), true)
		} else if meta.MemoryMB >= 2048 {
			add("memory", "info", fmt.Sprintf("%d MB RAM assigned", meta.MemoryMB), true)
		}
		if meta.Modpack != nil {
			add("modpack", "info",
				fmt.Sprintf("modpack %s %s", meta.Modpack.Title, meta.Modpack.Version), true)
		}
	}

	ok := true
	for _, c := range checks {
		if !c.OK && c.Level == "error" {
			ok = false
			break
		}
	}
	return PreflightResult{OK: ok, Checks: checks}, nil
}

// ClientPackInfo is a short blurb players can use to match the server pack.
type ClientPackInfo struct {
	Text    string `json:"text"`
	Source  string `json:"source,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`
}

// ClientPackInfoFor builds a copy-paste note for modpack servers.
func (m *Manager) ClientPackInfoFor(id string) (ClientPackInfo, error) {
	srv, err := m.get(id)
	if err != nil {
		return ClientPackInfo{}, err
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	mp := srv.meta.Modpack
	if mp == nil {
		return ClientPackInfo{}, errors.New("server was not created from a modpack")
	}
	source := mp.Source
	if source == "" {
		source = SourceModrinth
	}
	link := ""
	switch source {
	case SourceCurseForge:
		link = "https://www.curseforge.com/minecraft/modpacks/" + mp.Slug
	default:
		link = "https://modrinth.com/modpack/" + mp.Slug
	}
	text := fmt.Sprintf(
		"Join with the matching client pack:\n%s\nVersion: %s\nLoader: %s %s\nMinecraft: %s\n%s",
		mp.Title, mp.Version, srv.meta.Type, srv.meta.LoaderVersion, srv.meta.Version, link,
	)
	return ClientPackInfo{
		Text:    text,
		Source:  source,
		Title:   mp.Title,
		Version: mp.Version,
		URL:     link,
	}, nil
}
