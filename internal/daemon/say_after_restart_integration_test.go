//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"
)

// A SAY TO A CARD REACHES THE TERMINAL THE BOARD ATTACHES TO, AFTER A RESTART.
//
// A restart ends the card's runner and a reopen puts a new one under the same card. The say is typed into whatever
// `sup.get` holds, and the board attaches to the same lookup, so they must be one terminal: the new one. The old one
// must hear nothing, or a say would vanish into a terminal nobody is looking at.
func TestASayAfterARestartIsTypedIntoTheTerminalTheBoardAttachesTo(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	peerCard(t, d, "minion")
	card := peerCard(t, d, "orchestrator")

	// Before the restart: a runner with an hour of history.
	old := narrowSession(t, d, card.ID, "before the restart\n")
	first := d.sup.get(card.ID)

	// The restart: the old runner is gone and the reopen starts a new one on the same card.
	d.sup.removeRunner(first)
	fresh := newFakePTY()
	t.Cleanup(func() { fresh.Close() })
	r := &runner{
		taskID: card.ID, pty: fresh, started: time.Now(),
		buf: newRing(1<<16, 120), watchers: map[chan []byte]struct{}{}, done: make(chan struct{}),
	}
	d.sup.add(r)

	if code := message(t, d, card.ID, map[string]string{"text": "done: P4 card closed", "from": "minion"}); code != 200 {
		t.Fatalf("the say answered %d", code)
	}

	got := fresh.written()
	if !strings.Contains(got, "@minion says:") || !strings.Contains(got, "done: P4 card closed") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("the say was not typed and submitted into the reopened terminal: %q", got)
	}
	if w := old.written(); w != "" {
		t.Fatalf("the say went to the terminal that ended in the restart: %q", w)
	}

	// The runner echoes what it was given, which is what puts it on the board's screen.
	r.deliverOutput([]byte("> [atrium] @minion says: done: P4 card closed\r\n"))
	seen := attachAs(t, d, card.ID, 120, 30)
	if !strings.Contains(seen, "P4 card closed") {
		t.Fatalf("the terminal the board attaches to does not show the say: %q", seen)
	}
}
