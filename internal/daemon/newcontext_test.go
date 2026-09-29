package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// New context: capture, clear, wake. See newcontext.go.

// fastNewContext shrinks every wait so a whole sequence runs in a fraction of a
// second, and puts them back. The waits are what is under test, so a test that
// wants one to expire sets that one shorter still.
func fastNewContext(t *testing.T) {
	t.Helper()
	old := ncTiming
	ncTiming.poll = 5 * time.Millisecond
	ncTiming.typeWait = 400 * time.Millisecond
	ncTiming.captureBegin = 400 * time.Millisecond
	ncTiming.captureEnd = 2 * time.Second
	ncTiming.turnSettle = 40 * time.Millisecond
	ncTiming.clearWait = 400 * time.Millisecond
	ncTiming.sessionSettle = 20 * time.Millisecond
	t.Cleanup(func() { ncTiming = old })
}

// ncCard is a card with a terminal and a real directory, so HANDOFF.md can be
// looked for.
func ncCard(t *testing.T, d *Daemon) (*store.Task, *fakePTY, string) {
	t.Helper()
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "cycler", Worktree: filepath.ToSlash(dir), Runner: "claude", PID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, f := typedRunner(t, d, task.ID)
	t.Cleanup(func() { d.pending.stopAll(); d.nctx.stopAll() })
	return task, f, dir
}

// until waits for cond, failing the test with what if it never holds.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func failedWith(d *Daemon, id string) string {
	v, _ := d.newContextFor(id).(map[string]any)
	if v == nil || v["step"] != NewContextFailed {
		return ""
	}
	r, _ := v["reason"].(string)
	return r
}

// The whole sequence, in order, each step held until the one before it is over.
func TestNewContextCapturesClearsAndWakes(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["step"] != NewContextCapture {
		t.Fatalf("the card does not show the capture step: %v", d.newContextFor(id))
	}

	// 1. The capture prompt, and only that.
	until(t, "the capture prompt", func() bool {
		return strings.Contains(f.written(), "HANDOFF.") && strings.HasSuffix(f.written(), "\r")
	})
	if got := f.written(); strings.Contains(got, "/clear") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("the capture prompt was not typed and sent alone: %q", got)
	}

	// The turn runs. It writes the file, and goes on for a while: /clear must not
	// be typed into it.
	d.act.set(id, ActivityThinking, "")
	if err := os.WriteFile(filepath.Join(dir, HandoffName(task)), []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("/clear was typed while the capture turn was still running: %q", f.written())
	}
	d.act.set(id, ActivityIdle, "")

	// 2. The turn ended, so /clear goes, and nothing else yet.
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["step"] != NewContextClear {
		t.Fatalf("the card does not show the clear step: %v", d.newContextFor(id))
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(f.written(), "Read "+HandoffName(task)+" and continue") {
		t.Fatalf("the wake was typed before the new session started: %q", f.written())
	}

	// 3. The new session starts, and the wake follows.
	d.wake.sawSession(id, time.Now())
	until(t, "the wake prompt", func() bool {
		return strings.Contains(f.written(), newContextWake(HandoffName(task))) && strings.HasSuffix(f.written(), "\r")
	})
	got := f.written()
	if !(strings.Index(got, HandoffName(task)+" in the current") < strings.Index(got, "/clear") &&
		strings.Index(got, "/clear") < strings.Index(got, newContextWake(HandoffName(task)))) {
		t.Fatalf("the steps were not typed in order: %q", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Fatalf("the wake was typed and not sent: %q", got)
	}
	// /clear is a slash command, so nothing goes ahead of it.
	if strings.Contains(got, "[atrium] new context: \x1b[0m/clear") {
		t.Fatalf("/clear was labelled, which stops it being a command: %q", got)
	}
	// Gone when the wake lands.
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
}

// A step that times out leaves the chip failed with the reason and types
// nothing further.
func TestNewContextStopsWhenTheCapturePromptStartsNoTurn(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, "capture prompt") {
		t.Fatalf("the reason does not say which step: %q", r)
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("typed /clear after the capture failed: %q", f.written())
	}
}

func TestNewContextStopsWhenTheCaptureTurnNeverEnds(t *testing.T) {
	fastNewContext(t)
	ncTiming.captureEnd = 200 * time.Millisecond
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(task.ID, ActivityTool, "Bash")
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("typed /clear over a turn that never ended: %q", f.written())
	}
}

// The clear cannot be taken back, so a capture that wrote nothing stops it.
func TestNewContextDoesNotClearOverAMissingHandoff(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(task.ID, ActivityThinking, "")
	time.Sleep(20 * time.Millisecond)
	d.act.set(task.ID, ActivityIdle, "")

	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, HandoffName(task)) || !strings.Contains(r, filepath.ToSlash(dir)) {
		t.Fatalf("the reason does not name the file and the place: %q", r)
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("cleared a session that had written no handoff: %q", f.written())
	}
}

// A HANDOFF.md left by an earlier session is not this capture's.
func TestNewContextDoesNotTrustAStaleHandoff(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	stale := filepath.Join(dir, HandoffName(task))
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(task.ID, ActivityThinking, "")
	time.Sleep(20 * time.Millisecond)
	d.act.set(task.ID, ActivityIdle, "")
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("cleared on the strength of an old handoff: %q", f.written())
	}
}

