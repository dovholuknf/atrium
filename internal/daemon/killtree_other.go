//go:build !windows

package daemon

import "syscall"

// killTree ends the process group pid leads. Best effort: the caller still kills pid itself.
func killTree(pid int) {
	if pid > 1 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
