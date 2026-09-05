package daemon

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// What a shell refuses, and the one thing it must never do to a card.
//
// The tests are named for the ways this gets broken rather than for the
// behaviour, because every one of them is a mistake that would compile, pass
// the other tests, and be found by an operator wondering why their card went
// grey when they closed a terminal.

func cardAt(t *testing.T, d *Daemon, name, dir string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// A shell has to open where the WORK is. Opening in the daemon's directory
// would answer no question anybody had, and it is the failure the design
// review called out as the one nothing would catch: it compiles, it runs, and
// it is silently useless.
func TestAShellOpensInTheCardsDirectory(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	dir := t.TempDir()
	task := cardAt(t, d, "with-a-shell", dir)

	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatalf("could not open a shell: %v", err)
	}
	defer d.CloseShell(task.ID)

	sh := d.sup.getShell(task.ID)
	if sh == nil {
		t.Fatal("no shell was recorded for the card")
	}
	if got := filepath.Clean(sh.cmd.Dir); got != filepath.Clean(dir) {
		t.Fatalf("the shell opened in %s, not in the card's directory %s", got, dir)
	}
}

// A card with nowhere to open must say so rather than opening somewhere.
func TestACardWithNoDirectoryCannotHaveAShell(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := cardAt(t, d, "nowhere", "")
	err := d.EnsureShell(task.ID)
	if err == nil {
		d.CloseShell(task.ID)
		t.Fatal("a card with no working directory got a shell anyway")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
}

// A directory that has since been removed is the ordinary version of the
// above, and it has to fail as an error the operator can read rather than as a
// terminal that appears and vanishes. This is the whole reason opening a shell
// is a POST and not a side effect of the websocket upgrade.
func TestAShellRefusesADirectoryThatIsGone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	gone := filepath.Join(t.TempDir(), "not-here")
	task := cardAt(t, d, "moved", gone)
	err := d.EnsureShell(task.ID)
	if err == nil {
		d.CloseShell(task.ID)
		t.Fatal("a shell opened in a directory that does not exist")
	}
	if !strings.Contains(err.Error(), "not there any more") {
		t.Fatalf("the refusal does not name the problem: %v", err)
	}
}

// Asking twice returns the same terminal.
//
// The board asks on every press rather than tracking which cards have one,
// because a board that tracked it would be wrong after a restart. So the
// second ask must not start a second shell, which would leak a process per
// press and put two terminals under one card.
func TestAskingForAShellTwiceGivesTheSameOne(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := cardAt(t, d, "twice", t.TempDir())
	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatalf("first ask failed: %v", err)
	}
	defer d.CloseShell(task.ID)
	first := d.sup.getShell(task.ID)

	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatalf("second ask failed: %v", err)
	}
	if d.sup.getShell(task.ID) != first {
		t.Fatal("asking twice started a second shell")
	}
}

// THE ONE THAT MATTERS. A shell must not be visible to anything that means
// "the process doing the work".
//
// `d.sup.get` is said fifteen times across the reaper, the park, shelving,
// actions, messages and the exit recorder, and every one of them means the
// runner. Folding shells into that map is the obvious simplification and it
// would file a card as dead the moment somebody typed `exit` in a shell.
func TestAShellIsNotARunner(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := cardAt(t, d, "not-a-runner", t.TempDir())
	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatalf("could not open a shell: %v", err)
	}
	defer d.CloseShell(task.ID)

	if d.sup.get(task.ID) != nil {
		t.Fatal("a shell turned up as the card's runner")
	}
	if d.sup.has(task.ID) {
		t.Fatal("a shell made the card look supervised")
	}
	for _, r := range d.sup.all() {
		if r.taskID == task.ID {
			t.Fatal("a shell is in the list of runners to stop, park and reap")
		}
	}
}

// Closing a shell leaves the card exactly as it was.
//
// The runner's exit path marks a card dead, appends an exit event, and can
// retry a stale resume. None of that may happen here, and the way this breaks
// is somebody reusing `awaitExit` because the two look alike.
func TestClosingAShellDoesNotTouchTheCard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := cardAt(t, d, "untouched", t.TempDir())
	before, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := d.st.Events(task.ID, 200)
	if err != nil {
		t.Fatal(err)
	}

	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatalf("could not open a shell: %v", err)
	}
	d.CloseShell(task.ID)
	// The exit path is a goroutine, so give it the chance to do the wrong
	// thing rather than passing because it had not run yet.
	time.Sleep(250 * time.Millisecond)

	after, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status {
		t.Fatalf("closing a shell moved the card from %s to %s", before.Status, after.Status)
	}
	now, err := d.st.Events(task.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(now) != len(events) {
		t.Fatalf("closing a shell wrote %d event(s) to the card's log", len(now)-len(events))
	}
	if d.sup.getShell(task.ID) != nil {
		t.Fatal("the shell is still recorded after being closed")
	}
}

// A shell knows which card it is in and deliberately does not claim to be its
// agent.
//
// Every runner carries both variables and every hook reads them to say who did
// what. A shell carrying the agent name would have anything started from it,
// including a second agent, filing activity against the card as though the
// runner had done it.
func TestAShellSaysWhichCardButNotWhichAgent(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	env := d.shellEnv("card-7")
	var sawTask bool
	for _, kv := range env {
		if kv == "ATRIUM_TASK_ID=card-7" {
			sawTask = true
		}
		if strings.HasPrefix(kv, "ATRIUM_AGENT_NAME=") {
			t.Fatalf("a shell was given an agent name: %s", kv)
		}
	}
	if !sawTask {
		t.Fatal("a shell was not told which card it is in")
	}
}

// The daemon's own environment must not leak a task id from whatever started
// it. A daemon launched from inside a supervised session has `ATRIUM_TASK_ID`
// set, and a shell inheriting that would report itself as a different card.
func TestAShellDoesNotInheritSomebodyElsesCard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	t.Setenv("ATRIUM_TASK_ID", "the-daemons-own-card")
	n := 0
	for _, kv := range d.shellEnv("card-9") {
		if strings.HasPrefix(kv, "ATRIUM_TASK_ID=") {
			n++
			if kv != "ATRIUM_TASK_ID=card-9" {
				t.Fatalf("a shell inherited %s", kv)
			}
		}
	}
	if n != 1 {
		t.Fatalf("the shell environment names a card %d times, wanted once", n)
	}
}

// The configured shell wins over what the operating system reports, because
// the one wanted is the operator's own and `COMSPEC` on a PowerShell machine
// is not it.
func TestTheConfiguredShellWins(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.st.SetSetting(SettingShellCommand, "pwsh -NoLogo"); err != nil {
		t.Fatal(err)
	}
	cmd, args := d.shellFor()
	if cmd != "pwsh" {
		t.Fatalf("the configured shell was ignored, got %q", cmd)
	}
	if len(args) != 1 || args[0] != "-NoLogo" {
		t.Fatalf("the configured arguments were dropped: %v", args)
	}
}

// With nothing configured there is still an answer, on both platforms, because
// a shell that cannot be named cannot produce an error worth reading either.
func TestThereIsAlwaysAShellToTry(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	cmd, _ := d.shellFor()
	if strings.TrimSpace(cmd) == "" {
		t.Fatal("no shell at all was worked out")
	}
	if runtime.GOOS == "windows" && !strings.Contains(strings.ToLower(cmd), "cmd") &&
		!strings.Contains(strings.ToLower(cmd), "powershell") &&
		!strings.Contains(strings.ToLower(cmd), "pwsh") {
		t.Fatalf("the windows fallback is %q, which is not a shell", cmd)
	}
}
