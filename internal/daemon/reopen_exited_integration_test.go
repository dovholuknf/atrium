//go:build integration

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func endedCard(t *testing.T, d *Daemon, name, dir string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: dir, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	// What StopRunner writes when somebody asks. `done` alone is what every
	// wind-down leaves behind, and is not an ending.
	if err := d.st.SetExitAsked(task.ID); err != nil {
		t.Fatal(err)
	}
	return task
}

// A CARD ASKED TO EXIT STAYS DOWN, even when its runner was still at the prompt when
// the daemon stopped and so is on the list of what was open.
func TestAnEndedCardIsNotReopened(t *testing.T) {
	d := reopenDaemon(t)
	task := endedCard(t, d, "exited", t.TempDir())
	d.saveReopen([]*runner{{taskID: task.ID, buf: newRing(64, 80)}})
	if got := d.reopenWanted(); len(got) != 0 {
		t.Fatalf("would reopen a card that was ended: %v", got)
	}
}

// A FIXTURE WHOSE CARD WAS ASKED TO EXIT DOES NOT START AT BOOT, and its row says why,
// which is where somebody looks for a fixture that did not come up.
func TestAFixtureWhoseCardWasEndedDoesNotStart(t *testing.T) {
	d := reopenDaemon(t)
	dir := t.TempDir()
	task := endedCard(t, d, "atrium-87300", dir)
	f, err := d.st.SaveFixture(&store.Fixture{Label: "atrium", Harness: "claude", Cwd: dir, Resume: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.NoteFixtureTask(f.ID, task.ID); err != nil {
		t.Fatal(err)
	}
	d.startFixtures()
	if d.sup.get(task.ID) != nil {
		t.Fatal("the fixture started a card that was ended")
	}
	if got, _ := d.st.Get(task.ID); got.Status != store.StatusDone {
		t.Fatalf("the ended card is %q after the boot", got.Status)
	}
	row, err := d.st.GetFixture(f.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(row)
	if !strings.Contains(string(raw), "asked to exit") {
		t.Fatalf("the fixture row does not say why it did not start: %s", raw)
	}
}

// A RESUME NEVER TAKES A CONVERSATION ANOTHER LIVE CARD HOLDS. The newest
// conversation in a shared checkout is often somebody else's.
func TestAFixtureDoesNotResumeAConversationALiveCardHolds(t *testing.T) {
	d := reopenDaemon(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, dir))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const theirs = "c6fdd51f-fc37-438d-9149-3d2bac73868a"
	if err := os.WriteFile(filepath.Join(proj, theirs+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mine, _, _ := d.st.Register(store.Observed{WireName: "fixture-card", Worktree: dir, Runner: "claude"})
	other, _, _ := d.st.Register(store.Observed{WireName: "orchestrator", Worktree: dir, Runner: "claude"})
	if err := d.st.SetResumeID(other.ID, theirs); err != nil {
		t.Fatal(err)
	}
	f := &store.Fixture{Label: "atrium", Cwd: dir, Resume: true}

	d.sup.add(&runner{taskID: other.ID, buf: newRing(64, 80), watchers: map[chan []byte]struct{}{},
		done: make(chan struct{})})
	if got, _ := d.fixtureResume(f, mine.ID); got != "" {
		t.Fatalf("the fixture resumed %q, which a live card holds", got)
	}
	// A NEWER DEAD HOLDER DOES NOT HIDE THE OLDER LIVE ONE (r-new-review-0b4e3f0d).
	stale, _, _ := d.st.Register(store.Observed{WireName: "stale-copy", Worktree: dir, Runner: "claude"})
	// Registered after the live holder, so it is the newer of the two.
	if err := d.st.SetResumeID(stale.ID, theirs); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(stale.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	got, held := d.fixtureResume(f, mine.ID)
	if got != "" || held == nil || held.holder.ID != other.ID {
		t.Fatalf("a newer dead holder hid the live one: resumed %q, held %+v", got, held)
	}
	if err := d.st.ClearResumeID(stale.ID); err != nil {
		t.Fatal(err)
	}
	// Once that card is gone, the conversation is the directory's again.
	d.sup.remove(other.ID)
	if err := d.st.SetStatus(other.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.fixtureResume(f, mine.ID); got != theirs {
		t.Fatalf("with its holder gone the fixture resumed %q, want %s", got, theirs)
	}
}

// ONE CONVERSATION, ONE CARD, OVER THE WHOLE REOPEN LIST: the first card on it
// takes the conversation and the second, which would have found the first not
// running yet, starts fresh.
func TestTwoCardsOnTheReopenListShareNoConversation(t *testing.T) {
	d := reopenDaemon(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := t.TempDir()
	proj := filepath.Join(home, ".claude", "projects", strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, dir))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const conv = "0b4e3f0d-0000-4000-8000-000000000001"
	if err := os.WriteFile(filepath.Join(proj, conv+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, _ := d.st.Register(store.Observed{WireName: "first", Worktree: dir, Runner: "claude"})
	b, _, _ := d.st.Register(store.Observed{WireName: "second", Worktree: dir, Runner: "claude"})
	for _, id := range []string{a.ID, b.ID} {
		if err := d.st.SetResumeID(id, conv); err != nil {
			t.Fatal(err)
		}
	}
	a, _ = d.st.Get(a.ID)
	b, _ = d.st.Get(b.ID)
	d.bootResumes.Store(conv, a.ID)
	defer d.bootResumes.Clear()
	if got := d.reopenResume(a); got != conv {
		t.Fatalf("the first card on the list resumed %q, want %s", got, conv)
	}
	if got := d.reopenResume(b); got != "" {
		t.Fatalf("the second card on the list resumed %q too", got)
	}
}

// A FIXTURE'S FIRST START NEVER HALTS THE STORE (r-new-review-c184ae8c). It has no
// card until it launches, and a refused resume noted on card "" failed the
// event's foreign key, which halted the room. The note lands on the card it made.
func TestAFixtureFirstStartNeverHaltsTheStore(t *testing.T) {
	d := testDaemon(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	holder := shellCard(t, d)
	// The fixture's own directory, so it has no card to adopt, holding a newest
	// conversation that the live holder elsewhere owns.
	dir, err := os.MkdirTemp("", "atrium-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir = filepath.ToSlash(dir)
	proj := filepath.Join(home, ".claude", "projects", strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, dir))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	const conv = "c184ae8c-0000-4000-8000-000000000001"
	if err := os.WriteFile(filepath.Join(proj, conv+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(holder.ID, conv); err != nil {
		t.Fatal(err)
	}
	f, err := d.st.SaveFixture(&store.Fixture{Label: "shared", Harness: "shelltest", Cwd: dir,
		Resume: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	d.startFixtures()
	if halted, cause := d.st.Halted(); halted {
		t.Fatalf("a fixture's first start halted the store: %v", cause)
	}
	row, _ := d.st.GetFixture(f.ID)
	if row.TaskID == "" || row.TaskID == holder.ID {
		t.Fatalf("the fixture did not start a card of its own: %+v", row)
	}
	t.Cleanup(func() {
		if r := d.sup.get(row.TaskID); r != nil {
			windDown(r, time.Second, d.exitKeysFor(row.TaskID))
		}
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && d.sup.get(row.TaskID) != nil; {
			time.Sleep(50 * time.Millisecond)
		}
	})
	evs, _ := d.st.Events(row.TaskID, 50)
	said := false
	for _, e := range evs {
		said = said || strings.Contains(string(e.Payload), "started fresh rather than resume")
	}
	if !said {
		t.Fatal("the refused resume is not on the card the fixture made")
	}
}
