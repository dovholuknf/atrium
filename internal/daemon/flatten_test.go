package daemon

import (
	"strings"
	"testing"
)

// Every test here names a way the replayed history destroyed itself.
//
// The measurements come from one real card: 1.6MB of carried scrollback
// holding 375 absolute cursor moves and 1,651 erase-in-line sequences. The
// operator's report was that two megabytes came down the socket and the
// browser showed a couple of screens.

// THE ONE THAT COST THE SCROLLBACK THREE TIMES.
//
// Move to the top of the screen and draw, which is what a redraw does, and
// everything above goes.
func TestAnAbsoluteCursorMoveCannotOverwriteHistory(t *testing.T) {
	got := string(flatten([]byte("an hour of history\r\n\x1b[Hthe latest redraw\r\n")))
	if !strings.Contains(got, "an hour of history") {
		t.Fatalf("the history was left able to be overwritten: %q", got)
	}
	if strings.Contains(got, "\x1b[H") {
		t.Fatalf("kept the move that lands on the history: %q", got)
	}
	if !strings.Contains(got, "the latest redraw") {
		t.Fatalf("dropped the text as well as the move: %q", got)
	}
}

// Erase in line and erase in display are the other half of a redraw: land on a
// row, wipe it, write the new version.
func TestErasesAreDropped(t *testing.T) {
	for _, seq := range []string{"\x1b[K", "\x1b[2K", "\x1b[J", "\x1b[2J", "\x1b[3J"} {
		got := string(flatten([]byte("kept" + seq + "also kept")))
		if strings.Contains(got, "\x1b") {
			t.Fatalf("%q survived: %q", seq, got)
		}
		if got != "keptalso kept" {
			t.Fatalf("%q took text with it: %q", seq, got)
		}
	}
}

// BRACKETED PASTE SURVIVES, and it is the one sequence kept for a reason that
// has nothing to do with drawing.
//
// `ESC [ ? 2004 h` is how a session says it understands a paste as one thing.
// The board reads it off this stream and only wraps a paste in the markers
// when it has seen it, so stripping it here turned every pane opened after a
// replay into one that pastes raw. A raw paste larger than the pty's input
// pipe, about four kilobytes, is delivered in installments, and a session that
// tells typing from pasting by timing sees one paste as five.
func TestBracketedPasteModeSurvivesFlattening(t *testing.T) {
	got := string(flatten([]byte("history\r\n\x1b[?2004hlive\r\n")))
	if !strings.Contains(got, "\x1b[?2004h") {
		t.Fatalf("stripped the enable, so a pane after this replay pastes raw: %q", got)
	}
	if got := string(flatten([]byte("\x1b[?2004l"))); !strings.Contains(got, "\x1b[?2004l") {
		t.Fatalf("kept the enable but not the disable: %q", got)
	}
}

// AND NOTHING ELSE THAT LOOKS LIKE IT. Keeping every private mode that does
// not draw is the shorter rule and it lets the alternate screen buffer back
// in, which takes the whole scrollback off the display at once.
func TestOnlyBracketedPasteIsKeptAmongThePrivateModes(t *testing.T) {
	for _, seq := range []string{
		"\x1b[?1049h", "\x1b[?1049l", // the alternate screen
		"\x1b[?25l", "\x1b[?25h", // the cursor
		"\x1b[?1h", "\x1b[?7l", // application keys, autowrap
		"\x1b[?2004", // truncated, not a whole sequence
	} {
		if got := string(flatten([]byte("kept" + seq + "also kept"))); strings.Contains(got, "\x1b") {
			t.Fatalf("%q survived: %q", seq, got)
		}
	}
}

// The alternate screen buffer is the worst of them. Switching to it replaces
// the whole display, so one of these in a replay takes every byte of history
// off the screen at once.
func TestTheAlternateScreenIsNeverEntered(t *testing.T) {
	got := string(flatten([]byte("history\r\n\x1b[?1049hfullscreen thing\x1b[?1049l")))
	if strings.Contains(got, "1049") {
		t.Fatalf("left a screen buffer switch in the replay: %q", got)
	}
	if !strings.Contains(got, "history") {
		t.Fatalf("lost the history: %q", got)
	}
}

