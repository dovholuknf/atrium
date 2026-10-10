//go:build integration

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

type usageFix struct {
	t    *testing.T
	st   *store.Store
	u    *usageTracker
	task *store.Task
	path string
	base time.Time
}

// assertNoMoney fails when v, as JSON, carries a cost, price, spend or budget key or a dollar sign.
func assertNoMoney(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"cost", "price", "spent", "budget", "usd", "$"} {
		if strings.Contains(strings.ToLower(string(b)), bad) {
			t.Fatalf("%q on the wire: %s", bad, b)
		}
	}
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
	f.lineTo(f.path, "claude-opus-5-5", at, id, w5, w1, read, out, sidechain)
}

// lineTo appends one line to any transcript, a subagent's among them, and
// stamps the file with the line's time as Claude Code writing it would.
func (f *usageFix) lineTo(path, model string, at time.Time, id string, w5, w1, read, out int64, sidechain bool) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	defer os.Chtimes(path, at, at)
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano), "isSidechain": sidechain,
		"message": map[string]any{"id": id, "model": model, "usage": map[string]any{
			"input_tokens": 2, "cache_creation_input_tokens": w5 + w1, "cache_read_input_tokens": read,
			"output_tokens":  out,
			"cache_creation": map[string]any{"ephemeral_5m_input_tokens": w5, "ephemeral_1h_input_tokens": w1},
		}},
	})
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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
	if row.Cost != 0 || row.Prices != "" {
		t.Fatalf("row priced, money is not worked out any more: %+v", row)
	}
	assertNoMoney(t, row)
	assertNoMoney(t, usageEvent(row))
	// The live event carries the row's calls, so rows that arrive between reads
	// count on the usage tab too (r-012).
	if got, ok := usageEvent(row)["replies"].(int); !ok || got != row.Replies {
		t.Fatalf("usage event replies %v, want %d", usageEvent(row)["replies"], row.Replies)
	}
	// A Stop with no new request writes nothing.
	if again := f.record(f.u.endSegment(f.task.ID, b.Add(20*time.Second))); again != nil {
		t.Fatalf("an empty turn wrote %+v", again)
	}
}

