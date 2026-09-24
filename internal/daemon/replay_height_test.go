package daemon

import (
	"fmt"
	"strings"
	"testing"
)

// A SESSION OPENED SHORT AND ATTACHED TALL keeps what it drew while short.
//
// gwt opens a runner in a console about thirty rows tall, and the board
// attaches later at sixty. Replayed into a grid at the later height, the
// output drawn at thirty rows never scrolled into history, so a repaint from
// the top of the old screen erased it. The operator attached to a fresh
// session and saw one screen of it.
func TestReplayKeepsOutputDrawnAtAnEarlierHeight(t *testing.T) {
	r := newRingSized(1<<20, 80, 24)
	var b strings.Builder
	b.WriteString("the banner\r\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "line %d of the prompt\r\n", i)
	}
	// A repaint from the top of the 24 row screen. On a real terminal the
	// banner and the first lines are history by now and this cannot touch them.
	b.WriteString("\x1b[H\x1b[J> the repainted frame\r\n")
	r.Write([]byte(b.String()))

	r.SetSize(80, 62)
	r.Write([]byte("\x1b[55;1H> typed at the new height"))

	out, cuts, rows, _ := r.ReplayCuts()
	got := plain(string(replayCut(out, "screen", cuts, rows)))
	for _, want := range []string{"the banner", "line 1 of the prompt", "line 17 of the prompt",
		"> the repainted frame", "> typed at the new height"} {
		if !strings.Contains(got, want) {
			t.Fatalf("replay lost %q:\n%s", want, got)
		}
	}
}

// A height that changes and changes back with nothing written in between
// leaves no mark, the same as a width does.
func TestAHeightNothingWasWrittenAtIsNotAMark(t *testing.T) {
	r := newRingSized(1024, 80, 30)
	r.Write([]byte("output\r\n"))
	r.SetSize(80, 10)
	r.SetSize(80, 30)
	r.mu.Lock()
	marks := len(r.marks)
	r.mu.Unlock()
	if marks != 1 {
		t.Fatalf("kept %d marks for a size that came back before any output", marks)
	}
}

// Shrinking the grid scrolls rows off the top into history and keeps the
// cursor on the row it was writing.
func TestShrinkingTheGridFilesTheTopAway(t *testing.T) {
	s := newScreenSized(20, 10)
	for i := 0; i < 10; i++ {
		s.apply([]byte(fmt.Sprintf("row %d", i)))
		if i < 9 {
			s.apply([]byte("\r\n"))
		}
	}
	s.resizeRows(4)
	if len(s.history) != 6 || s.row != 3 {
		t.Fatalf("history %d rows, cursor on %d, want 6 and 3", len(s.history), s.row)
	}
	s.apply([]byte("\x1b[1;1Hover"))
	if got := plain(s.text()); !strings.Contains(got, "row 5") || !strings.Contains(got, "over6") {
		t.Fatalf("a move after the shrink landed on history:\n%s", got)
	}
}
