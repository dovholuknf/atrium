package daemon

import (
	"bytes"
	"runtime"
	"slices"
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

// READING THE END OF A RUNNER'S OUTPUT MUST NOT COPY THE WHOLE RING.
//
// `awaitExit` wants the last twelve lines to say why a runner died. It asked
// for `Snapshot`, which copies everything retained, and `lastOutput` then made
// a string of it, ran a regexp over that, and split the result. At the
// scrollback ceiling that is over a gigabyte of allocation to read about a
// kilobyte, on every single exit.

func TestTailReturnsTheEndOfAWrappedRing(t *testing.T) {
	r := newRing(64, testCols)
	for i := 0; i < 40; i++ {
		r.Write([]byte("line of output\n"))
	}
	got := r.Tail(20)
	if len(got) > 20 {
		t.Fatalf("asked for 20 bytes and got %d", len(got))
	}
	// Whatever came back is the END of the stream, so it is a suffix of what
	// the buffer holds.
	whole := r.Snapshot()
	if !bytes.HasSuffix(whole, got) {
		t.Fatalf("the tail is not the end of the buffer:\nwhole %q\ntail  %q", whole, got)
	}
}

// NEVER OLDER THAN THE OLDEST BYTE HELD. A ring that has wrapped has
// overwritten what came before, and asking for more than it holds must not
// read whatever happens to be in the slice.
func TestTailNeverReachesPastWhatIsRetained(t *testing.T) {
	r := newRing(32, testCols)
	r.Write([]byte("this line is overwritten by what follows it\n"))
	r.Write([]byte("the newest output\n"))

	got := r.Tail(1 << 20)
	if len(got) > 32 {
		t.Fatalf("returned %d bytes from a 32 byte ring", len(got))
	}
	if bytes.Contains(got, []byte("overwritten by")) {
		t.Fatalf("handed back bytes that had been overwritten: %q", got)
	}
	// And it is the same answer Snapshot gives, since everything retained is
	// less than what was asked for.
	if !bytes.Equal(got, r.Snapshot()) {
		t.Fatalf("tail %q disagrees with snapshot %q", got, r.Snapshot())
	}
}

// A ring bigger than what has been written to it returns all of it, rather
// than a short read or a panic reaching back before the start of the stream.
func TestTailOfALightlyUsedRingReturnsEverything(t *testing.T) {
	r := newRing(1<<20, testCols)
	r.Write([]byte("only a little output\n"))
	if got := string(r.Tail(64 << 10)); got != "only a little output\n" {
		t.Fatalf("got %q", got)
	}
	// Nothing at all is a legitimate answer and must not be a panic.
	if got := newRing(1024, testCols).Tail(64 << 10); len(got) != 0 {
		t.Fatalf("invented output from an empty ring: %q", got)
	}
	if got := r.Tail(0); got != nil {
		t.Fatalf("asking for nothing returned %q", got)
	}
}

// THE POINT OF THE WHOLE CHANGE, asserted rather than described: reading the
// tail costs what was asked for, not what the ring is capable of holding.
//
// This is the shape of the bug. Nothing about the call site changed when the
// scrollback setting was raised, and the cost went up by three orders of
// magnitude, because the call asked for "everything" and everything got
// bigger.
func TestReadingTheTailDoesNotCopyTheWholeRing(t *testing.T) {
	const content = "a line of output that is worth reading at the end\n"

	// FILLED, which is the whole condition. A ring only retains what has been
	// written to it, so a big EMPTY ring costs no more to snapshot than a
	// small one and would make this test pass against the bug it exists for.
	// The case in the report is a runner that lived all day.
	big := newRing(8<<20, testCols)
	for big.retained() < len(big.data) {
		big.Write([]byte(content))
	}

	// What the fix removed: the old call, measured, so the number below means
	// something.
	whole := testing.AllocsPerRun(5, func() { _ = big.Snapshot() })
	bounded := testing.AllocsPerRun(5, func() { _ = big.Tail(tailBytes) })
	t.Logf("snapshot of a full 8MB ring: %.0f allocations, bounded tail: %.0f", whole, bounded)

	// Measured in bytes rather than in counts, since one allocation of eight
	// megabytes and one of sixty four kilobytes are both one allocation.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	sink = big.Tail(tailBytes)
	runtime.ReadMemStats(&after)
	grew := after.TotalAlloc - before.TotalAlloc
	if grew > 4*tailBytes {
		t.Fatalf("reading %d bytes of tail allocated %d, which is the ring rather than the request",
			tailBytes, grew)
	}

	// And the same again through the function that actually consumes it, since
	// two of the three copies were its doing.
	if got := lastOutput(big.Tail(tailBytes), 12); !strings.Contains(got, "worth reading") {
		t.Fatalf("the bounded read lost the output: %q", got)
	}
}

// sink keeps a measured allocation alive so the compiler cannot decide the
// call had no effect and remove it.
var sink []byte

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

// AN HOUR OF EIGHTY COLUMN OUTPUT, AND A TWO HUNDRED COLUMN WINDOW ATTACHES.
//
// This used to assert that nothing came back. Those bytes carry line breaks
// and absolute cursor moves worked out for eighty columns, so replaying them
// into a wider grid puts things in the wrong places, and the buffer refused.
//
// The refusal was the wrong trade and the operator found it by dragging a
// window edge: a resize lays a mark before the pty is told, so attaching at a
// new size asked for a run that was zero bytes old, and an hour of scrollback
// went with nothing but a line saying it could not be redrawn. Imperfect
// history beats none, PROVIDED the reader is told which it is.
//
// So the output comes back and `widths` says what it was drawn for. The caller
// decides what to say; `attach.go` says it.
func TestOutputComposedAtAnotherWidthComesBackLabelled(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("an hour of narrow output\n"))

	got, widths, _ := r.Replay()
	if string(got) != "an hour of narrow output\n" {
		t.Fatalf("threw away readable history rather than labelling it: %q", got)
	}
	if !slices.Equal(widths, []int{80}) {
		t.Fatalf("did not say what the output was drawn for: got %v, want [80]", widths)
	}
}

