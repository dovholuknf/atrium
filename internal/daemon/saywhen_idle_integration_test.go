//go:build integration

package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// A done message to a session with no turn running has nothing to wait for.
func TestADoneMessageToAnIdleSessionIsTypedAtOnce(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityIdle, "")

	sayDone(t, d, target.ID, "build is green")
	if got := f.written(); !strings.Contains(got, "build is green") {
		t.Fatalf("a done message to an idle session was not typed: %q", got)
	}
}

// A busy session still gets it when the turn ends, not before.
func TestADoneMessageToABusySessionStillWaits(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	doneTo(t, d, target.ID, "sg4/doer", "build is green")
	d.pending.attempt(target.ID)
	if got := f.written(); got != "" {
		t.Fatalf("a done message was typed mid-turn: %q", got)
	}
	d.act.set(target.ID, ActivityIdle, "")
	d.pending.attempt(target.ID)
	if got := f.written(); !strings.Contains(got, "build is green") {
		t.Fatalf("the done message was not typed once the turn ended: %q", got)
	}
}

// A runner that takes no input mid-turn falls back to done, and idle means now.
func TestAMessageToAnIdleRunnerWithoutMidTurnInputIsTypedAtOnce(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	h, err := d.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.MidTurnInput = false
	if _, err := d.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityIdle, "")

	if code := message(t, d, target.ID, map[string]string{"text": "ping", "from": "sg4/doer"}); code != 200 {
		t.Fatalf("message answered %d", code)
	}
	if got := f.written(); !strings.Contains(got, "ping") {
		t.Fatalf("an idle target was not typed to: %q", got)
	}
}

// The turn ended but its Stop hook never landed, so the activity still reads busy. A card that looks idle has no turn.
func TestALooksIdleSessionHasNoTurnToWaitFor(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityThinking, "")
	doneTo(t, d, target.ID, "sg4/doer", "build is green")
	d.pending.attempt(target.ID)
	if got := f.written(); got != "" {
		t.Fatalf("typed mid-turn: %q", got)
	}

	d.act.markLooksIdle(target.ID, idleMark{at: time.Now()})
	d.pending.attempt(target.ID)
	if got := f.written(); !strings.Contains(got, "build is green") {
		t.Fatalf("a looks-idle session still held the done message: %q", got)
	}
}

// sayDone sends a `when: done` message without expecting it to be queued.
func sayDone(t *testing.T, d *Daemon, taskID, text string) {
	t.Helper()
	fields := map[string]string{"text": text, "when": "done", "from": "sg4/doer"}
	if code := message(t, d, taskID, fields); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
}
