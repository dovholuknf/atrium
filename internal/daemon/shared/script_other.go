//go:build !windows

package shared

import "os/exec"

// ViaShellIfScript exists for Windows, where an npm-installed tool is often a
// batch shim that CreateProcess cannot start.
//
// Elsewhere a script names its interpreter on the first line and the kernel
// honors it, so there is nothing to rewrite.
func ViaShellIfScript(resolved string, args []string) (string, []string, error) {
	return resolved, args, nil
}

// NewConsole is a no-op off Windows, where a runner never gets a console of its own.
func NewConsole(cmd *exec.Cmd) {}

// RefusedByCmd is false off Windows, where no runner goes through cmd.exe.
func RefusedByCmd(resolved, arg string) bool { return false }
