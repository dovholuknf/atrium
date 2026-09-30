//go:build !windows

// Package detach starts a process that survives this one dying. See detach_windows.go.
package detach

import (
	"os"
	"os/exec"
	"syscall"
)

// Start: the POSIX half. A new session, so the child has no controlling terminal and does not take the SIGHUP that
// closing this room's terminal sends.
func Start(exe string, args []string, out *os.File) (*os.Process, error) {
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
