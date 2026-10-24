//go:build windows

package nowindow

import (
	"os/exec"
	"syscall"
)

// CreateNoWindow is the flag that stops a console process opening a console.
// Not in `syscall` as a named constant, so it is spelled here once.
const CreateNoWindow = 0x08000000

// Hide starts cmd without a console window. It adds to whatever SysProcAttr
// already holds, so call it after anything that replaces SysProcAttr whole.
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= CreateNoWindow
}