// AND A WIDTH CHANGE WITH NOTHING WRITTEN AT IT STILL REPLAYS WHAT CAME
// BEFORE.
//
// The first shape of the operator's complaint. `setViewport` marks the new
// width before any output is composed at it, so the trailing run is empty and
// a naive read of "the last mark" returns nothing at all.
func TestAResizeWithNoOutputAfterItStillReplaysWhatCameBefore(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("an hour of narrow output\n"))
	r.SetWidth(200)

	got, widths, _ := r.Replay()
	if string(got) != "an hour of narrow output\n" {
		t.Fatalf("a resize emptied the scrollback: %q", got)
	}
	if !slices.Equal(widths, []int{80}) {
		t.Fatalf("mislabelled the output: got %v, want [80]", widths)
	}
}

// The ordinary attach, which none of this is allowed to make worse: one width,
// and it is the one in force.
func TestOutputComposedAtThisWidthIsReplayedWhole(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("an hour of narrow output\n"))

	got, widths, _ := r.Replay()
	if string(got) != "an hour of narrow output\n" {
		t.Fatalf("threw away history that renders correctly: %q", got)
	}
	if !slices.Equal(widths, []int{80}) {
		t.Fatalf("named a width the output was not drawn at: %v", widths)
	}
}

// THE SECOND SHAPE OF THE COMPLAINT, AND THE ONE THAT REOPENED IT.
//
// Returning the trailing run fixed the empty attach and left this: a run ENDS
// AT EVERY MARK, so a session that had been resized handed back only what it
// drew afterwards. For a quiet agent that is a page or two out of sixteen
// megabytes, which on screen is indistinguishable from the original bug.
//
// Both halves come back now, and `widths` names both.
func TestBothSidesOfAResizeComeBack(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("narrow, drawn before the drag\n"))
	r.SetWidth(200)
	r.Write([]byte("wide, drawn after it\n"))

	got, widths, _ := r.Replay()
	if !strings.Contains(string(got), "narrow, drawn before the drag") {
		t.Fatalf("a resize threw away everything before it: %q", got)
	}
	if !strings.Contains(string(got), "wide, drawn after it") {
		t.Fatalf("lost the output drawn since the resize: %q", got)
	}
	if !slices.Equal(widths, []int{80, 200}) {
		t.Fatalf("did not name both widths: got %v, want [80 200]", widths)
	}
}

// A window dragged narrow and back again leaves three stretches, and all three
// are handed over.
//
// This asserted the reverse: two readable stretches with something unreadable
// between them, joined only at the cost of splicing text across a hole. That
// reasoning held right up until the hole turned out to be most of the
// scrollback. The widths are reported in the order they occur so the note can
// say the session was resized rather than name one width for all of it.
func TestAWidthComingBackKeepsEveryStretch(t *testing.T) {
	r := newRing(1024, 80)
	r.Write([]byte("first narrow stretch\n"))
	r.SetWidth(200)
	r.Write([]byte("a wide stretch nobody can read at eighty\n"))
	r.SetWidth(80)
	r.Write([]byte("second narrow stretch\n"))

	got, widths, _ := r.Replay()
	for _, want := range []string{"first narrow stretch", "a wide stretch", "second narrow stretch"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("lost %q from the scrollback: %q", want, got)
		}
	}
	if !slices.Equal(widths, []int{80, 200, 80}) {
		t.Fatalf("did not report the widths in the order they happened: %v", widths)
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

	got, widths, _ := r.Replay()
	if string(got) != "before\nafter\n" {
		t.Fatalf("a redundant resize cut the scrollback in half: %q", got)
	}
	if !slices.Equal(widths, []int{80}) {
		t.Fatalf("a resize to the width already in force was reported: %v", widths)
	}
}

