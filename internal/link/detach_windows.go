//go:build windows

package link

import (
	"os/exec"
	"syscall"
)

// detach starts the child in its own process group with no console, so it outlives this process and a ctrl-c to
// the hub does not reach it.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS | CREATE_NO_WINDOW
	cmd.SysProcAttr.CreationFlags |= 0x00000200 | 0x00000008 | 0x08000000
}
