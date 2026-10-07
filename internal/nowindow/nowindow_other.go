//go:build !windows

package nowindow

import "os/exec"

// Hide does nothing off Windows: a console is a Windows idea, and a process
// started from a daemon on Linux or macOS has no desktop presence to suppress.
// It exists so the call site reads the same on every platform.
func Hide(cmd *exec.Cmd) {}
