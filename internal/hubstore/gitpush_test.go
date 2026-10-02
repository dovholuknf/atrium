package hubstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub store's push log, and migration 0008 under it.

const pushRepo = "github/o/r"

var sha40 = strings.Repeat("a", 40)

func pushAt(min int) time.Time {
	return time.Date(2026, 10, 2, 12, min, 0, 0, time.UTC)
}

func push(ref, room, card string, min int) gitsync.PushRow {
	return gitsync.PushRow{Repo: pushRepo, Ref: ref, Old: strings.Repeat("0", 40), New: sha40,
		Room: room, Card: card, At: pushAt(min)}
}

// THE TWO IMPLEMENTATIONS OF THE LOG ARE ONE SET OF RULES. gitsync's tests run on the in-memory one, so
// every behaviour they lean on is held to the table here as well.
func TestPushLogBehavesTheSameInMemoryAndInTheStore(t *testing.T) {
	for name, mk := range map[string]func(t *testing.T) gitsync.PushLog{
		"memory": func(*testing.T) gitsync.PushLog { return &gitsync.MemPushLog{} },
		"store":  func(t *testing.T) gitsync.PushLog { return open(t) },
	} {
		t.Run(name, func(t *testing.T) { pushLogRules(t, mk(t)) })
	}
}

func pushLogRules(t *testing.T, l gitsync.PushLog) {
	ctx := context.Background()
	owner := func(ref string) string {
		t.Helper()
		room, card, ok, err := l.Owner(ctx, pushRepo, ref)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return ""
		}
		return room + "/" + card
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const fix = "refs/heads/fix/x"

	if owner(fix) != "" {
		t.Fatal("a ref never pushed has an owner")
	}
	// The first pusher owns it, and a later push by the same card does not change that.
	must(l.Append(ctx, push(fix, "sg4", "C1", 1)))
	must(l.Append(ctx, push(fix, "sg4", "C1", 2)))
	if got := owner(fix); got != "sg4/C1" {
		t.Fatalf("owner = %q", got)
	}
	// The operator's push to a card's branch leaves the owner be: first push after the release is the owner.
	must(l.Append(ctx, push(fix, "", "", 3)))
	if got := owner(fix); got != "sg4/C1" {
		t.Fatalf("after an operator push, owner = %q", got)
	}
	// Refs and repositories are separate.
	if owner("refs/heads/fix/y") != "" {
		t.Fatal("another ref has the owner")
	}
	if _, _, ok, _ := l.Owner(ctx, "github/o/other", fix); ok {
		t.Fatal("another repository has the owner")
	}

	// Release: the owner is gone, a marker row names who it was, and releasing nothing writes nothing.
	must(l.Release(ctx, pushRepo, fix, "operator"))
	if owner(fix) != "" {
		t.Fatal("released and still owned")
	}
	must(l.Release(ctx, pushRepo, fix, "operator"))
	must(l.Release(ctx, pushRepo, "refs/heads/never", "operator"))
	// The next push after it owns the branch.
	must(l.Append(ctx, push(fix, "m1mini", "D9", 5)))
	if got := owner(fix); got != "m1mini/D9" {
		t.Fatalf("after release, owner = %q", got)
	}

	// The operator's branch is owned by the operator: room and card empty, and still an owner.
	must(l.Append(ctx, push("refs/heads/ops", "", "", 6)))
	if room, card, ok, _ := l.Owner(ctx, pushRepo, "refs/heads/ops"); !ok || room != "" || card != "" {
		t.Fatalf("operator branch: %q %q %v", room, card, ok)
	}

	// Branches: heads only, each once, with owner and latest push; the released one says so.
	must(l.Append(ctx, push("refs/tags/v1", "", "", 7)))
	must(l.Append(ctx, push("refs/heads/main", "", "", 8)))
	must(l.Append(ctx, push("refs/heads/gone", "sg4", "C3", 9)))
	must(l.Release(ctx, pushRepo, "refs/heads/gone", "card gone"))
	got, err := l.Branches(ctx, pushRepo)
	must(err)
	want := []gitsync.BranchRecord{
		{Ref: fix, Room: "m1mini", Card: "D9", At: pushAt(5)},
		{Ref: "refs/heads/gone", Room: "sg4", Card: "C3", At: pushAt(9), Released: true},
		{Ref: "refs/heads/main", At: pushAt(8)},
		{Ref: "refs/heads/ops", At: pushAt(6)},
	}
	if len(got) != len(want) {
		t.Fatalf("branches = %+v", got)
	}
	for i := range want {
		g := got[i]
		g.At = g.At.UTC()
		if g != want[i] {
			t.Errorf("branch %d = %+v, want %+v", i, g, want[i])
		}
	}
	if other, _ := l.Branches(ctx, "github/o/other"); len(other) != 0 {
		t.Fatalf("another repository lists %+v", other)
	}

	// The last operator push of a ref: the operator's only, the latest, per ref.
	must(l.Append(ctx, push("refs/heads/main", "", "", 10)))
	must(l.Append(ctx, push("refs/heads/main", "sg4", "C1", 11)))
	at, ok, err := l.LastOperatorPush(ctx, pushRepo, "refs/heads/main")
	if err != nil || !ok || !at.Equal(pushAt(10)) {
		t.Fatalf("last operator push = %v %v %v", at, ok, err)
	}
	if _, ok, _ := l.LastOperatorPush(ctx, pushRepo, "refs/heads/gone"); ok {
		t.Fatal("a card's branch has an operator push")
	}
	if at, ok, _ := l.LastOperatorPush(ctx, pushRepo, fix); !ok || !at.Equal(pushAt(3)) {
		t.Fatalf("the operator's push to a card's branch: %v %v", at, ok)
	}
	if _, ok, _ := l.LastOperatorPush(ctx, "github/o/other", "refs/heads/main"); ok {
		t.Fatal("another repository has an operator push")
	}
}

