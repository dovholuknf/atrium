//go:build !windows

package dictate

import "os/exec"

// procTree is nothing off Windows: Stop kills the server, and a crashed room's orphan is left on a port nobody asks.
type procTree struct{}

func startTree(cmd *exec.Cmd) *procTree { return nil }

func (t *procTree) kill() error { return nil }

func (t *procTree) close() {}
