//go:build integration

package daemon

import (
	"testing"
	"time"
)

// A focus report cut after its Esc, with the tail arriving later than the rejoin
// window (a slow link), used to leave "[I" on the line as two typed characters.
// The line looked empty and every say sat held until two backspaces.
func TestFocusReportSplitAfterItsEscLeavesTheLineEmpty(t *testing.T) {
	t.Parallel()
	for _, tail := range []string{"[I", "[O"} {
		now := time.Unix(1000, 0)
		l := &typedLine{clock: func() time.Time { return now }, escClears: true}
		l.feed([]byte("\x1b"))
		now = now.Add(300 * time.Millisecond)
		l.feed([]byte(tail))
		l.settle()
		if !l.empty() {
			t.Errorf("Esc then %q 300ms later left %d characters (%q)", tail, l.count(), string(l.text))
		}
	}
}
