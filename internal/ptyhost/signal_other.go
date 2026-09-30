//go:build !windows

package ptyhost

import (
	"os"
	"syscall"
)

// termProcess is `signal term`: SIGTERM to the runner.
func termProcess(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
