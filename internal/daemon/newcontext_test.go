package daemon

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// New context: the limit prompt, the ack, clear, wake. See newcontext.go.

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
	ncTiming.promptGap = 0
	ncTiming.promptLost = 300 * time.Millisecond
	oldBin := atriumBinary
	atriumBinary = func() string { return "/opt/atrium/atrium" }
	oldSteps := append([]time.Duration(nil), backoffSteps...)
	// A say held through the cycle is retried on this front step.
	backoffSteps[0] = 50 * time.Millisecond
	t.Cleanup(func() { ncTiming = old; copy(backoffSteps, oldSteps); atriumBinary = oldBin })
}

// ncCard is a card with a terminal and a real directory, which is also where its
// handoff goes, so nothing is written to the system temp dir.
func ncCard(t *testing.T, d *Daemon) (*store.Task, *fakePTY, string) {
	t.Helper()
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "cycler", Worktree: filepath.ToSlash(dir), Runner: "claude", PID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Between turns, where a real card sits when a cycle is typed into it (r-022).
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetSetting(store.SettingContextHandoffDir, dir); err != nil {
		t.Fatal(err)
	}
	_, f := typedRunner(t, d, task.ID)
	t.Cleanup(func() { d.pending.stopAll(); d.nctx.stopAll() })
	return task, f, dir
}

// limitPrompt is the start of the limit prompt, to look for in what was typed.
const limitPrompt = "you are at context limit."

