//go:build !windows

package gitsync

import (
	"os/exec"
	"syscall"
)

// procTree is a git child's process group: the child and everything it starts, such as
// `git-remote-http` during a fetch. See tree_windows.go for why the whole tree has to go.
type procTree struct{ pgid int }

// prepareTree starts the child as the leader of its own process group.
func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// startTree answers the started child's group.
func startTree(cmd *exec.Cmd) *procTree { return &procTree{pgid: cmd.Process.Pid} }

// kill ends every process in the group.
func (t *procTree) kill() error { return syscall.Kill(-t.pgid, syscall.SIGKILL) }

// close ends whatever is left in the group once the child has exited.
func (t *procTree) close() { _ = syscall.Kill(-t.pgid, syscall.SIGKILL) }
