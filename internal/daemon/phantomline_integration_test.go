//go:build integration

package daemon

import (
	"testing"
	"time"
)

// Eleven characters tracked on the line while the prompt on screen is empty held a say for forty minutes. The screen
// wins once the terminal has settled.
func TestATrackedLineOverAnEmptyPromptOnScreenOpensTheGate(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, r := runningCard(t, d, idleFrame, time.Minute)
	r.typeMu.Lock()
	for _, c := range "hello world" {
		r.line.add(c)
	}
	r.typeMu.Unlock()
	if r.peerGateOpen() {
		t.Fatal("the gate was open before the screen was read")
	}

	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !r.peerGateOpen() {
		t.Fatalf("the gate stayed shut over an empty prompt: %+v", r.typing())
	}
	if n := r.typing().Count; n != 0 {
		t.Fatalf("the tracked line kept %d chars", n)
	}
}

func TestATrackedLineBesideTextOnScreenStaysHeld(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	frame := "\x1b[H\x1b[2J✻ Crunched for 1m 0s\r\n\r\n" + frameRule + "\r\n❯ hello world\r\n" + frameRule +
		"\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"
	_, r := runningCard(t, d, frame, time.Minute)
	r.typeMu.Lock()
	for _, c := range "hello world" {
		r.line.add(c)
	}
	r.typeMu.Unlock()

	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.peerGateOpen() {
		t.Fatal("the gate opened over text that is on the screen")
	}
}
