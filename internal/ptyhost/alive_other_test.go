//go:build !windows

package ptyhost

import (
	"os"
	"syscall"
)

// pidAlive is a kernel question: is this pid still running.
func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
