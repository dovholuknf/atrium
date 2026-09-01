//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// processAlive reports whether a pid is still running. Signal 0 performs the
// permission and existence checks without delivering anything, which is the
// portable way to ask.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means it exists but belongs to someone else, which still counts.
	return err == syscall.EPERM
}
