package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A room restart brought back a card that was exited, into another card's
// conversation (r-new-reopen-resumes-exited-card). It was the fixture: the
// `atrium` fixture's card was ended at 11:20, and the 11:28 room deploy started
// it again, resuming the newest conversation in the shared checkout, which was
// the orchestrator's on the other room.

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
	if got := d.fixtureResume(f, mine.ID); got != "" {
		t.Fatalf("the fixture resumed %q, which a live card holds", got)
	}
	// Once that card is gone, the conversation is the directory's again.
	d.sup.remove(other.ID)
	if err := d.st.SetStatus(other.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if got := d.fixtureResume(f, mine.ID); got != theirs {
		t.Fatalf("with its holder gone the fixture resumed %q, want %s", got, theirs)
	}
}
