//go:build !windows

package daemon

import "os/exec"

// hideWindow does nothing off Windows.
//
// Nothing here opens a window to begin with: a console is a Windows idea, and
// a process started from a daemon on Linux or macOS has no desktop presence to
// suppress. It exists so the call site reads the same on every platform rather
// than being wrapped in a build tag of its own.
func hideWindow(cmd *exec.Cmd) {}
