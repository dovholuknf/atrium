//go:build !windows

package daemon

import (
	"syscall"
	"time"
)

// killTree ends the process group pid leads. Best effort: the caller still kills pid itself.
func killTree(pid int) {
	if pid > 1 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

// reapLeft ends what an exited runner left in its process group, its MCP servers among them.
func reapLeft(pid int, _ time.Time) { killTree(pid) }