// A row written is announced once, with its card id and figures and no message.
func TestUsageRowWrittenIsBroadcast(t *testing.T) {
	f := newUsageFix(t)
	var kinds []string
	var got map[string]any
	f.u.broadcast = func(kind string, v any) {
		kinds = append(kinds, kind)
		got, _ = v.(map[string]any)
	}
	f.u.prompted(f.task.ID, store.UsageOperator)
	f.line(f.base, "m1", 0, 1000, 9000, 50, false)
	f.record(f.u.endSegment(f.task.ID, f.base.Add(10*time.Second)))
	if len(kinds) != 1 || kinds[0] != "usage" {
		t.Fatalf("events %v, want one usage", kinds)
	}
	if got["task_id"] != f.task.ID || got["cache_read"] != int64(9000) || got["cause"] != store.UsageOperator {
		t.Fatalf("payload %+v", got)
	}
	if _, leaked := got["last_message"]; leaked {
		t.Fatal("the event carries message text")
	}
	f.record(f.u.endSegment(f.task.ID, f.base.Add(20*time.Second)))
	if len(kinds) != 1 {
		t.Fatalf("an empty turn announced a row: %v", kinds)
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
	if r.Cause != store.UsageKeepalive || r.CacheRead != 297_000 || r.CacheWrite1h != 1_000 || r.Cost != 0 {
		t.Fatalf("row %+v", r)
	}
}

// Claude Code subagents are a row of their own, from both layouts: files
// beside the transcript and lines inline marked isSidechain. Nothing is counted
// twice: the card's row and the subagent row add up to the per-file sums, a
// second Stop and a restarted daemon count nothing again, and a reply id the
// card's row holds is never also a subagent's.
func TestUsageCountsSubagentsOnceInTheirOwnRow(t *testing.T) {
	f := newUsageFix(t)
	b := f.base
	dir := strings.TrimSuffix(f.path, ".jsonl")
	a1 := filepath.Join(dir, "subagents", "agent-a1.jsonl")
	a2 := filepath.Join(dir, "subagents", "workflows", "wf_1", "agent-a2.jsonl")
	f.u.prompted(f.task.ID, store.UsageOperator)
	// The card: m1 in two blocks, then m2 after the subagents report.
	f.line(b, "m1", 0, 1000, 9000, 50, false)
	f.line(b.Add(10*time.Millisecond), "m1", 0, 1000, 9000, 50, false)
	// An older runner's subagent, inline.
	f.line(b.Add(time.Second), "s0", 0, 3000, 0, 30, true)
	// A newer runner's two subagents, one on another model, one in a workflow.
	f.lineTo(a1, "claude-opus-5-5", b.Add(2*time.Second), "s1", 0, 7000, 0, 70, true)
	f.lineTo(a1, "claude-opus-5-5", b.Add(2100*time.Millisecond), "s1", 0, 7000, 0, 70, true)
	f.lineTo(a1, "claude-opus-5-5", b.Add(3*time.Second), "s2", 0, 100, 7000, 20, true)
	f.lineTo(a2, "claude-haiku-4-5-20251001", b.Add(4*time.Second), "s3", 500, 0, 0, 10, true)
	// A reply the card's row already has, in a subagent file, is the card's.
	f.lineTo(a2, "claude-opus-5-5", b.Add(5*time.Second), "m2", 200, 0, 10000, 80, true)
	f.line(b.Add(5*time.Second), "m2", 200, 0, 10000, 80, false)
	// Not a subagent transcript.
	f.lineTo(filepath.Join(dir, "subagents", "workflows", "wf_1", "journal.jsonl"), "claude-opus-5-5",
		b.Add(5*time.Second), "j1", 0, 99999, 0, 1, false)
	stop := b.Add(10 * time.Second)

	row := f.record(f.u.endSegment(f.task.ID, stop))
	if row == nil || row.Cause != store.UsageOperator || row.Replies != 2 || row.CacheWrite1h != 1000 ||
		row.CacheWrite5m != 200 || row.CacheRead != 19000 || row.Output != 130 || row.LastMessage != "m2" {
		t.Fatalf("card row %+v", row)
	}
	subs := f.rowsOf(store.UsageSubagent)
	if len(subs) != 1 {
		t.Fatalf("%d subagent rows", len(subs))
	}
	sub := subs[0]
	if sub.Replies != 4 || sub.CacheWrite1h != 10100 || sub.CacheWrite5m != 500 || sub.CacheRead != 7000 ||
		sub.Output != 130 || sub.Input != 8 || sub.LastMessage != "s3" || sub.AfterResume {
		t.Fatalf("subagent row %+v", sub)
	}
	if sub.Cost != 0 {
		t.Fatalf("subagent row priced: %v", sub.Cost)
	}

	// Reconciled: every file summed on its own, one per id, is the two rows.
	var fileSum store.UsageTotals
	for _, p := range []string{f.path, a1, a2} {
		fh, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		scanReplies(fh, func(r *mainReply) {
			if seen[r.MessageID] || (p == a2 && r.MessageID == "m2") {
				return
			}
			seen[r.MessageID] = true
			fileSum.Replies++
			fileSum.CacheWrite1h += r.Write1h
			fileSum.CacheWrite5m += r.Write5m
			fileSum.CacheRead += r.CacheRead
			fileSum.Output += r.Output
		})
		fh.Close()
	}
	all, byCause, err := f.st.SessionUsageTotals(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if all.Replies != fileSum.Replies || all.CacheWrite1h != fileSum.CacheWrite1h ||
		all.CacheWrite5m != fileSum.CacheWrite5m || all.CacheRead != fileSum.CacheRead ||
		all.Output != fileSum.Output {
		t.Fatalf("totals %+v, files %+v", all, fileSum)
	}
	if byCause[store.UsageSubagent] == nil || byCause[store.UsageSubagent].Replies != 4 {
		t.Fatalf("by cause %+v", byCause)
	}

	// Nothing new: nothing written.
	if again := f.record(f.u.endSegment(f.task.ID, b.Add(20*time.Second))); again != nil {
		t.Fatalf("an empty turn wrote %+v", again)
	}
	if n := len(f.rowsOf(store.UsageSubagent)); n != 1 {
		t.Fatalf("%d subagent rows after an empty turn", n)
	}

	// A subagent still at work past the Stop is counted at the next one.
	f.lineTo(a1, "claude-opus-5-5", b.Add(30*time.Second), "s4", 0, 0, 7100, 5, true)
	f.lineTo(a1, "claude-opus-5-5", b.Add(50*time.Second), "s5", 0, 0, 7200, 6, true)
	f.record(f.u.endSegment(f.task.ID, b.Add(40*time.Second)))
	if s := f.rowsOf(store.UsageSubagent); len(s) != 2 || s[0].Replies != 1 || s[0].LastMessage != "s4" {
		t.Fatalf("subagent rows %+v", s)
	}
	// A restarted daemon counts s5 once, and none of what came before it.
	f.u = f.tracker()
	f.record(f.u.endSegment(f.task.ID, b.Add(time.Minute)))
	s := f.rowsOf(store.UsageSubagent)
	if len(s) != 3 || s[0].Replies != 1 || s[0].LastMessage != "s5" {
		t.Fatalf("subagent rows after a restart %+v", s)
	}
	if n := len(f.rowsOf(store.UsageOperator)) + len(f.rowsOf(store.UsageUnknown)); n != 1 {
		t.Fatalf("%d card rows", n)
	}
}

// Every current Claude model has a usage price, and the models keep-alive may
// not refresh stay out of its table.
func TestUsagePricesEveryCurrentModel(t *testing.T) {
	for model, in := range map[string]float64{
		"claude-fable-5-1": 10, "claude-opus-5-5": 4, "claude-opus-5-5[1m]": 4, "claude-sonnet-5": 2,
		"claude-sonnet-5-5": 2, "claude-sonnet-5-5[1m]": 2,
		"claude-haiku-4-5-20251001": 1, "claude-haiku-4-5": 1,
	} {
		if p, ok := usagePriceFor(model); !ok || p.In != in {
			t.Fatalf("%s: %+v %v, want input %v", model, p, ok, in)
		}
	}
	for _, model := range []string{"claude-sonnet-5-6", "claude-haiku-4-5-fast", "gpt-5.5", ""} {
		if p, ok := usagePriceFor(model); ok {
			t.Fatalf("%s priced %+v", model, p)
		}
	}
	for _, model := range []string{"claude-sonnet-5", "claude-sonnet-5-5", "claude-haiku-4-5-20251001"} {
		if _, ok := keepalivePriceFor(model); ok {
			t.Fatalf("%s is in keep-alive's table, so keep-alive would refresh it", model)
		}
	}
}

// rowsOf is a card's rows of one cause, newest first.
func (f *usageFix) rowsOf(cause string) []*store.SessionUsage {
	f.t.Helper()
	rows, err := f.st.SessionUsageOf(f.task.ID, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []*store.SessionUsage
	for _, r := range rows {
		if r.Cause == cause {
			out = append(out, r)
		}
	}
	return out
}
