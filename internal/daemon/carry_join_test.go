package daemon

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
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
	out, cuts, trimmed := r.withCarried(live, []widthCut{{0, 164}}, 164, 1<<20)
	if trimmed {
		t.Fatal("trimmed with plenty of room")
	}
	if !bytes.HasPrefix(out, []byte("● old line")) || !bytes.HasSuffix(out, live) {
		t.Fatalf("the join is not saved-then-live: %q", out)
	}
	if len(cuts) != 2 || cuts[0] != (widthCut{0, 188}) || cuts[1].cols != 164 ||
		!bytes.HasPrefix(out[cuts[1].at:], live) {
		t.Fatalf("the width marks are wrong: %+v", cuts)
	}
}

// Against a real saved file and a real live ring, when both are handed in.
// ATRIUM_CARRY_OLD is a `.scrollback` file, ATRIUM_CARRY_LIVE the raw ring.
func TestRealCarryJoinFindsTheReprint(t *testing.T) {
	oldPath, livePath := os.Getenv("ATRIUM_CARRY_OLD"), os.Getenv("ATRIUM_CARRY_LIVE")
	if oldPath == "" || livePath == "" {
		t.Skip("set ATRIUM_CARRY_OLD and ATRIUM_CARRY_LIVE to run against real data")
	}
	raw, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	_, old, _ := bytes.Cut(raw, []byte("\n"))
	live, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatal(err)
	}
	cut := reprintCut(old, live)
	t.Logf("saved %d bytes, live %d bytes, cut at %d (%.1f%% of the saved history kept)",
		len(old), len(live), cut, 100*float64(cut)/float64(len(old)))
	if cut == len(old) {
		t.Log("no anchor matched: the whole saved history is kept")
	}
	r := &runner{carried: &carryover{cols: 188, bytes: old}}
	joined, cuts, _ := r.withCarried(live, nil, 164, 32<<20)
	began := time.Now()
	out := replayCut(joined, "screen", cuts, 48)
	t.Logf("screen replay of %d joined bytes took %v and produced %d bytes",
		len(joined), time.Since(began), len(out))
	tail, _ := plainText(old[max(0, cut-400):cut])
	head, _ := plainText(old[cut:min(len(old), cut+300)])
	t.Logf("kept ends with:\n%s\n---- dropped starts with:\n%s", tail, head)
}
