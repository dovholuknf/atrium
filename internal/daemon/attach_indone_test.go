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

// An `in` frame with an id is answered with one `in-done` after its bytes are in
// the pty, and one without is answered with nothing. See attachInDone.
func TestAPasteWithAnIDIsAnsweredOnceItsWriteReturns(t *testing.T) {
	d := testDaemon(t)
	f, _ := sizedSession(t, d, "in-done", 120, 30)

	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/tasks/in-done/attach", nil)
	if err != nil {
		t.Fatalf("could not attach: %v", err)
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 20)

	// Each in-done, with how much the pty held when it arrived.
	type done struct {
		id      string
		written int
	}
	dones := make(chan done, 8)
	go func() {
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m attachInDone
			if typ == websocket.MessageText && json.Unmarshal(data, &m) == nil && m.T == "in-done" {
				dones <- done{m.ID, len(f.written())}
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

	paste := strings.Repeat("x", 1<<20)
	send(attachIn{T: "in", D: paste, ID: "p1"})
	send(attachIn{T: "in", D: "no id"})
	send(attachIn{T: "in", D: "y", ID: "p2"})

	var got []done
	for len(got) < 2 {
		select {
		case d := <-dones:
			got = append(got, d)
		case <-ctx.Done():
			t.Fatalf("got %d in-done frames, want 2: %+v", len(got), got)
		}
	}
	if got[0].id != "p1" || got[1].id != "p2" {
		t.Fatalf("want in-done for p1 then p2, got %+v", got)
	}
	if got[0].written < len(paste) {
		t.Fatalf("in-done for the paste came when the pty held %d of its %d bytes", got[0].written, len(paste))
	}
	// Nothing more: the frame with no id was answered with nothing, and each id once.
	select {
	case d := <-dones:
		t.Fatalf("an extra in-done arrived: %+v", d)
	case <-time.After(300 * time.Millisecond):
	}
	if w := f.written(); !strings.Contains(w, paste+"no id") {
		t.Fatalf("the paste did not reach the pty whole and in order: %d bytes", len(w))
	}
}
