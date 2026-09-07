package store

import (
	"os"
	"strings"
	"testing"
)

// The defect these are written against: a card held ONE question, in three
// columns, so asking a second thing overwrote the first with nothing anywhere
// recording that it had been asked.
//
//	atrium ask --continue "which of these two schemas is authoritative"
//	atrium ask "which branch is base"
//
// One of those got answered. The other was gone.

func askCard(t *testing.T, s *Store, name string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{
		WireName: name, Worktree: "d:/git/atrium", Runner: "claude", PID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// THE BUG, PINNED. Two questions, both still there.
func TestASecondAskDoesNotDestroyTheFirst(t *testing.T) {
	s := open(t)
	task := askCard(t, s, "asker")

	if _, err := s.AddAsk(task.ID, "which of these two schemas is authoritative", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAsk(task.ID, "which branch is base", ""); err != nil {
		t.Fatal(err)
	}

	outstanding, err := s.OpenAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 2 {
		t.Fatalf("asking twice left %d questions, so one was destroyed", len(outstanding))
	}
	if !strings.Contains(outstanding[0].Text, "authoritative") {
		t.Fatalf("the first question is not first: %q", outstanding[0].Text)
	}

	// And the card draws the OLDEST, so the board keeps working and the one
	// that has waited longest is never hidden behind a fresher question.
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Ask, "authoritative") {
		t.Fatalf("the card is drawing %q rather than the oldest outstanding question", got.Ask)
	}
}

// Answering one leaves the other standing, and the card moves on to it. This
// is what makes the card a queue that drains rather than a single slot.
func TestAnsweringOneAskLeavesTheOtherStanding(t *testing.T) {
	s := open(t)
	task := askCard(t, s, "asker")

	first, err := s.AddAsk(task.ID, "which schema is authoritative", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAsk(task.ID, "which branch is base", ""); err != nil {
		t.Fatal(err)
	}

	settled, err := s.AnswerAsk(first.ID, "the operator")
	if err != nil {
		t.Fatal(err)
	}
	if !settled {
		t.Fatal("answering an outstanding question reported that it was not")
	}

	outstanding, err := s.OpenAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 1 {
		t.Fatalf("answering one question settled %d of them", 2-len(outstanding))
	}
	if !strings.Contains(outstanding[0].Text, "base") {
		t.Fatalf("the wrong question survived: %q", outstanding[0].Text)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Ask, "base") {
		t.Fatalf("the card did not move on to the next question: %q", got.Ask)
	}

	// Answering it twice is not an error and does not claim to have settled
	// anything, because two doors reach this and they must not both report a
	// success for one act.
	again, err := s.AnswerAsk(first.ID, "the operator")
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("answering an already answered question claimed to settle it")
	}
}

// A card can be waiting on a peer for one thing and on you for another. A peer
// replying settles only what it was asked, or a question meant for a human
// disappears off the board without ever being answered.
func TestAPeerAskAndAHumanAskCoexist(t *testing.T) {
	s := open(t)
	task := askCard(t, s, "asker")

	if _, err := s.AddAsk(task.ID, "which schema is authoritative", "helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAsk(task.ID, "do you want the postgres path stubbed", ""); err != nil {
		t.Fatal(err)
	}

	settled, err := s.AnswerAsksFrom(task.ID, "helper", "helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(settled) != 1 || !strings.Contains(settled[0].Text, "authoritative") {
		t.Fatalf("a peer's answer settled %v", settled)
	}

	outstanding, err := s.OpenAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 1 || !strings.Contains(outstanding[0].Text, "postgres") {
		t.Fatalf("the peer's answer took the human's question off the card too: %v", outstanding)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AskPeer != "" {
		t.Fatalf("the card still names %q as owing it an answer", got.AskPeer)
	}
	if !got.Asking() {
		t.Fatal("the card stopped asking while a human question was still outstanding")
	}

	// A peer with nothing outstanding settles nothing, rather than clearing
	// whatever the card happens to be drawing.
	settled, err = s.AnswerAsksFrom(task.ID, "someone-else", "someone-else")
	if err != nil {
		t.Fatal(err)
	}
	if len(settled) != 0 {
		t.Fatalf("an unasked peer settled %d question(s)", len(settled))
	}
}

// The cap holds, it drops the OLDEST, and it says so. A model in a loop must
// not be able to turn a card into a wall, and a question thrown away silently
// is the defect this file exists to fix wearing different clothes.
func TestTheAskCapHoldsAndRecordsWhatItDropped(t *testing.T) {
	s := open(t)
	task := askCard(t, s, "flood")

	for i := 0; i < MaxOpenAsks+3; i++ {
		if _, err := s.AddAsk(task.ID, "question "+string(rune('a'+i)), ""); err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
	}

	outstanding, err := s.OpenAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != MaxOpenAsks {
		t.Fatalf("%d questions are outstanding, the cap is %d", len(outstanding), MaxOpenAsks)
	}
	// The NEWEST survives, because that is the one the session is stopped on.
	if !strings.HasSuffix(outstanding[len(outstanding)-1].Text, string(rune('a'+MaxOpenAsks+2))) {
		t.Fatalf("the newest question was the one dropped: %q", outstanding[len(outstanding)-1].Text)
	}

	all, err := s.AllAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != MaxOpenAsks+3 {
		t.Fatalf("%d rows survived, so a dropped question left no trace", len(all))
	}
	dropped := 0
	for _, a := range all {
		if a.AnsweredBy == askDropped {
			dropped++
		}
	}
	if dropped != 3 {
		t.Fatalf("%d questions say they were dropped, expected 3", dropped)
	}
}

// A long question is truncated rather than refused, the same rule the recap
// follows, and the truncation lands on a rune boundary so the board never
// renders half a character.
func TestALongAskIsTruncatedOnARuneBoundary(t *testing.T) {
	s := open(t)
	task := askCard(t, s, "wordy")

	a, err := s.AddAsk(task.ID, strings.Repeat("é", MaxAsk), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Text) > MaxAsk+8 {
		t.Fatalf("stored at %d bytes", len(a.Text))
	}
	if !strings.ContainsRune(a.Text, 'é') || strings.Contains(a.Text, "\ufffd") {
		t.Fatalf("truncation broke the text: %q", a.Text)
	}
}

// A card upgraded from the three columns keeps the question it was holding.
// Dropping the outstanding ask on upgrade is the exact loss 0044 exists to
// stop, so the migration has to carry it.
func TestTheMigrationCarriesAnAskFromTheOldColumns(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task := askCard(t, s, "upgraded")

	// The pre-0044 shape: a live ask in the columns, no row, and the migration
	// not yet recorded.
	if _, err := s.db.Exec(
		`UPDATE task SET ask = ?, ask_at = ?, ask_peer = ? WHERE id = ?`,
		"which of these two schemas is authoritative", ts(now()), "helper", task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM ask`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`DELETE FROM schema_migration WHERE name = '0044_ask_table'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading a database with a live ask failed, which would halt a daemon: %v", err)
	}
	defer reopened.Close()

	outstanding, err := reopened.OpenAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 1 {
		t.Fatalf("the upgrade carried %d questions across, expected 1", len(outstanding))
	}
	if !strings.Contains(outstanding[0].Text, "authoritative") {
		t.Fatalf("the question came across as %q", outstanding[0].Text)
	}
	if outstanding[0].Peer != "helper" {
		t.Fatalf("the upgrade lost who was asked: %q", outstanding[0].Peer)
	}

	// And it is not carried twice. The migration is recorded by name so it
	// cannot run again, but the guard is what makes a repair run safe.
	if err := reopened.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.db.Exec(
		`DELETE FROM schema_migration WHERE name = '0044_ask_table'`); err != nil {
		t.Fatal(err)
	}
	if err := reopened.migrate(); err != nil {
		t.Fatal(err)
	}
	outstanding, err = reopened.OpenAsks(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 1 {
		t.Fatalf("re-running the migration duplicated the question into %d rows", len(outstanding))
	}
}

// The same carry, against a COPY of a real database, which is the rule in
// internal/store/CLAUDE.md: a migration is only proved by the rows that were
// already there. Copy the database out, point this at the copy, delete the
// copy.
func TestTheAskMigrationAgainstACopyOfALiveDatabase(t *testing.T) {
	path := os.Getenv("ATRIUM_LIVE_COPY")
	if path == "" {
		t.Skip("set ATRIUM_LIVE_COPY to a COPY of a real atrium.db")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("opening a copy of a real database failed, so the migration would halt a daemon: %v", err)
	}
	defer s.Close()

	var carried, columns int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ask`).Scan(&carried); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM task WHERE ask != ''`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d ask row(s), %d card(s) still drawing one", carried, columns)
	if carried < columns {
		t.Fatalf("%d cards hold an ask and only %d rows came across", columns, carried)
	}

	// A copy where nothing happens to be asking proves nothing about carrying
	// an ask, and that is the usual case. So plant one on a real card the way
	// the old code would have, roll 0044 back, and reopen. It writes to the
	// COPY, which is why the rule is to work on one.
	var id string
	if err := s.db.QueryRow(
		`SELECT id FROM task ORDER BY created_at DESC LIMIT 1`).Scan(&id); err != nil {
		t.Skipf("this copy has no cards to plant an ask on: %v", err)
	}
	if _, err := s.db.Exec(
		`UPDATE task SET ask = ?, ask_at = ?, ask_peer = ? WHERE id = ?`,
		"which of these two schemas is authoritative", ts(now()), "helper", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM ask WHERE task_id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`DELETE FROM schema_migration WHERE name = '0044_ask_table'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("upgrading a real database with a live ask failed: %v", err)
	}
	defer reopened.Close()
	outstanding, err := reopened.OpenAsks(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 1 || outstanding[0].Peer != "helper" {
		t.Fatalf("the ask on a real card came across as %v", outstanding)
	}
	t.Logf("carried %q (asked %s) across on card %s",
		outstanding[0].Text, outstanding[0].Peer, id)
}
