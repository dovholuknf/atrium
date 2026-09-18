package store

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bigPayload is an event body of a known, roughly fixed size, so a test can set
// a window in bytes and predict how many events fit.
func bigPayload(i int) map[string]any {
	return map[string]any{"n": i, "pad": strings.Repeat("x", 100)}
}

func payloadBytes(events []*Event) int {
	total := 0
	for _, e := range events {
		total += len(e.Payload)
	}
	return total
}

// The default is unbounded, byte-for-byte phase 1: with no window setting the db
// keeps every event no matter how much it holds, and nothing reports as rolled
// off.
func TestHotWindowUnboundedByDefault(t *testing.T) {
	s := open(t)
	if db, ok := s.hot.(*dbSink); !ok || db.windowBytes != 0 {
		t.Fatalf("default hot sink is not an unbounded dbSink: %#v", s.hot)
	}
	task, _, err := s.Register(Observed{WireName: "unbounded", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 50
	for i := 0; i < n; i++ {
		if err := s.AppendEvent(task.ID, EventPrompted, bigPayload(i)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// created (from Register) plus every appended event: nothing rolled off.
	if len(got) != n+1 {
		t.Fatalf("default kept %d events, want %d; the default must stay unbounded", len(got), n+1)
	}
	if got[0].Kind != EventCreated {
		t.Fatalf("oldest event is %q, want the created event still present", got[0].Kind)
	}
	rolled, err := s.HistoryRolledOff(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled {
		t.Fatal("default reports history rolled off; nothing should roll off unbounded")
	}
}

// reopenWithSettings writes settings on a fresh db, then reopens it so
// configureSinks reads them, and returns the reopened store.
func reopenWithSettings(t *testing.T, kv map[string]string) (*Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "atrium.db")
	first, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range kv {
		if err := first.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	first.Close()
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dbPath
}

// With the window set, the db keeps only the recent window and the OLDEST events
// roll off. The newest is always kept, the retained bytes fit the window, and
// the card reports its history as rolled off.
func TestHotWindowRollsOffOldest(t *testing.T) {
	const window = 400
	s, _ := reopenWithSettings(t, map[string]string{
		SettingEventWindowBytes: strconv.Itoa(window),
	})
	if db, ok := s.hot.(*dbSink); !ok || db.windowBytes != window {
		t.Fatalf("window setting did not reach the db sink: %#v", s.hot)
	}
	task, _, err := s.Register(Observed{WireName: "bounded", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 40
	var lastN int
	for i := 0; i < n; i++ {
		lastN = i
		if err := s.AppendEvent(task.ID, EventPrompted, bigPayload(i)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) >= n+1 {
		t.Fatalf("window kept %d events; expected a bound below the %d appended", len(got), n+1)
	}
	// The newest event survives: it carries the highest n.
	newest := got[len(got)-1]
	if !strings.Contains(string(newest.Payload), `"n":`+strconv.Itoa(lastN)) {
		t.Fatalf("newest retained event is not the last appended: %s", newest.Payload)
	}
	// The created event is the oldest, so it rolled off first.
	if got[0].Kind == EventCreated {
		t.Fatal("created event survived; the oldest should have rolled off")
	}
	// Retained bytes fit the window, unless only the single newest is kept.
	if b := payloadBytes(got); b > window && len(got) > 1 {
		t.Fatalf("retained %d payload bytes over a %d window", b, window)
	}
	rolled, err := s.HistoryRolledOff(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rolled {
		t.Fatal("card lost older events but does not report history rolled off")
	}
}

// A window larger than one payload always keeps at least the newest event, even
// when that one event is itself bigger than the window.
func TestHotWindowKeepsNewestEvenIfLarger(t *testing.T) {
	s, _ := reopenWithSettings(t, map[string]string{SettingEventWindowBytes: "10"})
	task, _, err := s.Register(Observed{WireName: "tiny-window", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventPrompted, bigPayload(1)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("a window smaller than one event kept %d events, want the newest alone", len(got))
	}
}

// Roll-off with a file cold sink present preserves the rolled-off events in the
// file: cold sinks receive every event at append time, so dropping it from the
// db later loses nothing durable.
func TestRollOffPreservedInFileSink(t *testing.T) {
	s, dbPath := reopenWithSettings(t, map[string]string{
		SettingEventSink:        "db,file",
		SettingEventWindowBytes: "400",
	})
	if len(s.cold) != 1 {
		t.Fatalf("expected one file cold sink, got %d", len(s.cold))
	}
	task, _, err := s.Register(Observed{WireName: "roll-file", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 40
	for i := 0; i < n; i++ {
		if err := s.AppendEvent(task.ID, EventPrompted, bigPayload(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Close flushes the file sink writer.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// The db rolled off; the file kept everything.
	file := readSinkEvents(t, filepath.Join(filepath.Dir(dbPath), "events"))
	if len(file) != n+1 { // created plus every appended event
		t.Fatalf("file cold sink holds %d events, want %d; roll-off lost durable history", len(file), n+1)
	}
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	hot, err := s2.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hot) >= n+1 {
		t.Fatalf("db kept %d events; the window should have rolled some off", len(hot))
	}
	if len(hot) >= len(file) {
		t.Fatalf("db window (%d) is not smaller than the cold file (%d)", len(hot), len(file))
	}
}
