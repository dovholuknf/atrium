//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// inATurn puts a card in a turn that began at `began`, with `calls` tool calls in
// the ten minutes before `at`, and leaves it thinking.
func inATurn(d *Daemon, id string, began, at time.Time, calls int) {
	d.act.now = func() time.Time { return began }
	d.act.set(id, ActivityThinking, "")
	for i := 0; i < calls; i++ {
		step := at.Add(-time.Duration(i+1) * time.Minute / 2)
		d.act.now = func() time.Time { return step }
		d.act.set(id, ActivityTool, "Bash")
		d.act.set(id, ActivityThinking, "")
	}
	d.act.now = func() time.Time { return at }
}

func TestALongTurnIsReportedOnce(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	began := time.Now().Add(-2 * time.Hour)
	at := began.Add(47 * time.Minute)
	inATurn(d, worker.ID, began, at, 12)

	for i := 0; i < 3; i++ {
		if err := d.watchWorkers(at); err != nil {
			t.Fatal(err)
		}
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "one turn for 47 minutes") ||
		!strings.Contains(msgs[0].Text, "12 tool calls in the last 10 minutes") {
		t.Fatalf("launcher has %v, want one long-turn notice with the rate", msgs)
	}
	x := d.esc.get(worker.ID)
	if x == nil || x.Source != NoticeLongTurn || x.Minutes != 47 {
		t.Fatalf("escalation %+v, want long-turn at 47 minutes", x)
	}
	// Ninety minutes in, the same turn: no second notice.
	d.act.now = func() time.Time { return began.Add(90 * time.Minute) }
	if err := d.watchWorkers(began.Add(90 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 {
		t.Fatalf("the same turn was reported %d times", len(msgs))
	}
	// A new turn re-arms it.
	d.act.set(worker.ID, ActivityIdle, "")
	next := began.Add(3 * time.Hour)
	inATurn(d, worker.ID, next, next.Add(50*time.Minute), 1)
	if err := d.watchWorkers(next.Add(50 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 2 {
		t.Fatalf("a new long turn was reported %d times in all, want 2", len(msgs))
	}
}

// Under the setting, nothing. A card with no launcher is flagged and nobody is
// told. One long tool call inside a long turn shows the tool call.
func TestALongTurnOnEveryCardAndItsPriority(t *testing.T) {
	d := testDaemon(t)
	mine := peerCard(t, d, "mine")
	if err := d.st.SetStatus(mine.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	began := time.Now().Add(-2 * time.Hour)
	inATurn(d, mine.ID, began, began.Add(40*time.Minute), 3)
	if err := d.watchWorkers(began.Add(40 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(mine.ID); x != nil {
		t.Fatalf("a 40 minute turn was flagged: %+v", x)
	}
	d.act.now = func() time.Time { return began.Add(50 * time.Minute) }
	if err := d.watchWorkers(began.Add(50 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(mine.ID); x == nil || x.Source != NoticeLongTurn {
		t.Fatalf("a card with no launcher was not flagged: %+v", x)
	}

	// The setting moves it.
	if err := d.st.SetSetting(store.SettingEscalateTurnAfter, "120"); err != nil {
		t.Fatal(err)
	}
	if err := d.watchWorkers(began.Add(50 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(mine.ID); x != nil {
		t.Fatalf("a 50 minute turn was flagged under a 120 minute setting: %+v", x)
	}
	_ = d.st.SetSetting(store.SettingEscalateTurnAfter, "")

	_, worker := launchedPair(t, d)
	start := time.Now().Add(-3 * time.Hour)
	inATurn(d, worker.ID, start, start, 0)
	d.act.set(worker.ID, ActivityTool, "Bash")
	at := start.Add(LongToolAfter + 30*time.Minute)
	d.act.now = func() time.Time { return at }
	if err := d.watchWorkers(at); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(worker.ID); x == nil || x.Source != NoticeLongTool {
		t.Fatalf("one long call in a long turn showed %+v, want the long call", x)
	}
}
