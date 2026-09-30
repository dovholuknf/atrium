package daemon

import (
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
		cmd, args = "cmd.exe", []string{"/k"}
	}
	if _, err := d.st.SaveHarness(store.Harness{ID: "shelltest", Label: "shell test", Enabled: true,
		Cmd: cmd, Args: args, LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}
	task, err := d.Launch(LaunchRequest{Harness: "shelltest", Cwd: t.TempDir()})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() {
		if r := d.sup.get(task.ID); r != nil {
			windDown(r, time.Second, d.exitKeysFor(task.ID))
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
	if err := d.onSession(SessionEvent{Agent: task.WireName, Event: "end", Reason: "prompt_input_exit",
		TaskID: task.ID}); err != nil {
		t.Fatal(err)
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
