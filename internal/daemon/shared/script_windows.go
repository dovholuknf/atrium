//go:build windows

package shared

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
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

// ViaShellIfScript rewrites a resolved command into something CreateProcess can
// start, when what was resolved is a script. A real executable passes through
// unchanged.
//
// The arguments carry card text, and cmd.exe parses its whole line: Go quotes a
// `"` as `\"`, which cmd does not honor, so `&` and `%VAR%` in a prompt run or
// expand. An npm shim runs as the node script it wraps, with no cmd.exe, and any
// other batch file is refused an argument cmd would read as syntax.
func ViaShellIfScript(resolved string, args []string) (string, []string, error) {
	switch strings.ToLower(filepath.Ext(resolved)) {
	case ".cmd", ".bat":
		if node, script, ok := npmNodeEntry(resolved); ok {
			return node, append([]string{script}, args...), nil
		}
		for _, a := range args {
			if i := strings.IndexFunc(a, cmdLive); i >= 0 {
				return "", nil, fmt.Errorf("%s is a batch file atrium can only run through cmd.exe, and an argument "+
					"holds %q, which cmd.exe reads as syntax. refusing rather than hand card text to cmd.exe",
					resolved, a[i:i+1])
			}
		}
		// cmd.exe, not PowerShell: PowerShell re-parses the arguments it
		// forwards to a batch file, so anything containing a space, quote or
		// brace arrives changed. These arguments are flags, model names and
		// session ids the runner needs verbatim. `cmd /c` passes them through.
		//
		// /c so the shell exits with the script instead of dropping to a prompt
		// inside the pseudo terminal atrium is watching.
		return comspec(), append([]string{"/c", resolved}, args...), nil
	case ".ps1":
		return Powershell(), append([]string{
			"-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", resolved,
		}, args...), nil
	default:
		return resolved, args, nil
	}
}

// cmdLive is a character cmd.exe or a batch file's %* reads as syntax (% and !
// even inside quotes), or one that ends the line.
func cmdLive(r rune) bool {
	return r < 0x20 && r != '\t' || strings.ContainsRune(`"%!^&|<>()`, r)
}

// npmShimLine is the last line cmd-shim, npm's batch shim writer, gives a node
// shim. Only that exact shape is read, anything else falls back to cmd.exe.
var npmShimLine = regexp.MustCompile(`(?i)^.*"%_prog%"\s+"%dp0%\\([^"%]+\.[cm]?js)"\s+%\*\s*$`)

// npmNodeEntry reads an npm node shim for the node.exe and script it would run,
// choosing as the shim does: node.exe beside the shim, else node on PATH.
func npmNodeEntry(shim string) (node, script string, ok bool) {
	f, err := os.Open(shim)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil || !strings.Contains(strings.ToLower(string(b)), `set "_prog=node"`) {
		return "", "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		m := npmShimLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		dir := filepath.Dir(shim)
		script = filepath.Join(dir, m[1])
		if st, err := os.Stat(script); err != nil || st.IsDir() {
			return "", "", false
		}
		node = filepath.Join(dir, "node.exe")
		if _, err := os.Stat(node); err != nil {
			if node, err = exec.LookPath("node"); err != nil || !strings.EqualFold(filepath.Ext(node), ".exe") {
				return "", "", false
			}
		}
		return node, script, true
	}
	return "", "", false
}

// comspec is the command shell, taken from the environment so a machine
// configured to use something else is respected.
func comspec() string {
	if v := strings.TrimSpace(os.Getenv("COMSPEC")); v != "" {
		return v
	}
	return "cmd.exe"
}

// Powershell prefers pwsh when installed, falling back to the one every
// Windows install has.
func Powershell() string {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p
	}
	return "powershell.exe"
}

// NewConsole starts cmd in a console window of its own, the window mode launch with no terminal in front of it.
func NewConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_CONSOLE
	// exec.Cmd hands a nil stream NUL, which the new console never replaces: a null handle it does.
	none := os.NewFile(0, "none")
	if cmd.Stdin == nil {
		cmd.Stdin = none
	}
	if cmd.Stdout == nil {
		cmd.Stdout = none
	}
	if cmd.Stderr == nil {
		cmd.Stderr = none
	}
}
