//go:build integration

package daemon

import (
	"strings"
	"testing"
)

func sayHeld(t *testing.T, d *Daemon, id string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		target, err := d.st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		typed, mid, err := d.deliverPeerWhenID(target, "sg4/orchestrator", text, "", false)
		if err != nil || typed || mid == "" {
			t.Fatalf("say %q typed %v id %q err %v, want held", text, typed, mid, err)
		}
	}
}

func assertOnceInOrder(t *testing.T, written string, texts ...string) {
	t.Helper()
	at := -1
	for _, text := range texts {
		if n := strings.Count(written, text); n != 1 {
			t.Fatalf("%q was typed %d times, want once: %q", text, n, written)
		}
		i := strings.Index(written, text)
		if i < at {
			t.Fatalf("%q arrived out of order: %q", text, written)
		}
		at = i
	}
}

func assertNonePending(t *testing.T, d *Daemon, id string) {
	t.Helper()
	pending, err := d.st.PendingMessages(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d message(s) still pending", len(pending))
	}
}

func hooksPass(t *testing.T, d *Daemon, id string) {
	t.Helper()
	for _, via := range []string{"permission", "permission", "stop"} {
		msgs, err := d.takeMessages(id, via)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 0 {
			t.Fatalf("the %s hook carried a message the typist holds: %+v", via, msgs)
		}
	}
}

func TestHeldSaysArriveOnceInOrderWhenTheGateReopens(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	loseTheLine(r)

	sayHeld(t, d, target.ID, "HELDONE", "HELDTWO")
	hooksPass(t, d, target.ID)
	d.pending.attempt(target.ID)
	if strings.Contains(f.written(), "HELD") {
		t.Fatalf("typed through a closed gate: %q", f.written())
	}

	r.noteOperatorTyped([]byte("\r"))
	quietSince(r)
	d.pending.attempt(target.ID)

	assertOnceInOrder(t, f.written(), "HELDONE", "HELDTWO")
	assertNonePending(t, d, target.ID)
	hooksPass(t, d, target.ID)
}

func TestHeldSaysSurviveAForceSayAndStillArrive(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	loseTheLine(r)

	sayHeld(t, d, target.ID, "HELDONE", "HELDTWO")
	hooksPass(t, d, target.ID)

	code, out := forceSayVia(t, d, target.ID, `{"text":"FORCED","force":true}`)
	if code != 200 || out["delivered"] != "terminal" {
		t.Fatalf("force answered %d %v", code, out)
	}
	hooksPass(t, d, target.ID)

	r.noteOperatorTyped([]byte("\r"))
	quietSince(r)
	d.pending.attempt(target.ID)

	assertOnceInOrder(t, f.written(), "HELDONE", "HELDTWO")
	assertNonePending(t, d, target.ID)
}

func TestHeldSaysAreStillDeliveredAfterTheRoomRestarts(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	loseTheLine(r)

	sayHeld(t, d, target.ID, "HELDONE", "HELDTWO")
	d.pending.stopAll()

	d.reholdPending(target.ID)
	r.noteOperatorTyped([]byte("\r"))
	quietSince(r)
	d.pending.attempt(target.ID)

	assertOnceInOrder(t, f.written(), "HELDONE", "HELDTWO")
	assertNonePending(t, d, target.ID)
}
