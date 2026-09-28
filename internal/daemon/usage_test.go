package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The usage record with hand-written transcripts. Nothing here starts claude.

type usageFix struct {
	t    *testing.T
	st   *store.Store
	u    *usageTracker
	task *store.Task
	path string
	base time.Time
}

func newUsageFix(t *testing.T) *usageFix {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.ToSlash(filepath.Join(dir, "atrium.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	task, _, err := st.Register(store.Observed{WireName: "spender", Worktree: filepath.ToSlash(dir), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetResumeID(task.ID, "s1"); err != nil {
		t.Fatal(err)
	}
	f := &usageFix{t: t, st: st, path: filepath.Join(dir, "s1.jsonl"),
		base: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	f.task, _ = st.Get(task.ID)
	f.u = f.tracker()
	return f
}

// tracker is a fresh one, as a daemon restart makes.
func (f *usageFix) tracker() *usageTracker {
	u := newUsageTracker(f.st)
	u.started = f.base.Add(-time.Hour)
	u.transcript = func(cwd, id string) string {
		if _, err := os.Stat(f.path); err != nil || id != "s1" {
			return ""
		}
		return f.path
	}
	return u
}

// line appends one transcript line. Blocks of one reply repeat its id and usage.
func (f *usageFix) line(at time.Time, id string, w5, w1, read, out int64, sidechain bool) {
	f.t.Helper()
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano), "isSidechain": sidechain,
		"message": map[string]any{"id": id, "model": "claude-opus-5-5", "usage": map[string]any{
			"input_tokens": 2, "cache_creation_input_tokens": w5 + w1, "cache_read_input_tokens": read,
			"output_tokens": out,
			"cache_creation": map[string]any{"ephemeral_5m_input_tokens": w5, "ephemeral_1h_input_tokens": w1},
		}},
	})
	fh, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

func (f *usageFix) record(seg usageSegment) *store.SessionUsage {
	f.t.Helper()
	row, err := f.u.record(f.task, seg)
	if err != nil {
		f.t.Fatal(err)
	}
	return row
}

// A turn is one row: each reply once however many blocks it was written in,
// subagent replies left out, the split by TTL kept, and the context the last
// reply's.
func TestUsageRecordsOneRowPerTurn(t *testing.T) {
	f := newUsageFix(t)
	f.u.launched(f.task.ID, false)
	f.u.prompted(f.task.ID, store.UsageOperator)
	b := f.base
	f.line(b, "m1", 0, 1000, 9000, 50, false)
	f.line(b.Add(100*time.Millisecond), "m1", 0, 1000, 9000, 50, false)
	f.line(b.Add(2*time.Second), "sub1", 0, 7000, 0, 70, true)
	f.line(b.Add(5*time.Second), "m2", 200, 0, 10000, 80, false)
	row := f.record(f.u.endSegment(f.task.ID, b.Add(10*time.Second)))
	if row == nil {
		t.Fatal("no row")
	}
	if row.Replies != 2 || row.CacheWrite1h != 1000 || row.CacheWrite5m != 200 || row.CacheRead != 19000 ||
		row.Output != 130 || row.Input != 4 {
		t.Fatalf("row %+v", row)
	}
	if row.Context != 10202 || row.Cause != store.UsageOperator || row.AfterResume || row.LastMessage != "m2" {
		t.Fatalf("row %+v", row)
	}
	if row.Cost <= 0 || row.Prices == "" {
		t.Fatalf("row not priced %+v", row)
	}
	// A Stop with no new request writes nothing.
	if again := f.record(f.u.endSegment(f.task.ID, b.Add(20*time.Second))); again != nil {
		t.Fatalf("an empty turn wrote %+v", again)
	}
}

