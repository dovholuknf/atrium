//go:build integration

package daemon

import (
	"context"
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// coldCard is an idle card with a terminal whose 1h cache is inside the margin before expiry, on a room whose cold
// handoff switch was never touched. Its transcripts are named by session id in the card's directory.
func coldCard(t *testing.T, d *Daemon, keepalive string) (*store.Task, *fakePTY, *time.Time) {
	t.Helper()
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	if err := d.st.SetResumeID(task.ID, "cold-session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.SetKeepaliveState(task.ID, keepalive); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	d.Ka.Now = func() time.Time { return now }
	d.Ka.Transcript = func(_, id string) string { return filepath.Join(dir, id+".jsonl") }
	d.Ka.Session = nil
	appendReply(t, filepath.Join(dir, "cold-session-1.jsonl"), now.Add(-56*time.Minute), replyOpt{ctx: 136_000})
	return task, f, &now
}

// coldCycle runs the cycle the tick started to its wake: the handoff written, /clear, the new session read.
func coldCycle(t *testing.T, d *Daemon, task *store.Task, f *fakePTY) string {
	t.Helper()
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	path := cycleFile(d, task.ID)
	ncAck(t, d, task)
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.Wake.SawSession(task.ID, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), cycle.NewContextWakeText(path)) })
	until(t, "the cycle to end", func() bool { return d.Nctx.Get(task.ID) == nil })
	return path
}

func TestAColdCardIsTypedTheNewContextPromptClearedAndWoken(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	task, f, _ := coldCard(t, d, store.KeepaliveOff)
	d.Ka.Tick(context.Background())
	path := coldCycle(t, d, task, f)
	got := f.written()
	if !(strings.Index(got, limitPrompt) < strings.Index(got, "/clear") &&
		strings.Index(got, "/clear") < strings.Index(got, cycle.NewContextWakeText(path))) {
		t.Fatalf("the steps were not typed in order: %q", got)
	}
	evs := 0
	all, _ := d.st.Events(task.ID, 50)
	for _, e := range all {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), `"by":"`+cycle.ColdHandoffBy+`"`) &&
			strings.Contains(string(e.Payload), `"started":true`) {
			evs++
		}
	}
	if evs != 1 {
		t.Fatalf("cold handoff events = %d, want 1", evs)
	}
}

func TestAColdCardShowsColdAndIsNotKeptWarmAfterItsHandoff(t *testing.T) {
	t.Parallel()
	f := newKAFix(t)
	n := f.coldCounter()
	f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if n.Load() != 1 {
		t.Fatalf("cycles = %d, want 1", n.Load())
	}
	handedAt := f.now
	if _, err := f.st.SetKeepaliveStateAt(f.task.ID, store.KeepaliveOn, f.now); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(filepath.Dir(f.path), "card-session-2.jsonl")
	f.k.Session = func(*store.Task) string { return "card-session-2" }
	f.k.Transcript = func(_, id string) string {
		if id == "card-session-2" {
			return next
		}
		return f.path
	}
	f.now = f.now.Add(2 * time.Minute)
	appendReply(t, next, f.now, replyOpt{ctx: 120_000})
	f.now = f.now.Add(56 * time.Minute)
	f.tick()
	if f.forks() != 0 || n.Load() != 1 {
		t.Fatalf("forks = %d cycles = %d, want 0 and 1: a card handed off cold is not kept warm", f.forks(), n.Load())
	}
	v, _ := f.k.View(f.task.ID).(*cycle.KeepaliveCardView)
	if v == nil || v.Why != cycle.WhyColdHandedOff || v.WarmUntil == nil || !v.WarmUntil.Equal(handedAt) ||
		v.NextRefreshAt != nil {
		t.Fatalf("view = %+v, want cold since the handoff at %s", v, handedAt)
	}
	f.k.ColdRearm(f.task.ID)
	f.tick()
	if f.forks() != 1 {
		t.Fatalf("forks = %d, want 1: a person's turn puts keep-alive back", f.forks())
	}
}

func TestAColdCardIsHandedOffOnceAndNeverLoops(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	task, f, now := coldCard(t, d, store.KeepaliveOn)
	if err := d.st.SetKeepaliveSuspended("test"); err != nil {
		t.Fatal(err)
	}
	d.Ka.Tick(context.Background())
	coldCycle(t, d, task, f)
	if err := d.st.SetResumeID(task.ID, "cold-session-2"); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		*now = now.Add(2 * time.Hour)
		appendReply(t, d.Ka.Transcript("", "cold-session-2"), now.Add(-56*time.Minute), replyOpt{ctx: 90_000})
		d.Ka.Tick(context.Background())
		time.Sleep(20 * time.Millisecond) // wait: negative check, a stray second prompt would land asynchronously
	}
	if c := strings.Count(f.written(), limitPrompt); c != 1 {
		t.Fatalf("the limit prompt was typed %d times, want once: %q", c, f.written())
	}
}
