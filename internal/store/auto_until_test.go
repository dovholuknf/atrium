package store

import (
	"testing"
	"time"
)

// Auto mode until a time, rather than until somebody remembers.
//
// The deadline is read when a decision is made and never enforced by a timer.
// A timer that has to fire is a timer that does not fire across a restart, and
// auto mode surviving a restart it should not have survived is the failure
// worth designing against. That is why every test here moves the clock rather
// than waiting.

func TestAutoWithNoDeadlineIsOnForever(t *testing.T) {
	task := &Task{AutoApprove: true}
	far := time.Now().AddDate(10, 0, 0)
	if !task.AutoOn(far) {
		t.Fatal("auto mode with no deadline switched itself off")
	}
	if task.AutoExpired(far) {
		t.Fatal("auto mode with no deadline reported itself expired")
	}
}

func TestAutoIsOnUntilItsDeadlineAndOffAfter(t *testing.T) {
	at := time.Now().UTC()
	deadline := at.Add(time.Hour)
	task := &Task{AutoApprove: true, AutoUntil: &deadline}

	if !task.AutoOn(at) {
		t.Fatal("auto mode was off immediately after being turned on")
	}
	if !task.AutoOn(deadline.Add(-time.Second)) {
		t.Fatal("auto mode was off a second before its deadline")
	}
	if task.AutoOn(deadline) {
		t.Fatal("auto mode was still on at its deadline")
	}
	if task.AutoOn(deadline.Add(time.Second)) {
		t.Fatal("auto mode was still on a second after its deadline")
	}
}

// Off is off, whatever the deadline says. A deadline is a limit on being on,
// not a way of being on.
func TestADeadlineCannotTurnAutoModeOn(t *testing.T) {
	at := time.Now().UTC()
	deadline := at.Add(time.Hour)
	task := &Task{AutoApprove: false, AutoUntil: &deadline}
	if task.AutoOn(at) {
		t.Fatal("a deadline turned auto mode on for a card that had it off")
	}
	if task.AutoExpired(at) {
		t.Fatal("a card with auto mode off reported an expiry")
	}
}

// The state the chain cleans up: still flagged on, deadline gone by.
func TestExpiryIsDistinctFromBeingOff(t *testing.T) {
	at := time.Now().UTC()
	past := at.Add(-time.Minute)
	task := &Task{AutoApprove: true, AutoUntil: &past}
	if task.AutoOn(at) {
		t.Fatal("an expired card still answers as on")
	}
	if !task.AutoExpired(at) {
		t.Fatal("an expired card is not reported as needing tidying up")
	}
}

func TestADeadlineRoundTripsThroughTheDatabase(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "a", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	until := time.Now().UTC().Add(90 * time.Minute).Truncate(time.Millisecond)
	if err := s.SetAutoApproveUntil(task.ID, true, &until); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoApprove {
		t.Fatal("auto mode was not turned on")
	}
	if got.AutoUntil == nil {
		t.Fatal("the deadline was not stored")
	}
	if !got.AutoUntil.Equal(until) {
		t.Fatalf("the deadline came back as %v, wanted %v", got.AutoUntil, until)
	}
}

// Turning it off clears the deadline. "Off until Tuesday" is not a thing
// anybody means, and a deadline left behind by an off switch would turn itself
// back on the next time somebody flipped it.
func TestTurningItOffClearsTheDeadline(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "b", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(time.Hour)
	if err := s.SetAutoApproveUntil(task.ID, true, &until); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoApprove(task.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoUntil != nil {
		t.Fatalf("turning it off left a deadline behind: %v", got.AutoUntil)
	}

	// And turning it back on with no deadline is on with no deadline, rather
	// than picking up the old one.
	if err := s.SetAutoApprove(task.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoUntil != nil {
		t.Fatal("turning it back on resurrected an old deadline")
	}
}

// The board-wide switch, which stores its deadline in the value rather than in
// a second key, so the two cannot disagree.

func TestGlobalAutoWithADeadline(t *testing.T) {
	s := open(t)

	if on, until := s.GlobalAutoUntil(); on || until != nil {
		t.Fatal("a fresh database has global auto on")
	}

	until := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	if err := s.SetGlobalAutoUntil(true, &until); err != nil {
		t.Fatal(err)
	}
	on, got := s.GlobalAutoUntil()
	if !on {
		t.Fatal("global auto was off immediately after being turned on")
	}
	if got == nil || !got.Equal(until) {
		t.Fatalf("the deadline came back as %v, wanted %v", got, until)
	}
	if !s.GlobalAuto() {
		t.Fatal("GlobalAuto disagrees with GlobalAutoUntil")
	}
}

// A deadline in the past is off, without anything having run to make it so.
// This is the whole point: nothing has to fire.
func TestAGlobalDeadlineInThePastIsOff(t *testing.T) {
	s := open(t)
	past := time.Now().UTC().Add(-time.Minute)
	if err := s.SetGlobalAutoUntil(true, &past); err != nil {
		t.Fatal(err)
	}
	if s.GlobalAuto() {
		t.Fatal("a deadline that has passed still approves everything")
	}
	on, until := s.GlobalAutoUntil()
	if on {
		t.Fatal("an expired switch reports itself on")
	}
	if until == nil {
		t.Fatal("an expired switch forgot when it expired, so nothing can say why it is off")
	}
}

// Turning it on with no deadline is still available and still means forever.
func TestGlobalAutoWithNoDeadlineIsUnchanged(t *testing.T) {
	s := open(t)
	if err := s.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}
	on, until := s.GlobalAutoUntil()
	if !on {
		t.Fatal("global auto was not turned on")
	}
	if until != nil {
		t.Fatalf("a deadline appeared from nowhere: %v", until)
	}
}

// A stored value that will not parse is not a licence to approve everything.
// This is the failure posture the whole permission path uses: the safe answer
// to "should I stop asking" is no.
func TestAnUnreadableDeadlineIsOff(t *testing.T) {
	s := open(t)
	if err := s.SetSetting(SettingGlobalAuto, "until:not-a-time"); err != nil {
		t.Fatal(err)
	}
	if s.GlobalAuto() {
		t.Fatal("a value that will not parse turned auto mode on")
	}
}

// It survives a restart as whatever it was, deadline included. A restart is
// not consent to start asking again and it is not consent to keep approving.
func TestADeadlineSurvivesAReopen(t *testing.T) {
	path := t.TempDir() + "/atrium.db"
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	if err := first.SetGlobalAutoUntil(true, &until); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	on, got := second.GlobalAutoUntil()
	if !on {
		t.Fatal("a restart turned auto mode off")
	}
	if got == nil || !got.Equal(until) {
		t.Fatalf("the deadline came back as %v after a restart", got)
	}
}
