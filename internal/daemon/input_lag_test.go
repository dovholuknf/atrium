package daemon

import (
	"strings"
	"testing"
	"time"
)

// The operator's keystroke path must stay fast while a peer message is being
// injected into the same terminal.
//
// A peer injection on the peerFree path holds injectMu across a sayThenEnter
// pause. This drives one and, while it is mid-send, times the two calls the
// attach reader makes on every keystroke: noteOperatorTyped (the typing-state
// bookkeeping) and echoToPeers (the multi-pane mirror, off here). Neither may
// wait behind the injection. Before the lock split both took r.mu, the same
// lock injectPeer held across the pause, so each keystroke stalled ~130ms.
func TestOperatorKeystrokeStaysFastDuringPeerInjection(t *testing.T) {
	d := testDaemon(t)
	_, r, f := peerPair(t, d)

	// Operator has been quiet past peerQuiet, so the injection takes the peerFree
	// path (banner+body, sleep sayThenEnter, Enter) and holds injectMu throughout.
	r.typeMu.Lock()
	r.midLine = false
	r.lastTyped = time.Time{}
	r.typeMu.Unlock()

	roomCh := make(chan peerRoom, 1)
	go func() {
		room, _, _ := r.injectPeer("[peer sg4/doer] ", "the migration is ready")
		roomCh <- room
	}()
	// Let the injection acquire injectMu, write the banner+body, and enter its
	// held pause. Well inside sayThenEnter, so it is still sending when we measure.
	time.Sleep(15 * time.Millisecond)

	start := time.Now()
	r.noteOperatorTyped([]byte("x"))
	note := time.Since(start)

	start = time.Now()
	r.echoToPeers([]byte("x"), nil)
	echo := time.Since(start)

	// Generous ceiling. Uncontended these are microseconds; the bug made them
	// the remainder of the sayThenEnter pause. Anything near the pause is the bug.
	ceiling := sayThenEnter / 4
	if note > ceiling {
		t.Fatalf("noteOperatorTyped blocked %v behind the injection (ceiling %v)", note, ceiling)
	}
	if echo > ceiling {
		t.Fatalf("echoToPeers blocked %v behind the injection (ceiling %v)", echo, ceiling)
	}

	// The operator won: they started a line during the pause, so the peer text
	// was left in the prompt unsent rather than submitted tangled with it.
	room := <-roomCh
	if room != peerWatching {
		t.Fatalf("expected the injection to yield to the operator (peerWatching), got %v", room)
	}
	if got := f.written(); strings.Contains(got, "\r") {
		t.Fatalf("the peer pressed Enter over the operator's line: %q", got)
	}
	t.Logf("during-injection: note=%v echo=%v (sayThenEnter=%v)", note, echo, sayThenEnter)
}