// A push of two refs lands whole or not at all: the row of one must not be there without the other.
func TestStoreAppendIsAllOrNone(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER no_boom BEFORE INSERT ON git_push WHEN NEW.ref = 'refs/heads/boom'
		BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatal(err)
	}
	err := s.Append(ctx, push("refs/heads/fine", "sg4", "C1", 1), push("refs/heads/boom", "sg4", "C1", 1))
	if err == nil {
		t.Fatal("the append took a row the database refused")
	}
	if _, _, ok, _ := s.Owner(ctx, pushRepo, "refs/heads/fine"); ok {
		t.Fatal("the row before the refused one stayed")
	}
	if halted, _ := s.Halted(); halted {
		t.Fatal("a refused row halted the hub")
	}
}

// What was logged is there after the store is closed and opened again, and the order holds.
func TestPushLogSurvivesAReopenInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// The same instant for both: the order is the write order, not the clock.
	a, b := push("refs/heads/x", "sg4", "C1", 1), push("refs/heads/x", "sg4", "C2", 1)
	if err := s.Append(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, b); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if room, card, ok, _ := s.Owner(ctx, pushRepo, "refs/heads/x"); !ok || room != "sg4" || card != "C1" {
		t.Fatalf("owner = %q %q %v", room, card, ok)
	}
}

// IDS SORT IN WRITE ORDER even when the clock repeats or steps back.
func TestPushIDsGrowWhateverTheClockSays(t *testing.T) {
	at := pushAt(1)
	prev := nextPushID(at)
	for _, step := range []time.Duration{0, 0, -time.Hour, -time.Hour, time.Millisecond} {
		at = at.Add(step)
		id := nextPushID(at)
		if id <= prev {
			t.Fatalf("%s did not sort after %s", id, prev)
		}
		prev = id
	}
}

// ── migration 0008 ──────────────────────────────────────

func TestGitPushMigrationToleratesBeingThere(t *testing.T) {
	s := open(t)
	for _, m := range migrations {
		if m.name != "0008_git_push" {
			continue
		}
		for _, st := range m.stmts {
			if _, err := s.db.Exec(st); err != nil {
				t.Fatalf("running %q a second time: %v", st[:40], err)
			}
		}
		return
	}
	t.Fatal("no 0008_git_push migration")
}

// THE PUSH LOG MIGRATION IS LAST, so a hub that already has 0007 applies it.
func TestGitPushMigrationIsAtTheEnd(t *testing.T) {
	if got := migrations[len(migrations)-1].name; got != "0008_git_push" {
		t.Fatalf("the last migration is %q", got)
	}
}

// A hub database from before the push log, which has every earlier migration recorded and no table, gets the
// table on the next Open and keeps what it had.
func TestGitPushMigrationAppliesToAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := added(t, s, "sg4")
	// Put the database back to how 0007 left it.
	if _, err := s.db.Exec(`DROP TABLE git_push`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0008_git_push'`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopening a database from before 0008: %v", err)
	}
	defer s.Close()
	if err := s.Append(context.Background(), push("refs/heads/x", "sg4", "C1", 1)); err != nil {
		t.Fatalf("the table is not there: %v", err)
	}
	if rooms, _ := s.Rooms(); len(rooms) != 1 || rooms[0].ID != r.ID {
		t.Fatalf("the rooms it had are gone: %+v", rooms)
	}
}

// A row is a push or a release, and the table says so itself.
func TestGitPushTableRefusesAKindThatIsNeither(t *testing.T) {
	s := open(t)
	_, err := s.db.Exec(`INSERT INTO git_push (id, kind, repo, ref, at) VALUES ('1', 'delete', 'r', 'refs/heads/x', 'now')`)
	if err == nil {
		t.Fatal("a row of kind delete was taken")
	}
}
