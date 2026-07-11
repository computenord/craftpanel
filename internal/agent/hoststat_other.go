//go:build !linux

package agent

// On non-Linux hosts (development) host stats are not available. Managed mode
// targets Linux VMs; this keeps the package building on Windows.
func (h *hostSampler) readHost(dataDir string) HostStat {
	return HostStat{}
}
