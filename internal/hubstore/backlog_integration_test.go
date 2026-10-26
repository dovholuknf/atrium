//go:build integration

package hubstore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newItem(id, dept string) ItemNew {
	return ItemNew{ID: id, Dept: dept, Title: "do the thing", Body: "why\n\tand how", Priority: "HIGH",
		FiledBy: "orch@sg4", FiledRoom: "SG4"}
}

func TestItemFileThenGetAndListFromAnotherRoom(t *testing.T) {
	s := open(t)
	b, err := s.ItemFile(newItem("f-new-x", "fabric"))
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != ItemOpen || b.FiledRoom != "sg4" || b.CreatedAt == "" || b.ChangedBy != "orch@sg4" {
		t.Fatalf("filed: %+v", b)
	}
	if _, err := s.ItemFile(newItem("r-037", "runtime")); err != nil {
		t.Fatal(err)
	}
	got, err := s.ItemGet("f-new-x")
	if err != nil || got != b {
		t.Fatalf("get: %+v %v", got, err)
	}
	all, _ := s.ItemList(ItemFilter{})
	if len(all) != 2 || all[0].Dept != "fabric" || all[1].Dept != "runtime" {
		t.Fatalf("list: %+v", all)
	}
	only, _ := s.ItemList(ItemFilter{Dept: "runtime"})
	if len(only) != 1 || only[0].ID != "r-037" {
		t.Fatalf("by dept: %+v", only)
	}
}

