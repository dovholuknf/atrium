package daemon

import (
	"testing"
	"time"
)

// A message that waits for the turn because its sender asked it to is the
// system working, so the held signal says it is quiet and names the sender's
// rule. The `!` is for a message held against what its sender asked for.
func TestADoneMessageMidTurnIsAQuietHold(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	if typed, err := d.deliverPeerWhen(target, "sg4/doer", "after your turn", true); err != nil || typed {
		t.Fatalf("a done message was typed mid-turn: typed %v, err %v", typed, err)
	}
	a := d.act.get(target.ID)
	if a == nil || a.HeldFor != HeldForTurn || a.HeldTurn != HeldTurnAsked || !a.HeldQuiet {
		t.Fatalf("a done message mid-turn is not a quiet hold the sender asked for: %+v", a)
	}
	// A retry that finds the turn still going keeps it quiet.
	d.pending.attempt(target.ID)
	if a := d.act.get(target.ID); a == nil || !a.HeldQuiet {
		t.Fatalf("a retry mid-turn made the hold loud: %+v", a)
	}
}

// A runner that does not take input mid-turn holds every message for the turn,
// and the signal names the runner and not the sender.
func TestARunnerTurnWaitIsQuietAndNamesTheRunner(t *testing.T) {
	d := testDaemon(t)
	h, err := d.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.MidTurnInput = false
	if _, err := d.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	target, _, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityThinking, "")

	if typed, _ := d.deliverPeer(target, "sg4/doer", "stop now"); typed {
		t.Fatal("typed mid-turn into a runner that does not take it")
	}
	a := d.act.get(target.ID)
	if a == nil || a.HeldFor != HeldForTurn || a.HeldTurn != HeldTurnRunner || !a.HeldQuiet {
		t.Fatalf("a runner's turn wait is not a quiet hold naming the runner: %+v", a)
	}
}

// A message the line holds asked to go in now, so it is never quiet.
func TestALineHoldIsNotQuiet(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a thought"))

	if typed, _ := d.deliverPeer(target, "sg4/doer", "the build is green"); typed {
		t.Fatal("typed into a part written line")
	}
	a := d.act.get(target.ID)
	if a == nil || a.HeldFor != HeldForLine || a.HeldQuiet || a.HeldTurn != "" {
		t.Fatalf("a line hold is quiet or names a turn: %+v", a)
	}
}

// An immediate message queued behind a done one is held against its sender,
// although the oldest is only waiting for the turn. The hold is not quiet.
func TestAnImmediateBehindADoneMessageIsNotQuiet(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	if typed, _ := d.deliverPeerWhen(target, "sg4/doer", "after your turn", true); typed {
		t.Fatal("a done message was typed mid-turn")
	}
	r.noteOperatorTyped([]byte("half a thought"))
	if typed, _ := d.deliverPeer(target, "sg4/doer", "stop now"); typed {
		t.Fatal("typed into a part written line")
	}
	a := d.act.get(target.ID)
	if a == nil || a.HeldCount != 2 || a.HeldFor != HeldForTurn || a.HeldQuiet {
		t.Fatalf("an immediate message behind a done one reads as a quiet hold: %+v", a)
	}
}

// A turn wait past `heldTurnPatience` turns loud, and the retry that notices
// it reports the change so the board is told once.
func TestATurnWaitPastItsPatienceIsNotQuiet(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")
	clock := time.Now()
	d.act.mu.Lock()
	d.act.now = func() time.Time { return clock }
	d.act.mu.Unlock()

	if typed, _ := d.deliverPeerWhen(target, "sg4/doer", "after your turn", true); typed {
		t.Fatal("a done message was typed mid-turn")
	}
	if a := d.act.get(target.ID); a == nil || !a.HeldQuiet {
		t.Fatalf("a fresh done hold is not quiet: %+v", a)
	}
	r := heldPeer{from: "sg4/doer", why: HeldForTurn, turn: HeldTurnAsked, intended: true, count: 1}
	if d.act.setHeld(target.ID, r) {
		t.Fatal("an unchanged quiet hold reported a change")
	}

	d.act.mu.Lock()
	clock = clock.Add(heldTurnPatience + time.Minute)
	d.act.mu.Unlock()
	if a := d.act.get(target.ID); a == nil || a.HeldQuiet || a.HeldFor != HeldForTurn {
		t.Fatalf("a turn wait past its patience is still quiet: %+v", a)
	}
	if !d.act.setHeld(target.ID, r) {
		t.Fatal("a quiet hold turning loud did not report a change")
	}
	if d.act.setHeld(target.ID, r) {
		t.Fatal("a loud hold reported the same change twice")
	}
}
