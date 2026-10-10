//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A LENT CARD BY ITS NAME, and no other (u-new-card-urls R4). The guest listener
// serves the card's readable page and the page's lookup only when the name is
// this card's. Another card's alias and a name that exists nowhere are the same
// 403, so a guest cannot ask which names exist here.
func TestAGuestReachesItsCardByNameAndNoOther(t *testing.T) {
	d := testDaemon(t)
	d.opts.Room = "claude-sg4"
	mk := func(name, alias string) *store.Task {
		t.Helper()
		task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: t.TempDir(), Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.st.SetAlias(task.ID, alias); err != nil {
			t.Fatal(err)
		}
		return task
	}
	lent := mk("ui-director", "ui")
	mk("rnd-director", "rnd")
	guest := d.guestHandler(lent.ID)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		guest.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	for _, path := range []string{"/alias/ui", "/alias/@UI", "/room/claude-sg4/ui-director", "/room/claude-sg4/ui"} {
		if rec := get(path); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<") {
			t.Errorf("%s answered %d, want the page", path, rec.Code)
		}
	}
	for _, path := range []string{"/v1/tasks/ui", "/v1/tasks/ui@claude-sg4", "/v1/tasks/ui-director", "/v1/tasks/" + lent.ID} {
		rec := get(path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), lent.ID) {
			t.Errorf("%s answered %d %q, want this card", path, rec.Code, rec.Body.String())
		}
	}
	refused := get("/alias/rnd").Body.String()
	for _, path := range []string{"/alias/rnd", "/alias/nobody", "/room/claude-sg4/rnd", "/room/other/ui",
		"/v1/tasks/rnd", "/v1/tasks/nobody", "/v1/tasks/ui@other"} {
		rec := get(path)
		if rec.Code != http.StatusForbidden || rec.Body.String() != refused {
			t.Errorf("%s answered %d %q, want the one 403", path, rec.Code, rec.Body.String())
		}
	}
	// Renamed on the board: the alias goes, the handle the guest was given stays.
	if err := d.st.SetAlias(lent.ID, "ux"); err != nil {
		t.Fatal(err)
	}
	if rec := get("/alias/ui"); rec.Code != http.StatusForbidden {
		t.Errorf("the old alias still opens the lent card: %d", rec.Code)
	}
	if rec := get("/room/claude-sg4/ui-director"); rec.Code != http.StatusOK {
		t.Errorf("the handle stopped opening the lent card after a rename: %d", rec.Code)
	}
	// And the address handed out is the handle.
	if got := d.guestPath(lent.ID); got != "/room/claude-sg4/ui-director" {
		t.Errorf("the lent address is %q, want the handle", got)
	}
	d.opts.Room = ""
	if got := d.guestPath(lent.ID); got != "/#term="+lent.ID {
		t.Errorf("with no room name the lent address is %q, want #term=", got)
	}
}
