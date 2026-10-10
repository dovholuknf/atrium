//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"
)

func newMarkRunner(f *fakePTY) *runner {
	return &runner{
		taskID:   "mark",
		pty:      f,
		started:  time.Now(),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
}

// The mark is in the ring, so a replay has it, and on every watcher, so a live viewer gets it at the same byte. It is
// not the runner's output, so it does not move the clock the idle checks read.
func TestMarkClearReachesTheReplayAndTheWatchersAndNotTheOutputClock(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := newMarkRunner(f)
	r.deliverOutput([]byte("old line 1\r\n"))
	_, _, _, _, _, updates := r.subscribeSized()
	before := r.lastOut.Load()
	r.markClear()
	if r.lastOut.Load() != before {
		t.Fatal("the mark moved the output clock")
	}
	select {
	case chunk := <-updates:
		if string(chunk) != clearMarkOSC {
			t.Fatalf("the watcher got %q", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("the watcher never got the mark")
	}
	replay, _, _ := r.buf.Replay()
	if !strings.HasSuffix(string(replay), "old line 1\r\n"+clearMarkOSC) {
		t.Fatalf("the replay is %q", replay)
	}
	// The room's screen model reads the same bytes back and agrees.
	if got := plain(render(string(replay)+conptyClear, 40)); !strings.Contains(got, "old line 1") {
		t.Fatalf("the replayed stream lost the page:\n%q", got)
	}
}

// A `/clear` the room types itself marks first. Anything else does not.
func TestTypingSlashClearMarksTheStreamBeforeTheBytesAndOtherTextDoesNot(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := newMarkRunner(f)
	if ok, err := r.injectPeer("", "/clear"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	replay, _, _ := r.buf.Replay()
	if string(replay) != clearMarkOSC {
		t.Fatalf("typing /clear left %q in the stream", replay)
	}
	if !strings.Contains(f.written(), "/clear") {
		t.Fatalf("the pty got %q", f.written())
	}

	r2 := newMarkRunner(newFakePTY())
	r2.pty = f
	if ok, err := r2.injectPeer("", "/clearing house"); err != nil || !ok {
		t.Fatalf("injectPeer: %v %v", ok, err)
	}
	if replay, _, _ := r2.buf.Replay(); len(replay) != 0 {
		t.Fatalf("other text left %q in the stream", replay)
	}
}

func TestIsClearCommand(t *testing.T) {
	for in, want := range map[string]bool{
		"/clear": true, " /clear ": true, "\x1b[200~/clear\x1b[201~": true,
		"/clear now": false, "/clearing": false, "clear": false, "": false,
	} {
		if got := isClearCommand(in); got != want {
			t.Errorf("isClearCommand(%q) = %v, want %v", in, got, want)
		}
	}
}
