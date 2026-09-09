package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Every test here names a way a terminal failed to come back, or came back
// when it should not have.
//
// The report was "we keep restarting and i keep losing shit": six supervised
// terminals went down with the daemon and four came back, because only
// fixtures were ever started again.

// reopenDaemon is a daemon with a store and a temporary directory, and no
// listeners. Nothing here spawns a process: what is under test is which cards
// are chosen, not whether claude starts.
func reopenDaemon(t *testing.T) *Daemon {
	t.Helper()
	return testDaemon(t)
}

// THE BUG: a card with a runner that was not a fixture was left behind.
func TestWhatWasOpenIsRecordedAtTheWindDown(t *testing.T) {
	d := reopenDaemon(t)
	live := []*runner{
		{taskID: "card-a", buf: newRing(64, 80)},
		{taskID: "card-b", buf: newRing(64, 80)},
	}
	d.saveReopen(live)

	got := d.readReopen()
	if !slices.Equal(got, []string{"card-a", "card-b"}) {
		t.Fatalf("recorded %v", got)
	}
}

// AN EMPTY LIST IS A FACT, not an absence.
//
// Closing every terminal and then stopping must not reopen yesterday's set.
// Skipping the write when there is nothing open would leave the last list in
// place and do exactly that.
func TestStoppingWithNothingOpenClearsTheList(t *testing.T) {
	d := reopenDaemon(t)
	d.saveReopen([]*runner{{taskID: "card-a", buf: newRing(64, 80)}})
	if len(d.readReopen()) != 1 {
		t.Fatal("the fixture did not record anything")
	}

	d.saveReopen(nil)

	if got := d.readReopen(); len(got) != 0 {
		t.Fatalf("a stop with nothing open left %v behind", got)
	}
}

// A machine that has never run this, and every card that had no runner.
func TestNoRecordIsNormal(t *testing.T) {
	d := reopenDaemon(t)
	if got := d.readReopen(); got != nil {
		t.Fatalf("invented a list out of nothing: %v", got)
	}
}

// A daemon killed mid-write, or a file from a format that no longer exists.
// Nothing, never an error: this is a convenience over disposable state and
// refusing to start over it would be a self-inflicted outage.
func TestAnUnreadableRecordIsIgnored(t *testing.T) {
	d := reopenDaemon(t)
	if err := os.WriteFile(d.reopenPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := d.readReopen(); got != nil {
		t.Fatalf("accepted a corrupt record: %v", got)
	}
}

// WRITTEN ASIDE AND RENAMED, so a kill during the write leaves the last good
// list rather than half of this one.
func TestTheRecordIsWrittenAtomically(t *testing.T) {
	d := reopenDaemon(t)
	d.saveReopen([]*runner{{taskID: "card-a", buf: newRing(64, 80)}})
	if _, err := os.Stat(d.reopenPath() + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("left the temporary file behind")
	}
}

// WHICH CARDS ARE CHOSEN, which is the whole decision this makes.
//
// The launch itself is not exercised: `testDaemon` has no claude to spawn and
// a test that needed one would be a test of the operating system. What is
// asserted is the filter, one row at a time, because every one of these was a
// way to reopen the wrong thing.
func TestOnlyCardsWorthReopeningAreChosen(t *testing.T) {
	d := reopenDaemon(t)

	mk := func(t *testing.T, name, runner, worktree, status string) string {
		t.Helper()
		task, _, err := d.st.Register(store.Observed{
			WireName: name, Worktree: worktree, Runner: runner,
		})
		if err != nil {
			t.Fatal(err)
		}
		if status != "" {
			if err := d.st.SetStatus(task.ID, status); err != nil {
				t.Fatal(err)
			}
		}
		return task.ID
	}

	dir := t.TempDir()
	good := mk(t, "worth-reopening", "claude", dir, "")
	shelved := mk(t, "put-down-on-purpose", "claude", dir, store.StatusShelved)
	noRunner := mk(t, "no-runner-recorded", "", dir, "")
	noDir := mk(t, "no-directory-recorded", "claude", "", "")

	d.saveReopen([]*runner{
		{taskID: good, buf: newRing(64, 80)},
		{taskID: shelved, buf: newRing(64, 80)},
		{taskID: noRunner, buf: newRing(64, 80)},
		{taskID: noDir, buf: newRing(64, 80)},
		{taskID: "a card that has since been pruned", buf: newRing(64, 80)},
	})

	got := d.reopenWanted()
	var ids []string
	for _, task := range got {
		ids = append(ids, task.ID)
	}
	if !slices.Equal(ids, []string{good}) {
		t.Fatalf("chose %v, wanted just the one card worth reopening", ids)
	}
}

// A FIXTURE MUST WIN. It is on this list too, since it had a runner when the
// daemon stopped, and the fixture is what pins the card, themes it and decides
// whether it resumes from the directory or from the card. Reopening it here
// first would quietly change how a fixture starts.
func TestACardAFixtureAlreadyStartedIsSkipped(t *testing.T) {
	d := reopenDaemon(t)
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "a-fixture", Worktree: dir, Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.saveReopen([]*runner{{taskID: task.ID, buf: newRing(64, 80)}})

	// The fixture got there first.
	d.sup.add(&runner{
		taskID:   task.ID,
		buf:      newRing(64, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	})

	if got := d.reopenWanted(); len(got) != 0 {
		t.Fatalf("would have started a second runner on a card that already has one: %v", got)
	}
}

// A resume id that no longer names a conversation makes the runner exit within
// a second, which reaches the board as a dead card and a terminal that never
// appeared. Starting fresh in the right directory is worth more.
func TestAStaleResumeIsDroppedRatherThanUsed(t *testing.T) {
	d := reopenDaemon(t)
	task := &store.Task{
		ID:       "card-a",
		Worktree: t.TempDir(),
		ResumeID: "a-conversation-that-is-not-on-disk",
	}
	if got := d.reopenResume(task); got != "" {
		t.Fatalf("would have resumed %q, which is gone", got)
	}
	// And a card that never had one asks for nothing rather than for "".
	task.ResumeID = ""
	if got := d.reopenResume(task); got != "" {
		t.Fatalf("invented a resume id: %q", got)
	}
}

// The record lives beside the database, so naming another database isolates it
// and a test can never reach the machine's real one.
func TestTheRecordSitsBesideTheDatabase(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{opts: Options{DBPath: filepath.ToSlash(filepath.Join(dir, "a.db"))}}
	if filepath.Dir(d.reopenPath()) != dir {
		t.Fatalf("the record went to %q, outside the database's directory", d.reopenPath())
	}
}

// The shape on disk, locked so a future reader can tell an old file from a new
// one rather than guessing.
func TestTheRecordSaysWhenItWasWritten(t *testing.T) {
	d := reopenDaemon(t)
	d.saveReopen([]*runner{{taskID: "card-a", buf: newRing(64, 80)}})
	raw, err := os.ReadFile(d.reopenPath())
	if err != nil {
		t.Fatal(err)
	}
	var rec reopenRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.SavedAt.IsZero() {
		t.Fatal("wrote a record with no time on it")
	}
}
