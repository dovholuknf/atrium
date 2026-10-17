//go:build windows

// Package detach starts a process that survives this one dying. It was internal/cli's startDetached and moved here
// so the pty host's Start can use the same code, not a copy of it.
package detach

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// log is a path and never a file this process holds open: cmd's `>>` on the WMI retry cannot open a held file.
func Start(exe string, args []string, log string) (*os.Process, error) {
	const (
		detachedProcess  = 0x00000008
		breakawayFromJob = 0x01000000
	)
	base := uint32(syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess)
	start := func(flags uint32) (*os.Process, error) {
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
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd.Process, nil
	}
	p, err := start(base | breakawayFromJob)
	if err != nil && errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		// A job that forbids breakaway answers access denied, and a child left in it ends with it.
		if q, oerr := startOutsideJob(exe, args, log); oerr == nil {
			return q, nil
		}
		return start(base)
	}
	return p, err
}
