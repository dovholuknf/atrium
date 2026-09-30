//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// startDetached is internal/cli/spawn_windows.go startDetached, copied so the spike needs nothing from the
// product, including its retry without breakaway. It records which of the two took effect in the log.
func startDetached(exe string, args []string, out *os.File) (*os.Process, error) {
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
	if os.Getenv("SPIKE_NOBREAKAWAY") != "" { // spike control: skip the flag, as if the job had refused it
		logf("SPIKE_NOBREAKAWAY set: detaching WITHOUT breakaway on purpose")
		return start(base)
	}
	p, err := start(base | breakawayFromJob)
	if err == nil {
		logf("detached WITH breakaway")
	} else if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		logf("breakaway refused (%v), retrying without", err)
		p, err = start(base)
		if err == nil {
			logf("detached WITHOUT breakaway")
		}
	}
	return p, err
}
