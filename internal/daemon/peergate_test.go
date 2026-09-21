package daemon

import (
	"testing"
	"time"
)

// The unsent-buffer count is what makes the gate stricter than the old midLine
// bool: a line typed and then backspaced to nothing has to read as EMPTY.
func TestUnsentCountTracksTheOperatorsLine(t *testing.T) {
	d := testDaemon(t)
	_, r, _ := peerPair(t, d)

	check := func(want int) {
		t.Helper()
		r.typeMu.Lock()
		got := r.unsent
		r.typeMu.Unlock()
		if got != want {
			t.Fatalf("unsent = %d, want %d", got, want)
		}
	}

	r.noteOperatorTyped([]byte("git commit"))
	check(10)
	// Two backspaces take two off.
	r.noteOperatorTyped([]byte{0x7f, 0x08})
	check(8)
	// Backspacing past empty floors at zero rather than going negative.
	r.noteOperatorTyped([]byte("ab"))
	check(10)
	r.noteOperatorTyped([]byte{0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f})
	check(0)

	// Each line-ender resets a dirty line to empty.
	for _, end := range [][]byte{{'\r'}, {'\n'}, {0x03}, {0x15}} {
		r.noteOperatorTyped([]byte("dirty"))
		check(5)
		r.noteOperatorTyped(end)
		check(0)
	}
}

// The gate is the two facts together: empty line AND peerGateIdle of quiet.
func TestPeerGateNeedsEmptyLineAndIdle(t *testing.T) {
	d := testDaemon(t)
	_, r, _ := peerPair(t, d)

	// A terminal nobody has typed into is open at once.
	if !r.peerGateOpen() {
		t.Fatal("a fresh terminal should be open to a peer message")
	}

	// A part written line is closed however long ago it was touched.
	r.noteOperatorTyped([]byte("half a command"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-time.Hour)
	r.typeMu.Unlock()
	if r.peerGateOpen() {
		t.Fatal("a part written line must keep the gate shut")
	}

	// Emptied but touched a moment ago is still closed on the idle rule.
	r.noteOperatorTyped([]byte{0x15}) // control-u clears the line
	if r.peerGateOpen() {
		t.Fatal("an empty line touched inside peerGateIdle must stay shut")
	}

	// Emptied and quiet past the idle window is open.
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()
	if !r.peerGateOpen() {
		t.Fatal("an empty line past peerGateIdle should open the gate")
	}
}

// THE INPUT-LOCK ABORT. A keystroke that lands the instant before injectPeer
// takes the lock closes the gate, and the paste writes nothing rather than
// tangling into what the operator just started.
func TestInjectPeerAbortsWhenAKeystrokeRacesTheLock(t *testing.T) {
	d := testDaemon(t)
	_, r, f := peerPair(t, d)

	// Hold the input lock so the injection blocks on it, standing in for a paste
	// that has not reached the gate re-check yet.
	r.pasteMu.Lock()

	done := make(chan bool, 1)
	go func() {
		wrote, _ := r.injectPeer("[peer sg4/doer] ", "the migration is ready")
		done <- wrote
	}()

	// The operator types while the injection is blocked on the lock. When the
	// lock releases the injection re-checks the gate and finds a dirty line.
	r.noteOperatorTyped([]byte("x"))
	r.pasteMu.Unlock()

	if wrote := <-done; wrote {
		t.Fatalf("the injection wrote into a line the operator had just started: %q", f.written())
	}
	if f.written() != "" {
		t.Fatalf("the aborted paste still put bytes on the pty: %q", f.written())
	}
}

// And the operator's own bytes WAIT behind a paste in flight, which is how the
// two never interleave. The bookkeeping does not wait, only the write.
func TestOperatorBytesWaitBehindAPasteButBookkeepingDoesNot(t *testing.T) {
	d := testDaemon(t)
	_, r, _ := peerPair(t, d)

	r.pasteMu.Lock() // stand in for a paste holding the lock

	// Bookkeeping is not on the lock, so this returns at once.
	start := time.Now()
	r.noteOperatorTyped([]byte("x"))
	if took := time.Since(start); took > sayThenEnter/4 {
		t.Fatalf("noteOperatorTyped waited on the input lock (%v)", took)
	}

	// The write is on the lock, so it does not complete until the paste releases.
	wrote := make(chan struct{})
	go func() {
		_ = r.writeOperatorInput([]byte("x"))
		close(wrote)
	}()
	select {
	case <-wrote:
		t.Fatal("operator bytes reached the pty while a paste held the lock")
	case <-time.After(20 * time.Millisecond):
	}
	r.pasteMu.Unlock()
	select {
	case <-wrote:
	case <-time.After(time.Second):
		t.Fatal("operator bytes never landed after the paste released the lock")
	}
}
