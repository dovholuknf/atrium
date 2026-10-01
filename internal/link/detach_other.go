//go:build !windows

package link

import (
	"os/exec"
	"syscall"
)

// detach starts the child in its own session, so it outlives this process.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
