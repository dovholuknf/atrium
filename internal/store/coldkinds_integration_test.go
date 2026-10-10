//go:build integration

package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func countKinds(events []*Event) map[string]int {
	out := map[string]int{}
	for _, e := range events {
		out[e.Kind]++
	}
	return out
}

// The default routes nothing cold-only: every kind lands in the db.
func TestColdKindsOffByDefault(t *testing.T) {
	s := open(t)
	if got := s.ColdOnlyKinds(); len(got) != 0 {
		t.Fatalf("default install routes %v cold-only, want none", got)
	}
	task, _, err := s.Register(Observed{WireName: "cold-default", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventPermRequested, map[string]any{"id": "p1"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countKinds(got)[EventPermRequested] != 1 {
		t.Fatalf("default dropped a perm event from the db: %v", countKinds(got))
	}
}

// With a file sink and the perm kinds routed cold, the db holds none of them and
// the file holds all of them. Other kinds still land in both.
func TestColdKindsSkipDBAndLandInFile(t *testing.T) {
	s, dbPath := reopenWithSettings(t, map[string]string{
		SettingEventSink:      "db,file",
		SettingEventColdKinds: "perm-requested, PERM-DECIDED",
	})
	want := []string{EventPermDecided, EventPermRequested}
	if got := s.ColdOnlyKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ColdOnlyKinds = %v, want %v", got, want)
	}
	task, _, err := s.Register(Observed{WireName: "cold-perm", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{EventPermRequested, EventPermDecided, EventPrompted} {
		if err := s.AppendEvent(task.ID, k, map[string]any{"id": "p1"}); err != nil {
			t.Fatal(err)
		}
	}
	hot, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	hk := countKinds(hot)
	if hk[EventPermRequested] != 0 || hk[EventPermDecided] != 0 {
		t.Fatalf("cold-only kinds reached the db: %v", hk)
	}
	if hk[EventCreated] != 1 || hk[EventPrompted] != 1 {
		t.Fatalf("other kinds missing from the db: %v", hk)
	}
	// No cold kind lost a card's created event, so nothing reads as rolled off.
	if rolled, err := s.HistoryRolledOff(task.ID); err != nil || rolled {
		t.Fatalf("HistoryRolledOff = %v, %v; routing cold is not a roll-off", rolled, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fk := countKinds(readSinkEvents(t, filepath.Join(filepath.Dir(dbPath), "events")))
	if fk[EventPermRequested] != 1 || fk[EventPermDecided] != 1 || fk[EventPrompted] != 1 {
		t.Fatalf("file sink is missing events: %v", fk)
	}
}

// Without a cold sink a cold-only event would be written nowhere, so the setting
// is ignored and every kind stays in the db.
func TestColdKindsIgnoredWithoutColdSink(t *testing.T) {
	s, _ := reopenWithSettings(t, map[string]string{SettingEventColdKinds: "perm-requested"})
	if got := s.ColdOnlyKinds(); len(got) != 0 {
		t.Fatalf("cold kinds %v applied with no cold sink; events would be lost", got)
	}
	task, _, err := s.Register(Observed{WireName: "cold-nowhere", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventPermRequested, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countKinds(got)[EventPermRequested] != 1 {
		t.Fatal("perm event dropped with no cold sink to hold it")
	}
}

// created and submitted are read back from the table, so they stay in the db
// even when named; the rest of the list still applies.
func TestColdKindsKeepPinnedKindsInDB(t *testing.T) {
	s, _ := reopenWithSettings(t, map[string]string{
		SettingEventSink:      "db,file",
		SettingEventColdKinds: "created,submitted,notified",
	})
	if got := s.ColdOnlyKinds(); !reflect.DeepEqual(got, []string{EventNotified}) {
		t.Fatalf("ColdOnlyKinds = %v, want only notified", got)
	}
	task, _, err := s.Register(Observed{WireName: "cold-pinned", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventSubmitted, map[string]any{"summary": "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	k := countKinds(got)
	if k[EventCreated] != 1 || k[EventSubmitted] != 1 {
		t.Fatalf("pinned kinds left the db: %v", k)
	}
}
