package shellpick

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

// The order is the whole of this package, and the middle of it is the part
// that was wrong.
//
// What shipped went from the setting straight to `COMSPEC`, which on Windows
// is `cmd.exe`, so the answer was always cmd on a machine where everything
// else is PowerShell. The tempting fix is `pwsh` with a fall to cmd, which is
// the same bug with an extra step: `pwsh` is a separate install and `powershell`
// is on every Windows there has ever been.

// found builds a stand-in for `exec.LookPath` that knows about exactly these
// commands. The real one answers about the machine running the test, which
// would make this a test of the build agent.
func found(names ...string) func(string) (string, error) {
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	return func(name string) (string, error) {
		if have[name] {
			return `C:\fake\` + name, nil
		}
		return "", errors.New("not found")
	}
}

func pickWith(t *testing.T, look func(string) (string, error)) (string, []string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the order under test is the Windows one")
	}
	// The package resolves once and caches, which is right in a daemon and
	// useless in a test, so both are reset around each case.
	prev := lookPath
	lookPath = look
	pickCmd, pickArgs = "", nil
	once = resetOnce()
	t.Cleanup(func() {
		lookPath = prev
		pickCmd, pickArgs = "", nil
		once = resetOnce()
	})
	return Pick()
}

func TestPowerShell7IsPreferredWhenItIsThere(t *testing.T) {
	cmd, args := pickWith(t, found("pwsh.exe", "powershell.exe"))
	if cmd != "pwsh.exe" {
		t.Fatalf("opened %q with both installed", cmd)
	}
	if len(args) == 0 || args[0] != "-NoLogo" {
		t.Fatalf("no -NoLogo, so a pane opens onto a banner: %v", args)
	}
}

// THE CASE THE ORDER WAS WRITTEN FOR. `pwsh` is a separate install and is
// missing on a stock Windows. Falling to cmd here skips the shell that is
// always present, which is the original bug wearing a newer coat.
func TestWindowsPowerShellIsUsedWhenSevenIsMissing(t *testing.T) {
	cmd, _ := pickWith(t, found("powershell.exe"))
	if cmd != "powershell.exe" {
		t.Fatalf("skipped 5.1 and opened %q", cmd)
	}
}

// The floor, and it has to exist or a shell that cannot start is reported with
// nothing to report.
func TestTheCommandShellIsTheFloor(t *testing.T) {
	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	cmd, _ := pickWith(t, found())
	if cmd != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("with no PowerShell at all it chose %q", cmd)
	}
}

// LOOKED FOR, NOT ASSUMED FROM THE PLATFORM. Seen on another machine: an ssh
// default shell pointed at a `pwsh.exe` that was not installed, and the
// session came up on 5.1 with nothing saying why.
func TestAPwshThatIsNotInstalledIsNotChosen(t *testing.T) {
	cmd, _ := pickWith(t, found("powershell.exe"))
	if cmd == "pwsh.exe" {
		t.Fatal("chose a shell that is not on this machine")
	}
}

// Resolved once, because it is a scan of every directory on PATH and it sits
// on the path a person is waiting on.
func TestTheAnswerIsResolvedOnce(t *testing.T) {
	calls := 0
	counting := func(name string) (string, error) {
		calls++
		return exec.LookPath(name)
	}
	pickWith(t, counting)
	first := calls
	Pick()
	Pick()
	if calls != first {
		t.Fatalf("looked at PATH again on later calls: %d then %d", first, calls)
	}
}

// The arguments are copied out, so a caller that appends to what it was given
// cannot reach into the cached answer and change what everybody else gets.
func TestTheCallerCannotEditTheCachedAnswer(t *testing.T) {
	_, args := pickWith(t, found("pwsh.exe"))
	if len(args) == 0 {
		t.Skip("nothing to scribble on")
	}
	args[0] = "-Command"
	_, again := Pick()
	if again[0] != "-NoLogo" {
		t.Fatalf("a caller changed the shared answer to %v", again)
	}
}
