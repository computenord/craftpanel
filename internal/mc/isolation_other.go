//go:build !linux

package mc

func wrapIsolated(isolation, unitID string, memoryMB int, dataDir, ctlDir, bin string, args []string) (string, []string) {
	return bin, args
}
