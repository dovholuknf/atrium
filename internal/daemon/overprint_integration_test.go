//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"
)

const (
	sayLine  = `[atrium] @atrium says: "room deploy called off by deploy-batch. messages are flowing again, nothing to do here"`
	hookLine = `[hook] set-session-state.ps1 -State thinking timed out after 15s, output discarded. Raise the hook's "timeout"`
)

// inkFrame is a Claude Code style repaint: erase the `up` rows above, then draw the lines.
func inkFrame(up int, lines ...string) string {
	var b strings.Builder
	for i := 0; i < up; i++ {
		b.WriteString("\x1b[2K\x1b[1A")
	}
	b.WriteString("\x1b[2K\r")
	b.WriteString(strings.Join(lines, "\r\n"))
	b.WriteString("\r\n")
	return b.String()
}

// A STREAM DRAWN AT ONE SIZE AND REATTACHED AT ANOTHER REPLAYS WITH NO ROW OVERPRINTED.
func TestAReplayDrawnAtOneSizeAndAttachedAtAnotherHasNoOverprintedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	for _, c := range []struct {
		name               string
		drawCols, drawRows int
		attCols, attRows   int
	}{
		{"narrower", 120, 30, 60, 20},
		{"wider", 60, 20, 140, 40},
		{"shorter", 120, 30, 120, 8},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := testDaemon(t)
			f := newFakePTY()
			t.Cleanup(func() { f.Close() })
			r := &runner{
				taskID:   "overprint-" + c.name,
				pty:      f,
				started:  time.Now(),
				buf:      newRingSized(1<<16, c.drawCols, c.drawRows),
				watchers: map[chan []byte]struct{}{},
				done:     make(chan struct{}),
			}
			wrap := func(s string) int { return (len(s) + c.drawCols - 1) / c.drawCols }
			r.buf.Write([]byte("\x1b[2J\x1b[H> hello\r\n"))
			r.buf.Write([]byte(inkFrame(0, sayLine, "  working...")))
			r.buf.Write([]byte(inkFrame(wrap(sayLine)+1, sayLine, hookLine, "  working...")))
			for i := 0; i < 40; i++ {
				r.buf.Write([]byte("filler row\r\n"))
			}
			d.sup.add(r)

			got := plain(attachAs(t, d, r.taskID, c.attCols, c.attRows))
			flat := strings.Join(strings.Fields(got), "")
			for _, want := range []string{sayLine, hookLine} {
				if !strings.Contains(flat, strings.Join(strings.Fields(want), "")) {
					t.Fatalf("a line is missing or has another line's characters in it: %q\n%s", want, got)
				}
			}
			if n := strings.Count(got, "@atrium says"); n != 1 {
				t.Fatalf("the say line is on screen %d times:\n%s", n, got)
			}
		})
	}
}

// A SAVED FILE DRAWN AT TWO WIDTHS REPLAYS WITH NO ROW OVERPRINTED.
//
// The file records one width, so its first run was replayed at the last run's width and the cursor
// moves of a repaint drawn for 120 columns landed on the wrong rows at 60.
func TestACarriedFileDrawnAtTwoWidthsReplaysWithoutOverprintedRows(t *testing.T) {
	t.Parallel()
	before := carryRunner("card-two-widths", 1<<16, 120)
	before.buf.Write([]byte("\x1b[2J\x1b[H> hello\r\n"))
	before.buf.Write([]byte(inkFrame(0, sayLine, "  working...")))
	before.buf.Write([]byte(inkFrame(2, sayLine, hookLine, "  working...")))
	before.buf.SetSize(60, 20)
	before.buf.Write([]byte(inkFrame(0, "after the resize")))

	dir := t.TempDir()
	if err := writeCarry(dir, "card-two-widths", before.carryFrom(1<<16)); err != nil {
		t.Fatal(err)
	}

	after := carryRunner("card-two-widths", 1<<16, 60)
	after.carried = readCarry(dir, "card-two-widths", 1<<16)
	live := []byte("live after the restart\r\n")
	out, cuts, _, _ := after.withCarried(live, nil, 60, 1<<16, true)
	got := plain(string(replayCut(out, "screen", cuts, 20)))

	flat := strings.ReplaceAll(strings.Join(strings.Fields(got), ""), " ", "")
	for _, want := range []string{sayLine, hookLine} {
		w := strings.Join(strings.Fields(want), "")
		if n := strings.Count(flat, w); n < 1 {
			t.Fatalf("%q is missing or has other characters in it:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "@atrium says"); n != 1 {
		t.Fatalf("the say line was left behind by a repaint, it is on screen %d times:\n%s", n, got)
	}
}
