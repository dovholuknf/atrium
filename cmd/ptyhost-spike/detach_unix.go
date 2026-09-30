//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func umaskSet(m int) int { return syscall.Umask(m) }

// startDetached: Setsid, as the design says for everything that is not Windows.
func startDetached(exe string, args []string, out *os.File) (*os.Process, error) {
	cmd := exec.Command(exe, args...)
	if out != nil {
		cmd.Stdout, cmd.Stderr = out, out
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}
