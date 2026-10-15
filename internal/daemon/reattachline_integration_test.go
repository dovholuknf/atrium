//go:build integration

package daemon

import (
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
	if !d.cyclePromptDue(task.ID, &newContext{}) {
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
