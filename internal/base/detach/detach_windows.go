//go:build windows

// Package detach starts a process that survives this one dying. It was internal/cli's startDetached and moved here
// so the pty host's Start can use the same code, not a copy of it.
package detach

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"syscall"
)

// A caller inside a job object starts the child through WMI first, since CREATE_BREAKAWAY_FROM_JOB is not enough:
// a terminal's job may allow breakaway while a job above it does not, and the child then sits in that outer job,
// which ends with the terminal tab. Only a process WMI creates is in none of ours.
//
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
	if _, err := exec.LookPath(exe); err != nil {
		return nil, err
	}
	tried := false
	if in, _ := selfInJob(); in {
		tried = true
		q, err := startOutsideJob(exe, args, log)
		if err == nil {
			return q, nil
		}
		logFallback(exe, "this process is inside a job object", err)
	}
	p, err := start(base | breakawayFromJob)
	if err != nil && errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		if !tried {
			q, oerr := startOutsideJob(exe, args, log)
			if oerr == nil {
				return q, nil
			}
			logFallback(exe, "this process's job refused breakaway", oerr)
		}
		return start(base)
	}
	return p, err
}

// StartOutsideJob starts exe through WMI so it is in no job object, which the caller wants when it already knows it
// is in one that would end it.
func StartOutsideJob(exe string, args []string, log string) (*os.Process, error) {
	return startOutsideJob(exe, args, log)
}

func logFallback(exe, why string, err error) {
	log.Printf("[atrium] %s could not be started outside the job object (%v). %s, so it will end with the "+
		"process or terminal that started this one", exe, err, why)
}
