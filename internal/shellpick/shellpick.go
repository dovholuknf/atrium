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
// Picker resolves the shell once and keeps the answer. A test builds its own with a `look` of its own.
type Picker struct {
	look func(string) (string, error)
	once sync.Once
	cmd  string
	args []string
}

// std is the one the daemon asks, which looks at the machine.
var std = &Picker{look: exec.LookPath}

// RESOLVED ONCE. This is a scan of every directory on PATH and it runs on the
// path a shell is opened from, which is a person waiting. The answer cannot
// change while the process lives: a shell installed after the daemon started
// is found by the next restart, and the setting is the override for anybody
// who cannot wait for one.

// Pick is the shell to open, and the arguments to open it with.
func Pick() (string, []string) { return std.Pick() }

// Pick is the shell this picker resolved, and the arguments to open it with.
func (p *Picker) Pick() (string, []string) {
	p.once.Do(p.resolve)
	return p.cmd, append([]string(nil), p.args...)
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

func (p *Picker) resolve() {
	if runtime.GOOS != "windows" {
		if v := strings.TrimSpace(os.Getenv("SHELL")); v != "" {
			p.cmd = v
			return
		}
		p.cmd = "/bin/sh"
		return
	}
	for _, c := range windowsOrder {
		if _, err := p.look(c.cmd); err == nil {
			p.cmd, p.args = c.cmd, c.args
			return
		}
	}
	// The floor. `COMSPEC` before the literal, because a machine that has
	// moved its command shell has said so there.
	if v := strings.TrimSpace(os.Getenv("COMSPEC")); v != "" {
		p.cmd = v
		return
	}
	p.cmd = "cmd.exe"
}
