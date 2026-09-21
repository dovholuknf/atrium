package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readSinkEvents reads every JSONL segment in dir, in name order, and returns
// the events it holds. Name order is time order, which is how the segments were
// written.
func readSinkEvents(t *testing.T, dir string) []*Event {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read events dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			names = append(names, e.Name())
		}
	}
	var out []*Event
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read segment %s: %v", name, err)
		}
		for _, line := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
			if line == "" {
				continue
			}
			var e Event
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatalf("segment %s line is not an event: %v", name, err)
			}
			out = append(out, &e)
		}
	}
	return out
}

func segmentCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read events dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			n++
		}
	}
	return n
}

// The file sink writes one JSON line per event, and Close flushes what it holds.
func TestFileSinkWritesOneLinePerEvent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	fs, err := newFileSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		e := &Event{ID: newID(), TaskID: "task-1", At: at, Kind: EventPrompted,
			Payload: json.RawMessage(`{"n":` + string(rune('0'+i)) + `}`)}
		if err := fs.Append("task-1", e); err != nil {
			t.Fatalf("append never fails: %v", err)
		}
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	got := readSinkEvents(t, dir)
	if len(got) != 3 {
		t.Fatalf("wrote %d lines, want 3", len(got))
	}
	if got[0].Kind != EventPrompted || got[0].TaskID != "task-1" {
		t.Fatalf("event round-tripped wrong: %+v", got[0])
	}
}

// A full segment rolls to a new file, so an operator retires closed files.
func TestFileSinkRollsBySize(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	// One byte forces a roll before every event after the first.
	fs, err := newFileSinkWith(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		e := &Event{ID: newID(), TaskID: "t", At: at, Kind: EventPrompted, Payload: json.RawMessage(`{}`)}
		if err := fs.Append("t", e); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if n := segmentCount(t, dir); n < 2 {
		t.Fatalf("size rolling produced %d segments, want several", n)
	}
	// No event is lost across the roll.
	if got := readSinkEvents(t, dir); len(got) != 4 {
		t.Fatalf("rolling lost events: kept %d of 4", len(got))
	}
}

// Events on different days land in different segments, so a day can be retired
// whole.
func TestFileSinkRollsByDay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	fs, err := newFileSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	day1 := time.Date(2026, 9, 18, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 19, 0, 1, 0, 0, time.UTC)
	for _, at := range []time.Time{day1, day1, day2} {
		e := &Event{ID: newID(), TaskID: "t", At: at, Kind: EventPrompted, Payload: json.RawMessage(`{}`)}
		if err := fs.Append("t", e); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if n := segmentCount(t, dir); n != 2 {
		t.Fatalf("day rolling produced %d segments, want 2", n)
	}
	if got := readSinkEvents(t, dir); len(got) != 3 {
		t.Fatalf("day rolling lost events: kept %d of 3", len(got))
	}
}

// A cold sink does not serve reads.
func TestFileSinkRecentUnsupported(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	fs, err := newFileSink(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err := fs.Recent("t", 10); err != ErrRecentUnsupported {
		t.Fatalf("Recent returned %v, want ErrRecentUnsupported", err)
	}
}

// Backpressure drops rather than blocks. A sink whose writer is not running
// fills its queue and counts every event past it as a loss, and Append still
// returns nil.
func TestFileSinkDropsUnderBackpressure(t *testing.T) {
	// Built by hand with no writer goroutine, so the queue never drains.
	fs := &fileSink{ch: make(chan *Event, 2), done: make(chan struct{})}
	for i := 0; i < 5; i++ {
		if err := fs.Append("t", &Event{ID: newID()}); err != nil {
			t.Fatalf("append blocked or failed under backpressure: %v", err)
		}
	}
	// Two fit the queue, three were dropped.
	if d := fs.dropped.Load(); d != 3 {
		t.Fatalf("dropped %d events, want 3", d)
	}
}

// The setting turns the file sink on end to end: db stays hot, and every event
// also lands in the logs directory beside the database.
func TestEventSinkSettingAddsFileColdSink(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "atrium.db")
	first, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetSetting(SettingEventSink, "db,file"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	// Reopen so configureSinks reads the setting.
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.hot.(*dbSink); !ok {
		t.Fatalf("db stopped being the hot sink: %T", s.hot)
	}
	if len(s.cold) != 1 {
		t.Fatalf("file setting added %d cold sinks, want 1", len(s.cold))
	}
	task, _, err := s.Register(Observed{WireName: "sink-file", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventPrompted, map[string]any{"text": "hi"}); err != nil {
		t.Fatal(err)
	}
	// Close flushes the file sink.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	got := readSinkEvents(t, filepath.Join(filepath.Dir(dbPath), "events"))
	// created (from Register) plus the prompt.
	if len(got) != 2 {
		t.Fatalf("file cold sink holds %d events, want 2", len(got))
	}
	// The hot db still has them too.
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	hot, err := s2.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hot) != 2 {
		t.Fatalf("hot db holds %d events, want 2", len(hot))
	}
}
