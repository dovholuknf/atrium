//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A RUNNER THAT QUEUES A LINE TYPED MID-TURN is never typed into mid-turn, by any path. See saywhen.go and
// docs/terminal/queued-box-findings.md.

// withoutQueueBox turns off claude's `queues_mid_turn`, so a test of the typing gate's own rules runs against a runner
// that reads a typed line as it arrives, the way codex does.
func withoutQueueBox(t *testing.T, d *Daemon) {
	t.Helper()
	h, err := d.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.QueuesMidTurn = false
	if _, err := d.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
}

// codexPair is a supervised card whose runner is codex, which steers typed input into the active turn.
func codexPair(t *testing.T, d *Daemon) (*store.Task, *fakePTY) {
	t.Helper()
	target, _, err := d.st.Register(store.Observed{
		WireName: "steered", Worktree: "d:/git/atrium", Runner: "codex", PID: impossiblePID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, f := typedRunner(t, d, target.ID)
	fresh, err := d.st.Get(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	return fresh, f
}

func TestTheProfileAndTheSeededRowSayClaudeQueuesAndCodexDoesNot(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	claude, err := d.st.Harness("claude")
	if err != nil || !claude.QueuesMidTurn {
		t.Fatalf("claude does not queue a mid-turn line: %+v, %v", claude, err)
	}
	codex, err := d.st.Harness("codex")
	if err != nil || codex.QueuesMidTurn {
		t.Fatalf("codex queues a mid-turn line: %+v, %v", codex, err)
	}
}

// THE OPERATOR'S CHANNEL: no `from`. It used to type mid-turn and answer `terminal`.
func TestTheOperatorsChannelIsNotTypedIntoAClaudeMidTurn(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	out := sayViaMessage(t, d, "", target.ID, "stop now")
	if f.written() != "" {
		t.Fatalf("typed into a runner that would only queue it: %q", f.written())
	}
	if out["delivered"] != "queued" {
		t.Fatalf("the answer is not queued: %v", out)
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("the injector holds a line it must not type")
	}
	// The hook that is running carries it.
	msgs, err := d.takeMessages(target.ID, "permission")
	if err != nil || len(msgs) != 1 || msgs[0].Text != "stop now" {
		t.Fatalf("the permission hook did not carry it: %v, %v", msgs, err)
	}
}

// A SESSION'S SAY, with a hook not yet heard from: the runner is hook-capable, so it counts.
func TestAClaudeNotYetHeardOnAHookIsNotTypedIntoMidTurn(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityThinking, "")
	if target.ToolHookSeenAt != nil || target.StopHookSeenAt != nil {
		t.Fatal("the card was heard from already")
	}

	out := sayViaMessage(t, d, "sg4/doer", target.ID, "stop now")
	if f.written() != "" || out["delivered"] != "queued" {
		t.Fatalf("typed or answered wrongly: %q %v", f.written(), out)
	}
}

// THE ESCALATION: a done message aged past escalate_held_after was typed mid-turn. Now its hook carries it, with the
// waited line, and nothing is written.
func TestAnEscalatedDoneIsNotTypedIntoAClaudeMidTurn(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	out := sayWhen(t, d, "sg4/doer", target.ID, "neither matrix exists", "done")
	if out["delivered"] != "queued" {
		t.Fatalf("a done say is not queued: %v", out)
	}
	pending, _ := d.st.PendingMessages(target.ID)
	if len(pending) != 1 {
		t.Fatalf("pending %d", len(pending))
	}
	age(t, d, pending[0].ID, 20*time.Minute)
	d.pending.mu.Lock()
	if ht := d.pending.by[target.ID]; ht != nil {
		for i := range ht.entries {
			ht.entries[i].at = time.Now().Add(-20 * time.Minute)
		}
	}
	d.pending.mu.Unlock()
	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("the escalation typed mid-turn into a runner that queues it: %q", f.written())
	}

	msgs, err := d.takeMessages(target.ID, "permission")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("the hook did not carry the aged message: %d, %v", len(msgs), err)
	}
	if !strings.Contains(msgs[0].Text, "waited 20 minutes for your turn to end") ||
		!strings.Contains(msgs[0].Text, "neither matrix exists") {
		t.Fatalf("the carried text has no framing line: %q", msgs[0].Text)
	}
}

