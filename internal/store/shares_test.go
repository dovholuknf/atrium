package store

import "testing"

// A share outliving the process that made it is the whole point of this table,
// and the distinction that carries it is `wanted`. Every test here is about
// that one column, because getting it wrong does not fail loudly: it either
// loses an address somebody is holding, or leaves a name reserved on an
// account forever.

func TestShareSurvivesAShutdownAndDiesOnAStop(t *testing.T) {
	s := open(t)
	rec := CardShare{
		TaskID: "card-1", Kind: "zrok", Mode: "public",
		Namespace: "public", Name: "atrium-abc123def456",
		Token: "tok-first", Address: "https://atrium-abc123def456.example/#term=card-1",
		Wanted: true,
	}
	if err := s.PutCardShare(rec); err != nil {
		t.Fatalf("put: %v", err)
	}

	// A shutdown releases the share and rewrites the row with a new token on
	// the way back. The NAME must not move: it is the address somebody has.
	rec.Token = "tok-second"
	if err := s.PutCardShare(rec); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got, err := s.CardShareFor("card-1")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.Name != "atrium-abc123def456" {
		t.Fatalf("the reserved name changed on a rebind: %q. the link somebody was "+
			"given now reaches nothing", got.Name)
	}
	if got.Token != "tok-second" {
		t.Fatalf("token is %q, want the one from the latest bind", got.Token)
	}

	wanted, err := s.WantedCardShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(wanted) != 1 || wanted[0].TaskID != "card-1" {
		t.Fatalf("a shared card is not in the restore list: %+v", wanted)
	}

	// Stopping is the operator withdrawing the link. It must leave nothing to
	// restore, or a restart would put back an address they just gave up.
	if err := s.UnwantCardShare("card-1"); err != nil {
		t.Fatalf("unwant: %v", err)
	}
	wanted, err = s.WantedCardShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(wanted) != 0 {
		t.Fatalf("a stopped share is still queued for restore: %+v", wanted)
	}

	// The ROW survives the stop, holding the name that is still reserved on the
	// account. Losing it here is how an account fills up with names nothing
	// will ever ask for again.
	got, err = s.CardShareFor("card-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the row was deleted by a stop, so the reserved name it held can " +
			"never be released")
	}
	if got.Name != "atrium-abc123def456" {
		t.Fatalf("the name to release was lost: %q", got.Name)
	}
	if got.Wanted {
		t.Fatal("a stopped share still says it is wanted")
	}
}

// A card can be pruned while its share is recorded. Nothing on the board can
// see what is left, because the board draws cards and the card is what went.
func TestStaleSharesAreFindableAfterTheCardIsGone(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{
		WireName: "atrium-9", Worktree: "d:/git/atrium", Runner: "claude", PID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutCardShare(CardShare{
		TaskID: task.ID, Mode: "public", Namespace: "public",
		Name: "atrium-orphaned01", Wanted: true,
	}); err != nil {
		t.Fatal(err)
	}

	stale, err := s.StaleCardShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("a share on a live card is reported as orphaned: %+v", stale)
	}

	if err := s.Forget(task.ID); err != nil {
		t.Fatalf("delete the card: %v", err)
	}
	stale, err = s.StaleCardShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].Name != "atrium-orphaned01" {
		t.Fatalf("the orphaned name is not findable, so nothing will ever release "+
			"it: %+v", stale)
	}

	if err := s.ForgetCardShare(task.ID); err != nil {
		t.Fatal(err)
	}
	stale, err = s.StaleCardShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("forgetting left the row behind: %+v", stale)
	}
}

// Re-sharing a card that was stopped writes the same row rather than a second
// one. One card, one address: two rows would mean two names reserved and only
// one of them ever released.
func TestOneShareRowPerCard(t *testing.T) {
	s := open(t)
	for _, name := range []string{"atrium-first0000000", "atrium-second000000"} {
		if err := s.PutCardShare(CardShare{
			TaskID: "card-2", Mode: "public", Namespace: "public",
			Name: name, Wanted: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	wanted, err := s.WantedCardShares()
	if err != nil {
		t.Fatal(err)
	}
	if len(wanted) != 1 {
		t.Fatalf("one card ended up with %d share rows", len(wanted))
	}
	if wanted[0].Name != "atrium-second000000" {
		t.Fatalf("the row kept the old name %q", wanted[0].Name)
	}
}

// A mode outside the two that exist is refused by the schema rather than
// stored. A stored third mode would be a share nothing knows how to rebind.
func TestShareModeIsChecked(t *testing.T) {
	s := open(t)
	err := s.PutCardShare(CardShare{TaskID: "card-3", Mode: "sort-of-public", Wanted: true})
	if err == nil {
		t.Fatal("a mode that is neither public nor private was accepted")
	}
}
