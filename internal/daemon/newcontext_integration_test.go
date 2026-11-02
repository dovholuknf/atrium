//go:build integration

package daemon

import (
	"errors"
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/store"
)

// fastNewContext shrinks every wait of one daemon so a whole sequence runs in a fraction of a
// second. The waits are what is under test, so a test that wants one to expire sets that one shorter still.
func fastNewContext(t *testing.T, d *Daemon) {
	t.Helper()
	d.hk.NcTiming = cycle.NcTimings{
		Poll:          5 * time.Millisecond,
		TypeWait:      400 * time.Millisecond,
		AckWait:       d.hk.NcTiming.AckWait,
		CaptureBegin:  400 * time.Millisecond,
		CaptureEnd:    2 * time.Second,
		ClearTypeWait: 2 * time.Second,
		AckQuiet:      time.Hour,
		NoteAfter:     30 * time.Millisecond,
		LogEvery:      50 * time.Millisecond,
		TurnSettle:    40 * time.Millisecond,
		ClearWait:     400 * time.Millisecond,
		SessionSettle: 20 * time.Millisecond,
		PromptGap:     0,
		PromptLost:    300 * time.Millisecond,
	}
	d.hk.AtriumBinary = func() string { return "/opt/atrium/atrium" }
	// A say held through the cycle is retried on this front step.
	d.hk.backoffSteps[0] = 50 * time.Millisecond
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
	t.Cleanup(func() { d.pending.stopAll(); d.Nctx.StopAll() })
	return task, f, dir
}

// limitPrompt is the start of the limit prompt, to look for in what was typed.
const limitPrompt = "Pause your work now."

// cycleFile is the handoff file the card's cycle asked for.
func cycleFile(d *Daemon, id string) string {
	if c := d.Nctx.Get(id); c != nil {
		return c.File
	}
	return ""
}

// ncAck is the agent writing its handoff and running `atrium ready`.
func ncAck(t *testing.T, d *Daemon, task *store.Task) {
	t.Helper()
	if err := os.WriteFile(cycleFile(d, task.ID), handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Ready(task); err != nil {
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
	if !testwait.PollEvery(2*time.Millisecond, 3*time.Second, cond) {
		t.Fatalf("gave up waiting for %s", what)
	}
}

func failedWith(d *Daemon, id string) string {
	v, _ := d.NewContextFor(id).(map[string]any)
	if v == nil || v["step"] != cycle.NewContextFailed {
		return ""
	}
	r, _ := v["reason"].(string)
	return r
}

// The whole sequence, in order, each step held until the one before it is over.
func TestNewContextAsksClearsAndWakes(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	path := cycleFile(d, id)
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), id+"-") {
		t.Fatalf("the handoff is at %s, want %s-<time>.md in %s", path, id, dir)
	}
	if v, _ := d.NewContextFor(id).(map[string]any); v == nil || v["step"] != cycle.NewContextLimit || v["file"] != path {
		t.Fatalf("the card does not show the limit step on its handoff: %v", d.NewContextFor(id))
	}

	// 1. The limit prompt, naming the file and the binary, and only that.
	want := cycle.NewContextLimitPrompt(path, "/opt/atrium/atrium", false, 0, 0)
	until(t, "the limit prompt", func() bool {
		return strings.Contains(f.written(), want) && strings.HasSuffix(f.written(), "\r")
	})
	if want != "Atrium is starting a new context on this card. Pause your work now. Write a handoff to "+path+
		" that says what is complete, what is in flight, and what's still to be done. Then run /opt/atrium/atrium "+
		"ready. Atrium clears your context when this turn ends and starts a new one from your handoff." {
		t.Fatalf("the prompt reads %q", want)
	}

	// The turn runs and writes the file. Without the ack, nothing is cleared.
	d.act.set(id, ActivityThinking, "")
	if err := os.WriteFile(path, handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	ncTurnEnds(d, id)
	time.Sleep(150 * time.Millisecond) // wait: nothing may be cleared without the ack
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("/clear was typed with no ack: %q", f.written())
	}

	// The ack inside a turn: /clear waits for that turn to end.
	d.act.set(id, ActivityTool, "Bash")
	_ = d.st.SetStatus(id, store.StatusRunning)
	if _, _, err := d.Ready(task); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // wait: nothing may be cleared inside the turn
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("/clear was typed into the turn that ran atrium ready: %q", f.written())
	}
	ncTurnEnds(d, id)

	// 2. The turn ended, so /clear goes, and nothing else yet.
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if v, _ := d.NewContextFor(id).(map[string]any); v == nil || v["step"] != cycle.NewContextClear {
		t.Fatalf("the card does not show the clear step: %v", d.NewContextFor(id))
	}
	time.Sleep(60 * time.Millisecond) // wait: no wake may be typed before the new session starts
	if strings.Contains(f.written(), cycle.NewContextWakeText(path)) {
		t.Fatalf("the wake was typed before the new session started: %q", f.written())
	}

	// 3. The new session starts, and the wake follows.
	d.Wake.SawSession(id, time.Now())
	until(t, "the wake prompt", func() bool {
		return strings.Contains(f.written(), cycle.NewContextWakeText(path)) && strings.HasSuffix(f.written(), "\r")
	})
	got := f.written()
	if !(strings.Index(got, want) < strings.Index(got, "/clear") &&
		strings.Index(got, "/clear") < strings.Index(got, cycle.NewContextWakeText(path))) {
		t.Fatalf("the steps were not typed in order: %q", got)
	}
	if cycle.NewContextWakeText(path) != "read "+path+" and continue." {
		t.Fatalf("the wake reads %q", cycle.NewContextWakeText(path))
	}
	// /clear is a slash command, so nothing goes ahead of it.
	if strings.Contains(got, "[atrium] new context: \x1b[0m/clear") {
		t.Fatalf("/clear was labelled, which stops it being a command: %q", got)
	}
	until(t, "the chip to go", func() bool { return d.NewContextFor(id) == nil })
}

