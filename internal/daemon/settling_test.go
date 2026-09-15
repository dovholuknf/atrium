package daemon

import (
	"sync"
	"testing"
	"time"
)

// The window is bounded by the SET and not by a duration. This is the test the
// first version would have failed: it reported settling correctly for twenty
// seconds and the sessions took longer than that to come back.
func TestSettlingHoldsUntilEveryExpectedCardIsBack(t *testing.T) {
	var s settling
	s.begin()
	s.expect([]string{"a", "b", "c"})
	// The opening grace would answer true on its own, so it is taken out of
	// the way first. What is being tested is the set, not the clock.
	s.floor.Store(0)

	if !s.on() {
		t.Fatal("not settling with three cards still to come back")
	}
	s.arrived("a")
	s.arrived("b")
	if !s.on() {
		t.Fatal("stopped settling with one card still to come back")
	}
	s.arrived("c")
	if !s.on() {
		t.Fatal("stopped settling before the boot sequence itself finished")
	}
	s.arrived(settleBoot)
	if s.on() {
		t.Fatal("still settling after everything came back")
	}
}

// THE GAP BETWEEN TWO STAGES. `startFixtures` empties the list it named and
// `reopenSaved` has not named its own yet, so for an instant nothing is
// pending. That instant is where the arrivals leaked, and the boot entry is
// what closes it.
func TestSettlingSurvivesTheGapBetweenStages(t *testing.T) {
	var s settling
	s.begin()
	s.floor.Store(0)

	// Stage one names its list and finishes it.
	s.expect([]string{"fixture:one"})
	s.arrived("fixture:one")

	// Between the stages. Nothing is pending except the boot entry.
	if !s.on() {
		t.Fatal("the window closed between two stages of the same startup")
	}

	// Stage two names its own.
	s.expect([]string{"card-9"})
	if !s.on() {
		t.Fatal("not settling with the second stage still to run")
	}
	s.arrived("card-9")
	s.arrived(settleBoot)
	if s.on() {
		t.Fatal("still settling after both stages finished")
	}
}

// A card that never comes back must not buy silence forever, so failing to
// start counts as arriving. Otherwise a deleted worktree keeps the board quiet
// until the backstop on every restart.
func TestACardThatFailedToStartStopsHoldingTheWindowOpen(t *testing.T) {
	var s settling
	s.begin()
	s.floor.Store(0)
	s.expect([]string{"gone"})
	s.arrived(settleBoot)

	if !s.on() {
		t.Fatal("not settling while a card is still expected")
	}
	// What the caller does after `Launch` returns an error.
	s.arrived("gone")
	if s.on() {
		t.Fatal("a card that could not start held the window open")
	}
}

// A daemon with nothing to restore is still coming up: its sessions rejoin
// through their own hooks, which nothing here can see.
func TestADaemonWithNothingToRestoreIsStillSettlingBriefly(t *testing.T) {
	var s settling
	s.begin()
	s.arrived(settleBoot)
	if !s.on() {
		t.Fatal("a daemon that had nothing to bring back is not covered at all")
	}
	if s.waiting() != 0 {
		t.Fatalf("%d cards pending, want 0", s.waiting())
	}
	// And it ends on its own.
	s.floor.Store(time.Now().Add(-time.Second).UnixMilli())
	if s.on() {
		t.Fatal("the opening grace never ends")
	}
}

// The backstop overrides the set, so one card that never reports cannot
// silence the board indefinitely.
func TestTheBackstopEndsTheWindowWhateverIsPending(t *testing.T) {
	var s settling
	s.begin()
	s.floor.Store(0)
	s.expect([]string{"never"})
	if !s.on() {
		t.Fatal("not settling before the backstop")
	}
	s.deadline.Store(time.Now().Add(-time.Second).UnixMilli())
	if s.on() {
		t.Fatal("a card that never came back held the window open past the backstop")
	}
}

// A passive daemon starts nothing and must not be quiet about cards it had no
// hand in.
func TestAPassiveDaemonIsNotSettling(t *testing.T) {
	var s settling
	if s.on() {
		t.Fatal("settling without ever having begun")
	}
	s.begin()
	s.expect([]string{"x"})
	s.done()
	if s.on() {
		t.Fatal("still settling after done")
	}
}

// Every stage runs in a goroutine and the reader is an HTTP handler.
func TestSettlingIsRacefree(t *testing.T) {
	var s settling
	s.begin()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := string(rune('a' + n))
			s.expect([]string{id})
			_ = s.on()
			s.arrived(id)
		}(i)
	}
	wg.Wait()
	s.arrived(settleBoot)
	if s.waiting() != 0 {
		t.Fatalf("%d left pending, want 0", s.waiting())
	}
}
