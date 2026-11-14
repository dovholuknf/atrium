//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// rawShellLine hands cmd.exe the adapter command as written, since Go would escape its quotes as \" and cmd does not
// read that.
func rawShellLine(c *exec.Cmd, argv []string) {
	if len(argv) == 4 && argv[0] == "cmd" && argv[1] == "/S" && argv[2] == "/C" {
		c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + argv[3] + `"`}
	}
}
