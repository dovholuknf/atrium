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

// sayWhen posts a session's say with a `when`.
func sayWhen(t *testing.T, d *Daemon, from, id, text, when string) map[string]any {
	t.Helper()
	body := `{"from":"` + from + `","text":"` + text + `","when":"` + when + `"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/message", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleMessage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("message answered %d: %s", w.Code, w.Body.String())
	}
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// Says queued BEFORE the cycle began, while the card was mid-turn, arrive once each after the wake prompt.
func TestSaysQueuedBeforeACycleArriveOnceAfterTheWake(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	withoutQueueBox(t, d)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	// Mid-turn, as the orchestrator found the card.
	d.act.set(id, ActivityThinking, "")
	if err := d.st.SetStatus(id, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	sayWhen(t, d, "alice", id, "SAYDONEONE", "done")
	sayWhen(t, d, "alice", id, "SAYDONETWO", "done")
	sayWhen(t, d, "alice", id, "SAYNOW", "immediate")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	ncAck(t, d, task)
	time.Sleep(60 * time.Millisecond) // wait: lets the ack's handoff run before the turn's Stop, no event marks it
	// The Stop of the turn that ran the handoff.
	if msgs, _ := d.takeMessages(id, "stop"); len(msgs) != 0 {
		t.Fatalf("the stop hook carried %d message(s) into the context about to be cleared", len(msgs))
	}
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.Wake.SawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the chip to go", func() bool { return d.NewContextFor(id) == nil })
	// The wake's own turn runs and ends.
	d.act.set(id, ActivityThinking, "")
	time.Sleep(30 * time.Millisecond) // wait: the wake's turn runs a while, no event marks it
	d.act.set(id, ActivityIdle, "")
	ncTurnEnds(d, id)

	for _, s := range []string{"SAYDONEONE", "SAYDONETWO", "SAYNOW"} {
		s := s
		until(t, s, func() bool { return strings.Contains(f.written(), s) })
	}
	// Typed is not recorded: the injector marks each say delivered after it types it.
	until(t, "every say to be marked delivered", func() bool { return len(pendingFrom(t, d, id)) == 0 })
	time.Sleep(100 * time.Millisecond) // wait: a negative check, no say may be typed twice
	got := f.written()
	wake := strings.Index(got, ncWakeMark)
	for _, s := range []string{"SAYDONEONE", "SAYDONETWO", "SAYNOW"} {
		// A say typed into the old context is repeated after the wake, once.
		if n := strings.Count(got[wake:], s); n != 1 {
			t.Fatalf("%s arrived %d times after the wake: %q", s, n, got)
		}
	}
	if n := len(pendingFrom(t, d, id)); n != 0 {
		t.Fatalf("%d messages still queued", n)
	}
	_ = dir
}
