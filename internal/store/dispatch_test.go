package store

import (
	"strings"
	"testing"
	"time"
)

func queue(t *testing.T, s *Store, room, harness string) *Dispatch {
	t.Helper()
	d, err := s.QueueDispatch(Dispatch{Room: room, Harness: harness, Title: "an item"})
	if err != nil {
		t.Fatalf("queue for %s: %v", room, err)
	}
	return d
}

func TestAnItemIsOnlyOfferedToTheRoomItNames(t *testing.T) {
	s := open(t)
	queue(t, s, "cdaws", "claude")

	got, err := s.ClaimDispatches("cdzrok", MaxHandout)
	if err != nil {
		t.Fatalf("claim as the wrong room: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cdzrok was handed %d item(s) queued for cdaws", len(got))
	}

	got, err = s.ClaimDispatches("cdaws", MaxHandout)
	if err != nil || len(got) != 1 {
		t.Fatalf("cdaws should have been handed its own item: %d, %v", len(got), err)
	}
}

// The race that matters is not two different rooms, which the name already
// settles. It is ONE room name and two processes: a second `atrium room`
// started by hand, or a retry after a reply was lost.
func TestTwoClaimsForOneRoomCannotBothTakeAnItem(t *testing.T) {
	s := open(t)
	queue(t, s, "cdaws", "claude")

	first, err := s.ClaimDispatches("cdaws", MaxHandout)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %d, %v", len(first), err)
	}
	second, err := s.ClaimDispatches("cdaws", MaxHandout)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("the same item was handed out twice, so two runners would start")
	}
}

func TestOnlyTheHolderOfTheTokenMayReportAResult(t *testing.T) {
	s := open(t)
	item := queue(t, s, "cdaws", "claude")

	claimed, err := s.ClaimDispatches("cdaws", MaxHandout)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %d, %v", len(claimed), err)
	}
	if claimed[0].Token == "" {
		t.Fatal("a claim with no token means nothing can be checked on the way back")
	}

	if _, err := s.SettleDispatch(item.ID, "not-the-token", DispatchRunning, "card-1", "", ""); err == nil {
		t.Fatal("a result carrying the wrong token was accepted")
	}
	if _, err := s.SettleDispatch(item.ID, "", DispatchRunning, "card-1", "", ""); err == nil {
		t.Fatal("a result carrying no token at all was accepted")
	}

	got, err := s.SettleDispatch(item.ID, claimed[0].Token, DispatchRunning, "card-1", "", "")
	if err != nil {
		t.Fatalf("the holder could not report: %v", err)
	}
	if got.State != DispatchRunning || got.CardID != "card-1" {
		t.Fatalf("result not recorded: %+v", got)
	}
}

func TestAnItemCannotBeSettledTwice(t *testing.T) {
	s := open(t)
	item := queue(t, s, "cdaws", "claude")
	claimed, _ := s.ClaimDispatches("cdaws", MaxHandout)
	token := claimed[0].Token

	if _, err := s.SettleDispatch(item.ID, token, DispatchRunning, "card-1", "", ""); err != nil {
		t.Fatalf("first result: %v", err)
	}
	// A room whose result POST was retried after it landed must not be able to
	// overwrite what it already said.
	if _, err := s.SettleDispatch(item.ID, token, DispatchFailed, "", "", "changed my mind"); err == nil {
		t.Fatal("a second result for one item was accepted")
	}
}

func TestARoomMayOnlyReportRunningOrFailed(t *testing.T) {
	s := open(t)
	item := queue(t, s, "cdaws", "claude")
	claimed, _ := s.ClaimDispatches("cdaws", MaxHandout)

	for _, state := range []string{DispatchQueued, DispatchClaimed, DispatchCancelled, "made-up"} {
		if _, err := s.SettleDispatch(item.ID, claimed[0].Token, state, "", "", ""); err == nil {
			t.Fatalf("a room was allowed to put an item into %q", state)
		}
	}
}

// The hub never dials a room, so once an item has gone there is nothing here
// that can stop it. Saying no is the only truthful answer.
func TestAnItemAlreadyTakenCannotBeWithdrawn(t *testing.T) {
	s := open(t)
	item := queue(t, s, "cdaws", "claude")

	if err := s.CancelDispatch(item.ID); err != nil {
		t.Fatalf("withdrawing a waiting item: %v", err)
	}
	got, _ := s.Dispatch(item.ID)
	if got.State != DispatchCancelled {
		t.Fatalf("state after withdrawing: %q", got.State)
	}

	other := queue(t, s, "cdaws", "claude")
	if _, err := s.ClaimDispatches("cdaws", MaxHandout); err != nil {
		t.Fatalf("claim: %v", err)
	}
	err := s.CancelDispatch(other.ID)
	if err == nil {
		t.Fatal("an item a room has already taken was withdrawn from here")
	}
	if !strings.Contains(err.Error(), "that machine") {
		t.Fatalf("the refusal should say where to stop it, got: %v", err)
	}
}

// A cancelled item must not then be handed out. The state check in the claim is
// what stops it.
func TestAWithdrawnItemIsNeverHandedOut(t *testing.T) {
	s := open(t)
	item := queue(t, s, "cdaws", "claude")
	if err := s.CancelDispatch(item.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	got, err := s.ClaimDispatches("cdaws", MaxHandout)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatal("a withdrawn item was handed to a room anyway")
	}
}

