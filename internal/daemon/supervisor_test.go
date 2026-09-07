package daemon

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// The ring buffer is what an attaching browser sees first. Getting it wrong
// means landing in a terminal showing the wrong thing, which is worse than
// landing in an empty one.
//
// Every test here names the way the thing gets broken.

// The width every test opens a terminal at, when the test is not about width.
const testCols = 80

func TestRingBufferKeepsTheTail(t *testing.T) {
	r := newRing(16, testCols)

	r.Write([]byte("hello"))
	if got := string(r.Snapshot()); got != "hello" {
		t.Fatalf("short write: %q", got)
	}

	// Wrapping keeps the most recent bytes, in order, from the first line
	// boundary it can safely start at.
	r.Write([]byte("0123\n456789abcdef"))
	if got := string(r.Snapshot()); got != "456789abcdef" {
		t.Fatalf("after wrap: %q", got)
	}

	r2 := newRing(8, testCols)
	r2.Write([]byte("aaaa"))
	r2.Write([]byte("bb\nbb"))
	r2.Write([]byte("cc"))
	// Eight bytes are held, `abb\nbbcc`, and the safe place to start reading
	// them is after the line ending.
	if got := string(r2.Snapshot()); got != "bbcc" {
		t.Fatalf("wrapped tail wrong: %q", got)
	}
}

// A single write larger than the whole buffer keeps its tail, not its head.
// The end of a huge burst is the part that still matters.
func TestRingBufferHandlesOversizedWrite(t *testing.T) {
	r := newRing(8, testCols)
	r.Write(bytes.Repeat([]byte("x"), 20))
	r.Write([]byte("\nEND"))
	got := string(r.Snapshot())
	if !strings.HasSuffix(got, "END") {
		t.Fatalf("lost the end of the stream: %q", got)
	}
	if len(got) > 8 {
		t.Fatalf("buffer grew past its size: %d bytes", len(got))
	}
}

func TestRingBufferIsEmptyBeforeAnyWrite(t *testing.T) {
	if got := newRing(32, testCols).Snapshot(); len(got) != 0 {
		t.Fatalf("fresh buffer is not empty: %q", got)
	}
}

// THE WRITE CURSOR IS A BYTE OFFSET AND KNOWS NOTHING ABOUT WHAT IS AT IT.
//
// Once the buffer has wrapped, the oldest retained byte is wherever the cursor
// happens to have landed, which can be the middle of an escape sequence. The
// introducer is gone, so the terminal prints the tail of it: `2;34Hdone` typed
// across the screen instead of a cursor move.
func TestASnapshotNeverStartsInsideAnEscapeSequence(t *testing.T) {
	r := newRing(16, testCols)
	r.Write([]byte("first line\n"))
	// Lands so that the retained bytes begin part way through the escape.
	r.Write([]byte("\x1b[2;34Hdone\n"))
	got := r.Snapshot()
	if bytes.ContainsAny(got, "\x1b") {
		// Only reachable if a whole sequence survived, which is fine.
		return
	}
	if len(got) > 0 && got[0] != 'd' {
		t.Fatalf("snapshot began inside an escape sequence: %q", got)
	}
}

// A rune is several bytes and the cursor can land between two of them. A
// severed rune renders as a replacement character, and a terminal reading a
// continuation byte on its own can swallow what follows it.
func TestASnapshotNeverStartsInsideARune(t *testing.T) {
	r := newRing(24, testCols)
	r.Write([]byte("one\n"))
	r.Write([]byte("héllo wörld ünicode\n"))
	got := r.Snapshot()
	if !utf8.Valid(got) {
		t.Fatalf("snapshot is not valid utf-8, so it starts inside a rune: %q", got)
	}
}

// Nothing at all is the right answer for a buffer with no line boundary in it.
// That is megabytes of one line redrawing itself, which has nowhere safe to
// start and is about to be drawn again anyway.
func TestOutputWithNoLineBoundaryIsNotShippedHalfway(t *testing.T) {
	r := newRing(8, testCols)
	r.Write([]byte("\x1b[1;1Hprogress bar with no newline anywhere"))
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("shipped a fragment with no safe start: %q", got)
	}
}

// THE ONE FROM THE SCREENSHOT. An hour of output composed for eighty columns,
// then a two hundred column window attaches. Those bytes carry hard line
// breaks at eighty and absolute cursor moves worked out for eighty, so
// replaying them into the wider grid overwrites itself.
func TestOutputComposedAtAnotherWidthIsNotReplayed(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("an hour of narrow output\n"))

	got, dropped := r.SnapshotAt(200)
	if len(got) != 0 {
		t.Fatalf("replayed eighty column output into a two hundred column window: %q", got)
	}
	if !dropped {
		t.Fatal("dropped the history without saying so, which reads as a second bug")
	}
}

