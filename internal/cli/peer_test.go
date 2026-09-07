package cli

import (
	"bytes"
	"strings"
	"testing"
)

// Printing "which of these want me".
//
// The daemon decides what each card wants. What is tested here is that the
// four states which were repeatedly confused READ DIFFERENTLY on a terminal,
// because a list that ranks correctly and then prints sixteen identical-looking
// lines has not answered anything.

func render(peers []peerRow) string {
	var b bytes.Buffer
	printPeers(&b, peers)
	return b.String()
}

func TestTheFourStatesArePrintedUnderDifferentHeadings(t *testing.T) {
	out := render([]peerRow{
		{Handle: "stuck", Want: "blocked", Note: "stopped and asked: which schema", Seconds: 900},
		{Handle: "curious", Want: "question", Note: "still working, and asked: stub postgres?"},
		{Handle: "shipper", Want: "finished", Note: "finished and filed a recap", Recap: "wired the reaper"},
		{Handle: "vanished", Want: "quiet", Note: "no word for 52m", Seconds: 3120},
	})

	for _, want := range []string{
		"STOPPED AND ASKING", "ASKED WHILE STILL WORKING", "FINISHED", "NOTHING RECORDED",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("no heading for %q:\n%s", want, out)
		}
	}
	// The question and the recap are the reason to look. Ranking a card to the
	// top and then not saying what it wants sends somebody to a terminal to
	// find out, which is the thing this exists to save them from.
	for _, want := range []string{"which schema", "stub postgres?", "wired the reaper", "no word for 52m"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the words are missing: %q\n%s", want, out)
		}
	}
}

// The order on screen is the daemon's order, not this printer's. A bucket that
// wants a human is above one that does not, and a dispatcher reads the top and
// stops.
func TestBlockedIsPrintedAboveWorking(t *testing.T) {
	out := render([]peerRow{
		{Handle: "busy", Want: "working", Note: "running Bash"},
		{Handle: "stuck", Want: "blocked", Note: "stopped and asked: which schema"},
	})
	if strings.Index(out, "stuck") > strings.Index(out, "busy") {
		t.Fatalf("a working session is printed above a blocked one:\n%s", out)
	}
}

// A bucket this binary has never heard of still prints.
//
// An older CLI against a newer daemon would otherwise drop a whole group in
// silence, which is the worst way for a list of what wants you to be wrong.
func TestAnUnknownBucketStillPrints(t *testing.T) {
	out := render([]peerRow{{Handle: "odd", Want: "invented", Note: "something new"}})
	if !strings.Contains(out, "odd") || !strings.Contains(out, "something new") {
		t.Fatalf("a bucket from a newer daemon vanished:\n%s", out)
	}
}

// A recap is up to two thousand characters and this is a list somebody scans.
func TestALongRecapIsCutToOneLine(t *testing.T) {
	out := render([]peerRow{{
		Handle: "wordy", Want: "finished", Note: "finished and filed a recap",
		Recap: strings.Repeat("and then ", 200) + "\nwith a newline in it",
	}})
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if len(line) > 200 {
			t.Fatalf("a line is %d characters:\n%s", len(line), line)
		}
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 1 {
		t.Fatalf("a recap with a newline broke the row:\n%s", out)
	}
}
