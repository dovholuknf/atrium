//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// Starting a process that survives this one dying. The same two flags the CLI
// control server uses, for the same reasons: DETACHED_PROCESS gives the child no
// console, so it does not die when this room's terminal closes, and
// CREATE_NEW_PROCESS_GROUP keeps a ctrl-c aimed here from reaching it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
}
