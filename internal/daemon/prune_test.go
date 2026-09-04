package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Pruning on a timer, which is a different operation from sweeping and has to
// stay one.
//
// Sweeping archives: the card leaves the board and every word of its history
// stays. Pruning deletes and takes the history with it. That is why one is on
// by default and the other is off.

func finishedCard(t *testing.T, d *Daemon, name, status string, age time.Duration) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, status); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		if err := d.st.BackdateActivity(task.ID, age); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func alive(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	_, err := d.st.Get(id)
	return err == nil
}

// Nobody has turned it on, so nothing is deleted. This is the case that has to
// be right, because the alternative is a board that quietly eats its history.
func TestPruningIsOffUnlessAskedFor(t *testing.T) {
	d := testDaemon(t)
	old := finishedCard(t, d, "prune-1", store.StatusDone, 400*24*time.Hour)

	if err := d.pruneOld(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, d, old.ID) {
		t.Fatal("a card was deleted with no pruning configured")
	}
}

func TestPruningDeletesWhatIsOldEnoughAndNothingElse(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingPruneAfter, "86400"); err != nil {
		t.Fatal(err)
	}

	old := finishedCard(t, d, "prune-old", store.StatusDone, 72*time.Hour)
	recent := finishedCard(t, d, "prune-recent", store.StatusDone, time.Hour)
	dead := finishedCard(t, d, "prune-dead", store.StatusDead, 72*time.Hour)
	shelved := finishedCard(t, d, "prune-shelved", store.StatusShelved, 72*time.Hour)
	running := finishedCard(t, d, "prune-running", store.StatusRunning, 72*time.Hour)

	if err := d.pruneOld(); err != nil {
		t.Fatal(err)
	}

	if alive(t, d, old.ID) {
		t.Fatal("a card older than the age survived")
	}
	if alive(t, d, dead.ID) {
		t.Fatal("a dead card older than the age survived")
	}
	if !alive(t, d, recent.ID) {
		t.Fatal("a card younger than the age was deleted")
	}
	// Shelving says the work is coming back. A sweep that took shelved cards
	// would discard exactly the ones kept on purpose.
	if !alive(t, d, shelved.ID) {
		t.Fatal("a shelved card was pruned")
	}
	if !alive(t, d, running.ID) {
		t.Fatal("a running card was pruned")
	}
}

// An offered item nobody started is still work somebody found. Deleting it on
// a schedule would make the inbox quietly lossy, which is worse than an inbox
// with something stale in it.
func TestPruningLeavesTheInboxAlone(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingPruneAfter, "3600"); err != nil {
		t.Fatal(err)
	}
	offered, _, err := d.st.Offer(store.IntakeItem{
		Source: "github", ExternalID: "4211", Title: "old news",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(offered.ID, 400*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	if err := d.pruneOld(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, d, offered.ID) {
		t.Fatal("an offered item was deleted by the pruning timer")
	}
}

// `off` is a value and it means never, whatever else is true.
func TestOffMeansNever(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingPruneAfter, "off"); err != nil {
		t.Fatal(err)
	}
	old := finishedCard(t, d, "prune-off", store.StatusDone, 400*24*time.Hour)
	if err := d.pruneOld(); err != nil {
		t.Fatal(err)
	}
	if !alive(t, d, old.ID) {
		t.Fatal("a card was deleted with pruning switched off")
	}
}

// A mistyped setting must not become "delete everything that finished". The
// floor is far shorter than anybody would choose and far longer than an
// accident.
func TestAnAbsurdlyShortAgeIsRefused(t *testing.T) {
	d := testDaemon(t)
	for _, v := range []string{"1", "60", "3599", "-5", "banana"} {
		if err := d.st.SetSetting(store.SettingPruneAfter, v); err != nil {
			t.Fatal(err)
		}
		if _, on := d.pruneAfter(); on {
			t.Fatalf("%q was accepted as an age to delete cards at", v)
		}
	}
	if err := d.st.SetSetting(store.SettingPruneAfter, "3600"); err != nil {
		t.Fatal(err)
	}
	if got, on := d.pruneAfter(); !on || got != time.Hour {
		t.Fatalf("an hour came back as %v on=%v", got, on)
	}
}

// Sweeping and pruning are different operations, and a card being archived
// does not put it out of reach of the one that deletes.
func TestAnArchivedCardIsStillPrunable(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingPruneAfter, "3600"); err != nil {
		t.Fatal(err)
	}
	card := finishedCard(t, d, "prune-archived", store.StatusDead, 72*time.Hour)
	if _, err := d.st.Archive(0, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("the card under test was not archived")
	}

	if err := d.pruneOld(); err != nil {
		t.Fatal(err)
	}
	if alive(t, d, card.ID) {
		t.Fatal("an archived card outlived the pruning timer, so archived rows grow forever")
	}
}
