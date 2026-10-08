package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The restart wrap-up: working cards are asked to wrap up, wait for ready or idle, get a wake, and the unanswered
// are named. See docs/changes/r-graceful-room-restart.md.

const wrapPrompt = "the room is restarting."

func fastWrap(t *testing.T) {
	t.Helper()
	fastNewContext(t)
	old := wrapTiming
	wrapTiming.poll = 5 * time.Millisecond
	t.Cleanup(func() { wrapTiming = old })
}

func wraps(f *fakePTY) int { return strings.Count(f.written(), wrapPrompt) }

// runWrap runs the wrap-up in the background and returns where its report lands.
func runWrap(d *Daemon, wait time.Duration) chan WrapReport {
	out := make(chan WrapReport, 1)
	go func() { out <- d.wrapUpForRestart(context.Background(), wait, "a test") }()
	return out
}

func report(t *testing.T, ch chan WrapReport) WrapReport {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the wrap-up never finished")
		return WrapReport{}
	}
}

func wakeOf(d *Daemon, id string) *store.RestartWake {
	ws, _ := d.st.RestartWakes()
	for _, w := range ws {
		if w.TaskID == id {
			return w
		}
	}
	return nil
}

func TestWrapAskedCardSaysReadyAndIsWoken(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	midTurn(d, task.ID)

	ch := runWrap(d, time.Minute)
	until(t, "the wrap-up prompt", func() bool { return wraps(f) == 1 && strings.Contains(f.written(), "/opt/atrium/atrium ready") })
	if !d.wrap.waiting(task.ID) {
		t.Fatal("the wrap-up is not waiting on the card's ack")
	}
	if _, _, err := d.ready(task); err != nil {
		t.Fatalf("ready was refused: %v", err)
	}
	rep := report(t, ch)
	if len(rep.Ready) != 1 || len(rep.Unanswered) != 0 || len(rep.Woken) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	w := wakeOf(d, task.ID)
	if w == nil || w.By != wrapBy || !strings.Contains(w.Text, "carry on") {
		t.Fatalf("wake = %+v", w)
	}
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("a restart wrap-up cleared the context: %q", f.written())
	}
}

func TestWrapAnIdleCardCountsAsAnswered(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	midTurn(d, task.ID)

	ch := runWrap(d, time.Minute)
	until(t, "the wrap-up prompt", func() bool { return wraps(f) == 1 })
	ncTurnEnds(d, task.ID)
	rep := report(t, ch)
	if len(rep.Idle) != 1 || len(rep.Unanswered) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if wakeOf(d, task.ID) == nil {
		t.Fatal("an idle card that was asked got no wake")
	}
}

func TestWrapTimeoutNamesTheCardAndWakesIt(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	midTurn(d, task.ID)

	start := time.Now()
	rep := report(t, runWrap(d, 300*time.Millisecond))
	if time.Since(start) < 250*time.Millisecond {
		t.Fatalf("the wrap-up gave up after %s, before its wait", time.Since(start))
	}
	if wraps(f) != 1 {
		t.Fatalf("the card was asked %d times", wraps(f))
	}
	if len(rep.Unanswered) != 1 || rep.Unanswered[0] != task.DisplayTitle() || len(rep.Woken) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	w := wakeOf(d, task.ID)
	if w == nil || !strings.Contains(w.Text, "before you answered") {
		t.Fatalf("wake = %+v", w)
	}
	evs, _ := d.st.Events(task.ID, 100)
	found := false
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "did not answer the wrap-up") {
			found = true
		}
	}
	if !found {
		t.Fatal("the card's history does not say it never answered")
	}
	if d.wrap.waiting(task.ID) {
		t.Fatal("the wrap-up still waits on the card's ack after it ended")
	}
}

func TestWrapLeavesAnIdleCardAlone(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	ncTurnEnds(d, task.ID)

	rep := report(t, runWrap(d, time.Minute))
	if len(rep.Asked) != 0 || wraps(f) != 0 || wakeOf(d, task.ID) != nil {
		t.Fatalf("an idle card was asked or woken: %+v %q", rep, f.written())
	}
}

func TestWrapKeepsACardsOwnWake(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	if _, _, err := d.queueWake(task.ID, "my own wake", "me"); err != nil {
		t.Fatal(err)
	}
	midTurn(d, task.ID)
	ch := runWrap(d, time.Minute)
	until(t, "the wrap-up prompt", func() bool { return wraps(f) == 1 })
	if _, _, err := d.ready(task); err != nil {
		t.Fatal(err)
	}
	rep := report(t, ch)
	if len(rep.Woken) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if w := wakeOf(d, task.ID); w == nil || w.Text != "my own wake" {
		t.Fatalf("the card's own wake was replaced: %+v", w)
	}
}

func wrapShutdownReq(query string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/shutdown"+query, nil)
	req.Header.Set("X-Atrium-Token", "tok")
	return req
}

func stopped(d *Daemon) bool {
	select {
	case <-d.stop.ch:
		return true
	default:
		return false
	}
}

func TestShutdownWrapsUpFirstAndNowSkipsIt(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	d.opts.ShutdownToken = "tok"
	task, f, _ := ncCard(t, d)
	midTurn(d, task.ID)
	if err := d.st.SetSetting(store.SettingRestartWrapWait, "10"); err != nil {
		t.Fatal(err)
	}

	d.handleShutdown(httptest.NewRecorder(), wrapShutdownReq(""))
	until(t, "the wrap-up prompt", func() bool { return wraps(f) == 1 })
	if stopped(d) {
		t.Fatal("the room stopped while the wrap-up waited")
	}
	if _, _, err := d.ready(task); err != nil {
		t.Fatal(err)
	}
	until(t, "the stop after the ack", func() bool { return stopped(d) })
	if wakeOf(d, task.ID) == nil {
		t.Fatal("no wake was queued before the stop")
	}
}

func TestShutdownNowDoesNotAskOrWake(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	d.opts.ShutdownToken = "tok"
	task, f, _ := ncCard(t, d)
	midTurn(d, task.ID)

	d.handleShutdown(httptest.NewRecorder(), wrapShutdownReq("?now=1"))
	until(t, "the stop", func() bool { return stopped(d) })
	if wraps(f) != 0 || wakeOf(d, task.ID) != nil {
		t.Fatalf("the opt-out asked or woke a card: %q", f.written())
	}
}

func TestShutdownNowEndsAWrapUpAlreadyWaiting(t *testing.T) {
	fastWrap(t)
	d := testDaemon(t)
	d.opts.ShutdownToken = "tok"
	task, f, _ := ncCard(t, d)
	midTurn(d, task.ID)
	if err := d.st.SetSetting(store.SettingRestartWrapWait, "3600"); err != nil {
		t.Fatal(err)
	}

	d.handleShutdown(httptest.NewRecorder(), wrapShutdownReq(""))
	until(t, "the wrap-up prompt", func() bool { return wraps(f) == 1 })
	d.handleShutdown(httptest.NewRecorder(), wrapShutdownReq("?now=1"))
	until(t, "the stop", func() bool { return stopped(d) })
}

func TestRestartWrapWaitSetting(t *testing.T) {
	d := testDaemon(t)
	if got := d.st.RestartWrapWait(); got != 5*time.Minute {
		t.Fatalf("default = %s", got)
	}
	for _, bad := range []string{"5", "9999", "soon"} {
		if _, err := store.CheckRestartWrapWait(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
	_ = d.st.SetSetting(store.SettingRestartWrapWait, "90")
	if got := d.st.RestartWrapWait(); got != 90*time.Second {
		t.Fatalf("set = %s", got)
	}
}
