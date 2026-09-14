package daemon

import (
	"os"
	"strconv"
	"testing"
)

// DUMP WHAT EACH REPLAY PATH ACTUALLY PRODUCES, so a human can read it.
//
// Not an assertion. The screen model was wired once, every test passed, the
// numbers were excellent, and the pane was junk. That is what a metric does
// when it measures the wrong thing, and the way out is to put both outputs on
// disk beside a capture of the same session taken from a real terminal.
//
// ATRIUM_DUMP_IN names a `.scrollback`, ATRIUM_DUMP_OUT a directory. Skipped
// without both, so it costs nothing in CI.
func TestDumpBothReplayPaths(t *testing.T) {
	in := os.Getenv("ATRIUM_DUMP_IN")
	out := os.Getenv("ATRIUM_DUMP_OUT")
	if in == "" || out == "" {
		t.Skip("set ATRIUM_DUMP_IN and ATRIUM_DUMP_OUT")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("input %d bytes", len(raw))

	flat := flatten(raw)
	if err := os.WriteFile(out+"/flatten.txt", flat, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("flatten  %d bytes", len(flat))

	// THE WIDTH THE SESSION WAS DRAWN AT, which is the card's `last_cols` and
	// what the ring's width marks carry. Not a guess: a grid one column wider
	// than the stream wraps every full line one character late, and the row
	// that was pushed down lands spliced into the one under it. ATRIUM_DUMP_COLS
	// overrides it so the same capture can be replayed at the wrong width on
	// purpose, which is how that failure was recognised.
	cols := 163
	if v := os.Getenv("ATRIUM_DUMP_COLS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cols = n
		}
	}
	rows := 0
	if v := os.Getenv("ATRIUM_DUMP_ROWS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rows = n
		}
	}
	t.Logf("replaying at %d columns, %d rows", cols, rows)
	sc := newScreenSized(cols, rows)
	sc.apply(raw)
	buf := []byte(sc.text())
	if err := os.WriteFile(out+"/screen.txt", buf, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("screen   %d bytes, %d history rows", len(buf), len(sc.history))
}
