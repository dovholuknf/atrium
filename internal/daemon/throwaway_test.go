package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Every test here names a way a throwaway session fails to be thrown away, or
// throws away something it was never given.

// aThrowaway is a card in a temporary directory atrium made, with a file in it
// so that a directory which was not deleted can be told from one that never
// had anything in it.
func aThrowaway(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	dir, err := makeThrowawayDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.FromSlash(dir)) })
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(dir), "notes.md"),
		[]byte("an hour of work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{
		WireName: "tryout", Worktree: dir, Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// THE POINT OF THE FEATURE: the directory and the card both go.
func TestTheEndOfAThrowawayTakesItsDirectoryAndItsCard(t *testing.T) {
	d := testDaemon(t)
	task := aThrowaway(t, d)

	d.endThrowaway(task.ID)

	if _, err := os.Stat(filepath.FromSlash(task.Worktree)); !os.IsNotExist(err) {
		t.Fatalf("%s is still there", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card outlived its directory")
	}
}

// AN ORDINARY CARD IS NOT TOUCHED, and this is asked of the same call every
// runner exit makes. A session in somebody's repository must be able to end
// without its directory being considered.
func TestAnOrdinaryCardIsNotThrownAway(t *testing.T) {
	d := testDaemon(t)
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "real-work", Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	d.endThrowaway(task.ID)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("an ordinary directory was deleted: %v", err)
	}
	if _, err := d.st.Get(task.ID); err != nil {
		t.Fatal("an ordinary card was deleted")
	}
}

// THE GUARD. A card that says it is temporary while pointing at a directory
// atrium did not make is a bug, and carrying it out would cost somebody their
// work. Nothing is deleted and the card stays, so the mistake is visible.
func TestAThrowawayPointingSomewhereRealIsRefused(t *testing.T) {
	d := testDaemon(t)
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "mislabelled", Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}

	d.endThrowaway(task.ID)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("a directory atrium never made was deleted: %v", err)
	}
	if _, err := d.st.Get(task.ID); err != nil {
		t.Fatal("the card went even though the directory did not")
	}
}

// PROMOTING IS THE UNDO, and it happens at the end of the session because that
// is when the directory is free to move.
func TestAPromotedThrowawayMovesInsteadOfGoing(t *testing.T) {
	d := testDaemon(t)
	task := aThrowaway(t, d)
	to := filepath.Join(t.TempDir(), "kept")
	if err := d.st.SetPromoteTo(task.ID, to); err != nil {
		t.Fatal(err)
	}

	d.endThrowaway(task.ID)

	if _, err := os.Stat(filepath.Join(to, "notes.md")); err != nil {
		t.Fatalf("the work did not arrive at %s: %v", to, err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal("the card was deleted despite being promoted")
	}
	if got.Throwaway || got.PromoteTo != "" {
		t.Fatalf("the card is still temporary: throwaway=%v promote_to=%q",
			got.Throwaway, got.PromoteTo)
	}
	if got.Worktree != filepath.ToSlash(to) {
		t.Fatalf("the card still points at %s", got.Worktree)
	}
}

// A RESTART MUST NOT REOPEN ONE. Its directory is gone, so the launch would
// fail and leave a dead card after every restart with nothing on it to say
// why. See B2-06, which is what made this possible.
func TestAThrowawayIsNotReopened(t *testing.T) {
	d := testDaemon(t)
	task := aThrowaway(t, d)
	other, _, err := d.st.Register(store.Observed{
		WireName: "real-work", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.saveReopen([]*runner{
		{taskID: task.ID, buf: newRing(64, 80)},
		{taskID: other.ID, buf: newRing(64, 80)},
	})

	var ids []string
	for _, w := range d.reopenWanted() {
		ids = append(ids, w.ID)
	}
	if !slices.Equal(ids, []string{other.ID}) {
		t.Fatalf("reopening %v, wanted only the ordinary card", ids)
	}
}

// THE BACKSTOP, for the case that will happen: the daemon was killed, so
// nothing waited on the runner and nothing cleaned up after it.
func TestAThrowawayLeftBySomethingThatDiedIsSweptAtStartup(t *testing.T) {
	d := testDaemon(t)
	task := aThrowaway(t, d)

	d.sweepThrowaways()

	if _, err := os.Stat(filepath.FromSlash(task.Worktree)); !os.IsNotExist(err) {
		t.Fatalf("%s survived the sweep", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card survived the sweep")
	}
}
