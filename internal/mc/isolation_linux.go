//go:build linux

package mc

import (
	"os/exec"
	"strconv"
	"strings"
)

// wrapIsolated rewrites bin/args for systemd-run or docker isolation.
// Returns the new bin/args. On failure or unknown mode, returns originals.
func wrapIsolated(isolation, unitID string, memoryMB int, dataDir, ctlDir, bin string, args []string) (string, []string) {
	switch isolation {
	case "systemd":
		if _, err := exec.LookPath("systemd-run"); err != nil {
			return bin, args
		}
		unit := "craftpanel-" + sanitizeUnit(unitID)
		props := []string{
			"--pipe", "--wait", "--collect",
			"--unit=" + unit,
			"--property=PrivateTmp=yes",
			"--property=NoNewPrivileges=yes",
			"--property=ProtectHome=read-only",
			"--property=ProtectSystem=strict",
			"--property=ReadWritePaths=" + dataDir + " " + ctlDir,
			"--working-directory=" + dataDir,
		}
		if memoryMB > 0 {
			props = append(props, "--property=MemoryMax="+strconv.Itoa(memoryMB)+"M")
		}
		out := append(props, bin)
		out = append(out, args...)
		return "systemd-run", out
	case "docker":
		if _, err := exec.LookPath("docker"); err != nil {
			return bin, args
		}
		name := "craftpanel-" + sanitizeUnit(unitID)
		// Host networking keeps Minecraft ports simple; console uses inherited stdio.
		img := "eclipse-temurin:21-jre"
		out := []string{
			"run", "--rm", "-i",
			"--name", name,
			"--network", "host",
			"-v", dataDir + ":/data",
			"-w", "/data",
		}
		if memoryMB > 0 {
			out = append(out, "--memory", strconv.Itoa(memoryMB)+"m")
		}
		// Rewrite java path to container java when host path was used.
		cbin := bin
		if strings.Contains(bin, "java") || bin == "java" {
			cbin = "java"
		}
		out = append(out, img, cbin)
		out = append(out, args...)
		return "docker", out
	default:
		return bin, args
	}
}

func sanitizeUnit(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := b.String()
	if s == "" {
		return "srv"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}
