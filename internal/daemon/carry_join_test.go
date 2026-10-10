package daemon

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// A resumed claude reprints only its recent conversation. The saved bytes are
// cut where that reprint begins, so the joined replay holds the older history
// once and the recent part once.
func TestTheSavedHistoryIsCutWhereTheReprintBegins(t *testing.T) {
	old := []byte("banner\r\n● first answer, long enough to be an anchor line\r\n" +
		"● second answer, which the reprint repeats from here on\r\nmore of it\r\n")
	live := []byte("\x1b[2J\x1b[H Claude Code v2\r\n● second\x1b[1Canswer, which the reprint repeats\r\n" +
		" from here on\r\nmore of it\r\nnew work after the restart\r\n")
	cut := reprintCut(old, live)
	kept := string(old[:cut])
	if !strings.Contains(kept, "first answer") {
		t.Fatalf("the older history was cut away: %q", kept)
	}
	if strings.Contains(kept, "second answer") {
		t.Fatalf("the part the reprint repeats was kept, so it shows twice: %q", kept)
	}
}

// No line in common keeps the saved bytes whole. A duplicate can be read past,
// a gap cannot be read at all.
func TestNoMatchKeepsTheSavedHistoryWhole(t *testing.T) {
	old := []byte("● something the reprint never mentions again at all\r\n")
	live := []byte("● an entirely different conversation line here\r\n")
	if cut := reprintCut(old, live); cut != len(old) {
		t.Fatalf("cut at %d of %d with nothing in common", cut, len(old))
	}
}

// The joined replay puts the saved run at its own width and moves the live
// ring's marks past it.
func TestTheJoinedReplayKeepsEachRunsWidth(t *testing.T) {
	r := &runner{carried: &carryover{cols: 188, bytes: []byte("● old line that only the saved file holds\r\n")}}
	live := []byte("● live line from the new process, long enough\r\n")
	out, cuts, trimmed, _ := r.withCarried(live, []sizeCut{{0, 164, 0}}, 164, 1<<20, false)
	if trimmed {
		t.Fatal("trimmed with plenty of room")
	}
	if !bytes.HasPrefix(out, []byte("● old line")) || !bytes.HasSuffix(out, live) {
		t.Fatalf("the join is not saved-then-live: %q", out)
	}
	if len(cuts) != 2 || cuts[0] != (sizeCut{0, 188, 0}) || cuts[1].cols != 164 ||
		!bytes.HasPrefix(out[cuts[1].at:], live) {
		t.Fatalf("the width marks are wrong: %+v", cuts)
	}
}

// savedLines is n lines of saved history, each one numbered so a test can say
// which end was kept.
func savedLines(n int) []byte {
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "saved line %07d %s\r\n", i, strings.Repeat("x", 60))
	}
	return b.Bytes()
}

// A scrollback setting smaller than the replay bound still cuts first, and says
// so the way it always has.
func TestASmallScrollbackSettingStillCutsTheJoin(t *testing.T) {
	r := &runner{carried: &carryover{cols: 188, bytes: savedLines(1000)}}
	live := []byte("● live line from the new process, long enough\r\n")
	out, _, trimmed, held := r.withCarried(live, nil, 164, 16<<10, false)
	if !trimmed || len(out) > 16<<10 {
		t.Fatalf("trimmed=%v, %d bytes out of a 16KB setting", trimmed, len(out))
	}
	if held != 0 {
		t.Fatal("the replay bound's notice on a cut the setting made")
	}
}
