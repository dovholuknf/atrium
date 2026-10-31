//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestReadyWithTextWritesTheHandoffAndCompletesTheCycle(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })

	text := string(handoffBody) + "\nwritten by the tool"
	out, code, err := d.ReadyHandoff(task, &text)
	if err != nil {
		t.Fatalf("refused with %d: %v", code, err)
	}
	if out["stored"] != true {
		t.Fatalf("the handoff was not stored on the card: %v", out)
	}
	got, err := os.ReadFile(cycleFile(d, task.ID))
	if err != nil || string(got) != text {
		t.Fatalf("the handoff file holds %q, %v", got, err)
	}
	evs, _ := d.st.Events(task.ID, 100)
	stored := false
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "written by the tool") {
			stored = true
		}
	}
	if !stored {
		t.Fatal("no event on the card carries the handoff")
	}
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

func TestReadyWithTextIsRefusedWithNoCycle(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, _, _ := ncCard(t, d)
	text := string(handoffBody)
	_, code, err := d.ReadyHandoff(task, &text)
	if err == nil || code != http.StatusConflict || !strings.Contains(err.Error(), "no context cycle") {
		t.Fatalf("got %d, %v", code, err)
	}
	if _, statErr := os.Stat(d.handoffPath(task)); statErr == nil {
		t.Fatal("a handoff was written for a card with no cycle")
	}
}

func TestReadyWithEmptyTextIsRefused(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	empty := "  \n"
	_, code, err := d.ReadyHandoff(task, &empty)
	if err == nil || code != http.StatusConflict || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("got %d, %v", code, err)
	}
	if !d.nctx.waitingForAck(task.ID) {
		t.Fatal("an empty handoff acked the cycle")
	}
}

// The tool is not on every card's list, so a runner with a shell is told only the CLI.
func TestTheLimitPromptNamesOnlyTheCLIAndAShellessRunnerGetsOnlyTheTool(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if w := f.written(); strings.Contains(w, "atrium_ready") || !strings.Contains(w, "/opt/atrium/atrium ready") {
		t.Fatalf("an ordinary runner was told %q", w)
	}

	d2 := testDaemon(t)
	fastNewContext(t, d2)
	task2, f2, _ := ncCard(t, d2)
	if _, err := d2.st.SaveHarness(store.Harness{ID: "claude", Cmd: "claude", LaunchMode: store.LaunchPTY,
		Args: []string{"--disallowedTools", "Bash,PowerShell,Edit,Write"}}); err != nil {
		t.Fatal(err)
	}
	if err := d2.StartNewContext(task2.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f2.written(), limitPrompt) })
	if w := f2.written(); !strings.Contains(w, "atrium_ready") || strings.Contains(w, "/opt/atrium/atrium") {
		t.Fatalf("a runner with no shell was told %q", w)
	}
}

func TestTheCycleStopsAskingAfterThreeAsks(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	d.hk.ncTiming.promptLost = 40 * time.Millisecond
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "three asks", func() bool { return strings.Count(f.written(), limitPrompt) >= maxLimitAsks })
	time.Sleep(400 * time.Millisecond) // wait: proving a fourth ask never comes, so there is no condition to spin on
	if n := strings.Count(f.written(), limitPrompt); n != maxLimitAsks {
		t.Fatalf("asked %d times", n)
	}
	v, _ := d.newContextFor(task.ID).(map[string]any)
	if v == nil || v["label"] != "new context waiting: the session did not call ready after 3 asks" {
		t.Fatalf("the card says %v", v)
	}
	if !d.nctx.waitingForAck(task.ID) {
		t.Fatal("the cycle gave up the card instead of leaving it waiting")
	}
	text := string(handoffBody)
	if _, code, err := d.ReadyHandoff(task, &text); err != nil {
		t.Fatalf("a late ready was refused with %d: %v", code, err)
	}
}

func TestReadyNamesWhatItLookedUpAndFindsACardByAliasOrID(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, _, _ := ncCard(t, d)
	for _, who := range []string{task.WireName, task.ID} {
		if got, err := d.taskFor(who, ""); err != nil || got.ID != task.ID {
			t.Fatalf("%q did not find the card: %v", who, err)
		}
	}
	rec := httptest.NewRecorder()
	d.HandleReady(rec, httptest.NewRequest(http.MethodPost, "/ready", strings.NewReader(`{"agent":"nobody-here"}`)))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "nobody-here") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
