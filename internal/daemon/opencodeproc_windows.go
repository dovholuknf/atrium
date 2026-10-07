//go:build windows

package daemon

import (
	"os/exec"
	"strconv"
)

// prepareTree makes a kill take the whole tree: an npm opencode.cmd runs through
// cmd.exe, and killing only cmd.exe leaves opencode holding stdout.
func prepareTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideWindow(kill)
		return kill.Run()
	}
}

// reapTree has nothing to do: the process is gone and its pid may be reused.
func reapTree(cmd *exec.Cmd) {}
