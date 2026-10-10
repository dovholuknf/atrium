//go:build integration

package hubstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

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
	for name, mk := range logs {
		t.Run(name, func(t *testing.T) { pushLogRules(t, mk(t).log) })
		t.Run(name+" pending", func(t *testing.T) { pendingRules(t, mk(t).log) })
		t.Run(name+" clock behind", func(t *testing.T) { clockBehindRules(t, mk(t)) })
	}
}

// testLog is a PushLog that can also be put in a known state with settled rows.
type testLog interface {
	gitsync.PushLog
	Append(ctx context.Context, rows ...gitsync.PushRow) error
}

type logUnderTest struct {
	log testLog
	// setClock is the clock the log takes ids and times from, to model a machine whose clock is behind.
	setClock func(time.Time)
}

var logs = map[string]func(t *testing.T) logUnderTest{
	"memory": func(*testing.T) logUnderTest {
		m := &gitsync.MemPushLog{}
		return logUnderTest{log: m, setClock: func(at time.Time) { m.Now = func() time.Time { return at } }}
	},
	"store": func(t *testing.T) logUnderTest {
		s := open(t)
		return logUnderTest{log: s, setClock: func(at time.Time) { s.gitPushClock = func() time.Time { return at } }}
	},
}

func pushLogRules(t *testing.T, l testLog) {
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

func ownerOfRef(t *testing.T, l gitsync.PushLog, ref string) string {
	t.Helper()
	room, card, ok, err := l.Owner(context.Background(), pushRepo, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return ""
	}
	return room + "/" + card
}

// A push is written pending before git runs and settled after. A pending push owns its branch, a settled one is
// the same row done, a refused one leaves nothing, and a takeover releases only when it lands.
func pendingRules(t *testing.T, l testLog) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const x, y = "refs/heads/fix/x", "refs/heads/fix/y"

	// A new branch, pending: it is owned, it is listed, and the operator's pending push is no push yet.
	batch, err := l.Begin(ctx, push(x, "sg4", "C1", 1))
	must(err)
	if batch == "" {
		t.Fatal("no batch")
	}
	if got := ownerOfRef(t, l, x); got != "sg4/C1" {
		t.Fatalf("a pending push does not own its branch: %q", got)
	}
	pend, _ := l.Pending(ctx, pushRepo)
	if len(pend) != 1 || pend[0].Batch != batch || pend[0].Ref != x || pend[0].Card != "C1" || !pend[0].Pending {
		t.Fatalf("pending = %+v", pend)
	}
	if other, _ := l.Pending(ctx, "github/o/other"); len(other) != 0 {
		t.Fatalf("another repository has pending rows: %+v", other)
	}
	if all, _ := l.Pending(ctx, ""); len(all) != 1 {
		t.Fatalf("all pending = %+v", all)
	}
	opBatch, err := l.Begin(ctx, push("refs/heads/main", "", "", 2))
	must(err)
	if _, ok, _ := l.LastOperatorPush(ctx, pushRepo, "refs/heads/main"); ok {
		t.Fatal("a pending operator push is the last operator push")
	}
	must(l.Settle(ctx, opBatch, "refs/heads/main"))
	if at, ok, _ := l.LastOperatorPush(ctx, pushRepo, "refs/heads/main"); !ok || !at.Equal(pushAt(2)) {
		t.Fatalf("a settled operator push: %v %v", at, ok)
	}

	// Settled: the same owner, nothing pending.
	must(l.Settle(ctx, batch, x))
	if got := ownerOfRef(t, l, x); got != "sg4/C1" {
		t.Fatalf("settled, owner = %q", got)
	}
	if pend, _ := l.Pending(ctx, ""); len(pend) != 0 {
		t.Fatalf("still pending: %+v", pend)
	}

	// A push git took nothing of leaves no row, and so no owner.
	b2, err := l.Begin(ctx, push(y, "sg4", "C2", 3))
	must(err)
	must(l.Settle(ctx, b2))
	if got := ownerOfRef(t, l, y); got != "" {
		t.Fatalf("a dropped push owns %q", got)
	}
	if pend, _ := l.Pending(ctx, ""); len(pend) != 0 {
		t.Fatalf("still pending: %+v", pend)
	}

	// A push of two refs, one of which landed: the one that did is kept, the other is not.
	b3, err := l.Begin(ctx, push("refs/heads/a", "sg4", "C1", 4), push("refs/heads/b", "sg4", "C1", 4))
	must(err)
	must(l.Settle(ctx, b3, "refs/heads/b"))
	if ownerOfRef(t, l, "refs/heads/a") != "" || ownerOfRef(t, l, "refs/heads/b") != "sg4/C1" {
		t.Fatal("a half-landed push was settled whole")
	}

	// A takeover: while it is pending the old owner is the owner, when it lands the new one is, and when it
	// does not land NOTHING WAS RELEASED.
	take := push(x, "sg4", "C9", 5)
	take.ReleasedBy = "card gone"
	b4, err := l.Begin(ctx, take)
	must(err)
	if got := ownerOfRef(t, l, x); got != "sg4/C1" {
		t.Fatalf("a pending takeover owns the branch: %q", got)
	}
	must(l.Settle(ctx, b4))
	if got := ownerOfRef(t, l, x); got != "sg4/C1" {
		t.Fatalf("a takeover git refused released the branch: %q", got)
	}
	b5, err := l.Begin(ctx, take)
	must(err)
	must(l.Settle(ctx, b5, x))
	if got := ownerOfRef(t, l, x); got != "sg4/C9" {
		t.Fatalf("a takeover that landed: owner %q", got)
	}
	// And the next card is still held off by the new owner.
	b6, err := l.Begin(ctx, push(x, "sg4", "C1", 6))
	must(err)
	must(l.Settle(ctx, b6, x))
	if got := ownerOfRef(t, l, x); got != "sg4/C9" {
		t.Fatalf("after the takeover, owner %q", got)
	}
}

