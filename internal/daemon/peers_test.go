package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Sessions addressing each other.
//
// The load-bearing test in here is the last one: a peer message must be
// QUEUED, never typed into the terminal, even when atrium owns one and could.

func tell(t *testing.T, d *Daemon, from, to, text string) (map[string]any, int) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"from": from, "to": to, "text": text})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/tell", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	d.handleTell(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out, w.Code
}

func peerCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/" + name, Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestOneSessionCanTellAnother(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	bob := peerCard(t, d, "bob")

	out, code := tell(t, d, "alice", "bob", "have you got the lock on the db")
	if code != http.StatusOK {
		t.Fatalf("telling answered %d: %v", code, out)
	}

	pending, err := d.st.PendingMessages(bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d messages arrived", len(pending))
	}
	if pending[0].FromHuman() {
		t.Fatal("a peer message is attributed to the human")
	}
	if !strings.Contains(pending[0].FromPeer, "alice") {
		t.Fatalf("the sender is %q", pending[0].FromPeer)
	}
}

// THE one. `docs/supervision-design.md` settled that atrium does not type into
// a session as though the human had, and Charon's peer bus does exactly that.
// The temptation is to reuse handleMessage, which types when atrium owns the
// terminal. Doing so would reintroduce the injection this refuses.
func TestAPeerMessageIsQueuedEvenWhenAtriumOwnsTheTerminal(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	bob := peerCard(t, d, "bob")

	// A supervised runner, so the terminal branch exists and would be taken by
	// anything that reused the human path.
	fake := &runner{}
	d.sup.mu.Lock()
	d.sup.runners[bob.ID] = fake
	d.sup.mu.Unlock()
	if d.sup.get(bob.ID) == nil {
		t.Fatal("this test is not exercising a supervised card")
	}

	if _, code := tell(t, d, "alice", "bob", "do not type this at me"); code != http.StatusOK {
		t.Fatalf("telling a supervised session answered %d", code)
	}

	// Queued, not typed.
	pending, err := d.st.PendingMessages(bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("a peer message to a supervised session queued %d messages, "+
			"which means it was typed instead", len(pending))
	}
}

// A model messaging itself is a loop with extra steps.
func TestASessionCannotTellItself(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	if _, code := tell(t, d, "alice", "alice", "hello me"); code != http.StatusBadRequest {
		t.Fatalf("a session told itself, answering %d", code)
	}
}

// A handle that does not resolve answers with the ones that would have, which
// is what turns a guess into discovery. A bare subcommand cannot make listing
// mandatory the way a tool description can, so the failure has to teach.
func TestAnUnknownHandleAnswersWithTheList(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	peerCard(t, d, "bob")

	out, code := tell(t, d, "alice", "carol", "hello")
	if code != http.StatusNotFound {
		t.Fatalf("an unknown handle answered %d", code)
	}
	peers, ok := out["peers"].([]any)
	if !ok || len(peers) == 0 {
		t.Fatalf("a failed send did not offer the list: %v", out)
	}
	found := false
	for _, p := range peers {
		if m, ok := p.(map[string]any); ok && m["handle"] == d.st.Qualify("bob") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the offered list does not contain the session that exists: %v", out["peers"])
	}
}

// A session that has ended cannot read anything, and saying so beats queueing
// a message nobody will see.
func TestTellingAnEndedSessionIsRefused(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	bob := peerCard(t, d, "bob")
	if err := d.st.SetStatus(bob.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}

	if _, code := tell(t, d, "alice", "bob", "hello"); code != http.StatusConflict {
		t.Fatalf("telling a dead session answered %d", code)
	}
}

// A model in a loop is the failure the limit exists for. Without one, a
// confused session fills every other queue faster than any of them drains.
func TestASessionCannotFloodAnother(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	peerCard(t, d, "bob")

	for i := 0; i < peerSendsPerMinute; i++ {
		if _, code := tell(t, d, "alice", "bob", "spam"); code != http.StatusOK {
			t.Fatalf("send %d was refused early with %d", i+1, code)
		}
	}
	if _, code := tell(t, d, "alice", "bob", "one too many"); code != http.StatusTooManyRequests {
		t.Fatalf("the limit did not hold: answered %d", code)
	}
}

// And the window moves, or one busy minute silences a session forever.
func TestTheLimitIsAWindowAndNotACeiling(t *testing.T) {
	l := newPeerLimiter()
	at := time.Now()
	l.now = func() time.Time { return at }

	for i := 0; i < peerSendsPerMinute; i++ {
		if !l.allow("alice") {
			t.Fatalf("refused at %d, under the limit", i+1)
		}
	}
	if l.allow("alice") {
		t.Fatal("the limit did not hold")
	}

	at = at.Add(peerWindow + time.Second)
	if !l.allow("alice") {
		t.Fatal("the window never reopened, so one busy minute is permanent")
	}
}

// One sender's flood does not silence another.
func TestTheLimitIsPerSender(t *testing.T) {
	l := newPeerLimiter()
	for i := 0; i < peerSendsPerMinute; i++ {
		l.allow("alice")
	}
	if !l.allow("bob") {
		t.Fatal("one session hitting the limit stopped a different one")
	}
}

// A document is not a message. Handing over more than this means writing a
// file and saying where it is.
func TestAnEnormousMessageIsRefused(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	peerCard(t, d, "bob")

	huge := strings.Repeat("x", maxPeerMessage+1)
	if _, code := tell(t, d, "alice", "bob", huge); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized message answered %d", code)
	}
}

// Listing leaves the asker out of its own list, and leaves out anything that
// cannot be reached.
func TestListingIsWhatCanActuallyBeReached(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	peerCard(t, d, "bob")
	gone := peerCard(t, d, "gone")
	if err := d.st.SetStatus(gone.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	// A card with no wire name has no handle to address.
	if _, _, err := d.st.Offer(store.IntakeItem{
		Source: "github", ExternalID: "1", Title: "offered",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := d.peers("alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if strings.Contains(p.Handle, "alice") {
			t.Fatal("a session was offered itself")
		}
		if strings.Contains(p.Handle, "gone") {
			t.Fatal("a dead session was offered as reachable")
		}
	}
	if len(list) != 1 {
		t.Fatalf("offered %d peers, wanted only bob: %+v", len(list), list)
	}
}

// How much is already queued for a peer, so a model can tell that somebody is
// buried before adding to it.
func TestListingSaysHowMuchIsAlreadyWaiting(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	peerCard(t, d, "bob")

	tell(t, d, "alice", "bob", "one")
	tell(t, d, "alice", "bob", "two")

	list, err := d.peers("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Waiting != 2 {
		t.Fatalf("the list does not say what is waiting: %+v", list)
	}
}
