//go:build !windows

package ptyhost

import "syscall"

// tree is the process group a runner leads: the pty makes it a session leader, so its pid is its group id. unit is
// the systemd scope it was moved into, empty where there is none.
type tree struct {
	pid  int
	unit string
}

func newTree(pid int, label, runID string) *tree {
	return &tree{pid: pid, unit: intoScope(pid, label, runID)}
}

// kill ends the whole group. It does not wait.
func (t *tree) kill() {
	if t.pid > 1 {
		_ = syscall.Kill(-t.pid, syscall.SIGKILL)
	}
}

func (t *tree) mem() int64 { return treeMem(t.pid, t.unit) }
