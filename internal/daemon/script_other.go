//go:build !windows

package daemon

// viaShellIfScript exists for Windows, where an npm-installed tool is often a
// batch shim that CreateProcess cannot start.
//
// Elsewhere a script names its interpreter on the first line and the kernel
// honors it, so there is nothing to rewrite.
func viaShellIfScript(resolved string, args []string) (string, []string) {
	return resolved, args
}
