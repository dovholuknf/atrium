//go:build windows

package link

import (
	"os/exec"
	"syscall"
)

// detach starts the child in its own process group with a windowless console of its own, so it outlives this process
// and a ctrl-c to the hub does not reach it. Not DETACHED_PROCESS: with no console at all, every console grandchild
// the deploy script starts allocates one, and under Windows Terminal as the default terminal each leaves an
// OpenConsole host behind.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	// CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW
	cmd.SysProcAttr.CreationFlags |= 0x00000200 | 0x08000000
}
