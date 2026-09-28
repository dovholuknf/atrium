package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A card's alias is accepted wherever a handle is. See store/alias.go and
// docs/backlog-2.md item 35.

// `atrium tell sa89 ...` and `@sa89` both reach the card, and the handle still
// wins over an alias.
func TestTellAcceptsAnAlias(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	bob := peerCard(t, d, "bob-41800")
	if err := d.st.SetAlias(bob.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"bob", "@bob", "@Bob"} {
		out, code := tell(t, d, "alice", to, "ping via "+to)
		if code != http.StatusOK {
			t.Fatalf("telling %q answered %d: %v", to, code, out)
		}
	}
	pending, err := d.st.PendingMessages(bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("%d of 3 messages sent by alias arrived", len(pending))
	}
}

// Naming yourself by your own alias is still telling yourself.
func TestASessionCannotTellItselfByAlias(t *testing.T) {
	d := testDaemon(t)
	me := peerCard(t, d, "sa89-typing-gate")
	if err := d.st.SetAlias(me.ID, "sa89"); err != nil {
		t.Fatal(err)
	}
	if _, code := tell(t, d, "sa89-typing-gate", "@sa89", "hello me"); code != http.StatusBadRequest {
		t.Fatalf("telling yourself by alias answered %d", code)
	}
}

// The list says each peer's alias, so a session can learn the short name.
func TestPeersCarryTheAlias(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "alice")
	bob := peerCard(t, d, "bob-41800")
	if err := d.st.SetAlias(bob.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	list, err := d.peers("alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if strings.HasSuffix(p.Handle, "bob-41800") {
			if p.Alias != "bob" {
				t.Fatalf("the peer list gives bob's alias as %q", p.Alias)
			}
			return
		}
	}
	t.Fatalf("bob is not in the list: %+v", list)
}

// A launched worker starts with its title's prefix as its alias, and a second
// worker with the same prefix while the first is live starts with none rather
// than failing to launch.
func TestALaunchedWorkerTakesItsTitlePrefix(t *testing.T) {
	d := testDaemon(t)
	h := slowHarness(t, d)
	first, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(),
		Title: "sa89: typing gate and card aliases", Tags: []string{OriginAgentTag}})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(first.ID) })
	got, err := d.st.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Alias != "sa89" {
		t.Fatalf("the worker's alias is %q, want its title prefix", got.Alias)
	}

	second, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(),
		Title: "sa89: the same prefix again", Tags: []string{OriginAgentTag}})
	if err != nil {
		t.Fatalf("a clashing default alias failed the launch: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(second.ID) })
	if got, _ := d.st.Get(second.ID); got.Alias != "" {
		t.Fatalf("a second live card took alias %q", got.Alias)
	}
}

// The board sets an alias through the card's PATCH, and a clash is refused
// naming the holder.
func TestTheBoardSetsAnAliasAndHearsWhoHoldsIt(t *testing.T) {
	d := testDaemon(t)
	a := peerCard(t, d, "dotfiles-41800")
	b := peerCard(t, d, "other")
	h := d.BoardHandler()
	patch := func(id, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PATCH", "/v1/tasks/"+id, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := patch(a.ID, `{"alias":"@dotfiles"}`); w.Code != http.StatusOK {
		t.Fatalf("setting an alias answered %d: %s", w.Code, w.Body)
	}
	w := patch(b.ID, `{"alias":"dotfiles"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "dotfiles-41800") {
		t.Fatalf("a clash answered %d, not naming the holder: %s", w.Code, w.Body)
	}
	if w := patch(b.ID, `{"alias":"has space"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a malformed alias answered %d", w.Code)
	}
}
