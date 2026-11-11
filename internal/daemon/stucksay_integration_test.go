//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"
)

func stuckRunner(t *testing.T, frame string) (*runner, *fakePTY) {
	t.Helper()
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{stuckAfter: 20 * time.Millisecond, pty: f, buf: newRingSized(1<<16, 80, 24), watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	_, _ = r.buf.Write([]byte(frame))
	return r, f
}

// The first Enter of a long say is eaten and the text stays on the prompt: atrium presses Enter again.
func TestASayWhoseEnterWasEatenIsSubmittedAgain(t *testing.T) {
	r, f := stuckRunner(t, string(idleFrameWithText("[atrium] @x says: a long report")))
	if ok, err := r.injectPeer("[atrium] @x says: ", "a long report"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	until(t, "a second Enter", func() bool { return strings.Count(f.written(), "\r") >= 2 })
}

// A say that took leaves an empty prompt, and nothing presses Enter on it again.
func TestASayThatSubmittedIsNotSubmittedAgain(t *testing.T) {
	r, f := stuckRunner(t, string(idleFrameWithText("")))
	if ok, err := r.injectPeer("[atrium] @x says: ", "hi"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	time.Sleep(150 * time.Millisecond) // wait: longer than two stuckAfter, so a second Enter would have landed
	if n := strings.Count(f.written(), "\r"); n != 1 {
		t.Fatalf("%d Enters, want 1: %q", n, f.written())
	}
}

// A key the operator pressed after the say is theirs: no Enter is pressed under their hands.
func TestAKeyAfterASayStopsTheSecondEnter(t *testing.T) {
	r, f := stuckRunner(t, string(idleFrameWithText("[atrium] @x says: hi and more")))
	if ok, err := r.injectPeer("[atrium] @x says: ", "hi"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	r.noteOperatorTyped([]byte("a"))
	time.Sleep(150 * time.Millisecond) // wait: longer than two stuckAfter
	if n := strings.Count(f.written(), "\r"); n != 1 {
		t.Fatalf("%d Enters, want 1: %q", n, f.written())
	}
}
