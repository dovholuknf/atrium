package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A message posted to /v1/tasks/{id}/message carries its sender, so a peer
// message queued through the hub's atrium_say is attributed the same way a
// peer message told over the bus is, and a message with no sender stays the
// operator's own channel rather than a broken attribution.

func message(t *testing.T, d *Daemon, taskID string, fields map[string]string) int {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/tasks/"+taskID+"/message", bytes.NewReader(raw))
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	d.handleMessage(rec, req)
	return rec.Code
}

func TestMessageCarriesTheCaller(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")

	if code := message(t, d, bob.ID, map[string]string{
		"text": "have you got the lock", "from": "alice",
	}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}

	pending, err := d.st.PendingMessages(bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d messages arrived", len(pending))
	}
	if pending[0].FromHuman() {
		t.Fatal("a message with a sender is attributed to the human")
	}
	if pending[0].FromPeer != "alice" {
		t.Fatalf("the sender is %q, want alice", pending[0].FromPeer)
	}
}

// atrium_say reaches a session through this endpoint, so a message with a
// sender gets the peer bus's bounds: the size cap and the per-sender rate.
func TestAMessageFromASessionIsBoundedLikeTheBus(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")

	long := string(bytes.Repeat([]byte("x"), maxPeerMessage+1))
	if code := message(t, d, bob.ID, map[string]string{"text": long, "from": "alice"}); code !=
		http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized peer message answered %d, want 413", code)
	}

	for i := 0; i < peerSendsPerMinute; i++ {
		if code := message(t, d, bob.ID, map[string]string{"text": "ping", "from": "alice"}); code !=
			http.StatusOK {
			t.Fatalf("send %d answered %d before the limit", i+1, code)
		}
	}
	if code := message(t, d, bob.ID, map[string]string{"text": "ping", "from": "alice"}); code !=
		http.StatusTooManyRequests {
		t.Fatalf("a send past the limit answered %d, want 429", code)
	}
}

// The operator is not a peer, so neither bound applies to a message with no
// sender.
func TestTheOperatorsMessagesAreNotRateLimited(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")

	for i := 0; i <= peerSendsPerMinute; i++ {
		if code := message(t, d, bob.ID, map[string]string{"text": "ping"}); code != http.StatusOK {
			t.Fatalf("operator send %d answered %d", i+1, code)
		}
	}
}

func TestMessageWithNoCallerStaysTheOperator(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")

	// No `from`, as a human or a non-atrium caller sends. It must deliver and
	// carry no attribution rather than fail the send or render a broken one.
	if code := message(t, d, bob.ID, map[string]string{"text": "your turn"}); code != http.StatusOK {
		t.Fatalf("message answered %d, want it delivered without a sender", code)
	}

	pending, err := d.st.PendingMessages(bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d messages arrived", len(pending))
	}
	if !pending[0].FromHuman() {
		t.Fatalf("a message with no sender should read as the operator's, got from %q", pending[0].FromPeer)
	}
}
