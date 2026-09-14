package daemon

import (
	"strconv"
	"strings"
	"testing"
)

// The screen model, tested against the shapes that broke the flattener.
//
// Every one of these came off an operator's pane, not out of a spec. The
// flattener could not fix any of them because it has no grid, and a grid is the
// only thing that can: a repaint means "put this on top of that", and "that"
// lives on the screen.

// render is the whole pipeline at one width.
func render(in string, cols int) string {
	return string(renderHistory([]byte(in), cols))
}

// lines of output, with the trailing line ending dropped.
func lines(out string) []string {
	out = strings.TrimSuffix(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// plain strips colour, so an assertion about text is not an assertion about
// how it was painted.
func plain(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
					j++
				}
				if j < len(s) {
					j++
				}
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// ── the three failures that caused this ─────────────────

// A PARTIAL REPAINT LANDS ON THE ROW IT WAS AIMED AT.
//
// `ESC[K ESC[42;7H do…)` is columns 7 onward of a row drawn earlier. The
// flattener emitted the fragment alone, because it had no row 42 to put it on.
func TestAPartialRepaintLandsOnItsRow(t *testing.T) {
	// Column 14, which is where `file` starts: `CSI H` counts from one, so the
	// f is column 14 and index 13.
	in := "\x1b[3;1Hlooking at a file\x1b[3;14Hdirectory  "
	got := plain(render(in, 40))

	if strings.Contains(got, "\ndirectory") {
		t.Fatalf("the patch became its own line:\n%q", got)
	}
	if !strings.Contains(got, "looking at a directory") {
		t.Fatalf("the patch did not land on the row it was aimed at:\n%q", got)
	}
}

// A SPINNER REDRAWN IN PLACE IS ONE LINE, not one line per frame.
func TestASpinnerInPlaceIsOneLine(t *testing.T) {
	var b strings.Builder
	b.WriteString("some real work\r\n")
	for i := 1; i <= 200; i++ {
		b.WriteString("\r\x1b[K")
		b.WriteString("Kneading… (" + strconv.Itoa(i) + "s)")
	}
	got := plain(render(b.String(), 60))

	if n := strings.Count(got, "Kneading"); n != 1 {
		t.Fatalf("%d frames survived, wanted the last one only:\n%q", n, got)
	}
	if !strings.Contains(got, "Kneading… (200s)") {
		t.Fatalf("the surviving frame is not the last one drawn:\n%q", got)
	}
	if !strings.Contains(got, "some real work") {
		t.Fatal("the work above the spinner was lost")
	}
}

// THE PROMPT REDRAWN AS SOMEBODY TYPES IS ONE LINE, and it reads as the word
// they typed rather than as its pieces.
//
// This is the `f` `ou` `nd` `it` failure: each repaint drew more of "i found
// it", and without a grid each fragment became its own line between spinner
// frames.
func TestAPromptRedrawnWhileTypingReadsAsTheWord(t *testing.T) {
	var b strings.Builder
	b.WriteString("earlier output\r\n")
	for _, sofar := range []string{"i", "i f", "i fou", "i found", "i found it"} {
		// Each repaint: put the prompt row back, clear it, draw it, then draw
		// the spinner under it. Exactly the shape from the capture.
		b.WriteString("\x1b[5;1H\x1b[K> " + sofar)
		b.WriteString("\x1b[6;1H\x1b[KKneading…")
	}
	got := plain(render(b.String(), 60))

	if !strings.Contains(got, "> i found it") {
		t.Fatalf("the typed line did not assemble:\n%q", got)
	}
	for _, frag := range []string{"\nou", "\nnd", "\nit\n"} {
		if strings.Contains(got, frag) {
			t.Fatalf("a fragment became its own line (%q):\n%q", frag, got)
		}
	}
	if n := strings.Count(got, "Kneading"); n != 1 {
		t.Fatalf("%d spinner frames survived:\n%q", n, got)
	}
	if !strings.Contains(got, "earlier output") {
		t.Fatal("the output above was lost")
	}
}

// ── what a transcript has to keep ───────────────────────

// SCROLLED-OFF LINES ARE THE HISTORY. Without eviction this answers "what was
// on screen at the end", which is one screenful.
func TestLinesThatScrollOffAreKept(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 500; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\r\n")
	}
	got := lines(plain(render(b.String(), 40)))

	if len(got) < 500 {
		t.Fatalf("only %d of 500 lines survived, so the scrollback was dropped", len(got))
	}
	if !strings.Contains(got[0], "line 1") {
		t.Fatalf("the first line is %q", got[0])
	}
	if !strings.Contains(got[499], "line 500") {
		t.Fatalf("line 500 came out as %q", got[499])
	}
}

// Ordinary output is untouched: this must not become a reason to distrust the
// pane for the 99% of sessions that never repaint anything.
func TestPlainOutputIsUnchanged(t *testing.T) {
	in := "first\r\nsecond\r\nthird\r\n"
	if got := plain(render(in, 40)); got != "first\r\nsecond\r\nthird\r\n" {
		t.Fatalf("plain output came out as %q", got)
	}
}

// A full-width line does not gain a blank line after it. The deferred wrap is
// the detail every naive implementation gets wrong.
func TestAFullWidthLineDoesNotWrapEarly(t *testing.T) {
	got := lines(plain(render(strings.Repeat("x", 10)+"\r\nnext\r\n", 10)))
	if len(got) != 2 {
		t.Fatalf("a full width line produced %d lines: %q", len(got), got)
	}
	if got[0] != strings.Repeat("x", 10) || got[1] != "next" {
		t.Fatalf("came out as %q", got)
	}
}

// And a line PAST the width does wrap, rather than losing the tail.
func TestTextPastTheWidthWraps(t *testing.T) {
	got := lines(plain(render(strings.Repeat("x", 15), 10)))
	if len(got) != 2 {
		t.Fatalf("expected two lines, got %q", got)
	}
	if got[0] != strings.Repeat("x", 10) || got[1] != strings.Repeat("x", 5) {
		t.Fatalf("came out as %q", got)
	}
}

// CLEARING THE SCREEN IS NOT ERASING HISTORY. A program that clears to redraw
// does not mean the operator never saw what was there.
func TestClearingTheScreenKeepsWhatWasOnIt(t *testing.T) {
	in := "something worth reading\r\n\x1b[2J\x1b[Hafter the clear\r\n"
	got := plain(render(in, 40))
	if !strings.Contains(got, "something worth reading") {
		t.Fatalf("the cleared screen was lost:\n%q", got)
	}
	if !strings.Contains(got, "after the clear") {
		t.Fatalf("what followed the clear was lost:\n%q", got)
	}
}

// The alternate screen is NOT history. A full-screen program draws there and
// switches back, and what it drew was never part of the session's output.
func TestTheAlternateScreenIsNotHistory(t *testing.T) {
	in := "before\r\n" +
		"\x1b[?1049h" + "\x1b[2J\x1b[HI AM A PAGER\r\n" + "\x1b[?1049l" +
		"after\r\n"
	got := plain(render(in, 40))

	if strings.Contains(got, "I AM A PAGER") {
		t.Fatalf("the alternate screen leaked into the transcript:\n%q", got)
	}
	for _, want := range []string{"before", "after"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q was lost around the alternate screen:\n%q", want, got)
		}
	}
}

