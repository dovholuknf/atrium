package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// WHAT A LENT SESSION HANDS OVER, asserted rather than read.
//
// The guest handler is an allowlist, and the reason it is written as one is
// that the interesting failure is a route added later that nobody thought
// about. These tests are the other half of that: they name the things a guest
// must not reach, so adding a route does not quietly widen a share.

// The wire name is the dedup key, so two cards need two names. Passing the
// same one twice returns the same card, which is the store working correctly
// and made an earlier version of the last test below pass for the wrong
// reason: it was comparing a card against itself.
func sharedCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: t.TempDir(), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func guestGet(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// A SHARE IS THE AGENT'S TERMINAL, NEVER A COMMAND LINE ON THIS MACHINE.
//
// A card can hold two terminals, and they are told apart by a query parameter
// on the same path the share already allows. Without an explicit refusal, a
// WRITABLE guest could append `?kind=shell` to the one route they are given
// and reach a shell in the card's working directory. That is not lending a
// session, it is lending the machine, and it is the line `docs/overlays.md`
// says atrium does not cross.
func TestAWritableGuestCannotReachTheShell(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := sharedCard(t, d, "lent")
	h := d.guestHandler(task.ID, true)

	rec := guestGet(h, "/v1/tasks/"+task.ID+"/attach?kind=shell")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a guest asking for the shell got %d, wanted 403", rec.Code)
	}
}

// The same for a read-only guest, which reaches a different branch: it never
// consults the query at all. Asserted anyway, because "it happens not to look"
// is exactly the kind of protection that goes away in a refactor.
func TestAReadOnlyGuestCannotReachTheShell(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := sharedCard(t, d, "lent")
	h := d.guestHandler(task.ID, false)

	rec := guestGet(h, "/v1/tasks/"+task.ID+"/attach?kind=shell")
	if rec.Code == http.StatusSwitchingProtocols || rec.Code == http.StatusOK {
		t.Fatalf("a read-only guest reached the shell, answered %d", rec.Code)
	}
}

// Nothing a guest sends may CREATE a shell either. The POST is not on the
// allowlist, and this asserts that rather than trusting the list to stay short.
func TestAGuestCannotOpenAShell(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := sharedCard(t, d, "lent")
	h := d.guestHandler(task.ID, true)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tasks/"+task.ID+"/shell", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a guest opening a shell got %d, wanted 403", rec.Code)
	}
	if d.sup.getShell(task.ID) != nil {
		t.Fatal("a guest's request started a shell")
	}
}

// A share is one card. The rest of the board is the thing it exists to avoid
// handing over, and this is the list from the handler's own comment.
func TestAGuestSeesOneCardAndNoMore(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := sharedCard(t, d, "lent")
	other := sharedCard(t, d, "not-lent")
	h := d.guestHandler(task.ID, true)

	for _, path := range []string{
		"/v1/tasks",
		"/v1/events",
		"/v1/permissions",
		"/v1/settings",
		"/v1/browse",
		"/v1/tasks/" + task.ID + "/files",
		"/v1/tasks/" + other.ID,
		"/v1/tasks/" + other.ID + "/attach",
	} {
		if rec := guestGet(h, path); rec.Code != http.StatusForbidden {
			t.Errorf("%s answered %d, wanted 403", path, rec.Code)
		}
	}
}
