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
	h := d.guestHandler(task.ID)

	rec := guestGet(h, "/v1/tasks/"+task.ID+"/attach?kind=shell")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a guest asking for the shell got %d, wanted 403", rec.Code)
	}
}

// THERE IS NO READ-ONLY GUEST any more, which is why the test that used to be
// here is gone rather than renamed.
//
// It existed, defaulted to on, and was a lie about what a share is: the address
// is the whole credential, there is no login, and a guest owns their copy of
// the page. Enforcing "watch only" on the socket was real as far as it went,
// and what it bought was a checkbox that made handing out a link feel safer
// than it is.
//
// Anything reaching this handler drives the session. The tests above and below
// are about what it CANNOT reach, which is the part that still means something.

// Nothing a guest sends may CREATE a shell either. The POST is not on the
// allowlist, and this asserts that rather than trusting the list to stay short.
func TestAGuestCannotOpenAShell(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := sharedCard(t, d, "lent")
	h := d.guestHandler(task.ID)

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
	h := d.guestHandler(task.ID)

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

// The page a guest gets has to be the WHOLE page.
//
// `guestHandler` is an allow list, which is the point of it: everything not
// named is refused. The board was one HTML file when that list was written, so
// splitting it into a stylesheet and two dozen scripts added two dozen paths
// the list did not name. A guest refused those does not get a smaller board,
// they get an unstyled document with no script on it, and nothing says why.
func TestAGuestGetsTheWholeBoard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := sharedCard(t, d, "lent")
	h := d.guestHandler(task.ID)

	for _, path := range []string{"/", "/sw.js", "/board.css", "/js/core.js", "/js/boot.js"} {
		rec := guestGet(h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("a guest asking for %s got %d. the board cannot load without it", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("a guest asking for %s got an empty body", path)
		}
	}
}
