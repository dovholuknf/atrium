//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// The POSIX half: a new session, so the child has no controlling terminal.
//
// Same requirement as the Windows side and the same reason. When the terminal
// this process is attached to goes away, everything still in its session gets
// SIGHUP, and the thing being started here is specifically meant to outlive
// that.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