func TestItemFileUnderATakenIDKeepsTheFirst(t *testing.T) {
	s := open(t)
	first, _ := s.ItemFile(newItem("f-new-x", "fabric"))
	again := newItem("f-new-x", "fabric")
	again.Title = "something else"
	got, err := s.ItemFile(again)
	if !errors.Is(err, ErrItemExists) || got.Title != first.Title {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestItemFileRefusesWhatCannotBeFiled(t *testing.T) {
	s := open(t)
	for name, mod := range map[string]func(*ItemNew){
		"upper id":    func(n *ItemNew) { n.ID = "F-X" },
		"path id":     func(n *ItemNew) { n.ID = "../x" },
		"empty title": func(n *ItemNew) { n.Title = "  " },
		"bad dept":    func(n *ItemNew) { n.Dept = "Fabric" },
		"long title":  func(n *ItemNew) { n.Title = strings.Repeat("a", ItemTitleMax+1) },
		"control":     func(n *ItemNew) { n.Title = "a\x00b" },
	} {
		n := newItem("f-x", "fabric")
		mod(&n)
		if _, err := s.ItemFile(n); err == nil {
			t.Errorf("%s: filed", name)
		}
	}
	if all, _ := s.ItemList(ItemFilter{}); len(all) != 0 {
		t.Fatalf("something was written: %+v", all)
	}
}

func TestItemStatusAndOpenFilter(t *testing.T) {
	s := open(t)
	_, _ = s.ItemFile(newItem("a", "fabric"))
	_, _ = s.ItemFile(newItem("b", "fabric"))
	b, err := s.ItemSetStatus("a", ItemDone, "fabric@m1mini")
	if err != nil || b.Status != ItemDone || b.ChangedBy != "fabric@m1mini" {
		t.Fatalf("set: %+v %v", b, err)
	}
	open, _ := s.ItemList(ItemFilter{Open: true})
	if len(open) != 1 || open[0].ID != "b" {
		t.Fatalf("open: %+v", open)
	}
	if _, err := s.ItemSetStatus("a", "bogus", "x"); err == nil {
		t.Fatal("bogus status accepted")
	}
	if _, err := s.ItemSetStatus("nope", ItemHeld, "x"); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestReportsAreAppendedListedAndReadOnce(t *testing.T) {
	s := open(t)
	a, err := s.ReportAdd(ReportNew{FromBy: "fabric@m1mini", FromRoom: "M1Mini", Subject: "landed", Body: "all good"})
	if err != nil || a.ID != "rp_1" || a.FromRoom != "m1mini" || a.ReadAt != nil {
		t.Fatalf("add: %+v %v", a, err)
	}
	b, _ := s.ReportAdd(ReportNew{ToDept: "fabric", Subject: "second"})
	if b.ID != "rp_2" {
		t.Fatalf("id %q", b.ID)
	}
	list, _ := s.ReportList(ReportFilter{Unread: true})
	if len(list) != 2 || list[0].ID != "rp_2" {
		t.Fatalf("newest first: %+v", list)
	}
	r, err := s.ReportRead("rp_1", "orch@sg4")
	if err != nil || r.ReadAt == nil || *r.ReadBy != "orch@sg4" {
		t.Fatalf("read: %+v %v", r, err)
	}
	again, _ := s.ReportRead("rp_1", "someone-else")
	if *again.ReadBy != "orch@sg4" {
		t.Fatalf("first reader lost: %+v", again)
	}
	unread, _ := s.ReportList(ReportFilter{Unread: true})
	if len(unread) != 1 || unread[0].ID != "rp_2" {
		t.Fatalf("unread: %+v", unread)
	}
	forFabric, _ := s.ReportList(ReportFilter{ToDept: "fabric"})
	if len(forFabric) != 1 {
		t.Fatalf("to dept: %+v", forFabric)
	}
	if _, err := s.ReportRead("rp_9", "x"); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := s.ReportAdd(ReportNew{Subject: " "}); err == nil {
		t.Fatal("empty subject accepted")
	}
}

func TestAnItemFiledWithNoIDTakesTheNextNumberOfItsPrefix(t *testing.T) {
	s := open(t)
	for _, id := range []string{"f-002", "f-029", "f-new-thing", "r-037", "rnd-new-x", "u-4"} {
		d := map[string]string{"f": "fabric", "r": "runtime", "u": "ui", "rnd": "rnd"}[strings.SplitN(id, "-", 2)[0]]
		if _, err := s.ItemFile(newItem(id, d)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ dept, want string }{
		{"fabric", "f-030"}, {"fabric", "f-031"}, {"runtime", "r-038"}, {"ui", "u-005"}, {"rnd", "rnd-001"}, {"release", "m-001"}, {"docs", "d-001"}, {"docs", "d-002"},
	} {
		n := newItem("", c.dept)
		b, err := s.ItemFile(n)
		if err != nil || b.ID != c.want {
			t.Errorf("%s: got %q, %v, want %s", c.dept, b.ID, err, c.want)
		}
	}
	if _, err := s.ItemFile(newItem("", "nowhere")); err == nil || !IsRefusal(err) {
		t.Errorf("a department with no prefix took an id: %v", err)
	}
}

func TestItemUpsertIsIdempotentAndKeepsTheCard(t *testing.T) {
	s := open(t)
	n := newItem("f-003", "fabric")
	b, how, err := s.ItemUpsert(n, ItemHeld)
	if err != nil || how != "added" || b.Status != ItemHeld {
		t.Fatalf("first: %+v %s %v", b, how, err)
	}
	if _, how, _ = s.ItemUpsert(n, ItemHeld); how != "unchanged" {
		t.Errorf("a second import of the same file was %s", how)
	}
	if _, err := s.ItemLink("f-003", "sg4~c1", "orch"); err != nil {
		t.Fatal(err)
	}
	n.Title = "moved"
	b, how, err = s.ItemUpsert(n, ItemDone)
	if err != nil || how != "updated" || b.Title != "moved" || b.Status != ItemDone || b.Card != "sg4~c1" {
		t.Fatalf("changed file: %+v %s %v", b, how, err)
	}
	if _, _, err := s.ItemUpsert(n, "wat"); err == nil {
		t.Error("an unknown status was imported")
	}
}

func TestACardsReportMovesTheItemLinkedToIt(t *testing.T) {
	s := open(t)
	for _, id := range []string{"f-1", "f-2"} {
		if _, err := s.ItemFile(newItem(id, "fabric")); err != nil {
			t.Fatal(err)
		}
	}
	b, err := s.ItemLink("f-1", "sg4~c1", "orch")
	if err != nil || b.Status != ItemInProgress || b.Card != "sg4~c1" {
		t.Fatalf("link: %+v %v", b, err)
	}
	if _, err := s.ItemLink("nope", "sg4~c9", "orch"); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("link of an unknown item: %v", err)
	}
	for _, c := range []struct{ report, want string }{{"blocked", ItemBlocked}, {"done", ItemBuilt}, {"incomplete", ItemIncomplete}} {
		b, followed, err := s.ItemFollow("sg4~c1", c.report, "w")
		if err != nil || !followed || b.Status != c.want {
			t.Errorf("%s: %+v %v %v", c.report, b, followed, err)
		}
	}
	for _, report := range []string{"question", "progress"} {
		if _, followed, _ := s.ItemFollow("sg4~c1", report, "w"); followed {
			t.Errorf("a %s report moved the item", report)
		}
	}
	if _, followed, err := s.ItemFollow("sg4~c2", "done", "w"); followed || err != nil {
		t.Errorf("a card with no item: %v %v", followed, err)
	}
	if g, _ := s.ItemGet("f-2"); g.Status != ItemOpen {
		t.Errorf("another item moved: %+v", g)
	}
}

func TestPriorityIsHighMediumLowOrUnsetAndFoldsCase(t *testing.T) {
	s := open(t)
	for in, want := range map[string]string{"HIGH": "high", " Medium ": "medium", "low": "low", "": "", "unset": ""} {
		n := newItem("p-"+strings.ReplaceAll(strings.ToLower(strings.TrimSpace(in)), " ", "")+"x", "fabric")
		n.Priority = in
		b, err := s.ItemFile(n)
		if err != nil || b.Priority != want {
			t.Errorf("file %q: %+v %v", in, b, err)
		}
	}
	for _, bad := range []string{"p2", "urgent", "highest"} {
		n := newItem("bad", "fabric")
		n.Priority = bad
		_, err := s.ItemFile(n)
		if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), "high, medium, low") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestItemEdit(t *testing.T) {
	s := open(t)
	_, _ = s.ItemFile(newItem("a", "fabric"))
	title, body, blank := "new title", "new body", " "
	b, err := s.ItemEdit("a", &title, nil, "orch@sg4")
	if err != nil || b.Title != "new title" || b.Body == "new body" || b.ChangedBy != "orch@sg4" {
		t.Fatalf("title: %+v %v", b, err)
	}
	if b, err = s.ItemEdit("a", nil, &body, "x"); err != nil || b.Title != "new title" || b.Body != "new body" {
		t.Fatalf("body: %+v %v", b, err)
	}
	if _, err = s.ItemEdit("a", &blank, nil, "x"); err == nil || !IsRefusal(err) {
		t.Fatalf("blank title: %v", err)
	}
	if _, err = s.ItemEdit("a", nil, nil, "x"); err == nil || !IsRefusal(err) {
		t.Fatalf("nothing: %v", err)
	}
	if _, err = s.ItemEdit("nope", &title, nil, "x"); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestItemSetPriorityAndFilter(t *testing.T) {
	s := open(t)
	_, _ = s.ItemFile(newItem("a", "fabric"))
	n := newItem("b", "fabric")
	n.Priority = ""
	_, _ = s.ItemFile(n)
	b, err := s.ItemSetPriority("b", "Low", "orch@sg4")
	if err != nil || b.Priority != "low" || b.ChangedBy != "orch@sg4" {
		t.Fatalf("set: %+v %v", b, err)
	}
	if b, err = s.ItemSetPriority("b", "unset", "x"); err != nil || b.Priority != "" {
		t.Fatalf("unset: %+v %v", b, err)
	}
	if _, err := s.ItemSetPriority("b", "p1", "x"); err == nil || !strings.Contains(err.Error(), "high, medium, low") {
		t.Fatalf("bogus priority: %v", err)
	}
	if _, err := s.ItemSetPriority("nope", "high", "x"); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("missing: %v", err)
	}
	hi, _ := s.ItemList(ItemFilter{Priority: "high"})
	un, _ := s.ItemList(ItemFilter{Priority: "unset"})
	if len(hi) != 1 || hi[0].ID != "a" || len(un) != 1 || un[0].ID != "b" {
		t.Fatalf("filters: %+v %+v", hi, un)
	}
}

func TestPriorityMigrationNormalisesExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Put the table back to how 0012 left it: free text, no CHECK.
	for _, q := range []string{
		`DROP TABLE backlog_item`,
		`CREATE TABLE backlog_item (id TEXT PRIMARY KEY, dept TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open', card TEXT NOT NULL DEFAULT '',
			filed_by TEXT NOT NULL DEFAULT '', filed_room TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
			changed_at TEXT NOT NULL, changed_by TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO backlog_item (id, dept, title, priority, created_at, changed_at) VALUES
			('a','fabric','t','HIGH','x','x'), ('b','fabric','t','','x','x'), ('c','fabric','t','Medium','x','x'),
			('d','fabric','t','p2','x','x'), ('e','fabric','t',' low ','x','x')`,
		`DELETE FROM schema_migration WHERE name = '0014_backlog_priority'`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopening a database from before 0014: %v", err)
	}
	defer s.Close()
	for id, want := range map[string]string{"a": "high", "b": "", "c": "medium", "d": "", "e": "low"} {
		if b, err := s.ItemGet(id); err != nil || b.Priority != want {
			t.Errorf("%s: %+v %v", id, b, err)
		}
	}
	if _, err := s.db.Exec(`UPDATE backlog_item SET priority = 'HIGH' WHERE id = 'a'`); err == nil {
		t.Error("the CHECK constraint is not there")
	}
}
