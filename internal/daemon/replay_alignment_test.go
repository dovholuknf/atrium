package daemon

import (
	"strings"
	"testing"
)

// A REPLAY HAS TO PUT THE SCREEN WHERE THE SESSION THINKS IT IS, row for row.
//
// Claude draws its input box with absolute moves (`CSI row;col H`) once the box
// grows past one line. Those rows are the session's own screen rows, so the
// attaching terminal only draws them in the right place when its viewport holds
// the session's screen at the same rows. The replay used to collapse a run of
// blank rows inside the live screen to one, and claude leaves two blank rows
// under its banner, so everything below them was replayed one row high. Typing
// a line long enough to wrap then drew the second line of input over the rule
// under the prompt, and the status lines twice. A fresh attach hid this, because
// claude repaints absolutely right after it starts. A late attach gets no
// repaint.
//
// The check is the property itself: replaying a session and then applying the
// session's next draw must leave the attaching terminal holding what the
// session's own terminal holds. The attaching terminal is modelled with the same
// screen grid, at the same size, since xterm scrolls and clamps the same way.
func TestReplayKeepsTheSessionsRows(t *testing.T) {
	const cols, rows = 60, 12
	// Claude's frame, shortened: banner, two blank rows, rule, prompt, rule,
	// status, and the cursor parked in the prompt.
	frame := "banner one\r\nbanner two\r\n\x1b[K\r\n\x1b[K\x1b[38;5;8m\r\n" +
		"------------\x1b[m\r\n> \r\n------------\r\nstatus line\r\n\x1b[6;3H"
	// The next draw after the input wraps: the second input line, the rule moved
	// down one, and the status addressed absolutely, the way claude does it.
	next := "long input\r\n  wrapped\x1b[K\r\n------------\x1b[9;1Hstatus line\x1b[K\x1b[7;10H"

	cases := map[string]string{
		"a short session":         frame,
		"a session with history":  strings.Repeat("older output\r\n", 40) + "\x1b[H\x1b[2J" + frame,
		"a session scrolled once": strings.Repeat("x\r\n", rows) + frame,
	}
	for name, session := range cases {
		t.Run(name, func(t *testing.T) {
			want := newScreenSized(cols, rows)
			want.apply([]byte(session + next))

			got := newScreenSized(cols, rows)
			got.apply(Replay([]byte(session), "screen", cols, rows))
			got.apply([]byte(next))

			w, g := gridText(want), gridText(got)
			if w != g {
				t.Fatalf("the attaching terminal does not hold the session's screen after its next draw.\n"+
					"session:\n%s\nattached:\n%s", w, g)
			}
			if got.row != want.row || got.col != want.col {
				t.Fatalf("cursor at %d,%d, the session's is at %d,%d", got.row, got.col, want.row, want.col)
			}
		})
	}
}

func gridText(s *screen) string {
	var b strings.Builder
	for i, r := range s.cells {
		line := make([]rune, 0, len(r))
		for _, c := range r {
			line = append(line, c.ch)
		}
		b.WriteString(strings.TrimRight(string(line), " \x00"))
		if i < len(s.cells)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
