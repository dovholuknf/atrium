package daemon

import (
	"strings"
	"testing"
	"time"
)

// A CARD OVER ITS CONTEXT THRESHOLD, MID-TURN, IS TOLD ONCE (held-message escalation,
// stage R2).

func setContext(d *Daemon, id string, tokens int64) {
	d.ctx.mu.Lock()
	d.ctx.m[id] = contextSeen{tokens: tokens}
	d.ctx.mu.Unlock()
}

func TestAnOverContextCardIsToldOnceMidTurn(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	began := time.Now().Add(-time.Hour)
	d.act.now = func() time.Time { return began }
	d.act.set(worker.ID, ActivityThinking, "")
	worker, _ = d.st.Get(worker.ID)

	setContext(d, worker.ID, 100_000)
	if l := d.contextLine(worker); l != "" {
		t.Fatalf("a card under the threshold was told %q", l)
	}
	setContext(d, worker.ID, 253_000)
	if l := d.contextLine(worker); !strings.Contains(l, "you are at 253k context") {
		t.Fatalf("first line %q", l)
	}
	if l := d.contextLine(worker); l != "" {
		t.Fatalf("told twice at the same size: %q", l)
	}
	// +50k: again, and the launcher hears of it once.
	setContext(d, worker.ID, 303_000)
	if l := d.contextLine(worker); !strings.Contains(l, "303k") {
		t.Fatalf("second line %q", l)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 || !strings.Contains(msgs[0].Text, "told twice") {
		t.Fatalf("launcher has %v, want one told-twice notice", msgs)
	}
	// +100k: nothing more.
	setContext(d, worker.ID, 360_000)
	if l := d.contextLine(worker); l != "" {
		t.Fatalf("told a third time: %q", l)
	}
	// A new turn re-arms it.
	d.act.set(worker.ID, ActivityIdle, "")
	d.act.now = func() time.Time { return began.Add(30 * time.Minute) }
	d.act.set(worker.ID, ActivityThinking, "")
	if l := d.contextLine(worker); !strings.Contains(l, "360k") {
		t.Fatalf("a new turn was not told: %q", l)
	}
}
