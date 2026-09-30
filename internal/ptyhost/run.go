package ptyhost

import (
	"fmt"
	"os"
	"time"
)

// RunOptions start a host in this process. It is what `atrium ptyhost` calls.
type RunOptions struct {
	StateDir string
	Build    string
	IdleExit time.Duration // DefaultIdleExit when zero
	Logf     func(format string, args ...any)
}

// Listen starts a host in this process on Address(stateDir) and serves it in the background. It is what a test
// that needs a real host without a child process calls, and closing the returned host stops it.
func Listen(stateDir string, o Options) (*Host, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	ln, err := listenChannel(Address(stateDir))
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	h := NewHost(o)
	go func() { _ = h.Serve(ln) }()
	return h, nil
}

// Run listens on Address(StateDir) and serves until the host is idle for IdleExit or is closed. It returns nil for
// both. A failure to listen is returned, including "already listening": two hosts for one state dir would split the
// runners between them.
func Run(o RunOptions) error {
	if o.StateDir == "" {
		return fmt.Errorf("a state dir is required")
	}
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return err
	}
	addr := Address(o.StateDir)
	ln, err := listenChannel(addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	h := NewHost(Options{Build: o.Build, IdleExit: o.IdleExit, Logf: o.Logf})
	h.logf("up pid=%d proto=%d build=%q at %s", os.Getpid(), Proto, o.Build, addr)
	err = h.Serve(ln)
	h.Close()
	return err
}
