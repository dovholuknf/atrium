//go:build integration

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

func reattachedCard(t *testing.T, d *Daemon, kind, status, frame string, quiet time.Duration) (*store.Task, *runner) {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: "idle-" + kind, Worktree: "/tmp/atrium-test-" + kind, Runner: kind})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, status); err != nil {
		t.Fatal(err)
	}
	r := &runner{
		taskID: task.ID, pty: newFakePTY(), started: time.Now().Add(-time.Hour),
		buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}, done: make(chan struct{}),
	}
	_, _ = r.buf.Write([]byte(frame))
	r.lastOut.Store(time.Now().Add(-quiet).UnixNano())
	r.reattached.Store(true)
	r.line.lost(reattachLineKey)
	d.sup.add(r)
	return task, r
}

func TestAReattachedIdleWorkerInAnyColumnOpensItsGateAfterAFewSeconds(t *testing.T) {
	t.Parallel()
	for _, status := range []string{store.StatusRunning, store.StatusNeedsInput, store.StatusDone} {
		t.Run(status, func(t *testing.T) {
			d := testDaemon(t)
			_, r := reattachedCard(t, d, "claude", status, idleFrame, 6*time.Second)
			if err := d.watchLooksIdle(time.Now()); err != nil {
				t.Fatal(err)
			}
			if !r.peerGateOpen() {
				t.Fatalf("a reattached claude at an empty prompt in %s kept its gate shut", status)
			}
		})
	}
}

func TestAReattachedClaudeStillDrawingItsFrameKeepsTheGateShut(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, r := reattachedCard(t, d, "claude", store.StatusNeedsInput, idleFrame, time.Second)
	_ = d.watchLooksIdle(time.Now())
	if r.peerGateOpen() {
		t.Fatal("the gate opened on a runner that had written a second ago")
	}
	_, w := reattachedCard(t, d, "claude", store.StatusNeedsInput, workingFrame, time.Minute)
	_ = d.watchLooksIdle(time.Now())
	if w.peerGateOpen() {
		t.Fatal("the gate opened on a working frame")
	}
}

const codexIdleFrame = "\x1b[H\x1b[2J  OpenAI Codex\r\n\r\n> done: the change is in\r\n\r\n› \r\n\r\n  ? for shortcuts\r\n"
const codexWorkingFrame = "\x1b[H\x1b[2J• Working (14s • esc to interrupt)\r\n\r\n› \r\n\r\n  ? for shortcuts\r\n"

func TestAReattachedCodexThatHasBeenQuietOpensItsGateAndOneWorkingDoesNot(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, idle := reattachedCard(t, d, "codex", store.StatusNeedsInput, codexIdleFrame, time.Minute)
	if err := d.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !idle.peerGateOpen() {
		t.Fatal("a quiet reattached codex kept its gate shut")
	}
	d2 := testDaemon(t)
	_, busy := reattachedCard(t, d2, "codex", store.StatusRunning, codexWorkingFrame, time.Minute)
	_ = d2.watchLooksIdle(time.Now())
	if busy.peerGateOpen() {
		t.Fatal("the gate opened under an interrupt hint")
	}
	d3 := testDaemon(t)
	_, young := reattachedCard(t, d3, "codex", store.StatusNeedsInput, codexIdleFrame, 5*time.Second)
	_ = d3.watchLooksIdle(time.Now())
	if young.peerGateOpen() {
		t.Fatal("a codex quiet for five seconds was taken as idle")
	}
}

func TestAKeyTypedAfterTheReattachKeepsTheGateShut(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	_, r := reattachedCard(t, d, "claude", store.StatusNeedsInput, idleFrame, time.Minute)
	r.noteOperatorTyped([]byte("ab"))
	_ = d.watchLooksIdle(time.Now())
	r.typeMu.Lock()
	open, _ := r.gateLocked()
	r.typeMu.Unlock()
	if open {
		t.Fatal("the gate opened over a line the operator had started")
	}
}

func TestASayHeldPastFiveMinutesIsFlaggedOnTheCardAndTheSenderToldOnce(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	d.hk.stuckAfter = 80 * time.Millisecond
	alice := peerCard(t, d, "alice")
	bob := peerCard(t, d, "bob")
	r := &runner{
		taskID: bob.ID, pty: newFakePTY(), started: time.Now(),
		buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}, done: make(chan struct{}),
	}
	r.line.lost("something the gate cannot read")
	d.sup.add(r)

	if code := message(t, d, bob.ID, map[string]string{"text": "are you there", "from": "alice"}); code != http.StatusOK {
		t.Fatalf("the say answered %d", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a := d.act.get(bob.ID); a != nil && a.HeldStuck {
			break
		}
		time.Sleep(20 * time.Millisecond) // wait: poll step under the 5 s deadline
	}
	if a := d.act.get(bob.ID); a == nil || !a.HeldStuck {
		t.Fatalf("the card was not flagged: %+v", a)
	}
	time.Sleep(400 * time.Millisecond) // wait: absence of a second sender notice has no event to wait on
	told := 0
	pending, _ := d.st.PendingMessages(alice.ID)
	for _, m := range pending {
		if strings.Contains(m.Text, "has not been typed") {
			told++
		}
	}
	if told != 1 {
		t.Fatalf("the sender was told %d times, wanted once: %+v", told, pending)
	}
}

func TestAForcedSayToAnotherRoomCarriesTheForce(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	peerCard(t, d, "boss")
	rl := &fakeRelay{}
	d.SetRelay(rl)
	raw, _ := json.Marshal(map[string]any{"from": "boss", "to": "worker@sg4", "text": "carry on", "force": true})
	w := httptest.NewRecorder()
	d.HandleSay(w, httptest.NewRequest(http.MethodPost, "/v1/say", bytes.NewReader(raw)))
	if w.Code != http.StatusOK {
		t.Fatalf("the say answered %d: %s", w.Code, w.Body.String())
	}
	got := rl.says()
	if len(got) != 1 || !got[0].Force {
		t.Fatalf("the hub was handed %+v", got)
	}
}
