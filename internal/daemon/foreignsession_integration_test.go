//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestAForeignSessionsPromptDoesNotMarkTheCardRunning(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()
	parent := nestedParent(t, d)

	if got := d.onActivity(ActivityEvent{Agent: "parent", TaskID: parent.ID, PID: 200, Event: "prompt"}); got != "" {
		t.Fatalf("a foreign session's prompt landed on %s", got)
	}
	if got := d.onActivity(ActivityEvent{Agent: "parent", TaskID: parent.ID, PID: 100, Event: "prompt"}); got != parent.ID {
		t.Fatalf("the card's own prompt landed on %q", got)
	}
	if got := d.onActivity(ActivityEvent{Agent: "parent", TaskID: parent.ID, Event: "tool-start", Tool: "Bash"}); got != parent.ID {
		t.Fatalf("a hook with no pid is believed, landed on %q", got)
	}
}

func TestAClosedSendersHeldMessageIsReleased(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a command"))
	sender, _, err := d.st.Register(store.Observed{WireName: "sender", Worktree: "D:/git/atrium", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	m, _ := d.st.QueueFromPeer(target.ID, "one", sender.WireName)
	d.deferPeerInjection(target.ID, m.ID, sender.WireName, "one", "", false)
	if got := heldCount(d.pending, target.ID); got != 1 {
		t.Fatalf("the message should be held, got %d", got)
	}

	d.releaseSentBy(sender)
	if msgs, _ := d.st.PendingMessages(target.ID); len(msgs) != 0 {
		t.Fatalf("a closed sender's message is still queued: %d", len(msgs))
	}
	if got := heldCount(d.pending, target.ID); got != 0 {
		t.Fatalf("the injector still holds %d entries for a closed sender", got)
	}
}
