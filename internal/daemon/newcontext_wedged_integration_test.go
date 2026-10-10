//go:build integration

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func dismissCycle(t *testing.T, d *Daemon, id string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleNewContext(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dismiss answered %d: %s", w.Code, w.Body.String())
	}
}

// The agent never acks, so the cycle sits on the limit step for good. Typing is not
// held meanwhile, and "let me type" ends the cycle, sends the queued text and leaves
// the card free.
func TestAWedgedWaitForTheAckIsNeverATrap(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	// Stuck: no ack comes and the run does not move. Given time, still not held.
	until(t, "the write to end", func() bool { return !d.nctx.typingHeld(id) })
	d.act.set(id, ActivityThinking, "")
	for i := 0; i < 5; i++ {
		time.Sleep(10 * time.Millisecond)
		if d.nctx.typingHeld(id) {
			t.Fatal("typing held while the cycle waits for an ack that never comes")
		}
	}
	sayViaMessage(t, d, "", id, "WEDGEDQUEUE")
	if strings.Contains(f.written(), "WEDGEDQUEUE") {
		t.Fatal("queued text typed before the cycle ended")
	}

	dismissCycle(t, d, id)
	if d.newContextFor(id) != nil || d.holdingMessages(id) || d.nctx.typingHeld(id) {
		t.Fatalf("the cycle is still there after 'let me type': %v", d.newContextFor(id))
	}
	d.act.set(id, ActivityIdle, "")
	until(t, "the queued text", func() bool { return strings.Contains(f.written(), "WEDGEDQUEUE") })
}

// The room's cycle state is stuck mid-type: the flag is on and nothing will ever
// clear it, as if the writer were wedged. The board's key is refused, then "let me
// type" frees it at once, queued text goes, and the terminal takes keys again.
func TestAWedgedMidTypeCycleIsFreedByLetMeType(t *testing.T) {
	d := testDaemon(t)
	f, _ := sizedSession(t, d, "nc-wedge", 120, 30)

	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/tasks/nc-wedge/attach", nil)
	if err != nil {
		t.Fatalf("could not attach: %v", err)
	}
	defer c.CloseNow()
	frames := make(chan string, 16)
	go func() {
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m attachInDone
			if typ == websocket.MessageText && json.Unmarshal(data, &m) == nil &&
				(m.T == "in-done" || m.T == "in-refused") {
				frames <- m.T + ":" + m.ID
			}
		}
	}()
	send := func(in attachIn) {
		t.Helper()
		raw, _ := json.Marshal(in)
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatalf("could not send: %v", err)
		}
	}
	send(attachIn{T: "resize", Cols: 120, Rows: 30})

	gen, ok := d.nctx.begin("nc-wedge", "HANDOFF.x.md", "")
	if !ok {
		t.Fatal("could not begin")
	}
	d.nctx.setTyping("nc-wedge", gen, true) // and never off: no goroutine will ever clear it
	send(attachIn{T: "in", D: "stuck-bytes", ID: "w1"})
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case fr := <-frames:
			seen[fr] = true
		case <-ctx.Done():
			t.Fatalf("want in-done and in-refused while wedged, got %v", seen)
		}
	}
	if !seen["in-refused:"] || strings.Contains(f.written(), "stuck-bytes") {
		t.Fatalf("a wedged write did not refuse the key: %v %q", seen, f.written())
	}

	// What the DELETE handler does (this session has no card for the handler to find).
	d.nctx.clear("nc-wedge")
	d.releaseHeld("nc-wedge")
	if d.nctx.typingHeld("nc-wedge") || d.holdingMessages("nc-wedge") {
		t.Fatal("still held after 'let me type'")
	}
	send(attachIn{T: "in", D: "free-bytes", ID: "w2"})
	select {
	case fr := <-frames:
		if fr != "in-done:w2" {
			t.Fatalf("want in-done:w2 once freed, got %q", fr)
		}
	case <-ctx.Done():
		t.Fatal("no ack once freed")
	}
	if !strings.Contains(f.written(), "free-bytes") {
		t.Fatalf("the terminal did not take keys once freed: %q", f.written())
	}
}
