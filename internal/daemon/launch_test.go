package daemon

import "testing"

// One session per directory, for callers that are not a person.
//
// The board's launch dialog means it: somebody is looking at the card list and
// pressed start. A script is not, and the case this exists for is `gwt new`
// run twice on a worktree that already has a session, which used to put two
// claudes in one directory with neither aware of the other.

func TestTheDefaultIsStillToStart(t *testing.T) {
	for _, live := range []bool{true, false} {
		if got := handOverTo("", live); got != startAnyway {
			t.Fatalf("an unasked launch was interfered with (live=%v): %v", live, got)
		}
	}
}

func TestSkipHandsBackWhateverIsThere(t *testing.T) {
	for _, live := range []bool{true, false} {
		if got := handOverTo("skip", live); got != handBack {
			t.Fatalf("skip started something (live=%v): %v", live, got)
		}
	}
}

// THE ONE THAT MATTERS. Adopting a card that has a runner on it would mean two
// processes on one card, writing to one directory, and the card describing
// whichever spoke last. It degrades to skip rather than to start.
func TestAdoptWillNotJoinALiveRunner(t *testing.T) {
	if got := handOverTo("adopt", true); got != handBack {
		t.Fatalf("adopt put a second runner on a live card: %v", got)
	}
	if got := handOverTo("adopt", false); got != startOnto {
		t.Fatalf("adopt made a second card instead of continuing the one here: %v", got)
	}
}

// A value nobody implemented is not a licence to guess. It means the same as
// asking for nothing, so a typo in a script starts one session rather than
// silently skipping every launch it ever makes.
func TestAnUnknownAnswerIsTheDefault(t *testing.T) {
	if got := handOverTo("SKIP", false); got != startAnyway {
		t.Fatalf("an unrecognised if_running was acted on: %v", got)
	}
}
