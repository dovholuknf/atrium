package daemon

import (
	"strings"
	"testing"
	"time"
)

// held reads the number of entries waiting for a card, under the lock.
func heldCount(pi *pendingInjector, taskID string) int {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if ht := pi.by[taskID]; ht != nil {
		return len(ht.entries)
	}
	return 0
}

func heldStep(pi *pendingInjector, taskID string) int {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	if ht := pi.by[taskID]; ht != nil {
		return ht.step
	}
	return -1
}

// A deferred message is registered, raises the live board signal, and does not
// touch the terminal while the gate is shut.
func TestADeferredMessageIsHeldAndNotTyped(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	// The operator is mid-line, so the gate is shut.
	r.noteOperatorTyped([]byte("half a command"))

	m, err := d.st.QueueFromPeer(target.ID, "the migration is ready", "sg4/doer")
	if err != nil {
		t.Fatal(err)
	}
	d.deferPeerInjection(target.ID, m.ID, "sg4/doer", "the migration is ready")
	t.Cleanup(func() { d.pending.stopAll() })

	if got := heldCount(d.pending, target.ID); got != 1 {
		t.Fatalf("the message was not held for retry: %d entries", got)
	}
	a := d.act.get(target.ID)
	if a == nil || a.HeldPeer != "sg4/doer" {
		t.Fatalf("the held-message board signal is not set: %+v", a)
	}
	if f.written() != "" {
		t.Fatalf("typed into a mid-line terminal instead of holding: %q", f.written())
	}
}

// A retry into a still-shut gate types nothing, warns by widening the backoff,
// and keeps the message.
func TestARetryIntoAShutGateWidensTheBackoff(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a command"))

	m, _ := d.st.QueueFromPeer(target.ID, "look at the redo", "sg4/doer")
	d.deferPeerInjection(target.ID, m.ID, "sg4/doer", "look at the redo")
	if heldStep(d.pending, target.ID) != 0 {
		t.Fatalf("a fresh hold should start at the front of the backoff")
	}

	d.pending.attempt(target.ID)

	if f.written() != "" {
		t.Fatalf("a shut gate still typed into the terminal: %q", f.written())
	}
	if got := heldStep(d.pending, target.ID); got != 1 {
		t.Fatalf("a blocked retry did not widen the backoff: step %d", got)
	}
	if got := heldCount(d.pending, target.ID); got != 1 {
		t.Fatalf("a blocked retry dropped the message: %d entries", got)
	}
}

// A retry into an open gate types the message, submits it, marks it delivered
// so the hooks do not send it twice, and clears the hold.
func TestARetryIntoAnOpenGateLandsAndClears(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	// Emptied and quiet past the idle window: the gate is open.
	r.noteOperatorTyped([]byte("ls\r"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()

	m, _ := d.st.QueueFromPeer(target.ID, "the build is green", "ci-green")
	d.deferPeerInjection(target.ID, m.ID, "ci-green", "the build is green")

	d.pending.attempt(target.ID)

	got := f.written()
	if !strings.Contains(got, "the build is green") {
		t.Fatalf("the retry never reached the terminal: %q", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Fatalf("the retry did not submit: %q", got)
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("the hold was not cleared after landing")
	}
	pending, err := d.st.PendingMessages(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("the landed message was not marked delivered: %d still pending", len(pending))
	}
	if a := d.act.get(target.ID); a != nil && a.HeldPeer != "" {
		t.Fatalf("the board signal still says a message is held: %+v", a)
	}
}

// A message the hooks delivered another way is dropped on the next retry rather
// than typed on top.
func TestARetryDropsAMessageTheHooksAlreadyDelivered(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	// Open gate, so the only thing that can stop a type is the reconcile.
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.unsent = 0
	r.typeMu.Unlock()

	m, _ := d.st.QueueFromPeer(target.ID, "handled already", "sg4/doer")
	d.deferPeerInjection(target.ID, m.ID, "sg4/doer", "handled already")
	// The Stop hook drains it before the retry fires.
	if err := d.st.MarkDelivered(target.ID, "stop", []string{m.ID}); err != nil {
		t.Fatal(err)
	}

	d.pending.attempt(target.ID)

	if f.written() != "" {
		t.Fatalf("typed a message the hooks had already delivered: %q", f.written())
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("the hold was not cleared after the hook delivered it")
	}
}

// An operator keystroke re-arms the backoff to the front, so a message that had
// slid out to a long interval gets an early retry once he is back.
func TestAKeystrokeReArmsTheBackoff(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a command"))

	m, _ := d.st.QueueFromPeer(target.ID, "still waiting", "sg4/doer")
	d.deferPeerInjection(target.ID, m.ID, "sg4/doer", "still waiting")

	// Slide it out to a multi-hour interval.
	d.pending.mu.Lock()
	d.pending.by[target.ID].step = 6
	d.pending.mu.Unlock()

	// A keystroke on this terminal fires the reset through runner.onKey.
	r.noteOperatorTyped([]byte("x"))

	if got := heldStep(d.pending, target.ID); got != 0 {
		t.Fatalf("a keystroke did not re-arm the backoff to the front: step %d", got)
	}
}

// A card that refuses peer typing is not held at all: there is nothing to retry
// on screen, and the queue plus the hooks carry it.
func TestACardThatRefusesPeerTypingIsNotHeld(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	if err := d.st.SetPeerTyping(target.ID, false); err != nil {
		t.Fatal(err)
	}

	m, _ := d.st.QueueFromPeer(target.ID, "anything", "sg4/doer")
	d.deferPeerInjection(target.ID, m.ID, "sg4/doer", "anything")

	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("held a message for a card that refuses peer typing")
	}
	if a := d.act.get(target.ID); a != nil && a.HeldPeer != "" {
		t.Fatalf("raised the held signal for a card that refuses peer typing: %+v", a)
	}
}