// ── colour ──────────────────────────────────────────────

// Colour survives, and is emitted once per run rather than once per character.
func TestColourSurvivesAndIsNotRepeatedPerCharacter(t *testing.T) {
	in := "\x1b[31mred text\x1b[m plain\r\n"
	got := render(in, 40)

	if !strings.Contains(got, "\x1b[31m") {
		t.Fatalf("the colour was lost: %q", got)
	}
	if n := strings.Count(got, "\x1b[31m"); n != 1 {
		t.Fatalf("the colour was emitted %d times for one run: %q", n, got)
	}
	if plain(got) != "red text plain\r\n" {
		t.Fatalf("the text came out as %q", plain(got))
	}
}

// A line that leaves colour on closes it, or every line after it is painted by
// a sequence that belonged to one.
func TestALineClosesItsColour(t *testing.T) {
	got := render("\x1b[31mred\r\nplain\r\n", 40)
	first := strings.SplitN(got, "\r\n", 2)[0]
	if !strings.HasSuffix(first, "\x1b[m") {
		t.Fatalf("the coloured line did not reset: %q", first)
	}
}

// ── the bits that are easy to get wrong ─────────────────

// Erase to end of line removes the tail and nothing else.
func TestEraseToEndOfLine(t *testing.T) {
	got := plain(render("abcdefgh\r\x1b[3C\x1b[K", 20))
	if strings.TrimRight(got, "\r\n") != "abc" {
		t.Fatalf("came out as %q", got)
	}
}

// Backspace moves without erasing, which is how a shell redraws a line.
func TestBackspaceMovesWithoutErasing(t *testing.T) {
	got := plain(render("abc\b\bX", 20))
	if strings.TrimRight(got, "\r\n") != "aXc" {
		t.Fatalf("came out as %q", got)
	}
}

// Insert and delete line, which is how a list grows and shrinks in place.
func TestInsertAndDeleteLine(t *testing.T) {
	got := lines(plain(render("one\r\ntwo\r\nthree\r\n\x1b[2;1H\x1b[L", 20)))
	if len(got) < 4 || got[0] != "one" || got[1] != "" || got[2] != "two" {
		t.Fatalf("insert line came out as %q", got)
	}

	got = lines(plain(render("one\r\ntwo\r\nthree\r\n\x1b[2;1H\x1b[M", 20)))
	if len(got) < 2 || got[0] != "one" || got[1] != "three" {
		t.Fatalf("delete line came out as %q", got)
	}
}

// A sequence cut in half by the ring's own boundary must not panic or eat the
// rest of the buffer.
func TestATruncatedSequenceIsSurvivable(t *testing.T) {
	for _, in := range []string{"text\x1b", "text\x1b[", "text\x1b[3", "text\x1b[38;2;1"} {
		got := plain(render(in, 20))
		if !strings.HasPrefix(got, "text") {
			t.Fatalf("%q lost its text: %q", in, got)
		}
	}
}

// A parameter nobody would type is bounded rather than believed.
func TestAnAbsurdParameterIsBounded(t *testing.T) {
	got := plain(render("top\x1b[999999;1Hbottom", 20))
	if !strings.Contains(got, "top") || !strings.Contains(got, "bottom") {
		t.Fatalf("text was lost: %q", got)
	}
	if n := len(lines(got)); n > screenMaxRows+2 {
		t.Fatalf("a corrupt row parameter produced %d lines", n)
	}
}

func TestEmptyInput(t *testing.T) {
	if got := renderHistory(nil, 80); len(got) != 0 {
		t.Fatalf("empty input produced %q", got)
	}
}
