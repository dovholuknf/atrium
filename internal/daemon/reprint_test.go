package daemon

import "testing"

// A CLAUDE CARD REPLAYS ONE COPY OF ITS TRANSCRIPT, not one per width it was
// ever drawn at. Claude reprints everything at a width change, so each older
// width in the ring is a stale copy. A shell keeps every width, because a
// resize does not reprint there and the older runs are the only copy.
func TestAReprintingRunnerReplaysFromTheLastWidth(t *testing.T) {
	r := newRing(1<<16, 80)
	r.Write([]byte("the transcript at eighty\n"))
	r.SetWidth(120)
	r.Write([]byte("the transcript at one twenty\n"))
	backlog, cuts, _, _ := r.ReplayCuts()

	got, gotCuts := lastWidthOnly(backlog, cuts, true)
	if string(got) != "the transcript at one twenty\n" {
		t.Fatalf("a claude card replayed an older copy of its transcript: %q", got)
	}
	if len(gotCuts) != 1 || gotCuts[0] != (widthCut{0, 120}) {
		t.Fatalf("the cut was not rebased onto the trimmed replay: %+v", gotCuts)
	}

	got, gotCuts = lastWidthOnly(backlog, cuts, false)
	if string(got) != string(backlog) || len(gotCuts) != 2 {
		t.Fatalf("a shell lost the history drawn at an older width: %q %+v", got, gotCuts)
	}
}

// One width is nothing to trim.
func TestOneWidthReplaysWhole(t *testing.T) {
	r := newRing(1<<16, 80)
	r.Write([]byte("only width\n"))
	backlog, cuts, _, _ := r.ReplayCuts()
	if got, _ := lastWidthOnly(backlog, cuts, true); string(got) != "only width\n" {
		t.Fatalf("a single-width replay was trimmed: %q", got)
	}
}
