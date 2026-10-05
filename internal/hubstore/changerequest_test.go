package hubstore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// Change requests, and migration 0009 under them.

func newCR(branch, target string) CRNew {
	return CRNew{Repo: "github/o/r", SourceRoom: "sg3", SourceBranch: branch, SourceSHA: sha40, Target: target,
		Title: "add the thing", Why: "so it works", CreatedBy: CRParty{Room: "sg3", Card: "C1"},
		Owner: CRParty{Room: "sg3", Card: "C1"}}
}

func mustCR(t *testing.T, s *Store, in CRNew) ChangeRequest {
	t.Helper()
	c, existed, err := s.CRCreate(in)
	if err != nil || existed {
		t.Fatalf("create: %+v existed=%v err=%v", c, existed, err)
	}
	return c
}

func TestCRCreateGivesSequentialIDsAndTheShapeTheAPIPromises(t *testing.T) {
	s := open(t)
	a := mustCR(t, s, newCR("claude/x", "main"))
	b := mustCR(t, s, newCR("claude/y", "main"))
	if a.ID != "cr_1" || b.ID != "cr_2" {
		t.Fatalf("ids %q %q", a.ID, b.ID)
	}
	if a.State != CROpen || a.ClosedAt != nil || a.ClosedBy != nil || a.MergedSHA != nil || a.CreatedAt == "" {
		t.Fatalf("a new request: %+v", a)
	}
	if a.Source != (CRSource{Room: "sg3", Branch: "claude/x", SHA: sha40}) || a.Target.Branch != "main" ||
		a.Owner != (CRParty{Room: "sg3", Card: "C1"}) || a.CreatedBy.Card != "C1" || a.Change != "" || a.Note != "" {
		t.Fatalf("a new request: %+v", a)
	}
	got, err := s.CRGet("cr_2")
	if err != nil || got.ID != "cr_2" || got.Source.Branch != "claude/y" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.CRGet("cr_99"); !errors.Is(err, ErrCRNotFound) {
		t.Fatalf("an id that is not there: %v", err)
	}
}

// AN ID IS NEVER REUSED: it is the highest n plus one, and a finished request still counts.
func TestCRIDsAreNotReusedAfterARequestEnds(t *testing.T) {
	s := open(t)
	a := mustCR(t, s, newCR("claude/x", "main"))
	if _, err := s.CREnd(a.ID, CRClosed, CRParty{Card: CROperator}, "no", ""); err != nil {
		t.Fatal(err)
	}
	if b := mustCR(t, s, newCR("claude/x", "main")); b.ID != "cr_2" {
		t.Fatalf("the next id after a closed cr_1 is %q", b.ID)
	}
}

func TestCRAnOpenRequestForTheSameSourceAndTargetIsHandedBack(t *testing.T) {
	s := open(t)
	a := mustCR(t, s, newCR("claude/x", "main"))
	again := newCR("claude/x", "main")
	again.Title = "a different title"
	b, existed, err := s.CRCreate(again)
	if err != nil || !existed || b.ID != a.ID || b.Title != a.Title {
		t.Fatalf("duplicate: %+v existed=%v err=%v", b, existed, err)
	}
	// Another target, another source branch, another source room: each is a request of its own.
	other := newCR("claude/x", "claude/main")
	hub := newCR("claude/x", "main")
	hub.SourceRoom = ""
	for _, in := range []CRNew{other, newCR("claude/z", "main"), hub} {
		if c, existed, err := s.CRCreate(in); err != nil || existed {
			t.Fatalf("%+v: %+v existed=%v err=%v", in, c, existed, err)
		}
	}
	// A closed one may be asked again.
	if _, err := s.CREnd(a.ID, CRWithdrawn, CRParty{Room: "sg3", Card: "C1"}, "", ""); err != nil {
		t.Fatal(err)
	}
	if c, existed, err := s.CRCreate(again); err != nil || existed || c.ID == a.ID {
		t.Fatalf("after withdrawn: %+v existed=%v err=%v", c, existed, err)
	}
}

