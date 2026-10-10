//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A room's git surface (/v1/git/) is mounted on the link handler only, in internal/cli's
// roomHandler, which is what the hub's own sync and collect reach. It is not on the room's
// human listener, not on a lent session's guest listener, and not on the agent listener.
func TestTheRoomsGitSurfaceIsNotOnTheHumanGuestOrAgentListeners(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := sharedCard(t, d, "lent")

	paths := []struct{ method, path string }{
		{"GET", "/v1/git/status"},
		{"GET", "/v1/git/github/o/r.git/info/refs?service=git-upload-pack"},
		{"POST", "/v1/git/github/o/r.git/git-upload-pack"},
		{"POST", "/v1/git/sync"},
	}
	board := d.BoardHandler()
	guest := d.guestHandler(task.ID)
	for _, p := range paths {
		serve := func(h http.Handler) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(p.method, p.path, strings.NewReader(`{"name":"github/o/r","init":true}`)))
			return rec
		}
		if rec := serve(board); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s answered %d on the human board, want 404", p.method, p.path, rec.Code)
		}
		if rec := serve(guest); rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d to a guest, want 403 or 404", p.method, p.path, rec.Code)
		}
		req, _ := http.NewRequest(p.method, addressOf(d.opts.AgentAddr)+p.path, strings.NewReader(`{}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("agent listener: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s answered %d on the agent listener, want 404", p.method, p.path, resp.StatusCode)
		}
	}
	// And the human listener itself, over a socket.
	resp, err := http.Get(addressOf(d.opts.HumanAddr) + "/v1/git/status")
	if err != nil {
		t.Fatalf("human listener: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("the human listener answered %d for /v1/git/status, want 404", resp.StatusCode)
	}
}
