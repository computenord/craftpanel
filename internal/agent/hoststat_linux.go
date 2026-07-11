//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// cpuTimes returns total and idle jiffies from /proc/stat.
func cpuTimes() (total, idle uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0
	}
	for i := 1; i < len(fields); i++ {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		total += v
		if i == 4 { // idle is the 4th value
			idle = v
		}
	}
	return total, idle
}

func (h *hostSampler) readHost(dataDir string) HostStat {
	var st HostStat

	// Memory from /proc/meminfo.
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		var totalKB, availKB int64
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			kb, _ := strconv.ParseInt(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				totalKB = kb
			case "MemAvailable:":
				availKB = kb
			}
		}
		st.MemTotalMB = int(totalKB / 1024)
		st.MemUsedMB = int((totalKB - availKB) / 1024)
	}

	// Disk of the data directory.
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dataDir, &fs); err == nil {
		total := fs.Blocks * uint64(fs.Bsize)
		free := fs.Bavail * uint64(fs.Bsize)
		st.DiskTotalGB = int(total / (1 << 30))
		st.DiskUsedGB = int((total - free) / (1 << 30))
	}

	// Host uptime.
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if first, _, ok := strings.Cut(string(data), " "); ok {
			if secs, err := strconv.ParseFloat(first, 64); err == nil {
				st.UptimeS = int64(secs)
			}
		}
	}

	// CPU percent from the delta since the last sample.
	total, idle := cpuTimes()
	if h.prevTotal != 0 && total > h.prevTotal {
		dTotal := total - h.prevTotal
		dIdle := idle - h.prevIdle
		st.CPUPct = float64(int((float64(dTotal-dIdle)/float64(dTotal))*1000)) / 10
	}
	h.prevTotal, h.prevIdle = total, idle
	return st
}