func TestNewContextStopsWhenNoSessionStartsAfterTheClear(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	ncToClear(t, d, task, f)

	// No SessionStart ever comes.
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, "/clear") {
		t.Fatalf("the reason does not name the step: %q", r)
	}
	if strings.Contains(f.written(), cycle.NewContextWakeText(cycleFile(d, task.ID))) {
		t.Fatalf("woke a session that never started: %q", f.written())
	}
}

// A SessionStart from before the /clear is not the new session.
func TestNewContextIgnoresASessionStartFromBeforeTheClear(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	d.Wake.SawSession(task.ID, time.Now().Add(-time.Minute))
	ncToClear(t, d, task, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if strings.Contains(f.written(), cycle.NewContextWakeText(cycleFile(d, task.ID))) {
		t.Fatalf("took an old SessionStart for the new session: %q", f.written())
	}
}

// The gate every automated write goes through: a part written line is waited out.
func TestNewContextWaitsForAnEmptyLine(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	run := d.sup.get(task.ID)
	run.noteOperatorTyped([]byte("half a thou"))

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // wait: nothing may be typed over the operator's line
	if f.written() != "" {
		t.Fatalf("typed over the operator's line: %q", f.written())
	}
}

func TestNewContextIsRefusedWithoutATerminalAndWhileRunning(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)

	bare := cardFor(t, d, "not-ours")
	if err := d.StartNewContext(bare.ID); err != cycle.ErrNoTerminal {
		t.Fatalf("started on a card atrium owns no terminal for: %v", err)
	}

	task, _, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.StartNewContext(task.ID); !errors.Is(err, cycle.ErrNewContextBusy) {
		t.Fatalf("started a second sequence over the first: %v", err)
	}
}

// A failed chip stays until it is dismissed or the action is run again, and a rerun
// after the ack resumes where it stopped rather than asking for a second handoff.
func TestNewContextFailedChipIsDismissedOrReplaced(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	ncToClear(t, d, task, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	time.Sleep(30 * time.Millisecond) // wait: the failed chip must stay
	if failedWith(d, task.ID) == "" {
		t.Fatal("a failed chip went away by itself")
	}

	// Run again: /clear was typed, so it resumes at the wake, and asks for nothing.
	prompts := strings.Count(f.written(), limitPrompt)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatalf("could not run again over a failed chip: %v", err)
	}
	if v, _ := d.NewContextFor(task.ID).(map[string]any); v == nil || v["step"] != cycle.NewContextWake {
		t.Fatalf("the rerun did not resume at the wake: %v", d.NewContextFor(task.ID))
	}
	d.Wake.SawSession(task.ID, time.Now())
	until(t, "the wake", func() bool { return strings.Contains(f.written(), cycle.NewContextWakeText(cycleFile(d, task.ID))) })
	if strings.Count(f.written(), limitPrompt) != prompts {
		t.Fatalf("a resumed cycle asked for its handoff again: %q", f.written())
	}
	until(t, "the chip to go", func() bool { return d.NewContextFor(task.ID) == nil })

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
	d.HandleNewContext(rec, req)
	if rec.Code != http.StatusOK || d.NewContextFor(task.ID) != nil {
		t.Fatalf("dismiss left the chip: %d %s", rec.Code, rec.Body)
	}
	written := f.written()
	d.act.set(task.ID, ActivityThinking, "")
	ncTurnEnds(d, task.ID)
	time.Sleep(150 * time.Millisecond) // wait: a dismissed run must type nothing
	if f.written() != written {
		t.Fatalf("a dismissed run kept typing: %q", f.written()[len(written):])
	}
}

func TestNewContextEndpoint(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)

	post := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/new-context", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		d.HandleNewContext(rec, req)
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
	t.Parallel()
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
