package daemon

import (
	"strings"
	"testing"
	"time"
)

// The operator's keystroke BOOKKEEPING must stay fast while a peer message is
// being injected into the same terminal.
//
// injectPeer holds injectMu and the input lock pasteMu across a sayThenEnter
// pause. This drives one and, while it is mid-send, times the two calls the
// attach reader makes on every keystroke: noteOperatorTyped (the typing-state
// bookkeeping) and echoToPeers (the multi-pane mirror, off here). Neither may
// wait behind the injection. Before the lock split both took r.mu, the same
// lock injectPeer held across the pause, so each keystroke stalled ~130ms. The
// bytes an operator types DO wait on pasteMu, on purpose, so a paste and typing
// never interleave, but that is a separate path (writeOperatorInput) and is not
// on the recording that the input-lag fix protects.
func TestOperatorKeystrokeStaysFastDuringPeerInjection(t *testing.T) {
	d := testDaemon(t)
	_, r, f := peerPair(t, d)

	// A terminal nobody has typed into, so the gate is open and the injection
	// runs the full banner+body, sleep sayThenEnter, Enter, holding pasteMu the
	// whole time.
	r.typeMu.Lock()
	r.midLine, r.unsent = false, 0
	r.lastTyped = time.Time{}
	r.typeMu.Unlock()

	wroteCh := make(chan bool, 1)
	go func() {
		wrote, _ := r.injectPeer("[peer sg4/doer] ", "the migration is ready")
		wroteCh <- wrote
	}()
	// Let the injection acquire its locks, write the banner+body, and enter its
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

	// The injection ran to completion and submitted on its own line. The
	// operator's keystroke was recorded but its byte was never written here, so
	// nothing is tangled into the peer message.
	if wrote := <-wroteCh; !wrote {
		t.Fatal("the injection did not complete into an open gate")
	}
	if got := f.written(); !strings.Contains(got, "\r") {
		t.Fatalf("the peer message never submitted: %q", got)
	}
	if got := f.written(); strings.Contains(got, "x") {
		t.Fatalf("the operator's keystroke byte tangled into the paste: %q", got)
	}
	t.Logf("during-injection: note=%v echo=%v (sayThenEnter=%v)", note, echo, sayThenEnter)
}
