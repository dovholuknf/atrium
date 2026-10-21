//go:build integration

package daemon

import (
	"context"
	"fmt"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func attachKeptOpen(t *testing.T, wsURL string, cols, rows int) *keptOpenViewer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	v := &keptOpenViewer{c: c, cancel: cancel}
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			v.mu.Lock()
			v.buf.Write(data)
			v.mu.Unlock()
		}
	}()
	v.resize(t, cols, rows)
	t.Cleanup(func() { cancel(); c.CloseNow() })
	return v
}

// The move `textWithCursor` appends as the last of the replay, ahead of any later size frame.
func hasCursorMove(s string) bool {
	return regexp.MustCompile(`\x1b\[\d+;\d+H(\{"t":"size"[^}]*\})*$`).MatchString(s)
}

func sizeFrame(cols, rows int) string {
	return fmt.Sprintf(`{"t":"size","cols":%d,"rows":%d}`, cols, rows)
}

func TestALiveRefitRestoresTheCursorOnlyWhenThePtyMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	// A parked cursor, not at the bottom: row 2 col 6 of three drawn rows.
	f := narrowSession(t, d, "refit", "line one\r\nline two\r\nline three\r\n\x1b[2;6H")

	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/tasks/refit/attach"

	small := attachKeptOpen(t, wsURL, 140, 20)
	// The height waits out the hold (item 74): it must land before the large replay or it buries the cursor.
	hostWait(t, 10*time.Second, "the pty to take the small viewer's size", func() bool {
		s := f.resized()
		return len(s) > 0 && s[len(s)-1] == (viewport{140, 20})
	})

	large := attachKeptOpen(t, wsURL, 200, 30)
	hostWait(t, 10*time.Second, "both viewers to be told 200x20", func() bool {
		return strings.Contains(large.seen(), sizeFrame(200, 20)) && strings.Contains(small.seen(), sizeFrame(200, 20))
	})
	if !hasCursorMove(large.seen()) {
		t.Fatalf("the initial attach did not restore the cursor: %q", large.seen())
	}
	before := len(small.seen())
	ptyMovesBefore := len(f.resized())

	// Non-binding: 200 is still the widest, so the pty does not move and nothing is sent.
	small.resize(t, 130, 20)

	// Binding: the large viewer is the widest, so the pty moves and every viewer is told.
	large.resize(t, 180, 28)
	hostWait(t, 10*time.Second, "the pty to take the binding re-fit", func() bool {
		sizes := f.resized()
		return len(sizes) > 0 && sizes[len(sizes)-1] == (viewport{180, 20}) &&
			strings.Contains(small.seen()[before:], sizeFrame(180, 20))
	})
	if moves := len(f.resized()); moves != ptyMovesBefore+1 {
		t.Fatalf("a non-binding re-fit moved the pty: %+v", f.resized())
	}
	if delta := small.seen()[before:]; delta != sizeFrame(180, 20) {
		t.Fatalf("the narrower viewer got more than the new size: %q", delta)
	}
}
