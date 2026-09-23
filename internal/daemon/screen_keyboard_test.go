package daemon

import (
	"strings"
	"testing"
)

// The shape claude draws on Windows, through ConPTY: a banner, a rule, the
// input row addressed absolutely, the keyboard-protocol push and pop it emits
// after a key like ctrl-delete, and then the typed text redrawn in place.
//
// The screen model used to read `CSI < u` and `CSI > 5 u` as a cursor restore
// and `CSI > 4 ; 2 m` as dim underline. Nothing had saved a cursor, so the
// restore went to the top-left and the typed text landed on the banner. Every
// attach replayed it there: `load this prlandeinspect2it.280` over
// `Claude Code v2.1.280`.
func TestKeyboardProtocolSequencesDoNotMoveTheCursor(t *testing.T) {
	in := "\x1b[HClaude Code v2.1.280\r\n" +
		"----------\r\n" +
		"> \r\n" +
		"----------\r\n" +
		"\x1b[3;3H\x1b[?25h" +
		// The push, pop and modifyOtherKeys claude sends, then typing.
		"\x1b[<u\x1b[>5u\x1b[>4;2m" + "can we\x1b[3;9H" +
		// ctrl-delete: the line redrawn from the input's start, absolutely.
		"\x1b[3;3H\x1b[<u\x1b[>5u\x1b[>4;2m" + "an we\x1b[K\x1b[3;8H"
	got := string(Replay([]byte(in), "screen", 40, 10))

	lines := strings.Split(got, "\r\n")
	if len(lines) < 4 {
		t.Fatalf("replay lost rows:\n%q", got)
	}
	if lines[0] != "Claude Code v2.1.280" {
		t.Errorf("the banner was drawn over: %q", lines[0])
	}
	if lines[2] != "> an we" {
		t.Errorf("the input row is %q, want %q", lines[2], "> an we")
	}
	if strings.Contains(got, "2m") || strings.Contains(got, "4m") {
		t.Errorf("modifyOtherKeys was read as colour:\n%q", got)
	}
	// And the cursor is restored into the input row, row 3 at column 8.
	if !strings.HasSuffix(got, "\x1b[3;8H") {
		t.Errorf("the cursor was not put back in the input row:\n%q", got)
	}
}

// The standard sequences sharing those finals still work: `CSI s` and `CSI u`
// save and restore, and a plain SGR still colours.
func TestSaveAndRestoreStillWork(t *testing.T) {
	in := "top\r\nmid\x1b[s\r\nbottom\x1b[u!"
	got := string(Replay([]byte(in), "screen", 40, 10))
	if !strings.Contains(got, "mid!") {
		t.Fatalf("CSI s / CSI u no longer save and restore:\n%q", got)
	}
}
