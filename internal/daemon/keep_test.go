package daemon

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A tell to a done card is kept on it, with wake or without, when there is no conversation to resume.
func TestTellToADoneCardIsKept(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	if err := d.st.SetStatus(target.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	d.sup.remove(target.ID)
	out, code := tell(t, d, "orchestrator", strings.TrimPrefix(target.WireName, ""), "are you there")
	if code != http.StatusOK || out["queued"] != true || out["reachable"] != "kept" {
		t.Fatalf("tell to a done card: %d %v", code, out)
	}
	if n := len(pendingFrom(t, d, target.ID)); n != 1 {
		t.Fatalf("%d kept, want 1", n)
	}
	if n, _ := d.st.UndeliveredCount(target.ID); n != 1 {
		t.Fatalf("undelivered %d, want 1", n)
	}
	// A wake with nothing to resume keeps it too, rather than refusing.
	out, code = tell(t, d, "orchestrator", target.WireName, "and again")
	if code != http.StatusOK || out["reachable"] != "kept" {
		t.Fatalf("second tell: %d %v", code, out)
	}
	if n := len(pendingFrom(t, d, target.ID)); n != 2 {
		t.Fatalf("%d kept, want 2", n)
	}
}

// A kept message is a row, so it survives the daemon: the store, not memory, holds it.
func TestAKeptMessageIsADurableRow(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "sleeper", store.StatusRunning)
	peerCard(t, d, "alice")
	sayViaMessage(t, d, "alice", card.ID, "remember this")
	msgs, err := d.st.PendingMessages(card.ID)
	if err != nil || len(msgs) != 1 || msgs[0].Text != "remember this" || msgs[0].FromPeer == "" {
		t.Fatalf("pending %v %v", msgs, err)
	}
	if msgs[0].DeliveredAt != nil {
		t.Fatal("marked delivered before anyone read it")
	}
}

// A relayed say that is given up on lands on the sender's card as a message with its words, not only an event.
func TestAGivenUpRelayComesBackAsAMessage(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	d.giveUpRelay(store.RelayRow{ID: "r1", FromTask: target.ID, FromWire: target.WireName,
		ToRoom: "far", ToName: "x", Text: "important words"}, "it was held too long")
	msgs, err := d.st.PendingMessages(target.ID)
	if err != nil || len(msgs) != 1 || !strings.Contains(msgs[0].Text, "important words") {
		t.Fatalf("pending %v %v", msgs, err)
	}
}
