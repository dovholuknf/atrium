package daemon

import (
	"fmt"
	"strings"
	"testing"
)

// Regression cases from captured Claude Code output. Synchronized-output
// markers delimit frames even without carriage returns. Cursor-forward
// sequences must preserve spacing between fields when flattened.

// frame is one spinner frame the way claude-code draws it: hold the screen,
// position absolutely, write, release.
func frame(text string) string {
	return "\x1b[?2026h\x1b[46;3H\x1b[38;2;215;119;87m" + text + "\x1b[m\x1b[?2026l"
}

// Captured frame boundaries use synchronized output with no carriage returns.
func TestASynchronizedSpinnerCollapses(t *testing.T) {
	var b strings.Builder
	b.WriteString("real work above\r\n")
	for i := 0; i < 200; i++ {
		b.WriteString(frame(fmt.Sprintf("Lollygagging… %d tokens", i)))
	}
	b.WriteString("\r\nreal work below\r\n")

	got := string(collapseRedraws([]byte(b.String())))

	if n := strings.Count(got, "Lollygagging"); n != 1 {
		t.Fatalf("%d spinner frames survived, wanted 1", n)
	}
	// The LAST frame, because that is the one that was on screen.
	if !strings.Contains(got, "Lollygagging… 199 tokens") {
		t.Fatal("the surviving frame is not the last one drawn")
	}
	for _, keep := range []string{"real work above", "real work below"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("collapsing ate %q, which is not part of the animation", keep)
		}
	}
}

// Several absolute cursor positions can occur within one frame. They must
// not count as redraw boundaries or the earlier parts of a frame will be lost.
func TestAbsolutePositioningIsNotAFrameBoundary(t *testing.T) {
	one := "\x1b[?2026h" +
		"\x1b[43;1H first line of the block" +
		"\x1b[43;3H second line of the block" +
		"\x1b[46;3H third line of the block" +
		"\x1b[?2026l"

	got := string(collapseRedraws([]byte(one)))

	for _, keep := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("%q was dropped, so a single frame was cut at a cursor move", keep)
		}
	}
}

// The end marker must not count. Counting both halves of every pair would
// double the boundary count and move where the two-boundary threshold falls,
// so a single frame would start collapsing.
func TestOneFrameIsLeftAlone(t *testing.T) {
	one := frame("Lollygagging… 1 token")
	if got := string(collapseRedraws([]byte(one))); got != one {
		t.Fatalf("a single frame was collapsed:\n got %q\nwant %q", got, one)
	}
}

// What was already true stays true: a shell-script progress bar uses carriage
// returns and still collapses.
func TestACarriageReturnSpinnerStillCollapses(t *testing.T) {
	in := "10%\r50%\r99%\r100%\r\ndone\r\n"
	got := string(collapseRedraws([]byte(in)))
	if strings.Contains(got, "10%") || strings.Contains(got, "50%") {
		t.Fatalf("earlier frames survived: %q", got)
	}
	if !strings.Contains(got, "100%") || !strings.Contains(got, "done") {
		t.Fatalf("the last frame or the line after it was eaten: %q", got)
	}
}

// Preserve spacing in this captured table header, which uses cursor-forward
// instead of literal spaces.
func TestCursorForwardBecomesSpacing(t *testing.T) {
	row := "id" + "\x1b[4C" + "wire_name" + "\x1b[2C" + "status"

	got := string(flatten([]byte(row)))

	if strings.Contains(got, "idwire_name") {
		t.Fatalf("the fields ran together: %q", got)
	}
	if got != "id    wire_name  status" {
		t.Fatalf("spacing came out as %q", got)
	}
}

// No parameter means one, the same as every other movement sequence.
func TestCursorForwardDefaultsToOneColumn(t *testing.T) {
	if got := string(flatten([]byte("a\x1b[Cb"))); got != "a b" {
		t.Fatalf("got %q, wanted one space", got)
	}
}

// The parameter is somebody else's number, so it is bounded. A corrupt one
// must not become a megabyte of spaces in the middle of history.
func TestCursorForwardIsBounded(t *testing.T) {
	got := string(flatten([]byte("a\x1b[99999999Cb")))
	if n := strings.Count(got, " "); n > maxSkipColumns {
		t.Fatalf("%d spaces, which is past the bound", n)
	}
	if !strings.HasPrefix(got, "a") || !strings.HasSuffix(got, "b") {
		t.Fatalf("the text either side was lost: %q", got)
	}
}

// Drop movements that could overwrite emitted history. Only forward moves
// are translated: horizontal moves become spaces and downward row jumps
// become line breaks. See flatten_rows_test.go.
func TestMovementsThatCouldReachHistoryAreDropped(t *testing.T) {
	for _, seq := range []string{
		"\x1b[3A",     // up
		"\x1b[3D",     // back
		"\x1b[H",      // home, which is row 1 and therefore never forward
		"\x1b[2J",     // erase display
		"\x1b[K",      // erase line
		"\x1b[?1049h", // alternate screen
		"\x1b[3M",     // delete lines
		"\x1b[S",      // scroll up
	} {
		got := string(flatten([]byte("kept" + seq + "also kept")))
		if strings.Contains(got, "\x1b") {
			t.Fatalf("%q survived flattening: %q", seq, got)
		}
		if got != "keptalso kept" {
			t.Fatalf("%q changed the output, and it may not: %q", seq, got)
		}
	}
}
