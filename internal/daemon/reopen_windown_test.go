package daemon

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A ROOM RESTART BRINGS BACK WHAT WAS RUNNING, and only an asked exit stays
// down. On 2026-09-30 at 12:58 every supervised card on claude-sg4 came back
// `done` and none reopened: the wind-down ends each session, its SessionEnd hook
// moves the card to `done`, and the boot rules had read `done` as "somebody
// ended this".

// shellCard launches a card on a real shell in a pty, the nearest thing to a
// runner at its prompt a test can have.
func shellCard(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	cmd, args := "sh", []string{"-c", "read x"}
	if runtime.GOOS == "windows" {
		// /d: no AutoRun, so nothing in the machine's own cmd setup runs here.
		cmd, args = "cmd.exe", []string{"/d", "/k"}
	}
	if _, err := d.st.SaveHarness(store.Harness{ID: "shelltest", Label: "shell test", Enabled: true,
		Cmd: cmd, Args: args, LaunchMode: store.LaunchPTY}); err != nil {
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
	task, err := d.Launch(LaunchRequest{Harness: "shelltest", Cwd: dir})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	// Waited out, not just asked: on Windows the shell holds its directory, and
	// the test's TempDir cannot be removed while it does.
	t.Cleanup(func() {
		if r := d.sup.get(task.ID); r != nil {
			windDown(r, time.Second, d.exitKeysFor(task.ID))
		}
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && d.sup.get(task.ID) != nil; {
			time.Sleep(50 * time.Millisecond)
		}
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
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if d.sup.get(id) == nil {
			return
		}
	}
	t.Fatal("the runner did not stop")
}

func TestARestartBringsBackACardAtItsPrompt(t *testing.T) {
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
	d := testDaemon(t)
	task := shellCard(t, d)
	r := d.sup.get(task.ID)
	windDown(r, 5*time.Second, d.exitKeysFor(task.ID))
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
	if _, err := d.Launch(LaunchRequest{Harness: "shelltest", Cwd: task.Worktree, TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if asked, _ := d.st.ExitAsked(task.ID); asked {
		t.Fatal("a launch after the exit did not undo it")
	}
}
