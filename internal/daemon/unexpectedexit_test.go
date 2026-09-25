package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The unexpected-exit notice. See docs/unexpected-exit-wake.md.

// roomOn opens a room on the database in dir, as a restart would.
func roomOn(t *testing.T, dir string) *Daemon {
	t.Helper()
	d, err := New(Options{
		AgentAddr: freePort(t), HumanAddr: freePort(t),
		DBPath:   filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll: time.Second, LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// cardInStatus is a supervised-style card in a status, with no pid, so nothing on this
// machine can be mistaken for a session that outlived the room.
func cardInStatus(t *testing.T, d *Daemon, name, status string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: "d:/git/atrium", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, status); err != nil {
		t.Fatal(err)
	}
	return task
}

// comeBack puts the resumed runner on the card, started after anything queued,
// and returns its terminal and a moment by which the gate is open.
func comeBack(t *testing.T, d *Daemon, taskID string) (*fakePTY, time.Time) {
	t.Helper()
	r, f := typedRunner(t, d, taskID)
	r.started = time.Now().Add(time.Second)
	return f, r.started.Add(wakeNoHook)
}

// exitEvents is the card's notice history.
func exitEvents(t *testing.T, d *Daemon, taskID string) []string {
	t.Helper()
	evs, err := d.st.Events(taskID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		var p map[string]any
		_ = json.Unmarshal(e.Payload, &p)
		switch {
		case e.Kind == store.EventNotified && p["by"] == store.UnexpectedExitBy:
			out = append(out, "notified:"+p["what"].(string))
		case e.Kind == store.EventPrompted && p["from"] == store.UnexpectedExitBy:
			out = append(out, "prompted")
		}
	}
	return out
}

// A crash leaves the card `running` and no planned-stop mark. The next room
// queues the notice, and types it once after the runner comes back.
func TestACrashMidTurnIsToldOnceTheRunnerIsBack(t *testing.T) {
	dir := t.TempDir()
	d := roomOn(t, dir)
	card := cardInStatus(t, d, "worker", store.StatusRunning)
	d.Close() // no wind-down: the crash

	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	f, open := comeBack(t, d, card.ID)
	d.wakeTick(open)
	got := f.written()
	want := "\x1b[38;5;244m[atrium] unexpected exit: \x1b[0m"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("the notice was not typed behind its label: %q", got)
	}
	if !strings.Contains(got, "atrium went away while you were working (crash) at ") ||
		!strings.Contains(got, "Your session was resumed. Check where you were and carry on.") ||
		!strings.HasSuffix(got, "\r") {
		t.Fatalf("the notice text is wrong or was not sent: %q", got)
	}
	d.wakeTick(open.Add(time.Minute))
	if again := f.written(); again != got {
		t.Fatalf("the notice was typed twice: %q", again)
	}
	if evs := exitEvents(t, d, card.ID); strings.Join(evs, ",") != "notified:queued,prompted" {
		t.Fatalf("the card's history is %v", evs)
	}
}

// A planned stop reads mid-turn in the wind-down, before a runner exiting moves
// the card. The next start sees the mark, adds nothing, and the notice says
// restart.
func TestAPlannedStopMidTurnIsToldToo(t *testing.T) {
	dir := t.TempDir()
	d := roomOn(t, dir)
	card := cardInStatus(t, d, "worker", store.StatusRunning)
	typedRunner(t, d, card.ID)
	d.noteStopMidTurn()
	// What the runner's exit does to the card during the wind-down.
	if err := d.st.SetStatus(card.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	d.Close()

	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	if ws, _ := d.st.RestartWakes(); len(ws) != 1 {
		t.Fatalf("want the one notice from the stop, have %+v", ws)
	}
	if _, planned, _ := d.st.TakeRoomStopped(); planned {
		t.Fatal("the planned-stop mark was not cleared by the start")
	}
	f, open := comeBack(t, d, card.ID)
	d.wakeTick(open)
	if got := f.written(); !strings.Contains(got, "(restart) at ") {
		t.Fatalf("the notice after a planned stop is wrong: %q", got)
	}
}

// Cards that had finished their turn get nothing, from a crash or a stop.
func TestAnIdleCardGetsNoNotice(t *testing.T) {
	dir := t.TempDir()
	d := roomOn(t, dir)
	idle := cardInStatus(t, d, "idle", store.StatusNeedsInput)
	typedRunner(t, d, idle.ID)
	d.noteStopMidTurn()
	d.Close()

	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	d.Close()

	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	f, open := comeBack(t, d, idle.ID)
	d.wakeTick(open)
	if f.written() != "" {
		t.Fatalf("an idle card was typed into: %q", f.written())
	}
	if ws, _ := d.st.RestartWakes(); len(ws) != 0 {
		t.Fatalf("an idle card has a row: %+v", ws)
	}
}

// A card that queued its own wake gets that one line and not the notice.
func TestASelfQueuedWakeWinsOverTheNotice(t *testing.T) {
	dir := t.TempDir()
	d := roomOn(t, dir)
	card := cardInStatus(t, d, "orchestrator", store.StatusRunning)
	if _, _, err := d.queueWake(card.ID, "we up", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	d.Close()

	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	f, open := comeBack(t, d, card.ID)
	d.wakeTick(open)
	got := f.written()
	if !strings.Contains(got, "restart wake:") || !strings.Contains(got, "we up") {
		t.Fatalf("the card's own wake was not typed: %q", got)
	}
	if strings.Contains(got, "unexpected exit") {
		t.Fatalf("the notice was typed as well: %q", got)
	}
	if evs := exitEvents(t, d, card.ID); len(evs) != 0 {
		t.Fatalf("the notice left history on a card with its own wake: %v", evs)
	}
}

// A crash loop before the runner is back leaves one notice, and a restart after
// the notice with no turn in between queues nothing more.
func TestRestartsInARowNeverStackNotices(t *testing.T) {
	dir := t.TempDir()
	d := roomOn(t, dir)
	card := cardInStatus(t, d, "worker", store.StatusRunning)
	d.Close()

	// Two crashes before the runner ever comes back.
	for i := 0; i < 2; i++ {
		d = roomOn(t, dir)
		d.noteCrashMidTurn()
		d.Close()
	}
	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	if ws, _ := d.st.RestartWakes(); len(ws) != 1 {
		t.Fatalf("three crashes queued %d rows", len(ws))
	}
	f, open := comeBack(t, d, card.ID)
	d.wakeTick(open)
	if n := strings.Count(f.written(), "unexpected exit"); n != 1 {
		t.Fatalf("typed %d notices: %q", n, f.written())
	}

	// The runner is up and its session started, so the card is ready. A restart
	// now, with no turn since, gives it nothing.
	if err := d.st.SetStatus(card.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	if ws, _ := d.st.RestartWakes(); len(ws) != 0 {
		t.Fatalf("a restart with no turn since queued %+v", ws)
	}
	if evs := exitEvents(t, d, card.ID); strings.Join(evs, ",") != "notified:queued,prompted" {
		t.Fatalf("the card's history is %v", evs)
	}
}

// Off means nothing is queued, and a notice already waiting is dropped untyped.
func TestTheSettingOffDeliversNothing(t *testing.T) {
	dir := t.TempDir()
	d := roomOn(t, dir)
	card := cardInStatus(t, d, "worker", store.StatusRunning)
	other := cardInStatus(t, d, "other", store.StatusRunning)
	// One notice waiting from before the switch went off.
	if _, ok, err := d.st.QueueUnexpectedExit(other.ID, exitNoticeText(exitCrash, time.Now())); err != nil || !ok {
		t.Fatalf("queue: %v %v", ok, err)
	}
	if err := d.st.SetSetting(store.SettingUnexpectedExit, "off"); err != nil {
		t.Fatal(err)
	}
	typedRunner(t, d, card.ID)
	d.noteStopMidTurn()
	d.Close()

	d = roomOn(t, dir)
	d.noteCrashMidTurn()
	f, open := comeBack(t, d, card.ID)
	g, _ := comeBack(t, d, other.ID)
	d.wakeTick(open)
	if f.written() != "" || g.written() != "" {
		t.Fatalf("typed with the setting off: %q %q", f.written(), g.written())
	}
	if ws, _ := d.st.RestartWakes(); len(ws) != 0 {
		t.Fatalf("rows left with the setting off: %+v", ws)
	}
}
