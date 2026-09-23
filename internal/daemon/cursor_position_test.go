package daemon

import (
	"strings"
	"testing"
)

// THE CURSOR HAS TO SURVIVE THE REPLAY, or the first keystroke after an attach
// lands in the wrong column.
//
// A terminal user interface parks its cursor where it wants input: inside its
// prompt box, some way up from the last row it drew and at a column it worked
// out itself. The screen model reconstructs every cell of that output, then the
// attaching xterm sits its cursor at the end of the last line it was handed,
// which is neither the row nor the column the session left it at. The next
// character the operator types echoes there, and the pane reads as corrupt:
// `Dhange.gnext` for what was typed cleanly.
//
// This was masked until the resize-sanity change (8400fa8). Every attach used
// to resize the pty, which raised SIGWINCH and made the runner repaint its
// whole frame, and that repaint carried an absolute cursor move that put xterm
// right. Once an attach at a size the pty was already at stopped resizing, no
// SIGWINCH fired, no repaint came, and the lost cursor was on screen.
//
// So the replay itself has to restore the cursor. It is the only thing that
// knows where the session left it and the only thing that runs on every attach.

// The screen model composes output, parks the cursor mid-screen the way a TUI
// leaves it in its input box, and the replay must end by putting the terminal
// cursor back there.
func TestReplayRestoresTheCursor(t *testing.T) {
	// Three rows drawn, then the cursor parked at row 2, column 6 (1-based),
	// which the app addresses absolutely. It is not the end of the output.
	in := "line one\r\nline two\r\nline three\r\n\x1b[2;6H"
	got := string(Replay([]byte(in), "screen", 40, 24))

	// The screen is replayed at its own rows, so the move is to the session's
	// own row and column. See `TestReplayKeepsTheSessionsRows`.
	want := "\x1b[2;6H"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("replay did not restore the cursor (want suffix %q):\n%q", want, got)
	}
}

// END TO END OVER THE SOCKET the board uses, because the unit test above proves
// the renderer and this proves nothing between it and the browser drops the
// move. The cursor is parked in the prompt row, and the attach must hand the
// browser the sequence that puts it there.
func TestAttachRestoresTheCursorOverTheSocket(t *testing.T) {
	d := testDaemon(t)
	// A prompt and a line under it, cursor left at row 1, column 3, which is
	// where a TUI sits it in the input box after drawing the screen.
	narrowSession(t, d, "cursor", "> hello\r\nsome more\r\n\x1b[1;3H")

	got := attachAs(t, d, "cursor", 80, 24)

	if !strings.Contains(got, "some more") {
		t.Fatalf("the replay itself did not arrive: %q", got)
	}
	// Row 1, column 3, where the session parked it.
	want := "\x1b[1;3H"
	if !strings.Contains(got, want) {
		t.Fatalf("the attach did not restore the cursor (want %q):\n%q", want, got)
	}
}
