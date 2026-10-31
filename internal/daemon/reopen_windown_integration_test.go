//go:build integration

package daemon

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// shellCard launches a card on a real shell in a pty, the nearest thing to a
// runner at its prompt a test can have.
func shellCard(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	cmd, args := "sh", []string{"-c", "read x"}
	if runtime.GOOS == "windows" {
		// /d: no AutoRun, so nothing in the machine's own cmd setup runs here.
		cmd, args = "cmd.exe", []string{"/d", "/k"}
	}
	// SEALED, whatever this shell's own environment holds: a home with no hooks
	// in it, and no daemon to reach. The daemon adds its own ATRIUM_LOCATION
	// (tellWhereIAm), which names the test daemon's file.
	home := t.TempDir()
	sealed := map[string]string{"HOME": home, "USERPROFILE": home, "ATRIUM_HUB_URL": "http://127.0.0.1:1",
		"ATRIUM_BOARD_URL": "http://127.0.0.1:1"}
	if _, err := d.st.SaveHarness(store.Harness{ID: "shelltest", Label: "shell test", Enabled: true,
		Cmd: cmd, Args: args, LaunchMode: store.LaunchPTY, Env: sealed}); err != nil {
		t.Fatal(err)
	}
	// NOT t.TempDir: on Windows the console host lets go of the directory a moment
	// after the shell exits, and TempDir fails the test when it cannot remove it.
	dir, err := os.MkdirTemp("", "atrium-reopen-")
	if err != nil {
		t.Fatal(err)
	}
	// Best effort, and never a failure: a directory the console host still holds
	// is left in the temp directory.
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	task, err := d.Launch(shared.LaunchRequest{Harness: "shelltest", Cwd: dir})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	// Waited out, not just asked: on Windows the shell holds its directory, and
	// the test's TempDir cannot be removed while it does.
	t.Cleanup(func() {
		if r := d.sup.get(task.ID); r != nil {
			windDown(r, time.Second, d.exitKeysFor(task.ID), d.hk.windDownKeyGap)
		}
		testwait.Poll(10*time.Second, func() bool { return d.sup.get(task.ID) == nil })
	})
	return task
}

