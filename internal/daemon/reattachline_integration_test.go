//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"strings"
	"testing"
	"time"
)

// A turn that ended while the room was down never posted its Stop, and the reattached line was marked unsure, so
// the gate stayed shut until a human pressed Enter. A settled idle prompt with nothing on it opens it.
func TestAReattachedRunnerAtAnEmptyIdlePromptOpensItsGateAndTakesTheCyclePrompt(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, r := runningCard(t, d, idleFrame, time.Minute)
	r.reattached.Store(true)
	r.line.lost(reattachLineKey)
	if r.peerGateOpen() {
		t.Fatal("the gate was open before the screen was read")
	}

	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !r.peerGateOpen() {
		t.Fatal("the gate stayed shut on an empty idle prompt")
	}
	if !d.CyclePromptDue(task.ID, &cycle.NewContext{}) {
		t.Fatal("the cycle prompt was still held after the reattach")
	}
}

func TestAReattachedRunnerWithTextOnItsPromptStaysUnsure(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	frame := "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ half a command\r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"
	_, r := runningCard(t, d, frame, time.Minute)
	r.reattached.Store(true)
	r.line.lost(reattachLineKey)

	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.peerGateOpen() {
		t.Fatal("the gate opened over a line with text on it")
	}
}

// A control-p on an empty prompt left the line unsure, and the gate held every say until the next keystroke even
// with the prompt visibly empty.
func TestAControlPLineQuietAtAnEmptyPromptOpensItsGateAndOneWithTextDoesNot(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, r := runningCard(t, d, idleFrame, time.Minute)
	r.noteOperatorTyped([]byte{0x10})
	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.peerGateOpen() {
		t.Fatal("the gate opened a moment after the key")
	}
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-time.Minute)
	r.typeMu.Unlock()
	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !r.peerGateOpen() {
		t.Fatal("the gate stayed shut on an empty prompt after control-p")
	}

	frame := "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ a recalled line\r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"
	d2 := testDaemon(t)
	_, w := runningCard(t, d2, frame, time.Minute)
	w.noteOperatorTyped([]byte{0x10})
	w.typeMu.Lock()
	w.lastTyped = time.Now().Add(-time.Minute)
	w.typeMu.Unlock()
	if err := d2.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if w.peerGateOpen() {
		t.Fatal("the gate opened over a recalled line")
	}
}

const ghostFrame = "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ \x1b[7mr\x1b[0m\x1b[38;2;136;136;136mun the tests\x1b[0m\r\n" +
	frameRule + "\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"

func TestAReattachedRunnerShowingOnlyASuggestionDeliversAHeldSayWithNoKey(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	t.Cleanup(func() { d.pending.stopAll() })
	alice := peerCard(t, d, "alice")
	task, r := runningCard(t, d, ghostFrame, time.Minute)
	d.act.set(task.ID, ActivityIdle, "")
	r.reattached.Store(true)
	r.line.lost(reattachLineKey)
	f := r.pty.(*fakePTY)

	if code := message(t, d, task.ID, map[string]string{"text": "GHOSTWORDS", "from": alice.WireName}); code != 200 {
		t.Fatalf("the say answered %d", code)
	}
	if strings.Contains(f.written(), "GHOSTWORDS") {
		t.Fatal("typed before the screen was read")
	}
	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(f.written(), "GHOSTWORDS") {
		time.Sleep(20 * time.Millisecond) // wait: poll step under the 10 s deadline
	}
	if !strings.Contains(f.written(), "GHOSTWORDS") {
		t.Fatalf("the held say was not delivered: %q", f.written())
	}
}

func TestAReattachedRunnerShowingASuggestionAfterANoBreakSpaceDeliversAHeldSay(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	t.Cleanup(func() { d.pending.stopAll() })
	alice := peerCard(t, d, "alice")
	task, r := runningCard(t, d, strings.Replace(ghostFrame, "❯ ", "❯\u00a0", 1), time.Minute)
	d.act.set(task.ID, ActivityIdle, "")
	r.reattached.Store(true)
	r.line.lost(reattachLineKey)
	f := r.pty.(*fakePTY)

	if code := message(t, d, task.ID, map[string]string{"text": "NBSPWORDS", "from": alice.WireName}); code != 200 {
		t.Fatalf("the say answered %d", code)
	}
	if strings.Contains(f.written(), "NBSPWORDS") {
		t.Fatal("typed before the screen was read")
	}
	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(f.written(), "NBSPWORDS") {
		time.Sleep(20 * time.Millisecond) // wait: poll step under the 10 s deadline
	}
	if !strings.Contains(f.written(), "NBSPWORDS") {
		t.Fatalf("the held say was not delivered: %q", f.written())
	}
}

// A turn that ended after a restart left the line unsure, because a Stop was not taken as proof, and an idle card at
// an empty prompt held its mail for 20 minutes.
func TestAStopAfterAReattachOpensTheGateUnlessAKeyCameSince(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, r := runningCard(t, d, "", time.Minute)
	r.reattached.Store(true)
	r.line.lost(reattachLineKey)
	if !r.resetLineAtStop() || !r.peerGateOpen() {
		t.Fatalf("the first Stop after the reattach left the gate shut: %+v", r.typing())
	}

	_, r2 := runningCard(t, d, "", time.Minute)
	r2.reattached.Store(true)
	r2.line.lost(reattachLineKey)
	r2.noteOperatorTyped([]byte("x"))
	if r2.resetLineAtStop() || r2.peerGateOpen() {
		t.Fatal("a Stop emptied a draft typed since the reattach")
	}
}