// A FULL RESET WOULD CLEAR EVERYTHING, and it is two bytes with no final byte
// to key on, so it is easy to miss when only CSI is being filtered.
func TestTwoByteEscapesAreDropped(t *testing.T) {
	for _, seq := range []string{"\x1bc", "\x1b7", "\x1b8", "\x1bM"} {
		got := string(flatten([]byte("kept" + seq + "still here")))
		if strings.Contains(got, "\x1b") {
			t.Fatalf("%q survived: %q", seq, got)
		}
		if got != "keptstill here" {
			t.Fatalf("%q took text with it: %q", seq, got)
		}
	}
}

// COLOUR IS KEPT. A transcript in one colour is much harder to read, and SGR
// cannot move or erase anything, so there is nothing to trade away.
func TestColourSurvives(t *testing.T) {
	in := "\x1b[38;5;244mgrey\x1b[0m and \x1b[1mbold\x1b[22m\r\n"
	got := string(flatten([]byte(in)))
	if got != in {
		t.Fatalf("colour was altered: %q", got)
	}
}

// A SPINNER IS ONE LINE REDRAWN A HUNDRED TIMES, by returning to column zero
// and writing over itself. Every version is kept, which is more lines than
// were ever on screen and all of them true.
func TestABareCarriageReturnBecomesALineEnding(t *testing.T) {
	got := string(flatten([]byte("10%\r50%\r100%\r\ndone\r\n")))
	want := "10%\r\n50%\r\n100%\r\ndone\r\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// And a run of them is one break rather than one each, or the transcript fills
// with blank lines.
func TestARunOfCarriageReturnsIsOneBreak(t *testing.T) {
	got := string(flatten([]byte("a\r\r\nb\r\n\r\nc")))
	want := "a\r\nb\r\n\r\nc"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// THE RING CUTS WHEREVER THE WRITE CURSOR LANDED, so a snapshot can begin or
// end halfway through a sequence. Neither may run off the end of the slice or
// swallow the buffer.
func TestASequenceCutInHalfDoesNotPanicOrEatTheBuffer(t *testing.T) {
	cases := []string{
		"text then a cut escape\x1b",
		"text then a cut csi\x1b[",
		"text then half a csi\x1b[38;5",
		"text then an unterminated osc\x1b]0;a title with no terminator",
		"text then an unterminated dcs\x1bPsomething",
	}
	for _, in := range cases {
		got := string(flatten([]byte(in)))
		if !strings.HasPrefix(got, "text then") {
			t.Fatalf("a cut sequence ate the text before it: %q -> %q", in, got)
		}
	}
}

// A window title is not history and an unterminated one would swallow
// everything after it, so the whole string is dropped.
func TestOperatingSystemCommandsAreDropped(t *testing.T) {
	got := string(flatten([]byte("before\x1b]0;a window title\x07after")))
	if got != "beforeafter" {
		t.Fatalf("got %q, want %q", got, "beforeafter")
	}
	got = string(flatten([]byte("before\x1b]8;;http://example.com\x1b\\after")))
	if got != "beforeafter" {
		t.Fatalf("the ESC-backslash terminator was not honoured: %q", got)
	}
}

// Text and runes pass through byte for byte. A replacement character in the
// scrollback would be this function's fault rather than the ring's.
func TestOrdinaryTextAndRunesAreUntouched(t *testing.T) {
	in := "héllo wörld ünicode ✓ 日本語\r\n"
	if got := string(flatten([]byte(in))); got != in {
		t.Fatalf("altered ordinary text: %q", got)
	}
}

func TestFlattenOfNothingIsNothing(t *testing.T) {
	if got := flatten(nil); len(got) != 0 {
		t.Fatalf("invented bytes: %q", got)
	}
}

// THE SHAPE OF THE REAL BUG, end to end on the buffer rather than a sequence
// at a time: an hour of output, then the redraw that used to wipe it.
func TestAnHourOfHistoryFollowedByARedrawKeepsTheHour(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("line of real work that must survive\r\n")
	}
	// What claude-code emits on every turn: home, erase down, redraw.
	b.WriteString("\x1b[H\x1b[2J\x1b[38;5;244mthe current screen\x1b[0m\r\n")

	got := string(flatten([]byte(b.String())))
	if n := strings.Count(got, "line of real work that must survive"); n != 500 {
		t.Fatalf("kept %d of 500 lines of history", n)
	}
	if !strings.Contains(got, "the current screen") {
		t.Fatalf("lost the newest output: %q", got[len(got)-200:])
	}
}