// THE RETRY BACKOFF: a line held for the operator's part written line is still retried, and when the runner has gone
// mid-turn meanwhile the retry does not type it, whatever the line now is. The turn ending re-arms it.
func TestTheRetryDoesNotTypeMidTurnIntoAClaude(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })

	// Idle, a part written line: queued and held for the line.
	partial := "half a thou"
	r.noteOperatorTyped([]byte(partial))
	if out := sayViaMessage(t, d, "sg4/doer", target.ID, "the build is green"); out["delivered"] != "queued" {
		t.Fatalf("not queued behind a part written line: %v", out)
	}
	if heldCount(d.pending, target.ID) != 1 {
		t.Fatal("the line is not held for the retry")
	}

	// The operator clears the line and walks away, and the runner starts a turn.
	r.noteOperatorTyped([]byte(strings.Repeat("\x7f", len(partial))))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()
	d.act.set(target.ID, ActivityTool, "Bash")
	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("the retry typed mid-turn into a runner that queues it: %q", f.written())
	}
	if heldCount(d.pending, target.ID) != 1 {
		t.Fatal("the held line was dropped")
	}

	// The turn ends: the retry types it into an idle runner.
	d.onActivity(ActivityEvent{TaskID: target.ID, Event: "idle"})
	d.pending.attempt(target.ID)
	if got := f.written(); !strings.Contains(got, "the build is green") {
		t.Fatalf("the line was not typed once the runner was idle: %q", got)
	}
}

// The held peer lines are left to the typist while a hook runs, except one that would only fill the box.
func TestAHookCarriesALineHeldWhileTheRunnerIsMidTurn(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a thou"))
	sayViaMessage(t, d, "sg4/doer", target.ID, "the build is green")
	d.act.set(target.ID, ActivityTool, "Bash")

	msgs, err := d.takeMessages(target.ID, "permission")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("the hook did not carry a line the typist cannot type mid-turn: %d, %v", len(msgs), err)
	}
	if f.written() != "" {
		t.Fatalf("something was typed: %q", f.written())
	}
}

// AN IDLE RUNNER is unchanged: typed, answered `terminal`, its prompt submit is the read.
func TestAnIdleClaudeIsStillTypedAndAnswersTerminal(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityIdle, "")

	for _, from := range []string{"", "sg4/doer"} {
		before := len(f.written())
		out := sayViaMessage(t, d, from, target.ID, "ping"+from)
		if out["delivered"] != "terminal" {
			t.Fatalf("from %q: an idle runner did not answer terminal: %v", from, out)
		}
		if len(f.written()) == before {
			t.Fatalf("from %q: nothing was typed into an idle runner", from)
		}
	}
}

// CODEX IS UNCHANGED: it steers typed input into the active turn, so it is typed mid-turn and answers `terminal`.
func TestCodexIsStillTypedIntoMidTurn(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, f := codexPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "shell")

	for _, from := range []string{"", "sg4/doer"} {
		out := sayViaMessage(t, d, from, target.ID, "stop now"+from)
		if out["delivered"] != "terminal" {
			t.Fatalf("from %q: codex was not typed into mid-turn: %v", from, out)
		}
	}
	if !strings.Contains(f.written(), "stop now") {
		t.Fatalf("nothing reached codex: %q", f.written())
	}
}

// FORCE types past the turn rule, so a forced say mid-turn into a runner that queues it is in the box and not read.
func TestAForcedSayMidTurnIntoAClaudeAnswersTypedQueued(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	code, out := forceSayVia(t, d, target.ID, `{"text":"FORCEDWORDS","force":true}`)
	if code != 200 || out["delivered"] != TypedQueued || out["state"] != TypedQueued || out["read"] != false {
		t.Fatalf("a forced say into the box did not answer typed-queued: %d %v", code, out)
	}
	if !strings.Contains(f.written(), "FORCEDWORDS") {
		t.Fatalf("force did not type: %q", f.written())
	}

	// Idle, the same force is delivered: its submit is its read.
	d.act.set(target.ID, ActivityIdle, "")
	_, out = forceSayVia(t, d, target.ID, `{"text":"MORE","force":true}`)
	if out["delivered"] != "terminal" || out["read"] != nil {
		t.Fatalf("a forced say into an idle runner is not terminal: %v", out)
	}
}