// The width it is at now replays in full, which is the ordinary attach and
// must not be made worse by any of this.
func TestOutputComposedAtThisWidthIsReplayedWhole(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("an hour of narrow output\n"))

	got, dropped := r.SnapshotAt(80)
	if string(got) != "an hour of narrow output\n" {
		t.Fatalf("threw away history that renders correctly: %q", got)
	}
	if dropped {
		t.Fatal("said history was dropped when all of it was sent")
	}
}

// Everything drawn since the resize is still good, and it is the part worth
// having: it is what is on screen.
//
// A resize lands between two arbitrary reads of the terminal, so the first
// line after one is treated as unsafe to start at, the same as the oldest
// retained byte. A runner repainting after being told its new size opens with
// control bytes and a line ending, which is what is lost here.
func TestOutputSinceTheResizeSurvivesIt(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("narrow and unreadable\n"))
	r.SetWidth(200)
	r.Write([]byte("\x1b[H\x1b[2J\nwide and correct\n"))

	got, dropped := r.SnapshotAt(200)
	if string(got) != "wide and correct\n" {
		t.Fatalf("wanted only what was drawn at two hundred columns, got %q", got)
	}
	if !dropped {
		t.Fatal("the narrow half was dropped and nobody was told")
	}
}

// A window dragged narrow and back again leaves two readable stretches with
// something unreadable between them. Joining them splices text across a hole.
func TestAWidthComingBackDoesNotSpliceAcrossTheGap(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("first narrow stretch\n"))
	r.SetWidth(200)
	r.Write([]byte("a wide stretch nobody can read at eighty\n"))
	r.SetWidth(80)
	r.Write([]byte("\nsecond narrow stretch\n"))

	got, _ := r.SnapshotAt(80)
	if strings.Contains(string(got), "first narrow stretch") {
		t.Fatalf("joined two stretches of eighty column output across a hole: %q", got)
	}
	if string(got) != "second narrow stretch\n" {
		t.Fatalf("wanted the trailing run only, got %q", got)
	}
}

// Two viewers agreeing on the size must not split the run of output they can
// both read. `setViewport` recomputes the agreed size on every frame, and a
// browser sends one whenever anything on the page moves.
func TestAgreeingOnTheSizeAgainDoesNotSplitTheRun(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("before\n"))
	r.SetWidth(80)
	r.Write([]byte("after\n"))

	got, dropped := r.SnapshotAt(80)
	if string(got) != "before\nafter\n" {
		t.Fatalf("a redundant resize cut the scrollback in half: %q", got)
	}
	if dropped {
		t.Fatal("claimed to have dropped output over a resize to the size it already was")
	}
}

// A day of dragging a window must not leave a mark per drag describing bytes
// that were overwritten hours ago.
func TestWidthMarksDoNotGrowWithTheSession(t *testing.T) {
	r := newRing(32, 80)
	for i := 0; i < 500; i++ {
		r.SetWidth(80 + i%7)
		r.Write([]byte("some output for this width\n"))
	}
	r.mu.Lock()
	marks := len(r.marks)
	r.mu.Unlock()
	if marks > 16 {
		t.Fatalf("kept %d width marks for a 32 byte buffer", marks)
	}
}

// The oldest retained byte was composed for something, and the mark that says
// what must survive being wrapped past. Losing it would leave the buffer
// claiming its whole contents were written at whatever width came later.
func TestTheWidthOfTheOldestRetainedByteIsKept(t *testing.T) {
	r := newRing(16, 80)
	r.SetWidth(200)
	// Enough to wrap several times over, all of it at two hundred columns.
	for i := 0; i < 10; i++ {
		r.Write([]byte("wide output\n"))
	}
	if got := r.CurrentWidth(); got != 200 {
		t.Fatalf("current width is %d, not the one in force", got)
	}
	got, _ := r.SnapshotAt(200)
	if len(got) == 0 {
		t.Fatal("threw away output written at the width the terminal is still at")
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
	_, _, ch := r.subscribe()

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

// Subscribing after the runner has gone must not hang the caller waiting for
// output that will never come.
func TestSubscribeAfterExitClosesImmediately(t *testing.T) {
	r := &runner{
		taskID:   "t",
		buf:      newRing(64, testCols),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	r.buf.Write([]byte("some earlier output"))
	close(r.done)

	backlog, _, ch := r.subscribe()
	if string(backlog) != "some earlier output" {
		t.Fatalf("backlog lost: %q", backlog)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should already be closed for a runner that has exited")
	}
}
