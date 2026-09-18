//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// The POSIX half: a new session, so the restarter has no controlling terminal
// and does not take the SIGHUP that closing this room's terminal sends.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
