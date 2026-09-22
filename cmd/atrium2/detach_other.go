//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// The POSIX half: a new session, so the restarter has no controlling terminal
// and does not take the SIGHUP that closing this room's terminal sends.
func startDetached(exe string, args []string, out *os.File) (*os.Process, error) {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	if out != nil {
		cmd.Stdout, cmd.Stderr = out, out
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}
