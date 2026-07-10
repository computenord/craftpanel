package mc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type cpuSample struct {
	ticks uint64
	at    time.Time
}

// procUsage reads CPU and memory usage of one process from /proc. CPU percent
// is computed against the previous sample. Non-Linux hosts get zeros, which
// only affects local development.
func procUsage(pid int, prev cpuSample) (cpuPct float64, rssMB int, next cpuSample) {
	next = prev
	if runtime.GOOS != "linux" || pid <= 0 {
		return 0, 0, next
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, next
	}
	s := string(data)
	// comm can contain spaces, fields start after the closing paren.
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, 0, next
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 13 {
		return 0, 0, next
	}
	ut, _ := strconv.ParseUint(fields[11], 10, 64)
	st, _ := strconv.ParseUint(fields[12], 10, 64)
	ticks := ut + st
	now := time.Now()
	next = cpuSample{ticks: ticks, at: now}

	rssMB = readRSSMB(pid)
	if prev.at.IsZero() || ticks < prev.ticks {
		return 0, rssMB, next
	}
	dt := now.Sub(prev.at).Seconds()
	if dt <= 0 {
		return 0, rssMB, next
	}
	// Kernel USER_HZ is 100 on every mainstream Linux.
	cpuPct = float64(ticks-prev.ticks) / 100.0 / dt * 100.0
	return cpuPct, rssMB, next
}

func readRSSMB(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				return int(kb / 1024)
			}
		}
	}
	return 0
}

// dirSizeMB walks a directory tree and sums file sizes. Unreadable entries
// are skipped, this is informational only.
func dirSizeMB(path string) int64 {
	var total int64
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total / (1 << 20)
}
