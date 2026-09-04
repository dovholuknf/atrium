//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// Starting a process that survives this one dying.
//
// Two flags, and both are needed for different reasons:
//
//   - DETACHED_PROCESS gives the child no console at all. Without it the child
//     inherits this one's, and when the daemon closes the pseudo terminal this
//     session is running in, the child's console goes with it. That is the
//     exact death this exists to avoid.
//   - CREATE_NEW_PROCESS_GROUP takes it out of this process's group, so a
//     ctrl-c or a group-wide terminate aimed here does not reach it.
//
// `taskkill /t` walks the process tree and would still find it. That is fine
// and not worth defending against: somebody killing the tree means it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
}
