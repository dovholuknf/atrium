//go:build !windows

// Package detach starts a process that survives this one dying. See detach_windows.go.
package detach

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Setsid, so the child has no controlling terminal and does not take the SIGHUP that closing the room's terminal sends.
func Start(exe string, args []string, log string) (*os.Process, error) {
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	if log != "" {
		f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		cmd.Stdout, cmd.Stderr = f, f
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// There are no job objects off Windows, so there is nothing to start outside of.
func StartOutsideJob(string, []string, string) (*os.Process, error) {
	return nil, errors.New("job objects exist only on Windows")
}

func InJob(int) (bool, error) { return false, nil }
