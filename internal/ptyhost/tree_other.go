//go:build !windows

package ptyhost

import "syscall"

// tree is the process group a runner leads: the pty makes it a session leader, so its pid is its group id.
type tree struct{ pid int }

func newTree(pid int, _, _ string) *tree { return &tree{pid: pid} }

// kill ends the whole group. It does not wait.
func (t *tree) kill() {
	if t.pid > 1 {
		_ = syscall.Kill(-t.pid, syscall.SIGKILL)
	}
}