// Replies stamped after the Stop belong to the turn a blocked Stop continued,
// and the next read counts them once.
func TestUsageLeavesTheNextTurnForTheNextRow(t *testing.T) {
	f := newUsageFix(t)
	b := f.base
	f.u.prompted(f.task.ID, store.UsageOperator)
	f.line(b, "m1", 0, 100, 1000, 10, false)
	stop := b.Add(time.Second)
	seg := f.u.endSegment(f.task.ID, stop)
	f.u.prompted(f.task.ID, store.UsageSay)
	f.line(b.Add(2*time.Second), "m2", 0, 100, 1100, 10, false)
	first := f.record(seg)
	if first == nil || first.Replies != 1 || first.LastMessage != "m1" || first.Cause != store.UsageOperator {
		t.Fatalf("first %+v", first)
	}
	f.line(b.Add(3*time.Second), "m2", 0, 100, 1100, 10, false)
	second := f.record(f.u.endSegment(f.task.ID, b.Add(5*time.Second)))
	if second == nil || second.Replies != 1 || second.LastMessage != "m2" || second.Cause != store.UsageSay {
		t.Fatalf("second %+v", second)
	}
}

// A restarted daemon starts from the last row, so nothing is counted twice and
// nothing written while it was down is lost. The resumed runner's first turn is
// flagged and, prompted by nobody else, called a resume.
func TestUsageAfterARestartCountsFromTheLastRow(t *testing.T) {
	f := newUsageFix(t)
	b := f.base
	f.line(b, "m1", 0, 100, 1000, 10, false)
	if f.record(f.u.endSegment(f.task.ID, b.Add(time.Second))) == nil {
		t.Fatal("no first row")
	}
	// A keep-alive row is not where the transcript read resumes.
	if err := f.st.AddSessionUsage(&store.SessionUsage{TaskID: f.task.ID, ResumeID: "s1",
		Ended: b.Add(time.Hour), Cause: store.UsageKeepalive, CacheRead: 1100}); err != nil {
		t.Fatal(err)
	}
	f.line(b.Add(30*time.Minute), "m2", 0, 100, 1100, 10, false)
	f.u = f.tracker()
	f.u.launched(f.task.ID, true)
	f.line(b.Add(2*time.Hour), "m3", 0, 1200, 0, 10, false)
	row := f.record(f.u.endSegment(f.task.ID, b.Add(2*time.Hour+time.Second)))
	if row == nil || row.Replies != 2 || row.LastMessage != "m3" {
		t.Fatalf("row %+v", row)
	}
	if !row.AfterResume || row.Cause != store.UsageResume || row.CacheWrite1h != 1300 {
		t.Fatalf("row %+v", row)
	}
	// A wake keeps its cause and the flag.
	f.u.launched(f.task.ID, true)
	f.u.prompted(f.task.ID, store.UsageRestartWake)
	seg := f.u.endSegment(f.task.ID, b.Add(3*time.Hour))
	if seg.cause != store.UsageRestartWake || !seg.afterResume {
		t.Fatalf("segment %+v", seg)
	}
}

// A resume flag whose Stop found no request stays for the turn that spends.
func TestUsageResumeFlagWaitsForASpendingTurn(t *testing.T) {
	f := newUsageFix(t)
	f.u.launched(f.task.ID, true)
	if row := f.record(f.u.endSegment(f.task.ID, f.base)); row != nil {
		t.Fatalf("row %+v", row)
	}
	f.line(f.base.Add(time.Minute), "m1", 0, 5000, 0, 10, false)
	row := f.record(f.u.endSegment(f.task.ID, f.base.Add(2*time.Minute)))
	if row == nil || !row.AfterResume {
		t.Fatalf("row %+v", row)
	}
}

// What a prompt atrium typed was, from its label.
func TestUsageCauseOfBanner(t *testing.T) {
	for banner, want := range map[string]string{
		wakeLabel: store.UsageRestartWake, exitLabel: store.UsageRestartWake,
		peerBanner("worker"): store.UsageSay,
	} {
		if got := causeOfBanner(banner); got != want {
			t.Fatalf("%q: %s, want %s", banner, got, want)
		}
	}
	if messagesCause([]*store.Message{{}}) != store.UsageOperator ||
		messagesCause([]*store.Message{{}, {FromPeer: "w"}}) != store.UsageSay {
		t.Fatal("messages cause")
	}
}

// A keep-alive refresh is a row in the card's usage record too.
func TestKeepaliveRefreshIsAUsageRow(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	rows, err := f.st.SessionUsageOf(f.task.ID, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %d (%v)", len(rows), err)
	}
	r := rows[0]
	if r.Cause != store.UsageKeepalive || r.CacheRead != 297_000 || r.CacheWrite1h != 1_000 || r.Cost <= 0 {
		t.Fatalf("row %+v", r)
	}
}