// IDS COME FROM THE ORDER ROWS ARE WRITTEN, never from a clock that is behind. A release written when the clock says
// an hour before the owner's push still releases, and the operator's push in that window does not take the branch
// from the card that owns it.
func clockBehindRules(t *testing.T, u logUnderTest) {
	ctx := context.Background()
	l := u.log
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	t0 := pushAt(30)
	u.setClock(t0)
	must(l.Append(ctx, push("refs/heads/a", "sg4", "C1", 30), push("refs/heads/b", "sg4", "C1", 30)))

	u.setClock(t0.Add(-time.Hour))
	// The operator pushes to the card's branch: the card is still the owner.
	must(l.Append(ctx, push("refs/heads/a", "", "", 31)))
	if got := ownerOfRef(t, l, "refs/heads/a"); got != "sg4/C1" {
		t.Fatalf("an operator push with the clock behind took the branch: owner %q", got)
	}
	// A release releases.
	must(l.Release(ctx, pushRepo, "refs/heads/b", "operator"))
	if got := ownerOfRef(t, l, "refs/heads/b"); got != "" {
		t.Fatalf("a release with the clock behind did not release: owner %q", got)
	}
	// And the next push owns it, still with the clock behind.
	must(l.Append(ctx, push("refs/heads/b", "m1mini", "D1", 32)))
	if got := ownerOfRef(t, l, "refs/heads/b"); got != "m1mini/D1" {
		t.Fatalf("the push after a release: owner %q", got)
	}
	// A takeover settled with the clock behind.
	take := push("refs/heads/a", "sg4", "C2", 33)
	take.ReleasedBy = "card gone"
	batch, err := l.Begin(ctx, take)
	must(err)
	must(l.Settle(ctx, batch, "refs/heads/a"))
	if got := ownerOfRef(t, l, "refs/heads/a"); got != "sg4/C2" {
		t.Fatalf("a takeover with the clock behind: owner %q", got)
	}
}

