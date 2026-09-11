// Package shellpick answers one question: which shell should atrium open for a
// person to type in.
//
// A package of its own for the same reason `internal/safepath` is one. Two
// unrelated places need the answer and neither can import the other: the
// daemon opens a card's shell, and the store seeds a `shell` harness row on
// first run. Answering it twice is how they disagree, and they already did.
// The harness said `pwsh` unconditionally and the daemon said `COMSPEC`, so on
// one machine the two shells offered by the same board were PowerShell 7 and
// cmd.
//
// WHAT IT IS NOT FOR: running a script. `script_windows.go` deliberately goes
// through `cmd /c` because PowerShell re-parses the arguments it is handed,
// and that is a correctness decision rather than a preference. This is only
// about the prompt a human is given.
package shellpick

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// The order on Windows, and it matters more than it looks.
//
// `pwsh` is PowerShell 7 and a separate install. `powershell` is 5.1 and is on
// every Windows there has ever been. `cmd` is the floor.
//
// The bug this replaces went straight from the setting to `COMSPEC`, which on
// Windows is `cmd.exe`, so the answer was always cmd on a machine where
// everything else is PowerShell. Falling to cmd when `pwsh` is missing would
// skip 5.1, which is the same bug with an extra step: the missing one is not
// the interesting case, the one that is always there is.
var windowsOrder = []struct {
	cmd  string
	args []string
}{
	// `-NoLogo` because a banner is a screen of text in a pane opened to type
	// one command in.
	{"pwsh.exe", []string{"-NoLogo"}},
	{"powershell.exe", []string{"-NoLogo"}},
}

// LOOKED FOR, NOT ASSUMED FROM THE PLATFORM.
//
// Seen while setting up a room on another machine: an ssh default shell
// pointed at a `pwsh.exe` that was not installed there, and the session came
// up on 5.1 with nothing saying why. A chooser that tests the platform and
// trusts the answer produces exactly that, one layer further in.
var lookPath = exec.LookPath

// RESOLVED ONCE. This is a scan of every directory on PATH and it runs on the
// path a shell is opened from, which is a person waiting. The answer cannot
// change while the process lives: a shell installed after the daemon started
// is found by the next restart, and the setting is the override for anybody
// who cannot wait for one.
var (
	once     = resetOnce()
	pickCmd  string
	pickArgs []string
)

// resetOnce hands back a fresh latch. Only the test uses it, and it lives here
// rather than in the test file so the reason is next to the thing it defeats:
// resolving once is correct in a daemon and useless in a test that has to ask
// several times with a different machine underneath it each time.
func resetOnce() *sync.Once { return new(sync.Once) }

// Pick is the shell to open, and the arguments to open it with.
func Pick() (string, []string) {
	once.Do(resolve)
	return pickCmd, append([]string(nil), pickArgs...)
}

// Chosen is what Pick decided, for a line in the log at startup. Somebody
// whose board opened the wrong shell needs to know what was found before they
// can say what to use instead.
func Chosen() string {
	cmd, args := Pick()
	if len(args) == 0 {
		return cmd
	}
	return cmd + " " + strings.Join(args, " ")
}

func resolve() {
	if runtime.GOOS != "windows" {
		if v := strings.TrimSpace(os.Getenv("SHELL")); v != "" {
			pickCmd = v
			return
		}
		pickCmd = "/bin/sh"
		return
	}
	for _, c := range windowsOrder {
		if _, err := lookPath(c.cmd); err == nil {
			pickCmd, pickArgs = c.cmd, c.args
			return
		}
	}
	// The floor. `COMSPEC` before the literal, because a machine that has
	// moved its command shell has said so there.
	if v := strings.TrimSpace(os.Getenv("COMSPEC")); v != "" {
		pickCmd = v
		return
	}
	pickCmd = "cmd.exe"
}
