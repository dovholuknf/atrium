package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A PHONE VIEW SENDS NO RESIZE (t-003b). It never enters the viewers, never moves
// the pty, and still hears the pty's size, at attach and when it changes.

type sizeWatcher struct {
	mu    sync.Mutex
	sizes []attachSize
}

func (s *sizeWatcher) last() (attachSize, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sizes) == 0 {
		return attachSize{}, false
	}
	return s.sizes[len(s.sizes)-1], true
}

func attachWithoutResize(t *testing.T, d *Daemon, taskID string) *sizeWatcher {
	t.Helper()
	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/tasks/" + taskID + "/attach"
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("could not attach: %v", err)
	}
	c.SetReadLimit(64 << 20)
	t.Cleanup(func() { c.CloseNow() })
	w := &sizeWatcher{}
	go func() {
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var m attachSize
			if json.Unmarshal(b, &m) == nil && m.T == "size" {
				w.mu.Lock()
				w.sizes = append(w.sizes, m)
				w.mu.Unlock()
			}
		}
	}()
	return w
}

func waitSize(t *testing.T, w *sizeWatcher, cols, rows int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if s, ok := w.last(); ok && s.Cols == cols && s.Rows == rows {
			return
		}
		if time.Now().After(deadline) {
			s, _ := w.last()
			t.Fatalf("want a size frame %dx%d, last was %+v", cols, rows, s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAPhoneAttachSendsNoResizeAndStillHearsTheSize(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "phone-view", 120, 30)

	w := attachWithoutResize(t, d, "phone-view")
	waitSize(t, w, 120, 30)
	if n := resizesAfterAMoment(f); n != 0 {
		t.Fatalf("a phone attach resized the pty: %+v", f.resized())
	}
	r.mu.Lock()
	views := len(r.views)
	r.mu.Unlock()
	if views != 0 {
		t.Fatalf("a phone attach became a viewer: %d", views)
	}

	holdAttach(t, d, "phone-view", 150, 40)
	ptyIs(t, f, r, viewport{150, 40})
	waitSize(t, w, 150, 40)
}
