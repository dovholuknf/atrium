package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func shelvable(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// Shelving a card atrium does not own a runner for is not a failure. A window
// mode launch owns itself and a session that joined by hand belongs to whoever
// started it, so there is nothing to stop and the card still goes down.
func TestShelvingWithoutARunnerSucceeds(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := shelvable(t, d, "unowned")
	if err := d.Shelve(task.ID); err != nil {
		t.Fatalf("shelving a card with no runner failed: %v", err)
	}
}

// Unshelving has to say why nothing started rather than looking like it worked.
// A card with no resume id has no conversation to pick up.
func TestUnshelveWithoutAResumeIDExplainsItself(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := shelvable(t, d, "no-resume")
	started, why, err := d.Unshelve(task.ID)
	if err != nil {
		t.Fatalf("unshelve returned an error instead of a reason: %v", err)
	}
	if started {
		t.Fatal("unshelve claimed to start a runner with no resume id")
	}
	if !strings.Contains(why, "resume id") {
		t.Fatalf("the reason does not name the missing piece: %q", why)
	}
}

// A runner atrium no longer has configuration for cannot be started, and the
// operator has to be told which half is missing.
func TestUnshelveWithAnUnknownRunnerExplainsItself(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := shelvable(t, d, "unknown-runner")
	if err := d.st.SetResumeID(task.ID, "sess-1"); err != nil {
		t.Fatal(err)
	}

	started, why, err := d.Unshelve(task.ID)
	if err != nil {
		t.Fatalf("unshelve returned an error instead of a reason: %v", err)
	}
	if started {
		t.Fatal("unshelve claimed to start a runner it cannot build")
	}
	// The harness `claude` exists but has no command configured on a fresh
	// database, so this fails at launch and reports that rather than panicking.
	if why == "" {
		t.Fatal("unshelve failed silently")
	}
}

// Starting onto a card that does not exist has to name the card, not fail
// somewhere deeper with a message about a directory.
func TestLaunchOntoAMissingCardSaysSo(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	_, err := d.Launch(LaunchRequest{
		Harness: "claude", Cwd: t.TempDir(), TaskID: "no-such-card",
	})
	if err == nil {
		t.Fatal("launching onto a card that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), "no-such-card") {
		t.Fatalf("the error does not name the card: %v", err)
	}
}

// Unshelving something already running must not start a second one.
func TestUnshelveRefusesWhenAlreadyRunning(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := shelvable(t, d, "already-up")
	if err := d.st.SetResumeID(task.ID, "sess-2"); err != nil {
		t.Fatal(err)
	}
	// Stand in for a live runner without spawning a process. Removed before the
	// daemon shuts down, since shutdown would otherwise try to wind down a
	// runner that has no terminal.
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	started, why, err := d.Unshelve(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started {
		t.Fatal("unshelve started a second runner for a card that already has one")
	}
	if !strings.Contains(why, "already") {
		t.Fatalf("the reason does not say it is already up: %q", why)
	}
}