// A ROOM NAME IS ONE NAME in any case, as it is everywhere else: two asks that differ only in the source room's case
// are one request, and the room is stored folded so the table's own index agrees.
func TestCRRoomsThatDifferOnlyInCaseAreOneSource(t *testing.T) {
	s := open(t)
	in := newCR("claude/x", "main")
	in.SourceRoom = "Room-A"
	a := mustCR(t, s, in)
	if a.Source.Room != "room-a" {
		t.Fatalf("the room is stored as %q", a.Source.Room)
	}
	for _, room := range []string{"room-a", "ROOM-A", "rOOm-A"} {
		again := in
		again.SourceRoom = room
		b, existed, err := s.CRCreate(again)
		if err != nil || !existed || b.ID != a.ID {
			t.Fatalf("%q: %+v existed=%v err=%v", room, b, existed, err)
		}
	}
	if rows, _ := s.CRList(CRFilter{}); len(rows) != 1 {
		t.Fatalf("%d requests, want 1", len(rows))
	}
	// and the list finds it by any case of the room
	for _, room := range []string{"room-a", "ROOM-A"} {
		if rows, _ := s.CRList(CRFilter{Room: room}); len(rows) != 1 {
			t.Fatalf("room %q finds %d", room, len(rows))
		}
	}
}

// A REPOSITORY IS ONE NAME in any case (github/O/R is github/o/r): two asks that differ only there are one open
// request, and the list finds it by either spelling. The row keeps the spelling of the first ask.
func TestCRReposThatDifferOnlyInCaseAreOneRepo(t *testing.T) {
	s := open(t)
	in := newCR("claude/x", "main")
	in.Repo = "github/O/R"
	a := mustCR(t, s, in)
	if a.Repo != "github/O/R" {
		t.Fatalf("the repo is stored as %q", a.Repo)
	}
	for _, repo := range []string{"github/o/r", "GITHUB/O/R", "github/o/R"} {
		again := in
		again.Repo = repo
		b, existed, err := s.CRCreate(again)
		if err != nil || !existed || b.ID != a.ID {
			t.Fatalf("%q: %+v existed=%v err=%v", repo, b, existed, err)
		}
	}
	if rows, _ := s.CRList(CRFilter{}); len(rows) != 1 {
		t.Fatalf("%d requests, want 1", len(rows))
	}
	for _, repo := range []string{"github/o/r", "github/O/R", "GITHUB/o/R"} {
		if rows, _ := s.CRList(CRFilter{Repo: repo}); len(rows) != 1 {
			t.Fatalf("repo %q finds %d", repo, len(rows))
		}
	}
	// another repo is not folded into it
	other := in
	other.Repo = "github/o/r2"
	if b, existed, err := s.CRCreate(other); err != nil || existed || b.ID == a.ID {
		t.Fatalf("another repo: %+v existed=%v err=%v", b, existed, err)
	}
}

func TestCRTheTableItselfRefusesASecondOpenRequest(t *testing.T) {
	s := open(t)
	mustCR(t, s, newCR("claude/x", "main"))
	_, err := s.db.Exec(`INSERT INTO change_request (id, n, repo, source_room, source_branch, target_branch, title,
		created_card, created_at) VALUES ('cr_9', 9, 'github/o/r', 'sg3', 'claude/x', 'main', 't', 'C1', 'now')`)
	if err == nil {
		t.Fatal("a second open row for the same source and target was taken")
	}
	if _, err := s.db.Exec(`INSERT INTO change_request (id, n, repo, source_branch, target_branch, title, state,
		created_card, created_at) VALUES ('cr_8', 8, 'r', 'b', 'main', 't', 'gone', 'C1', 'now')`); err == nil {
		t.Fatal("a row of state gone was taken")
	}
}

