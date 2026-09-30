//go:build windows

package link

import (
	"os/exec"
	"syscall"
)

// hideWindow stops the notify command opening a console on the operator's
// desktop. The same flags internal/daemon uses for a source, for the same
// reason: it runs unwatched.
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000
}
