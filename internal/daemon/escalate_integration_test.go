//go:build integration

package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// doneTo queues a `when: done` message and returns its id.
func doneTo(t *testing.T, d *Daemon, taskID, from, text string) string {
	t.Helper()
	fields := map[string]string{"text": text, "when": "done"}
	if from != "" {
		fields["from"] = from
	}
	if code := message(t, d, taskID, fields); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	pending, err := d.st.PendingMessages(taskID)
	if err != nil || len(pending) == 0 {
		t.Fatalf("nothing queued: %v", err)
	}
	return pending[len(pending)-1].ID
}

func age(t *testing.T, d *Daemon, msgID string, by time.Duration) {
	t.Helper()
	if err := d.st.BackdateMessage(msgID, by); err != nil {
		t.Fatal(err)
	}
}

// A done message is delivered by the first tool call after 15 minutes, with the
// framing line, and not before. The card and the sender's say both record it.
func TestADoneMessageIsCarriedByTheToolCallAfterFifteenMinutes(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	id := doneTo(t, d, bob.ID, "alice", "rebase when you can")

	age(t, d, id, 14*time.Minute)
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 0 {
		t.Fatalf("a 14 minute old done message was carried: %d", len(msgs))
	}

	age(t, d, id, 23*time.Minute)
	msgs, err := d.takeMessages(bob.ID, "permission")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("the aged message was not carried: %d, %v", len(msgs), err)
	}
	want := "[atrium] this message waited 23 minutes for your turn to end, so it is delivered now. " +
		"finish the step you are on, then read it.\nrebase when you can"
	if msgs[0].Text != want {
		t.Fatalf("framing is wrong:\n got %q\nwant %q", msgs[0].Text, want)
	}

	found := false
	evs, _ := d.st.Events(bob.ID, 50)
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "held message escalated after 23m") {
			found = true
		}
	}
	if !found {
		t.Fatal("the card has no escalation event")
	}
	says, _ := d.st.SaysFor(bob.ID, 10)
	noted := false
	for _, s := range says {
		if s.MessageID == id && strings.Contains(s.Note, "escalated after 23m") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("the sender's say record has no escalation note: %+v", says)
	}

	// Delivered, so it cannot escalate twice.
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 0 {
		t.Fatal("the message was carried twice")
	}
}

// The age is the room setting, in minutes.
func TestTheHeldAfterSettingMovesTheLine(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	if err := d.st.SetSetting(store.SettingEscalateHeldAfter, "5"); err != nil {
		t.Fatal(err)
	}
	id := doneTo(t, d, bob.ID, "alice", "rebase when you can")
	age(t, d, id, 4*time.Minute)
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 0 {
		t.Fatal("carried before the setting")
	}
	age(t, d, id, 6*time.Minute)
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 1 {
		t.Fatal("not carried after the setting")
	}
}

// A message younger than the setting at the turn's end is the Stop hook's, with no
// framing and no escalation event.
func TestAYoungDoneMessageIsStillDeliveredByTheStopHook(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	id := doneTo(t, d, bob.ID, "alice", "rebase when you can")
	age(t, d, id, 10*time.Minute)

	msgs, err := d.takeMessages(bob.ID, "stop")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("the Stop hook did not carry it: %d, %v", len(msgs), err)
	}
	if msgs[0].Text != "rebase when you can" {
		t.Fatalf("a Stop delivery was framed: %q", msgs[0].Text)
	}
	evs, _ := d.st.Events(bob.ID, 50)
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "escalated") {
			t.Fatalf("a Stop delivery was recorded as an escalation: %s", e.Payload)
		}
	}
}

// A card whose deploy wake is still to be typed keeps an old message waiting.
func TestAnAgedMessageWaitsForTheDeployWake(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	id := doneTo(t, d, bob.ID, "", "rebase when you can")
	age(t, d, id, 20*time.Minute)
	d.holds.await([]string{bob.ID}, time.Now())

	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 0 {
		t.Fatalf("an aged message landed ahead of the wake: %d", len(msgs))
	}
	d.holds.woke(bob.ID)
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 1 {
		t.Fatal("the aged message did not land once the wake was typed")
	}
}

// The typist, on a runner that takes input mid-turn: an aged message stops waiting
// for the turn and is typed.
func TestTheTypistStopsHoldingAnAgedMessageMidTurn(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")
	id := doneTo(t, d, target.ID, "sg4/doer", "neither matrix exists")

	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("typed a young done message mid-turn: %q", f.written())
	}

	age(t, d, id, 20*time.Minute)
	d.pending.mu.Lock()
	d.pending.by[target.ID].entries[0].at = time.Now().Add(-20 * time.Minute)
	d.pending.mu.Unlock()
	d.pending.attempt(target.ID)
	if got := f.written(); !strings.Contains(got, "neither matrix exists") {
		t.Fatalf("the aged message was not typed mid-turn: %q", got)
	}
	if got := f.written(); !strings.Contains(got, "waited 20 minutes for your turn to end") {
		t.Fatalf("the typed escalation has no framing line: %q", got)
	}
	evs, _ := d.st.Events(target.ID, 50)
	found := false
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "held message escalated after 20m") {
			found = true
		}
	}
	if !found {
		t.Fatal("the typed escalation was not recorded")
	}
}

// A runner that does not take input mid-turn: the typist keeps waiting for the
// turn however old the message is, and the hook route delivers it.
func TestARunnerWithoutMidTurnInputGetsAnAgedMessageByTheHookOnly(t *testing.T) {
	d := testDaemon(t)
	h, err := d.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.MidTurnInput = false
	if _, err := d.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")
	id := doneTo(t, d, target.ID, "sg4/doer", "neither matrix exists")
	age(t, d, id, 20*time.Minute)
	d.pending.mu.Lock()
	d.pending.by[target.ID].entries[0].at = time.Now().Add(-20 * time.Minute)
	d.pending.mu.Unlock()

	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("typed mid-turn into a runner that does not take it: %q", f.written())
	}
	// The injector still holds the message, and the typist will not type it
	// mid-turn, so the hook is the route that carries it.
	msgs, _ := d.takeMessages(target.ID, "permission")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "waited 20 minutes") {
		t.Fatalf("the hook route did not deliver the aged message: %+v", msgs)
	}
}
