//go:build integration

package daemon

import (
	"encoding/json"
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const heldSay = "PEERSAYHELD"

// sayViaMessage posts a session's say to the message endpoint.
func sayViaMessage(t *testing.T, d *Daemon, from, id, text string) map[string]any {
	t.Helper()
	body := `{"from":"` + from + `","text":"` + text + `"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/message", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleMessage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("message answered %d: %s", w.Code, w.Body.String())
	}
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// heldNotDelivered checks a say is queued, unread by either hook and untyped.
func heldNotDelivered(t *testing.T, d *Daemon, id string, f *fakePTY) {
	t.Helper()
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("a say was typed during the cycle: %q", f.written())
	}
	for _, via := range []string{"permission", "stop"} {
		if msgs, _ := d.takeMessages(id, via); len(msgs) != 0 {
			t.Fatalf("the %s hook took a held message", via)
		}
	}
	if n := len(pendingFrom(t, d, id)); n != 1 {
		t.Fatalf("%d messages queued, want the held one", n)
	}
}

func finishCycle(t *testing.T, d *Daemon, id, dir string, f *fakePTY) {
	t.Helper()
	d.act.set(id, ActivityThinking, "")
	task, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	ncAck(t, d, task)
	time.Sleep(60 * time.Millisecond) // wait: the turn must not end in the ack's own tick
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

// ncWakeMark is the part of the wake prompt that does not depend on the card's handoff path.
const ncWakeMark = "and continue."

func TestSayDuringCaptureIsHeldAndDeliveredAfterTheWake(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if !d.HoldingMessages(id) {
		t.Fatal("not holding while waiting for atrium ready")
	}
	out := sayViaMessage(t, d, "alice", id, heldSay)
	if out["delivered"] != "queued" || out["warning"] != cycle.NewContextHoldNote {
		t.Fatalf("answer while holding: %v", out)
	}
	heldNotDelivered(t, d, id, f)

	finishCycle(t, d, id, dir, f)
	d.Wake.SawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, heldSay) < strings.Index(got, ncWakeMark) {
		t.Fatalf("the held say went ahead of the wake prompt: %q", got)
	}
	if d.HoldingMessages(id) {
		t.Fatal("still holding after the wake")
	}
}

func TestSayDuringClearIsHeldAndTellIsQueued(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	finishCycle(t, d, id, dir, f)

	out, code := tell(t, d, "alice", "cycler", heldSay)
	if code != http.StatusOK || out["queued"] != true || out["typed"] == true || out["note"] != cycle.NewContextHoldNote {
		t.Fatalf("tell during clear: %d %v", code, out)
	}
	heldNotDelivered(t, d, id, f)

	d.Wake.SawSession(id, time.Now())
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, heldSay) < strings.Index(got, ncWakeMark) {
		t.Fatalf("the held say went ahead of the wake prompt: %q", got)
	}
}

func TestAFailedCycleReleasesWhatWasHeld(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	sayViaMessage(t, d, "alice", id, heldSay)
	if strings.Contains(f.written(), heldSay) {
		t.Fatal("typed while waiting for atrium ready")
	}
	// No new session starts after /clear, so the cycle fails.
	finishCycle(t, d, id, dir, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
	if d.HoldingMessages(id) {
		t.Fatal("a failed cycle is still holding")
	}
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
}

func TestDismissingACycleReleases(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.act.set(id, ActivityThinking, "")
	sayViaMessage(t, d, "alice", id, heldSay)
	r := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	r.SetPathValue("id", id)
	d.HandleNewContext(httptest.NewRecorder(), r)
	if d.HoldingMessages(id) {
		t.Fatal("still holding after a dismissal")
	}
}

// The cycle's own typing is not held, and a card outside a cycle is unaffected.
func TestHoldDoesNotStopTheCyclesOwnTyping(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if d.HoldingMessages(id) {
		t.Fatal("holding with no cycle")
	}
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	finishCycle(t, d, id, dir, f)
	d.Wake.SawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the chip to go", func() bool { return d.NewContextFor(id) == nil })
}

func TestOperatorSayDuringACycleIsNotHeldButAPeersIs(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if out := sayViaMessage(t, d, "alice", id, heldSay); out["warning"] != cycle.NewContextHoldNote {
		t.Fatalf("a peer's say was not held: %v", out)
	}
	out := sayViaMessage(t, d, "", id, "OPERATORLINE")
	if out["warning"] == cycle.NewContextHoldNote || out["delivered"] == "queued" {
		t.Fatalf("the operator's say was held by the cycle: %v", out)
	}
	until(t, "the operator's line", func() bool { return strings.Contains(f.written(), "OPERATORLINE") })
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("the peer's say was typed during the cycle: %q", f.written())
	}
}

func TestACycleWhoseLimitPromptWasNotTypedHoldsNothing(t *testing.T) {
	d := testDaemon(t)
	id := "cycler-untyped"
	d.Nctx.Mu.Lock()
	d.Nctx.By[id] = &cycle.NewContext{Step: cycle.NewContextLimit, Since: time.Now(), Armed: true}
	d.Nctx.Mu.Unlock()
	if d.HoldingMessages(id) || d.HoldingFrom(id, "alice") {
		t.Fatal("a cycle that has typed nothing held messages")
	}
	d.Nctx.Mu.Lock()
	d.Nctx.By[id].Prompted = 1
	d.Nctx.Mu.Unlock()
	if !d.HoldingMessages(id) || !d.HoldingFrom(id, "alice") || d.HoldingFrom(id, "") {
		t.Fatal("after the prompt the cycle should hold a peer's message and never the operator's")
	}
}

func TestABoardCycleWaitsForThePartTypedLineBeforeItHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	run := d.sup.get(id)
	run.noteOperatorTyped([]byte("sure. you can move them"))

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // wait: negative check, the cycle must not hold
	v, _ := d.NewContextFor(id).(map[string]any)
	if d.Nctx.TypingHeld(id) || d.HoldingMessages(id) || v["held"] != false || strings.Contains(f.written(), limitPrompt) {
		t.Fatalf("the cycle held input over a part-typed line: %v", v)
	}

	run.noteOperatorTyped([]byte(strings.Repeat("\x7f", len("sure. you can move them"))))
	run.typeMu.Lock()
	run.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	run.typeMu.Unlock()
	until(t, "the hold once the line is clear", func() bool { return d.Nctx.TypingHeld(id) })
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if v, _ := d.NewContextFor(id).(map[string]any); v["held"] != true {
		t.Fatalf("an armed cycle does not say it holds: %v", v)
	}
}

func TestEveryAutomaticTriggerWaitsForThePartTypedLine(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	for name, start := range map[string]func(d *Daemon, id string) error{
		"cold handoff": func(d *Daemon, id string) error {
			task, err := d.st.Get(id)
			if err != nil {
				return err
			}
			return d.ColdHandoff(task)
		},
		"context ceiling": func(d *Daemon, id string) error {
			task, err := d.st.Get(id)
			if err != nil {
				return err
			}
			d.startCycle(task, 300_000, 200_000)
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := testDaemon(t)
			fastNewContext(t, d)
			task, f, _ := ncCard(t, d)
			id := task.ID
			run := d.sup.get(id)
			run.noteOperatorTyped([]byte("ok now review the captions"))

			if err := start(d, id); err != nil {
				t.Fatal(err)
			}
			until(t, "the cycle's claim", func() bool { return d.Nctx.Get(id) != nil })
			time.Sleep(100 * time.Millisecond) // wait: negative check, a hold would have armed by now
			if d.Nctx.TypingHeld(id) || d.HoldingMessages(id) || strings.Contains(f.written(), limitPrompt) {
				t.Fatal("the cycle held input or typed over a part-typed line")
			}

			run.noteOperatorTyped([]byte(strings.Repeat("\x7f", len("ok now review the captions"))))
			run.typeMu.Lock()
			run.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
			run.typeMu.Unlock()
			until(t, "the hold once the line is clear", func() bool { return d.Nctx.TypingHeld(id) })
		})
	}
}

func TestTheLineMovedIntoTheComposeBoxIsClearedOffTheTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	run := d.sup.get(id)
	if d.NcClearLine(id, 3) {
		t.Fatal("a line was cleared with no new context holding input")
	}

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the hold", func() bool { return d.Nctx.TypingHeld(id) })
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	until(t, "atrium's typing to end", func() bool { return !d.Nctx.TypingNow(id) })
	run.noteOperatorTyped([]byte("héllo"))

	d.Nctx.Mu.Lock()
	d.Nctx.By[id].Typing = true
	d.Nctx.Mu.Unlock()
	if d.NcClearLine(id, 5) {
		t.Fatal("the line was cleared while atrium was typing into it")
	}
	d.Nctx.Mu.Lock()
	d.Nctx.By[id].Typing = false
	d.Nctx.Mu.Unlock()

	before := len(f.written())
	if !d.NcClearLine(id, 5) {
		t.Fatal("the line moved into the box was not cleared")
	}
	if got := f.written()[before:]; got != strings.Repeat("\x7f", 5) {
		t.Fatalf("typed %q to clear the line, want five backspaces", got)
	}
	run.typeMu.Lock()
	n := run.line.count()
	run.typeMu.Unlock()
	if n != 0 {
		t.Fatalf("the room still counts %d character(s) on the line", n)
	}
}
