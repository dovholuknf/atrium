//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
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
