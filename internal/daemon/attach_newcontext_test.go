package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Typing from a board while a new context runs is dropped, acked and refused out
// loud. Once the cycle ends the same typing reaches the pty.
func TestTypingDuringANewContextIsRefusedAndAcked(t *testing.T) {
	d := testDaemon(t)
	f, _ := sizedSession(t, d, "nc-input", 120, 30)

	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/tasks/nc-input/attach", nil)
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

	gen, ok := d.nctx.begin("nc-input", "HANDOFF.x.md", "")
	if !ok {
		t.Fatal("could not begin a new context")
	}
	send(attachIn{T: "in", D: "held-bytes", ID: "p1"})
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case fr := <-frames:
			seen[fr] = true
		case <-ctx.Done():
			t.Fatalf("want an in-done and an in-refused, got %v", seen)
		}
	}
	if !seen["in-done:p1"] || !seen["in-refused:"] {
		t.Fatalf("want in-done:p1 and in-refused:, got %v", seen)
	}
	if w := f.written(); strings.Contains(w, "held-bytes") {
		t.Fatalf("typing reached the pty during a new context: %q", w)
	}

	_ = gen
	d.nctx.clear("nc-input")
	send(attachIn{T: "in", D: "free-bytes", ID: "p2"})
	select {
	case fr := <-frames:
		if fr != "in-done:p2" {
			t.Fatalf("want in-done:p2 after the cycle, got %q", fr)
		}
	case <-ctx.Done():
		t.Fatal("no ack after the cycle ended")
	}
	if w := f.written(); !strings.Contains(w, "free-bytes") {
		t.Fatalf("typing did not reach the pty after the cycle: %q", w)
	}
}