func TestNewContextStopsWhenNoSessionStartsAfterTheClear(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(task.ID, ActivityThinking, "")
	_ = os.WriteFile(filepath.Join(dir, HandoffName(task)), []byte("state"), 0o644)
	time.Sleep(20 * time.Millisecond)
	d.act.set(task.ID, ActivityIdle, "")
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })

	// No SessionStart ever comes.
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, "/clear") {
		t.Fatalf("the reason does not name the step: %q", r)
	}
	if strings.Contains(f.written(), newContextWake(HandoffName(task))) {
		t.Fatalf("woke a session that never started: %q", f.written())
	}
}

// A SessionStart from before the /clear is not the new session.
func TestNewContextIgnoresASessionStartFromBeforeTheClear(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	d.wake.sawSession(task.ID, time.Now().Add(-time.Minute))

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(task.ID, ActivityThinking, "")
	_ = os.WriteFile(filepath.Join(dir, HandoffName(task)), []byte("state"), 0o644)
	time.Sleep(20 * time.Millisecond)
	d.act.set(task.ID, ActivityIdle, "")
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if strings.Contains(f.written(), newContextWake(HandoffName(task))) {
		t.Fatalf("took an old SessionStart for the new session: %q", f.written())
	}
}

// Pressed in the middle of a turn, the capture waits for the turn to end.
func TestNewContextWaitsOutATurnAlreadyRunning(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	d.act.set(task.ID, ActivityTool, "Bash")

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if f.written() != "" {
		t.Fatalf("typed the capture prompt into a running turn: %q", f.written())
	}
	d.act.set(task.ID, ActivityIdle, "")
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
}

// The gate every automated write goes through: a part written line is waited out.
func TestNewContextWaitsForAnEmptyLine(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	run := d.sup.get(task.ID)
	run.noteOperatorTyped([]byte("half a thou"))

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if f.written() != "" {
		t.Fatalf("typed over the operator's line: %q", f.written())
	}
}

func TestNewContextIsRefusedWithoutATerminalAndWhileRunning(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)

	bare := cardFor(t, d, "not-ours")
	if err := d.StartNewContext(bare.ID); err != errNoTerminal {
		t.Fatalf("started on a card atrium owns no terminal for: %v", err)
	}

	task, _, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.StartNewContext(task.ID); err != errNewContextBusy {
		t.Fatalf("started a second sequence over the first: %v", err)
	}
}

// A failed chip stays until it is dismissed or the action is run again.
func TestNewContextFailedChipIsDismissedOrReplaced(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	time.Sleep(30 * time.Millisecond)
	if failedWith(d, task.ID) == "" {
		t.Fatal("a failed chip went away by itself")
	}

	// Run again: replaced, and the prompt is typed again.
	before := strings.Count(f.written(), "HANDOFF.")
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatalf("could not run again over a failed chip: %v", err)
	}
	until(t, "the second capture prompt", func() bool {
		return strings.Count(f.written(), "HANDOFF.") > before && strings.HasSuffix(f.written(), "\r")
	})

	// Dismissed: gone, and the run it belonged to types nothing more.
	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+task.ID+"/new-context", nil)
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()
	d.handleNewContext(rec, req)
	if rec.Code != http.StatusOK || d.newContextFor(task.ID) != nil {
		t.Fatalf("dismiss left the chip: %d %s", rec.Code, rec.Body)
	}
	written := f.written()
	time.Sleep(150 * time.Millisecond)
	if f.written() != written {
		t.Fatalf("a dismissed run kept typing: %q", f.written()[len(written):])
	}
}

func TestNewContextEndpoint(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	post := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/new-context", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		d.handleNewContext(rec, req)
		return rec
	}
	if rec := post("nobody"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown card answered %d", rec.Code)
	}
	if rec := post(task.ID); rec.Code != http.StatusAccepted {
		t.Fatalf("start answered %d: %s", rec.Code, rec.Body)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	if rec := post(task.ID); rec.Code != http.StatusConflict {
		t.Fatalf("a second start answered %d, not a conflict", rec.Code)
	}
	bare := cardFor(t, d, "not-ours")
	if rec := post(bare.ID); rec.Code != http.StatusConflict {
		t.Fatalf("a card with no terminal answered %d, not a conflict", rec.Code)
	}
}

// A turn that begins and ends between two looks is still a turn.
func TestTurnsBegunCountsAFastTurn(t *testing.T) {
	d := testDaemon(t)
	before := d.act.turnsBegun("c1")
	d.act.set("c1", ActivityThinking, "")
	d.act.set("c1", ActivityTool, "Bash") // still the same turn
	d.act.set("c1", ActivityIdle, "")
	if got := d.act.turnsBegun("c1"); got != before+1 {
		t.Fatalf("one turn counted as %d", got-before)
	}
	d.act.set("c1", ActivityThinking, "")
	if got := d.act.turnsBegun("c1"); got != before+2 {
		t.Fatalf("a second turn counted as %d", got-before)
	}
}