// A hub that is restarted, on a machine whose clock is behind what the table holds, orders what it writes after what
// it wrote. There is no memory of the last id to lose: the table is where it is read from.
func TestAReleaseAfterARestartWithTheClockBehindStillReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t0 := pushAt(40)
	s.gitPushClock = func() time.Time { return t0 }
	if err := s.Append(ctx, push("refs/heads/x", "sg4", "C1", 40)); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.gitPushClock = func() time.Time { return t0.Add(-time.Hour) }
	if err := s.Release(ctx, pushRepo, "refs/heads/x", "operator"); err != nil {
		t.Fatal(err)
	}
	if got := ownerOfRef(t, s, "refs/heads/x"); got != "" {
		t.Fatalf("after a restart with the clock behind, the release did not release: %q", got)
	}
	if err := s.Append(ctx, push("refs/heads/x", "", "", 41)); err != nil {
		t.Fatal(err)
	}
	if room, card, ok, _ := s.Owner(ctx, pushRepo, "refs/heads/x"); !ok || room != "" || card != "" {
		t.Fatalf("the operator's push after the release: %q %q %v", room, card, ok)
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
	// Begin is the same: the first row of a push is not left when the second is refused.
	if _, err := s.Begin(ctx, push("refs/heads/fine", "sg4", "C1", 1), push("refs/heads/boom", "sg4", "C1", 1)); err == nil {
		t.Fatal("begin took a row the database refused")
	}
	if pend, _ := s.Pending(ctx, ""); len(pend) != 0 {
		t.Fatalf("a refused begin left %+v", pend)
	}
}

// Settle is one transaction too: when the move to done is refused, the release marker written before it goes too.
func TestStoreSettleIsAllOrNone(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.Append(ctx, push("refs/heads/x", "sg4", "C1", 1)); err != nil {
		t.Fatal(err)
	}
	take := push("refs/heads/x", "sg4", "C2", 2)
	take.ReleasedBy = "card gone"
	batch, err := s.Begin(ctx, take)
	if err != nil {
		t.Fatal(err)
	}
	// Refuse the update that makes the row done.
	if _, err := s.db.Exec(`CREATE TRIGGER no_done BEFORE UPDATE ON git_push WHEN NEW.state = 'done'
		BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle(ctx, batch, "refs/heads/x"); err == nil {
		t.Fatal("settle took a row the database refused")
	}
	if got := ownerOfRef(t, s, "refs/heads/x"); got != "sg4/C1" {
		t.Fatalf("a settle that failed released the branch: %q", got)
	}
	if pend, _ := s.Pending(ctx, ""); len(pend) != 1 {
		t.Fatalf("the batch is not still pending: %+v", pend)
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

// THE PUSH LOG MIGRATION CAME AFTER 0007, so a hub that already has 0007 applies it. 0009 is last now, and
// is checked in changerequest_test.go.
func TestGitPushMigrationFollowsDocs(t *testing.T) {
	for i, m := range migrations {
		if m.name == "0008_git_push" {
			if i == 0 || migrations[i-1].name != "0007_docs" {
				t.Fatalf("0008_git_push is migration %d, after %q", i+1, migrations[i-1].name)
			}
			return
		}
	}
	t.Fatal("no 0008_git_push migration")
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

// A row is a push or a release, done or pending, and the table says so itself.
func TestGitPushTableRefusesAKindThatIsNeither(t *testing.T) {
	s := open(t)
	_, err := s.db.Exec(`INSERT INTO git_push (id, kind, repo, ref, at) VALUES ('1', 'delete', 'r', 'refs/heads/x', 'now')`)
	if err == nil {
		t.Fatal("a row of kind delete was taken")
	}
	if _, err := s.db.Exec(`INSERT INTO git_push (id, kind, state, repo, ref, at) VALUES ('2', 'push', 'maybe', 'r', 'refs/heads/x', 'now')`); err == nil {
		t.Fatal("a row of state maybe was taken")
	}
}
