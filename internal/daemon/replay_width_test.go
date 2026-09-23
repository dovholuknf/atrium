package daemon

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// conptyLine is one full-width coloured diff line the way ConPTY hands it
// over for a terminal `cols` wide: the coloured run erased to its end with
// ECH, the cursor stepped over it, then plain spaces to the last column.
func conptyLine(n, cols int) string {
	text := fmt.Sprintf(" %d -    line %d of the diff", n, n)
	gap := cols - 7 - len(text)
	return fmt.Sprintf("\x1b[48;2;61;1;0m%s\x1b[%dX\x1b[m\x1b[%dC%s\r\n",
		text, gap, gap, strings.Repeat(" ", 7))
}

// A SESSION REOPENED WIDER THAN THE PANE REPLAYS WITHOUT DOUBLED LINES.
//
// The shape a room restart leaves in the ring. The card was saved at 60
// columns, the resumed session reprinted its transcript at 60, and the pane
// that attached is 57, so the ring holds a 60 run, a mark, and a 57 repaint.
// Replaying all of it at 57 wrapped the seven spaces of padding on every line
// onto a row of their own: a blank line under every coloured diff line, which
// is what the operator saw after the 14:39 deploy.
func TestAReplayAfterARestartKeepsWiderHistoryOnItsOwnWidth(t *testing.T) {
	d := testDaemon(t)
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{
		taskID:   "reopened-wide",
		pty:      f,
		started:  time.Now(),
		buf:      newRingSized(1<<16, 60, 10),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	var transcript strings.Builder
	for i := 1; i <= 30; i++ {
		transcript.WriteString(conptyLine(i, 60))
	}
	r.buf.Write([]byte(transcript.String()))
	// The pane attached narrower, and the session repainted its screen there.
	r.buf.SetSize(57, 10)
	r.buf.Write([]byte("\x1b[2J\x1b[H> prompt\x1b[K\r\n"))
	d.sup.add(r)

	got := attachAs(t, d, "reopened-wide", 57, 10)

	lines := strings.Split(got, "\r\n")
	for i := 0; i+1 < len(lines); i++ {
		if strings.Contains(lines[i], " of the diff") && strings.TrimSpace(plain(lines[i+1])) == "" {
			t.Fatalf("a blank line follows full-width line %q, the history was replayed at the pane's "+
				"width instead of its own:\n%s", plain(lines[i]), plain(got))
		}
	}
	for i := 1; i <= 30; i++ {
		if !strings.Contains(got, fmt.Sprintf("line %d of the diff", i)) {
			t.Fatalf("line %d of the history is missing:\n%s", i, plain(got))
		}
	}
}

// A WIND-DOWN DOES NOT MOVE THE PTY TO WHICHEVER VIEWER IS LEFT.
//
// The viewers of an exiting runner detach one at a time. Each detach used to
// resize the pty to the viewers left, so the size recorded for the next room
// was the last window left rather than the one the session was drawn for.
func TestAWindDownDetachLeavesTheSizeTheSessionHad(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{
		taskID:   "wind-down",
		pty:      f,
		buf:      newRingSized(1<<16, 120, 30),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	_ = r.setViewport("pane", 110, 40)
	_ = r.setViewport("wide window", 214, 50)
	before := len(f.resized())

	close(r.done)
	r.dropViewport("wide window")

	if n := len(f.resized()); n != before {
		t.Fatalf("an exited runner's pty was resized on detach: %+v", f.resized())
	}
	if cols := r.buf.CurrentWidth(); cols != 214 {
		t.Fatalf("the width saved for the next room moved to %d, want the 214 the session was drawn at", cols)
	}
}
