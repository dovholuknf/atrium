package api

import "testing"

// The scrollback box asks for megabytes per ring and is read as though it were
// the board's whole budget. These tests are about the figure that closes that
// gap: the setting multiplied by the rings that are live.

func TestTheScrollbackTotalCountsRunnersAndShells(t *testing.T) {
	srv, _, _ := fileServer(t)
	t.Cleanup(func() { LiveRings = nil })
	LiveRings = func() (int, int) { return 8, 2 }

	if rec := settingsPost(t, srv, `{"scrollback_mb":"64"}`); rec.Code != 200 {
		t.Fatalf("setting the megabytes answered %d: %s", rec.Code, rec.Body.String())
	}
	out := settingsGet(t, srv)
	if got := out["scrollback_runners"]; got != float64(8) {
		t.Errorf("runners came back %v, wanted 8", got)
	}
	// A shell is a second ring at the same size, and nothing in the interface
	// says opening one has a memory cost. It has to be in the total.
	if got := out["scrollback_shells"]; got != float64(2) {
		t.Errorf("shells came back %v, wanted 2", got)
	}
	if got := out["scrollback_mb_total"]; got != float64(64*10) {
		t.Errorf("the total came back %v, wanted 640", got)
	}
}

// An empty box means the default, and the total has to be the default times
// the rings rather than zero.
func TestTheScrollbackTotalUsesWhatIsInForce(t *testing.T) {
	srv, _, _ := fileServer(t)
	t.Cleanup(func() { LiveRings = nil })
	LiveRings = func() (int, int) { return 3, 0 }

	if got := settingsGet(t, srv)["scrollback_mb_total"]; got != float64(defaultScrollbackMB*3) {
		t.Errorf("an unset box gave a total of %v, wanted %d", got, defaultScrollbackMB*3)
	}
}

// A build with no supervision has no rings, so the answer is zero rather than
// a missing field the board would have to guess at.
func TestTheScrollbackTotalIsZeroWithNoSupervision(t *testing.T) {
	srv, _, _ := fileServer(t)
	t.Cleanup(func() { LiveRings = nil })
	LiveRings = nil

	out := settingsGet(t, srv)
	if got := out["scrollback_mb_total"]; got != float64(0) {
		t.Errorf("the total came back %v, wanted 0", got)
	}
	if _, ok := out["scrollback_runners"]; !ok {
		t.Error("the runner count is missing entirely")
	}
}