// ncAck is the agent writing its handoff and running `atrium ready`.
func ncAck(t *testing.T, d *Daemon, task *store.Task) {
	t.Helper()
	if err := os.WriteFile(d.handoffPath(task), handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.ready(task); err != nil {
		t.Fatalf("atrium ready was refused: %v", err)
	}
}

// ncToClear runs a manual cycle up to /clear: the prompt, the ack, the turn's end.
func ncToClear(t *testing.T, d *Daemon, task *store.Task, f *fakePTY) {
	t.Helper()
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	ncAck(t, d, task)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

// ncTurnEnds is what a Stop does to a card: its activity goes idle and its
// status leaves running. The cycle waits on both (r-022), since a card whose
// status still says running is not between turns whatever its activity says.
func ncTurnEnds(d *Daemon, id string) {
	d.act.set(id, ActivityIdle, "")
	_ = d.st.SetStatus(id, store.StatusNeedsInput)
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
func TestNewContextAsksClearsAndWakes(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	path := filepath.Join(dir, id+".md")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["step"] != NewContextLimit || v["file"] != path {
		t.Fatalf("the card does not show the limit step on its handoff: %v", d.newContextFor(id))
	}

	// 1. The limit prompt, naming the file and the binary, and only that.
	want := newContextLimitPrompt(path, "/opt/atrium/atrium")
	until(t, "the limit prompt", func() bool {
		return strings.Contains(f.written(), want) && strings.HasSuffix(f.written(), "\r")
	})
	if want != "you are at context limit. wrap what is in flight, write your handoff to "+path+
		", then run /opt/atrium/atrium ready." {
		t.Fatalf("the prompt reads %q", want)
	}

	// The turn runs and writes the file. Without the ack, nothing is cleared.
	d.act.set(id, ActivityThinking, "")
	if err := os.WriteFile(path, handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	ncTurnEnds(d, id)
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("/clear was typed with no ack: %q", f.written())
	}

	// The ack inside a turn: /clear waits for that turn to end.
	d.act.set(id, ActivityTool, "Bash")
	_ = d.st.SetStatus(id, store.StatusRunning)
	if _, _, err := d.ready(task); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("/clear was typed into the turn that ran atrium ready: %q", f.written())
	}
	ncTurnEnds(d, id)

	// 2. The turn ended, so /clear goes, and nothing else yet.
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["step"] != NewContextClear {
		t.Fatalf("the card does not show the clear step: %v", d.newContextFor(id))
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(f.written(), newContextWake(path)) {
		t.Fatalf("the wake was typed before the new session started: %q", f.written())
	}

	// 3. The new session starts, and the wake follows.
	d.wake.sawSession(id, time.Now())
	until(t, "the wake prompt", func() bool {
		return strings.Contains(f.written(), newContextWake(path)) && strings.HasSuffix(f.written(), "\r")
	})
	got := f.written()
	if !(strings.Index(got, want) < strings.Index(got, "/clear") &&
		strings.Index(got, "/clear") < strings.Index(got, newContextWake(path))) {
		t.Fatalf("the steps were not typed in order: %q", got)
	}
	if newContextWake(path) != "read "+path+" and continue." {
		t.Fatalf("the wake reads %q", newContextWake(path))
	}
	// /clear is a slash command, so nothing goes ahead of it.
	if strings.Contains(got, "[atrium] new context: \x1b[0m/clear") {
		t.Fatalf("/clear was labelled, which stops it being a command: %q", got)
	}
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
}

func TestNewContextStopsWhenNoSessionStartsAfterTheClear(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	ncToClear(t, d, task, f)

	// No SessionStart ever comes.
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, "/clear") {
		t.Fatalf("the reason does not name the step: %q", r)
	}
	if strings.Contains(f.written(), newContextWake(d.handoffPath(task))) {
		t.Fatalf("woke a session that never started: %q", f.written())
	}
}

// A SessionStart from before the /clear is not the new session.
func TestNewContextIgnoresASessionStartFromBeforeTheClear(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	d.wake.sawSession(task.ID, time.Now().Add(-time.Minute))
	ncToClear(t, d, task, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if strings.Contains(f.written(), newContextWake(d.handoffPath(task))) {
		t.Fatalf("took an old SessionStart for the new session: %q", f.written())
	}
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
	if err := d.StartNewContext(task.ID); !errors.Is(err, errNewContextBusy) {
		t.Fatalf("started a second sequence over the first: %v", err)
	}
}

// A failed chip stays until it is dismissed or the action is run again, and a rerun
// after the ack resumes where it stopped rather than asking for a second handoff.
func TestNewContextFailedChipIsDismissedOrReplaced(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	ncToClear(t, d, task, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	time.Sleep(30 * time.Millisecond)
	if failedWith(d, task.ID) == "" {
		t.Fatal("a failed chip went away by itself")
	}

	// Run again: /clear was typed, so it resumes at the wake, and asks for nothing.
	prompts := strings.Count(f.written(), limitPrompt)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatalf("could not run again over a failed chip: %v", err)
	}
	if v, _ := d.newContextFor(task.ID).(map[string]any); v == nil || v["step"] != NewContextWake {
		t.Fatalf("the rerun did not resume at the wake: %v", d.newContextFor(task.ID))
	}
	d.wake.sawSession(task.ID, time.Now())
	until(t, "the wake", func() bool { return strings.Contains(f.written(), newContextWake(d.handoffPath(task))) })
	if strings.Count(f.written(), limitPrompt) != prompts {
		t.Fatalf("a resumed cycle asked for its handoff again: %q", f.written())
	}
	until(t, "the chip to go", func() bool { return d.newContextFor(task.ID) == nil })

	// Started fresh and dismissed: gone, and the run types nothing more.
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the second limit prompt", func() bool {
		return strings.Count(f.written(), limitPrompt) > prompts && strings.HasSuffix(f.written(), "\r")
	})
	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+task.ID+"/new-context", nil)
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()
	d.handleNewContext(rec, req)
	if rec.Code != http.StatusOK || d.newContextFor(task.ID) != nil {
		t.Fatalf("dismiss left the chip: %d %s", rec.Code, rec.Body)
	}
	written := f.written()
	d.act.set(task.ID, ActivityThinking, "")
	ncTurnEnds(d, task.ID)
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
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
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
	ncTurnEnds(d, "c1")
	if got := d.act.turnsBegun("c1"); got != before+1 {
		t.Fatalf("one turn counted as %d", got-before)
	}
	d.act.set("c1", ActivityThinking, "")
	if got := d.act.turnsBegun("c1"); got != before+2 {
		t.Fatalf("a second turn counted as %d", got-before)
	}
}
