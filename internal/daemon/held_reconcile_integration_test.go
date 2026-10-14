//go:build integration

package daemon

import "testing"

func heldView(d *Daemon, id string) *Activity {
	a, _ := d.activityFor(id).(*Activity)
	return a
}

// Messages that left the queue by a path that never told the injector must not leave the chip standing, and a
// partial departure must leave the count at what is actually still pending.
func TestTheHeldChipIsRecomputedFromWhatIsStillPending(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a command"))

	m1, _ := d.st.QueueFromPeer(target.ID, "one", "sg4/doer")
	d.deferPeerInjection(target.ID, m1.ID, "sg4/doer", "one", "", false)
	m2, _ := d.st.QueueFromPeer(target.ID, "two", "sg4/doer")
	d.deferPeerInjection(target.ID, m2.ID, "sg4/doer", "two", "", false)
	if a := heldView(d, target.ID); a == nil || a.HeldCount != 2 {
		t.Fatalf("two held messages should read as two: %+v", a)
	}

	// One leaves the queue behind the injector's back.
	if err := d.st.MarkDelivered(target.ID, "stop", []string{m1.ID}); err != nil {
		t.Fatal(err)
	}
	if a := heldView(d, target.ID); a == nil || a.HeldCount != 1 {
		t.Fatalf("one delivered, one still held, but the chip says %+v", a)
	}

	// The other leaves too.
	if err := d.st.MarkDelivered(target.ID, "stop", []string{m2.ID}); err != nil {
		t.Fatal(err)
	}
	if a := heldView(d, target.ID); a != nil && a.HeldPeer != "" {
		t.Fatalf("nothing is pending and the chip still says held: %+v", a)
	}
	if got := heldCount(d.pending, target.ID); got != 0 {
		t.Fatalf("the injector still holds %d entries for nothing pending", got)
	}
}

// A chip with no injector entry behind it (a restart, a stopAll) has nothing retrying it and is cleared even
// though a message is still queued: the queue and hooks carry it, and the chip would never clear itself.
func TestAHeldChipWithNoInjectorEntryIsCleared(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	if _, err := d.st.QueueFromPeer(target.ID, "queued", "sg4/doer"); err != nil {
		t.Fatal(err)
	}
	d.act.setHeld(target.ID, heldPeer{from: "sg4/doer", why: HeldForLine, count: 1})
	if a := heldView(d, target.ID); a != nil && a.HeldPeer != "" {
		t.Fatalf("a chip nothing is retrying survived: %+v", a)
	}
}
