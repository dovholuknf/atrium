package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
)

func newEscRunner() *runner {
	return &runner{
		taskID:   "esc",
		started:  time.Now(),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
}

func replayOf(r *runner) string {
	b, _, _ := r.buf.Replay()
	return string(b)
}

// The mark is requested after the first k bytes of a stream, wherever k falls. It must come out whole, at the
// nearest point between sequences at or after k, and every other byte must be unchanged.
func TestMarkWaitsForTheEndOfASequenceSplitAtEveryOffset(t *testing.T) {
	t.Parallel()
	// each case is "pre", one sequence, then "tail": the parser is only in ground state in "pre" and after the sequence
	for _, tc := range []struct {
		name   string
		stream string
	}{
		{"csi", "pre\x1b[38;2;1;2;3mtail"},
		{"csi-private", "pre\x1b[?2004htail"},
		{"esc-two-byte", "pre\x1b7tail"},
		{"esc-intermediate", "pre\x1b(Btail"},
		{"osc-bel", "pre\x1b]0;title\x07tail"},
		{"osc-st", "pre\x1b]8;;http://x\x1b\\tail"},
		{"dcs-st", "pre\x1bPq#0;2;0\x1b\\tail"},
		{"utf8-2", "preétail"},
		{"utf8-3", "pre€tail"},
		{"utf8-4", "pre\U0001F600tail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k := 0; k <= len(tc.stream); k++ {
				r := newEscRunner()
				r.deliverOutput([]byte(tc.stream[:k]))
				r.markClear()
				r.deliverOutput([]byte(tc.stream[k:]))
				at := k
				end := len(tc.stream) - len("tail")
				for at > 3 && at < end {
					at++
				}
				want := tc.stream[:at] + screen.ClearMarkOSC + tc.stream[at:]
				if got := replayOf(r); got != want {
					t.Fatalf("split at %d: got %q want %q", k, got, want)
				}
				if r.markPending {
					t.Fatalf("split at %d: mark still pending", k)
				}
			}
		})
	}
}

// The same, with the stream split in a chunk of its own on every byte, so the held mark crosses many chunks.
func TestMarkHeldAcrossManyChunks(t *testing.T) {
	t.Parallel()
	stream := "a\x1b]0;a long title\x07b"
	r := newEscRunner()
	r.deliverOutput([]byte(stream[:5]))
	r.markClear()
	for i := 5; i < len(stream); i++ {
		r.deliverOutput([]byte{stream[i]})
	}
	want := "a\x1b]0;a long title\x07" + screen.ClearMarkOSC + "b"
	if got := replayOf(r); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// A watcher sees the same bytes as the ring, the mark whole and after the sequence.
func TestHeldMarkReachesWatchersAfterTheSequence(t *testing.T) {
	t.Parallel()
	r := newEscRunner()
	_, _, _, _, _, updates := r.subscribeSized()
	r.deliverOutput([]byte("x\x1b[3"))
	r.markClear()
	r.deliverOutput([]byte("1mmore"))
	var got strings.Builder
	for len(updates) > 0 {
		got.Write(<-updates)
	}
	if want := "x\x1b[31m" + screen.ClearMarkOSC + "more"; got.String() != want {
		t.Fatalf("got %q want %q", got.String(), want)
	}
}

// A sequence that never ends does not hold the mark for good: the byte bound releases it.
func TestHeldMarkIsForcedOutByBytes(t *testing.T) {
	t.Parallel()
	r := newEscRunner()
	r.deliverOutput([]byte("\x1b]0;"))
	r.markClear()
	if r.markPending == false {
		t.Fatal("the mark was not held")
	}
	r.deliverOutput([]byte(strings.Repeat("x", screen.MarkHoldBytes)))
	if r.markPending || !strings.Contains(replayOf(r), screen.ClearMarkOSC) {
		t.Fatal("the mark was not forced out by the byte bound")
	}
}

// A stream that stops mid-sequence does not hold it either: the timer releases it.
func TestHeldMarkIsForcedOutByTime(t *testing.T) {
	t.Parallel()
	r := newEscRunner()
	var fire func()
	var held time.Duration
	r.afterFunc = func(d time.Duration, f func()) *time.Timer {
		held, fire = d, f
		return nil
	}
	r.deliverOutput([]byte("\x1b[3"))
	r.markClear()
	if fire == nil || held != screen.MarkHoldTime {
		t.Fatalf("the hold timer was not armed for markHoldTime: %v", held)
	}
	if !r.markPending {
		t.Fatal("the mark was not held")
	}
	fire()
	if r.markPending {
		t.Fatal("the mark was never released")
	}
	if !strings.Contains(replayOf(r), screen.ClearMarkOSC) {
		t.Fatal("released without the mark")
	}
}
