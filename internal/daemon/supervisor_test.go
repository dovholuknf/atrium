package daemon

import (
	"strings"
	"testing"

	"github.com/aymanbagabas/go-pty"
)

// The ring buffer is what an attaching browser sees first. Getting it wrong
// means landing in a terminal showing the wrong thing, which is worse than
// landing in an empty one.
//
// Every test here names the way the thing gets broken.

// The width every test opens a terminal at, when the test is not about width.
const testCols = 80

func TestRingBufferIsEmptyBeforeAnyWrite(t *testing.T) {
	if got := newRing(32, testCols).Snapshot(); len(got) != 0 {
		t.Fatalf("fresh buffer is not empty: %q", got)
	}
}

// READING THE END OF A RUNNER'S OUTPUT MUST NOT COPY THE WHOLE RING.
//
// `awaitExit` wants the last twelve lines to say why a runner died. It asked
// for `Snapshot`, which copies everything retained, and `lastOutput` then made
// a string of it, ran a regexp over that, and split the result. At the
// scrollback ceiling that is over a gigabyte of allocation to read about a
// kilobyte, on every single exit.

// lastOutput trims what it is given, so widening a caller cannot quietly bring
// back the cost this was fixed to remove.
func TestLastOutputRefusesToProcessMoreThanTheBound(t *testing.T) {
	huge := []byte(strings.Repeat("padding that should never be scanned\n", 40000))
	huge = append(huge, "the line that matters\n"...)
	if len(huge) <= tailBytes {
		t.Fatal("the fixture is not bigger than the bound, so this proves nothing")
	}

	got := lastOutput(huge, 12)
	if !strings.Contains(got, "the line that matters") {
		t.Fatalf("trimmed away the end instead of the beginning: %q", got)
	}
	if len(got) > tailBytes {
		t.Fatalf("returned %d bytes for a bound of %d", len(got), tailBytes)
	}
}

// A failure message reads as text whatever the terminal was told around it. pwsh on Linux opens with ESC [?1h ESC =,
// and the bare ESC = once landed in front of a card's "failed to start" line.
func TestLastOutputStripsEveryEscapeATerminalConsumes(t *testing.T) {
	for _, in := range []string{
		"\x1b[?1h\x1b=boom-message\r\n",
		"\x1b=boom-message\x1b>\r\n",
		"\x1b(B\x1b[0mboom-message\x1b[0 q\r\n",
		"\x1b]0;title\x1b\\boom-message\x1b7\x1b8\r\n",
		"\x1b]0;title\x07boom-message\r\n",
	} {
		if got := lastOutput([]byte(in), 12); got != "boom-message" {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

// A slow attacher must be dropped rather than allowed to block the reader,
// because a blocked reader eventually stalls the runner itself.
func TestFanoutDoesNotBlockOnASlowWatcher(t *testing.T) {
	r := &runner{
		taskID:   "t",
		buf:      newRing(64, testCols),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	_, _, _, _, ch := r.subscribe()

	// Far more than the channel buffer, with nobody reading.
	for i := 0; i < 500; i++ {
		r.fanout([]byte("chunk"))
	}
	// Reaching here at all is the assertion: a blocking fanout would deadlock
	// the test rather than fail it.
	if len(ch) == 0 {
		t.Fatal("the watcher received nothing")
	}
	r.unsubscribe(ch)
	// A closed channel still yields its buffered chunks before reporting
	// closed, so drain before asking.
	for range ch {
	}
	if _, ok := <-ch; ok {
		t.Fatal("unsubscribe left the channel open")
	}
}

// stubbornPty is a pty that deliberately writes short.
//
// It embeds the interface so the methods no test here calls do not have to be
// written out. Calling one of those panics, which is the right outcome for a
// test that has wandered off its subject.
type stubbornPty struct {
	pty.Pty
	// most is the largest number of bytes one write will take.
	most int
	// got is everything it has accepted, in order.
	got []byte
	// calls counts how many writes it took to get there.
	calls int
}

func (s *stubbornPty) Write(p []byte) (int, error) {
	s.calls++
	n := len(p)
	if n > s.most {
		n = s.most
	}
	s.got = append(s.got, p[:n]...)
	return n, nil
}

// stuckPty reports no progress and no error, which is the one way a retry loop
// turns into a hang.
type stuckPty struct {
	pty.Pty
}

func (stuckPty) Write(p []byte) (int, error) { return 0, nil }
