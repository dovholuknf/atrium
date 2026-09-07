package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE OTHER HALF OF THE REAPER. It decides what is alive, so it has to run in
// both directions: a card in `running` whose process is gone goes to dead, and
// a card in `dead` with a runner atrium still owns comes back.
//
// This is what stops the bug reappearing through a path nobody has written
// yet. The launch corrects the card it starts onto; this corrects the card
// whatever moved it.

func TestTheReaperBringsBackADeadCardItStillOwnsARunnerFor(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	task, _, err := st.Register(store.Observed{
		WireName: "wrongly-dead", Worktree: "/tmp/atrium-wrongly-dead", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	// Atrium owns a runner for it, which is the fact the status contradicts.
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	if err := d.reapOnce(); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("a card with a live runner on it is still %q", got.Status)
	}
	if got.WaitingReason != store.WaitingStarted {
		t.Fatalf("the revived card does not say why it is waiting: %q", got.WaitingReason)
	}
}

// A card the sweep already archived is the symptom, not an exception. It is
// off the board and `List` cannot see it, which is why the check asks the
// supervisor rather than the board, and why coming back has to clear the
// archive stamp.
func TestTheReaperBringsBackAnArchivedCardWithALiveRunner(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	task, _, err := st.Register(store.Observed{
		WireName: "swept-away", Worktree: "/tmp/atrium-swept-away", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if err := st.BackdateActivity(task.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Archive(time.Minute, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	if err := d.reapOnce(); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("an archived card with a live runner is still %q", got.Status)
	}
	if got.ArchivedAt != nil {
		t.Fatal("the card came back but is still archived, so the board cannot see it")
	}
}

// A SHELVED CARD IS NOT REVIVED, even if a runner is somehow still attached.
// Shelving is a decision, and the reaper corrects atrium's own guesses rather
// than overruling the operator.
func TestTheReaperLeavesAShelvedCardAlone(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	task, _, err := st.Register(store.Observed{
		WireName: "put-down-hard", Worktree: "/tmp/atrium-put-down-hard", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	if err := d.reapOnce(); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusShelved {
		t.Fatalf("the reaper unshelved a card the operator put down: now %q", got.Status)
	}
}

// And a dead card with nothing on it stays dead. Reviving on the strength of
// the card's own stale pid would keep resurrecting sessions that really ended,
// because the operating system recycles pids.
func TestTheReaperLeavesADeadCardWithNoRunnerAlone(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	// A pid that is very much alive, on a card atrium owns no runner for.
	task, _, err := st.Register(store.Observed{
		WireName: "really-dead", Worktree: "/tmp/atrium-really-dead", Runner: "claude",
		PID: os.Getpid(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}

	if err := d.reapOnce(); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusDead {
		t.Fatalf("a dead card with no runner was revived to %q", got.Status)
	}
}