func TestCRRefusesWhatCannotBeStored(t *testing.T) {
	s := open(t)
	long := func(n int) string { return strings.Repeat("a", n) }
	for name, mod := range map[string]func(*CRNew){
		"no title":           func(n *CRNew) { n.Title = "  " },
		"a title over 200":   func(n *CRNew) { n.Title = long(201) },
		"a why over 4000":    func(n *CRNew) { n.Why = long(4001) },
		"a newline in title": func(n *CRNew) { n.Title = "a\nb" },
		"an escape in why":   func(n *CRNew) { n.Why = "a\x1b[31mb" },
		"a nul in why":       func(n *CRNew) { n.Why = "a\x00b" },
		"a bidi override":    func(n *CRNew) { n.Title = "a‮b" },
		"a line separator":   func(n *CRNew) { n.Why = "a b" },
		"no repo":            func(n *CRNew) { n.Repo = "" },
		"no source branch":   func(n *CRNew) { n.SourceBranch = "" },
		"no target":          func(n *CRNew) { n.Target = "" },
	} {
		in := newCR("claude/x", "main")
		mod(&in)
		if _, _, err := s.CRCreate(in); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	// The bounds themselves are in, and a why may have lines and tabs.
	in := newCR("claude/x", "main")
	in.Title, in.Why = long(200), strings.Repeat("a\n\tb ", 800)
	if _, _, err := s.CRCreate(in); err != nil {
		t.Fatalf("a request at the bounds: %v", err)
	}
	if rows, _ := s.CRList(CRFilter{}); len(rows) != 1 {
		t.Fatalf("a refused request left %d rows", len(rows))
	}
}

func TestCRListFilters(t *testing.T) {
	s := open(t)
	a := mustCR(t, s, newCR("claude/a", "main"))
	bIn := newCR("claude/b", "claude/main")
	bIn.SourceRoom, bIn.Owner = "sg4", CRParty{Room: "sg4", Card: "C2"}
	b := mustCR(t, s, bIn)
	// A hub branch: no source room, owned by a card on m1mini.
	cIn := newCR("fix/c", "main")
	cIn.Repo, cIn.SourceRoom, cIn.Owner = "github/o/other", "", CRParty{Room: "m1mini", Card: "C3"}
	c := mustCR(t, s, cIn)
	if _, err := s.CREnd(a.ID, CRMerged, CRParty{Card: CROperator}, "", sha40); err != nil {
		t.Fatal(err)
	}

	ids := func(f CRFilter) string {
		t.Helper()
		rows, err := s.CRList(f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return strings.Join(out, ",")
	}
	for name, tc := range map[string]struct {
		f    CRFilter
		want string
	}{
		"all":                 {CRFilter{}, c.ID + "," + b.ID + "," + a.ID},
		"open":                {CRFilter{State: CROpen}, c.ID + "," + b.ID},
		"merged":              {CRFilter{State: CRMerged}, a.ID},
		"closed (none)":       {CRFilter{State: CRClosed}, ""},
		"finished (merged)":   {CRFilter{Finished: true}, a.ID},
		"the source room":     {CRFilter{Room: "sg4"}, b.ID},
		"a room, any case":    {CRFilter{Room: "SG4"}, b.ID},
		"the owner's room":    {CRFilter{Room: "m1mini"}, c.ID},
		"a room with nothing": {CRFilter{Room: "sg9"}, ""},
		"a target":            {CRFilter{Target: "main"}, c.ID + "," + a.ID},
		"a repo":              {CRFilter{Repo: "github/o/other"}, c.ID},
		"open in a room":      {CRFilter{State: CROpen, Room: "sg3"}, ""},
		"two filters":         {CRFilter{Target: "main", Room: "sg3"}, a.ID},
	} {
		if got := ids(tc.f); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestCREndIsOnceAndRecordsWhoAndWhen(t *testing.T) {
	s := open(t)
	a := mustCR(t, s, newCR("claude/x", "main"))
	got, err := s.CREnd(a.ID, CRClosed, CRParty{Room: "sg3", Card: "C9"}, "wrong branch", "")
	if err != nil || got.State != CRClosed || got.Note != "wrong branch" || got.ClosedAt == nil ||
		got.ClosedBy == nil || *got.ClosedBy != (CRParty{Room: "sg3", Card: "C9"}) || got.MergedSHA != nil {
		t.Fatalf("closed: %+v %v", got, err)
	}
	// Final, whichever way it is tried, and the row is what it was.
	for _, to := range []string{CRClosed, CRWithdrawn, CRMerged} {
		again, err := s.CREnd(a.ID, to, CRParty{Card: CROperator}, "later", sha40)
		if !errors.Is(err, ErrCRFinal) || again.State != CRClosed || again.Note != "wrong branch" {
			t.Fatalf("%s after closed: %+v %v", to, again, err)
		}
	}
	if now, _ := s.CRGet(a.ID); now.ClosedBy.Card != "C9" || now.MergedSHA != nil {
		t.Fatalf("a refused change moved the row: %+v", now)
	}
	if _, err := s.CREnd("cr_77", CRClosed, CRParty{}, "", ""); !errors.Is(err, ErrCRNotFound) {
		t.Fatalf("an id that is not there: %v", err)
	}
	if _, err := s.CREnd(a.ID, CROpen, CRParty{}, "", ""); err == nil {
		t.Fatal("a request was ended as open")
	}
	b := mustCR(t, s, newCR("claude/y", "main"))
	if m, err := s.CREnd(b.ID, CRMerged, CRParty{Card: CROperator}, "", sha40); err != nil || m.MergedSHA == nil || *m.MergedSHA != sha40 {
		t.Fatalf("merged: %+v %v", m, err)
	}
	if _, err := s.CREnd(b.ID, CRClosed, CRParty{}, strings.Repeat("a", CRNoteMax+1), ""); err == nil {
		t.Fatal("an overlong note was taken")
	}
}

// ── migration 0009 ──────────────────────────────────────

func TestChangeRequestMigrationToleratesBeingThere(t *testing.T) {
	s := open(t)
	for _, m := range migrations {
		if m.name != "0009_change_request" {
			continue
		}
		for _, st := range m.stmts {
			if _, err := s.db.Exec(st); err != nil {
				t.Fatalf("running %q a second time: %v", st[:40], err)
			}
		}
		return
	}
	t.Fatal("no 0009_change_request migration")
}

// 0009 FOLLOWS 0008, so a hub that already has the push log applies it and what came after, and 0010 (the PR claim
// table) follows it. 0011 (backlog and reports) is the last.
func TestChangeRequestMigrationIsAtTheEnd(t *testing.T) {
	n := len(migrations)
	if got := migrations[n-1].name; got != "0011_backlog_reports" {
		t.Fatalf("the last migration is %q", got)
	}
	if got := migrations[n-2].name; got != "0010_pr_claim" {
		t.Fatalf("the one before it is %q", got)
	}
	if got := migrations[n-3].name; got != "0009_change_request" {
		t.Fatalf("the one before that is %q", got)
	}
	if got := migrations[n-4].name; got != "0008_git_push" {
		t.Fatalf("the one before that is %q", got)
	}
}

func TestChangeRequestMigrationAppliesToAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := added(t, s, "sg4")
	// Put the database back to how 0008 left it.
	if _, err := s.db.Exec(`DROP TABLE change_request`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0009_change_request'`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopening a database from before 0009: %v", err)
	}
	defer s.Close()
	if c := mustCR(t, s, newCR("claude/x", "main")); c.ID != "cr_1" {
		t.Fatalf("the first id is %q", c.ID)
	}
	if rooms, _ := s.Rooms(); len(rooms) != 1 || rooms[0].ID != r.ID {
		t.Fatalf("the rooms it had are gone: %+v", rooms)
	}
}

// A table that is already there, from a build that made it by hand, is the state 0009 wants: it is recorded as
// applied and the rows in it stay.
func TestChangeRequestMigrationKeepsATableThatWasAlreadyThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustCR(t, s, newCR("claude/x", "main"))
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0009_change_request'`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopening with the table there: %v", err)
	}
	defer s.Close()
	if rows, _ := s.CRList(CRFilter{}); len(rows) != 1 {
		t.Fatalf("the row it had: %+v", rows)
	}
}
