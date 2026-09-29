package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// WHERE THE ROUTES LIVE. `atrium merged`, the hold and archive-workers remove or
// keep directories and cards, so they are on the human's board handler and
// nowhere else: absent from the agent listener, which any session can reach, and
// refused to a lent-session guest.
func TestTheMergedCullRoutesAreOnTheBoardAndNowhereElse(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := sharedCard(t, d, "lent")

	paths := []string{
		"/v1/merged",
		"/v1/tasks/" + task.ID + "/cull/hold",
		"/v1/tasks/archive-workers",
	}
	board := d.BoardHandler()
	guest := d.guestHandler(task.ID)
	for _, p := range paths {
		post := func(h http.Handler) int {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)))
			return rec.Code
		}
		if code := post(board); code == http.StatusNotFound || code == http.StatusForbidden ||
			code == http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d on the board, want a real answer", p, code)
		}
		if code := post(guest); code != http.StatusForbidden {
			t.Errorf("%s answered %d to a guest, want 403", p, code)
		}
		resp, err := http.Post(addressOf(d.opts.AgentAddr)+p, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("agent listener: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d on the agent listener, want 404", p, resp.StatusCode)
		}
	}
}