// moveOn winds the store's clock forward for the rest of the test. Waiting out
// a five minute lease is not a test anybody runs.
func moveOn(t *testing.T, by time.Duration) {
	t.Helper()
	restore := now
	base := restore()
	now = func() time.Time { return base.Add(by) }
	t.Cleanup(func() { now = restore })
}

func TestAClaimThatIsNeverAnsweredGoesBackAndThenGivesUp(t *testing.T) {
	s := open(t)
	item := queue(t, s, "cdaws", "claude")

	first, _ := s.ClaimDispatches("cdaws", MaxHandout)
	if len(first) != 1 {
		t.Fatalf("first claim: %d", len(first))
	}
	// Nothing comes back, and the lease runs out.
	moveOn(t, DispatchLease+time.Minute)
	if _, err := s.ExpireDispatchClaims(DispatchLease); err != nil {
		t.Fatalf("expire: %v", err)
	}
	got, _ := s.Dispatch(item.ID)
	if got.State != DispatchQueued {
		t.Fatalf("a claim nobody answered should go back to the queue, got %q", got.State)
	}
	// And the old holder can no longer speak for it.
	if _, err := s.SettleDispatch(item.ID, first[0].Token, DispatchRunning, "card-1", "", ""); err == nil {
		t.Fatal("the previous holder reported on an item that had been taken back")
	}

	second, _ := s.ClaimDispatches("cdaws", MaxHandout)
	if len(second) != 1 {
		t.Fatalf("second claim: %d", len(second))
	}
	moveOn(t, 2*(DispatchLease+time.Minute))
	if _, err := s.ExpireDispatchClaims(DispatchLease); err != nil {
		t.Fatalf("expire twice: %v", err)
	}
	got, _ = s.Dispatch(item.ID)
	if got.State != DispatchFailed {
		t.Fatalf("after two silent handouts the item should fail, got %q", got.State)
	}
	if got.Error == "" {
		t.Fatal("an item that gave up has to say why on the row")
	}
	// And it must never be offered a third time.
	third, _ := s.ClaimDispatches("cdaws", MaxHandout)
	if len(third) != 0 {
		t.Fatal("a permanently broken item was handed out a third time")
	}
}

func TestOneCheckInCarriesAtMostAHandful(t *testing.T) {
	s := open(t)
	for i := 0; i < MaxHandout+3; i++ {
		queue(t, s, "cdaws", "claude")
	}
	got, err := s.ClaimDispatches("cdaws", 0)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != MaxHandout {
		t.Fatalf("a room came back to a backlog and was handed %d at once", len(got))
	}
}

func TestAQueueForOneRoomIsBounded(t *testing.T) {
	s := open(t)
	for i := 0; i < MaxOpenPerRoom; i++ {
		queue(t, s, "cdaws", "claude")
	}
	_, err := s.QueueDispatch(Dispatch{Room: "cdaws", Harness: "claude"})
	if err == nil {
		t.Fatal("the reply to every check-in that room makes is now unbounded")
	}
	// A different room is unaffected: the bound is per room because the reply
	// it protects is per room.
	if _, err := s.QueueDispatch(Dispatch{Room: "cdzrok", Harness: "claude"}); err != nil {
		t.Fatalf("a full queue for one room blocked another: %v", err)
	}
}

func TestAnItemHasToSayWhereItIsGoingAndWhatToStart(t *testing.T) {
	s := open(t)
	if _, err := s.QueueDispatch(Dispatch{Harness: "claude"}); err == nil {
		t.Fatal("an item with no room was queued, and nothing would ever collect it")
	}
	if _, err := s.QueueDispatch(Dispatch{Room: "cdaws"}); err == nil {
		t.Fatal("an item with no runner was queued, and the room could only refuse it")
	}
	long := strings.Repeat("x", MaxDispatchPrompt+1)
	if _, err := s.QueueDispatch(Dispatch{Room: "cdaws", Harness: "claude", Prompt: long}); err == nil {
		t.Fatal("an instruction past the limit was accepted")
	}
}

// The token is what authorizes the way back, so it must not be readable from
// the list the board draws.
func TestTheListNeverCarriesAToken(t *testing.T) {
	s := open(t)
	queue(t, s, "cdaws", "claude")
	if _, err := s.ClaimDispatches("cdaws", MaxHandout); err != nil {
		t.Fatalf("claim: %v", err)
	}
	items, err := s.Dispatches("")
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %d, %v", len(items), err)
	}
	if items[0].Token != "" {
		t.Fatal("the queue the board draws carries the token that authorizes a result")
	}
}

func TestSweepingLeavesOpenItemsAlone(t *testing.T) {
	s := open(t)
	waiting := queue(t, s, "cdaws", "claude")
	settled := queue(t, s, "cdaws", "claude")
	claimed, _ := s.ClaimDispatches("cdaws", MaxHandout)
	var token string
	for _, c := range claimed {
		if c.ID == settled.ID {
			token = c.Token
		}
	}
	if _, err := s.SettleDispatch(settled.ID, token, DispatchFailed, "", "", "no such directory"); err != nil {
		t.Fatalf("settle: %v", err)
	}
	moveOn(t, 48*time.Hour)
	if _, err := s.SweepDispatch(24 * time.Hour); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := s.Dispatch(settled.ID); err == nil {
		t.Fatal("a settled item survived the sweep")
	}
	if _, err := s.Dispatch(waiting.ID); err != nil {
		t.Fatalf("the sweep deleted a promise nobody has collected: %v", err)
	}
}
