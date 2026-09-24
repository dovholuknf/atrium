package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// legacyStore builds a database the way an install from before incremental
// auto_vacuum has one: an existing file in mode NONE, then the full schema and
// some history on top. It returns the closed file's path and the card ids.
func legacyStore(t *testing.T, cards, eventsPerCard int) (string, []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "atrium.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE legacy (a TEXT)"); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.IncrementalVacuumOn() {
		t.Fatal("fixture is already incremental; it should be a legacy file")
	}
	var ids []string
	for c := 0; c < cards; c++ {
		task, _, err := s.Register(Observed{WireName: "c" + string(rune('a'+c)), Worktree: "d:/w", Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
		for i := 0; i < eventsPerCard; i++ {
			kind := EventPrompted
			if i%2 == 0 {
				kind = EventPermRequested
			}
			if err := s.AppendEvent(task.ID, kind, bigPayload(i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path, ids
}

func eventRows(t *testing.T, path string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// With no options the copy holds every row, is incremental, and opens as a
// store that can hand pages back. The input keeps every row too.
func TestCompactCopiesEverythingAndSwitchesMode(t *testing.T) {
	in, _ := legacyStore(t, 3, 20)
	before := eventRows(t, in)
	out := filepath.Join(t.TempDir(), "compact.db")

	res, err := Compact(in, out, CompactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EventsBefore != before || res.EventsAfter != before {
		t.Fatalf("events %d -> %d, want %d kept with no options", res.EventsBefore, res.EventsAfter, before)
	}
	if got := eventRows(t, in); got != before {
		t.Fatalf("input lost rows: %d, was %d", got, before)
	}
	if res.OutBytes <= 0 || res.InBytes <= 0 {
		t.Fatalf("sizes not reported: %+v", res)
	}
	s, err := Open(out)
	if err != nil {
		t.Fatalf("copy does not open as a store: %v", err)
	}
	defer s.Close()
	if !s.IncrementalVacuumOn() {
		t.Fatal("copy is not in incremental auto_vacuum mode")
	}
}

// The window and the dropped kinds trim the copy, and only the copy.
func TestCompactWindowAndDropKinds(t *testing.T) {
	in, ids := legacyStore(t, 3, 40)
	before := eventRows(t, in)
	out := filepath.Join(t.TempDir(), "compact.db")

	res, err := Compact(in, out, CompactOptions{WindowBytes: 400, DropKinds: []string{EventPermRequested}})
	if err != nil {
		t.Fatal(err)
	}
	if res.EventsAfter >= res.EventsBefore {
		t.Fatalf("trims removed nothing: %d -> %d", res.EventsBefore, res.EventsAfter)
	}
	if got := eventRows(t, in); got != before {
		t.Fatalf("input lost rows: %d, was %d", got, before)
	}
	s, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range ids {
		got, err := s.Events(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			t.Fatalf("card %s kept no events; the window keeps the newest", id)
		}
		if b := payloadBytes(got); b > 400 && len(got) > 1 {
			t.Fatalf("card %s kept %d bytes over a 400 window", id, b)
		}
		if countKinds(got)[EventPermRequested] != 0 {
			t.Fatalf("card %s still has a dropped kind", id)
		}
	}
}

// A database somebody has open is refused, and nothing is written.
func TestCompactRefusesAnOpenDatabase(t *testing.T) {
	in, _ := legacyStore(t, 1, 2)
	s, err := Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A room reads and writes while it runs; do the same so it holds its locks.
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "compact.db")
	_, err = Compact(in, out, CompactOptions{})
	if !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("Compact on an open database = %v, want ErrDatabaseInUse", err)
	}
	if fileAndWALSize(out) != 0 {
		t.Fatal("a refused compact left a file behind")
	}
}

func TestCompactRefusals(t *testing.T) {
	in, _ := legacyStore(t, 1, 2)
	if _, err := Compact(in, in, CompactOptions{}); err == nil {
		t.Fatal("compacting onto the input was allowed")
	}
	if _, err := Compact(in, filepath.Join(t.TempDir(), "o.db"),
		CompactOptions{DropKinds: []string{EventCreated}}); err == nil {
		t.Fatal("dropping created events was allowed")
	}
	existing, _ := legacyStore(t, 1, 1)
	if _, err := Compact(in, existing, CompactOptions{}); err == nil {
		t.Fatal("compact overwrote an existing file")
	}
}