// THE ONE THAT COST AN HOUR OF SCROLLBACK.
//
// Popping a window out and closing it changes the agreed size twice in a
// moment, because the smallest attached viewer decides. No output arrives in
// between. Every byte is still composed for the width that came back, and a
// mark at each step cut the replay to whatever followed the last one: the
// operator reattached to an hour-old session and found one page.
func TestAWidthNothingWasWrittenAtIsNotAMark(t *testing.T) {
	r := newRing(1024, 200)
	r.Write([]byte("an hour of two hundred column output\n"))
	// A narrow window opens and closes without the runner saying anything.
	r.SetWidth(80)
	r.SetWidth(200)

	got, widths, _ := r.Replay()
	if string(got) != "an hour of two hundred column output\n" {
		t.Fatalf("a width nothing was written at threw away the scrollback: %q", got)
	}
	if !slices.Equal(widths, []int{200}) {
		t.Fatalf("reported a width nothing was ever written at: %v", widths)
	}
}

// And the same when it happens repeatedly, which is what a viewer attaching and
// detaching in a loop looks like.
func TestRepeatedEmptyWidthChangesKeepTheRun(t *testing.T) {
	r := newRing(1024, 200)
	r.Write([]byte("kept\n"))
	for i := 0; i < 5; i++ {
		r.SetWidth(80)
		r.SetWidth(120)
		r.SetWidth(200)
	}
	got, widths, _ := r.Replay()
	if string(got) != "kept\n" {
		t.Fatalf("five empty round trips lost the output: %q", got)
	}
	if !slices.Equal(widths, []int{200}) {
		t.Fatalf("five empty round trips left widths behind: %v", widths)
	}
}

// The merge must not eat a width that output WAS written at. Wide, write,
// narrow, write, wide again is a real stretch of eighty column output in the
// middle, and the note has to be able to say so.
func TestAWidthThatWasWrittenAtIsStillAMark(t *testing.T) {
	r := newRing(1024, 200)
	r.Write([]byte("wide\n"))
	r.SetWidth(80)
	r.Write([]byte("narrow\n"))
	r.SetWidth(200)

	got, widths, _ := r.Replay()
	if !strings.Contains(string(got), "wide") || !strings.Contains(string(got), "narrow") {
		t.Fatalf("lost half the scrollback: %q", got)
	}
	if !slices.Equal(widths, []int{200, 80}) {
		t.Fatalf("swallowed a width real output was drawn at: %v", widths)
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
	got, widths, _ := r.Replay()
	if len(got) == 0 {
		t.Fatal("threw away output written at the width the terminal is still at")
	}
	if len(widths) == 0 || widths[0] != 200 {
		t.Fatalf("lost the width the oldest retained byte was drawn at: %v", widths)
	}
}

// RAISING THE LIMIT KEEPS WHAT IS ALREADY HELD.
//
// The size was read once at spawn, so raising it did nothing until every
// runner had been restarted, and a restart is the thing somebody raising it is
// trying to survive.
func TestGrowingTheBufferKeepsEverythingInIt(t *testing.T) {
	r := newRing(64, 80)
	// Past its size, so it has wrapped and the oldest byte is mid-slice.
	for i := 0; i < 20; i++ {
		r.Write([]byte("some output\n"))
	}
	before, _, wrappedBefore := r.Replay()
	if !wrappedBefore {
		t.Fatal("the fixture did not wrap, so this proves nothing")
	}

	r.Grow(4096)

	after, _, _ := r.Replay()
	if !strings.HasSuffix(string(after), string(before)) {
		t.Fatalf("growing lost or reordered what was held:\nbefore %q\nafter  %q", before, after)
	}
	// And it goes on holding more than it used to.
	for i := 0; i < 20; i++ {
		r.Write([]byte("more output\n"))
	}
	grown, _, wrappedAfter := r.Replay()
	if len(grown) <= len(before) {
		t.Fatalf("held %d bytes after growing, %d before", len(grown), len(before))
	}
	// STILL NOT THE START OF THE SESSION, and it has to keep saying so. Those
	// bytes were discarded before the buffer was made bigger and raising a
	// limit does not go back in time. A buffer that reported itself whole here
	// would be telling the reader the top of their scrollback is the beginning
	// of the session when it is not.
	if !wrappedAfter {
		t.Fatal("claimed to hold the whole session after growing, having already dropped output")
	}
}

// Shrinking has to throw bytes away, so it is refused rather than obeyed.
func TestShrinkingTheBufferIsRefused(t *testing.T) {
	r := newRing(4096, 80)
	r.Write([]byte("output that must not be dropped by a settings change\n"))
	r.Grow(16)
	got, _, _ := r.Replay()
	if !strings.Contains(string(got), "must not be dropped") {
		t.Fatalf("a smaller size threw away the scrollback: %q", got)
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

	backlog, _, _, _, ch := r.subscribe()
	if string(backlog) != "some earlier output" {
		t.Fatalf("backlog lost: %q", backlog)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should already be closed for a runner that has exited")
	}
}
