//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Launching a command that is not an executable.
//
// Tools installed through npm are batch shims on Windows: `codex` resolves to
// `codex.cmd`, `claude` often to `claude.cmd`. CreateProcess cannot start a
// batch file, and reports 0x80070002, "The system cannot find the file
// specified", for a file that is sitting on PATH.
//
// The error names the wrong cause, which sends you to PATH, permissions and the
// inherited environment. A shell has to go in front of it instead.

// viaShellIfScript rewrites a resolved command into something CreateProcess can
// start, when what was resolved is a script. A real executable passes through
// unchanged.
func viaShellIfScript(resolved string, args []string) (string, []string) {
	switch strings.ToLower(filepath.Ext(resolved)) {
	case ".cmd", ".bat":
		// cmd.exe, not PowerShell: PowerShell re-parses the arguments it
		// forwards to a batch file, so anything containing a space, quote or
		// brace arrives changed. These arguments are flags, model names and
		// session ids the runner needs verbatim. `cmd /c` passes them through.
		//
		// /c so the shell exits with the script instead of dropping to a prompt
		// inside the pseudo terminal atrium is watching.
		return comspec(), append([]string{"/c", resolved}, args...)
	case ".ps1":
		return powershell(), append([]string{
			"-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", resolved,
		}, args...)
	default:
		return resolved, args
	}
}

// comspec is the command shell, taken from the environment so a machine
// configured to use something else is respected.
func comspec() string {
	if v := strings.TrimSpace(os.Getenv("COMSPEC")); v != "" {
		return v
	}
	return "cmd.exe"
}

// powershell prefers pwsh when installed, falling back to the one every
// Windows install has.
func powershell() string {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p
	}
	return "powershell.exe"
}
