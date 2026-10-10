//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// THE WHOLE PROMISE, with a fake runner in place of the launch. A say to a done card is kept, a wake say resumes the
// card, and the text kept earlier is typed into the new session, with the wake text queued behind it, not typed.
func TestASayToADoneCardIsKeptThenTypedInWhenWakeResumesIt(t *testing.T) {
	oldTick, oldSettle := keptTick, keptSettle
	keptTick, keptSettle = 20*time.Millisecond, 300*time.Millisecond
	defer func() { keptTick, keptSettle = oldTick, oldSettle }()
	d := testDaemon(t)
	target := cardFor(t, d, "finished")
	peerCard(t, d, "alice")
	t.Cleanup(func() { d.pending.stopAll() })
	if err := d.st.SetResumeID(target.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(target.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	d.sup.remove(target.ID)

	// 1. Kept, with no session and no wake.
	out, code := tell(t, d, "alice", "finished", "KEPTEARLIER")
	if code != http.StatusOK || out["reachable"] != "kept" {
		t.Fatalf("first say: %d %v", code, out)
	}
	if n, _ := d.st.UndeliveredCount(target.ID); n != 1 {
		t.Fatalf("undelivered %d, want 1", n)
	}

	// 2. A wake resumes it. The fake launch stands up a runner as the real one would.
	var f *fakePTY
	launched := 0
	d.wakeLaunch = func(req LaunchRequest) (*store.Task, error) {
		launched++
		if req.TaskID != target.ID {
			t.Errorf("launched %q, want the done card", req.TaskID)
		}
		_, f = typedRunner(t, d, target.ID)
		return d.st.Get(target.ID)
	}
	raw, _ := json.Marshal(map[string]any{"from": "alice", "to": "finished", "text": "WAKETEXT", "wake": true})
	w := httptest.NewRecorder()
	d.handleTell(w, httptest.NewRequest("POST", "/tell", strings.NewReader(string(raw))))
	if w.Code != http.StatusOK {
		t.Fatalf("wake say answered %d: %s", w.Code, w.Body.String())
	}
	if launched != 1 {
		t.Fatalf("launched %d times, want 1", launched)
	}

	// 3. Not typed by the say itself, and not before the runner has said it started. Once it has, the kept text and
	// the wake text go in as ONE submitted turn, the kept text first.
	if strings.Contains(f.written(), "WAKETEXT") || strings.Contains(f.written(), "KEPTEARLIER") {
		t.Fatalf("typed straight into a session that had only just started: %q", f.written())
	}
	time.Sleep(3 * keptTick)
	if got := f.written(); strings.Contains(got, "WAKETEXT") || strings.Contains(got, "KEPTEARLIER") {
		t.Fatalf("typed before the session started: %q", got)
	}
	d.wakeSawSession(target.ID, "conv-1")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.written(), "\r") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := f.written()
	i, j := strings.Index(got, "KEPTEARLIER"), strings.Index(got, "WAKETEXT")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("want the kept text typed in, then the wake text: %q", got)
	}
	if n := strings.Count(got, "\r"); n != 1 {
		t.Fatalf("%d submits, want ONE turn for everything held: %q", n, got)
	}
	time.Sleep(3 * keptTick)
	if n, _ := d.st.UndeliveredCount(target.ID); n != 0 {
		t.Fatalf("undelivered %d after typing, want 0", n)
	}
	if got := f.written(); strings.Count(got, "\r") != 1 {
		t.Fatalf("typed again: %q", got)
	}
}

// A woken card whose runner never becomes ready is not left holding the words in silence: the launcher is told, and
// the rows stay pending.
func TestAWokenCardThatNeverTakesItsMessagesTellsTheLauncher(t *testing.T) {
	d := testDaemon(t)
	launcher, target := launchedPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	if err := d.st.SetResumeID(target.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(target.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	d.sup.remove(target.ID)
	if out, code := tell(t, d, "orchestrator", "worker", "HELDWORDS"); code != http.StatusOK || out["reachable"] != "kept" {
		t.Fatalf("keep: %d %v", code, out)
	}
	old := keptGiveUp
	keptGiveUp = 200 * time.Millisecond
	defer func() { keptGiveUp = old }()
	var f *fakePTY
	d.wakeLaunch = func(req LaunchRequest) (*store.Task, error) {
		_, f = typedRunner(t, d, target.ID)
		return d.st.Get(target.ID)
	}
	if err := d.wakeGone(target.ID, "say"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, still := d.kept.Load(target.ID); !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the delivery never gave up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(f.written(), "HELDWORDS") {
		t.Fatalf("typed into a runner that never started: %q", f.written())
	}
	if n, _ := d.st.UndeliveredCount(target.ID); n != 1 {
		t.Fatalf("undelivered %d, want the row kept", n)
	}
	told := pendingFrom(t, d, launcher.ID)
	if len(told) != 1 || !strings.Contains(told[0].Text, "did not take") {
		t.Fatalf("the launcher has %+v, want one notice that nothing was delivered", told)
	}
}
