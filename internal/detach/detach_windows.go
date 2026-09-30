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

// Start: the same two flags the CLI control server uses, for the same reasons. DETACHED_PROCESS gives the child no
// console, so it does not die when this room's terminal closes, and CREATE_NEW_PROCESS_GROUP keeps a ctrl-c aimed
// here from reaching it.
//
// CREATE_BREAKAWAY_FROM_JOB takes it out of any job object this room is in. A room started from a terminal or an
// agent's shell usually is, and a job set to kill on close takes every member with it when its owner exits, detached
// or not. A job that forbids breaking away refuses the flag with access denied, so that case retries without it
// rather than not restarting at all.
func Start(exe string, args []string, out *os.File) (*os.Process, error) {
	const (
		detachedProcess  = 0x00000008
		breakawayFromJob = 0x01000000
	)
	base := uint32(syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess)
	start := func(flags uint32) (*os.Process, error) {
		cmd := exec.Command(exe, args...)
		cmd.Stdin = nil
		if out != nil {
			cmd.Stdout, cmd.Stderr = out, out
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd.Process, nil
	}
	p, err := start(base | breakawayFromJob)
	if err != nil && errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return start(base)
	}
	return p, err
}