// windDownLike is what a daemon stopping does to one card: the runner is
// recorded as open and ended, and its SessionEnd hook lands.
func windDownLike(t *testing.T, d *Daemon, task *store.Task) {
	t.Helper()
	d.saveReopen([]*runner{d.sup.get(task.ID)})
	if !d.stopOne(task.ID, 5*time.Second) {
		t.Fatal("the card had no runner to stop")
	}
	waitGone(t, d, task.ID)
	// THE RUNNER IS NAMED, as the real hook names it. Left out, the hook takes it
	// for claude, and the reopen then started a real claude in the test's
	// directory, whose own hooks put it on the live board.
	if err := d.onSession(SessionEvent{Agent: task.WireName, Event: "end", Reason: "prompt_input_exit",
		TaskID: task.ID, Runner: "shelltest"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(task.ID); got.Runner != "shelltest" {
		t.Fatalf("the wind-down made the card's runner %q, and a reopen would start that", got.Runner)
	}
}

func waitGone(t *testing.T, d *Daemon, id string) {
	t.Helper()
	testwait.Eventually(t, 10*time.Second, "the runner to stop", func() bool { return d.sup.get(id) == nil })
}

func TestARestartBringsBackACardAtItsPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	windDownLike(t, d, task)
	if got, _ := d.st.Get(task.ID); got.Status != store.StatusDone {
		t.Logf("after the wind-down the card is %q", got.Status)
	}
	d.reopenSaved()
	if d.sup.get(task.ID) == nil {
		t.Fatal("a card at its prompt when the daemon stopped did not come back")
	}
}

func TestARestartBringsBackAFixtureAtItsPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	f, err := d.st.SaveFixture(&store.Fixture{Label: "shell", Harness: "shelltest", Cwd: task.Worktree, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.NoteFixtureTask(f.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	windDownLike(t, d, task)
	d.startFixtures()
	if d.sup.get(task.ID) == nil {
		row, _ := d.st.GetFixture(f.ID)
		t.Fatalf("a fixture at its prompt when the daemon stopped did not come back: %+v", row)
	}
}

// A TERMINATE KEEPS A CARD DOWN like an asked exit.
func TestAKilledCardStaysDown(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	d.saveReopen([]*runner{d.sup.get(task.ID)})
	if err := d.Kill(task.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, d, task.ID)
	if got := d.reopenWanted(); len(got) != 0 {
		t.Fatalf("a card somebody terminated would reopen: %v", got)
	}
}

// /exit TYPED IN THE CARD'S OWN TERMINAL keeps it down, and the same ending
// during a wind-down does not.
func TestATypedExitStaysDownAndAWindDownDoesNot(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	end := SessionEvent{Agent: task.WireName, Event: "end", Reason: "prompt_input_exit", TaskID: task.ID,
		Runner: "shelltest"}
	d.windingDown.Store(true)
	if err := d.onSession(end); err != nil {
		t.Fatal(err)
	}
	if asked, _ := d.st.ExitAsked(task.ID); asked {
		t.Fatal("a session ended by the wind-down reads as asked to exit")
	}
	d.windingDown.Store(false)
	if err := d.onSession(end); err != nil {
		t.Fatal(err)
	}
	if asked, _ := d.st.ExitAsked(task.ID); !asked {
		t.Fatal("/exit typed in the card's own terminal is not kept")
	}
}

// AN EXIT ATRIUM TYPES ITSELF IS NOT A PERSON DECIDING (r-new-review-b144c66a). The
// idle park and the worktree sweep go through windDown, and the session's end
// that follows must not keep the card, or its fixture, down.
func TestAnExitAtriumTypesDoesNotKeepACardDown(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	r := d.sup.get(task.ID)
	windDown(r, 5*time.Second, d.exitKeysFor(task.ID), d.hk.windDownKeyGap)
	if err := d.onSession(SessionEvent{Agent: task.WireName, Event: "end", Reason: "prompt_input_exit",
		TaskID: task.ID, Runner: "shelltest"}); err != nil {
		t.Fatal(err)
	}
	if asked, _ := d.st.ExitAsked(task.ID); asked {
		t.Fatal("an exit atrium typed itself reads as somebody asking")
	}
}

// AN ASKED EXIT STAYS DOWN across the restart, and a launch afterwards undoes it.
func TestAnAskedExitStaysDown(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	d.saveReopen([]*runner{d.sup.get(task.ID)})
	if err := d.StopRunner(task.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, d, task.ID)
	d.reopenSaved()
	if d.sup.get(task.ID) != nil {
		t.Fatal("a card somebody asked to exit came back on the restart")
	}
	if asked, _ := d.st.ExitAsked(task.ID); !asked {
		t.Fatal("the asked exit is not recorded")
	}
	if _, err := d.Launch(shared.LaunchRequest{Harness: "shelltest", Cwd: task.Worktree, TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if asked, _ := d.st.ExitAsked(task.ID); asked {
		t.Fatal("a launch after the exit did not undo it")
	}
}

// A DAEMON TELLS ITS CHILDREN WHERE IT IS when it keeps its own address file, so
// their hooks reach it and never the machine's shared file, which names the live
// room. A test daemon is exactly that daemon.
func TestAChildIsToldWhereItsDaemonIs(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	env := map[string]string{}
	d.TellWhereIAm(env)
	if env["ATRIUM_LOCATION"] == "" || env["ATRIUM_LOCATION"] != d.opts.LocationFile {
		t.Fatalf("a runner is told %q, want the daemon's own file %q", env["ATRIUM_LOCATION"], d.opts.LocationFile)
	}
	found := ""
	for _, kv := range d.shellEnv("card") {
		if strings.HasPrefix(kv, "ATRIUM_LOCATION=") {
			found = strings.TrimPrefix(kv, "ATRIUM_LOCATION=")
		}
	}
	if found != d.opts.LocationFile {
		t.Fatalf("a card's shell is told %q, want %q", found, d.opts.LocationFile)
	}
}
