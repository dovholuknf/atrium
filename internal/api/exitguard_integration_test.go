//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func exitGuardRoom(t *testing.T) (*Server, *store.Store, *[]string) {
	t.Helper()
	srv, st, _ := fileServer(t)
	stopped := &[]string{}
	srv.StopRunner = func(id string) error { *stopped = append(*stopped, id); return nil }
	return srv, st, stopped
}

func exitCard(t *testing.T, st *store.Store, name string, tags ...string) *store.Task {
	t.Helper()
	c, _, err := st.Register(store.Observed{WireName: name, Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) > 0 {
		if err := st.SetTags(c.ID, tags); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func postExit(srv *Server, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/exit", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// An agent may exit itself and a card it launched, and no other.
func TestExitGuardSelfChildAndStranger(t *testing.T) {
	srv, st, stopped := exitGuardRoom(t)
	me, kid, stranger := exitCard(t, st, "me"), exitCard(t, st, "kid"), exitCard(t, st, "stranger")
	if err := st.SetLineage(kid.ID, "me", me.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		target string
		want   int
	}{{me.ID, 200}, {kid.ID, 200}, {stranger.ID, 403}} {
		if rec := postExit(srv, c.target, `{"from":"me"}`); rec.Code != c.want {
			t.Fatalf("exit %s answered %d %s, want %d", c.target, rec.Code, rec.Body.String(), c.want)
		}
	}
	if len(*stopped) != 2 {
		t.Fatalf("stopped %v, want only me and kid", *stopped)
	}
}

// A card that is not the caller's needs force, and the force is on the card.
func TestExitGuardForceIsRecorded(t *testing.T) {
	srv, st, stopped := exitGuardRoom(t)
	exitCard(t, st, "me")
	stranger := exitCard(t, st, "stranger")
	if rec := postExit(srv, stranger.ID, `{"from":"me","force":true}`); rec.Code != 200 {
		t.Fatalf("forced exit answered %d %s", rec.Code, rec.Body.String())
	}
	if len(*stopped) != 1 {
		t.Fatalf("stopped %v", *stopped)
	}
	evs, _ := st.Events(stranger.ID, 50)
	found := false
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "forced_exit") && strings.Contains(string(e.Payload), `"me"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no forced_exit event on the card: %+v", evs)
	}
}

// A director is the operator's to end: not by an agent, not with force.
func TestExitGuardDirectorsAreNeverAnAgents(t *testing.T) {
	srv, st, stopped := exitGuardRoom(t)
	exitCard(t, st, "me")
	for _, tag := range []string{"atrium:director", "atrium:context-ceiling"} {
		d := exitCard(t, st, "dir-"+strings.TrimPrefix(tag, "atrium:"), tag)
		for _, body := range []string{`{"from":"me"}`, `{"from":"me","force":true}`} {
			if rec := postExit(srv, d.ID, body); rec.Code != 403 {
				t.Fatalf("%s with %s answered %d, want 403", tag, body, rec.Code)
			}
		}
		// the operator sends no from
		if rec := postExit(srv, d.ID, `{}`); rec.Code != 200 {
			t.Fatalf("the operator's exit of %s answered %d %s", tag, rec.Code, rec.Body.String())
		}
	}
	if len(*stopped) != 2 {
		t.Fatalf("stopped %v, want only the operator's two", *stopped)
	}
}

// A name from another room is not this room's card of the same name: `me` with
// foreign set is a stranger to the local `me`, whatever the names say.
func TestExitGuardForeignCallerIsNotTheLocalCard(t *testing.T) {
	srv, st, _ := exitGuardRoom(t)
	me := exitCard(t, st, "me")
	if rec := postExit(srv, me.ID, `{"from":"me","foreign":true}`); rec.Code != 403 {
		t.Fatalf("a foreign me exiting the local me answered %d, want 403", rec.Code)
	}
	if rec := postExit(srv, me.ID, `{"from":"me","foreign":true,"force":true}`); rec.Code != 200 {
		t.Fatalf("a forced foreign exit answered %d", rec.Code)
	}
}

// A card launched from another room records its launcher as `name@room`, and the
// foreign asker of that name may exit it without force. Any other foreign name
// may not.
func TestExitGuardForeignLauncherMayExitItsChild(t *testing.T) {
	srv, st, stopped := exitGuardRoom(t)
	kid := exitCard(t, st, "kid")
	if err := st.SetLineage(kid.ID, "boss@claude-sg4", "claude-sg4~01ABC"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"from":"other@claude-sg4","foreign":true}`, 403},
		{`{"from":"boss@claude-mini","foreign":true}`, 403},
		{`{"from":"BOSS@claude-sg4","foreign":true}`, 200},
	} {
		if rec := postExit(srv, kid.ID, c.body); rec.Code != c.want {
			t.Fatalf("%s answered %d %s, want %d", c.body, rec.Code, rec.Body.String(), c.want)
		}
	}
	if len(*stopped) != 1 {
		t.Fatalf("stopped %v", *stopped)
	}
}

// A director is told it is the one being refused when it asks to exit itself.
func TestExitGuardADirectorExitingItselfIsToldSo(t *testing.T) {
	srv, st, _ := exitGuardRoom(t)
	d := exitCard(t, st, "boss", "atrium:director")
	rec := postExit(srv, d.ID, `{"from":"boss"}`)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "you are a director") {
		t.Fatalf("answered %d %s", rec.Code, rec.Body.String())
	}
}

// A body that does not parse is refused, never taken for the operator's. No body
// at all is the operator's.
func TestExitGuardMalformedBodyIsNotTheOperator(t *testing.T) {
	srv, st, stopped := exitGuardRoom(t)
	d := exitCard(t, st, "boss", "atrium:director")
	if rec := postExit(srv, d.ID, `{"from":"me","for`); rec.Code != 400 {
		t.Fatalf("a truncated body answered %d, want 400", rec.Code)
	}
	if len(*stopped) != 0 {
		t.Fatalf("stopped %v", *stopped)
	}
	if rec := postExit(srv, d.ID, ``); rec.Code != 200 {
		t.Fatalf("no body answered %d", rec.Code)
	}
}

// A launcher recorded by id is matched by id only: a later card that reuses the
// launcher's name is not the launcher.
func TestExitGuardLauncherIsMatchedByIDWhenThereIsOne(t *testing.T) {
	srv, st, _ := exitGuardRoom(t)
	kid := exitCard(t, st, "kid")
	if err := st.SetLineage(kid.ID, "gone", "some-other-id"); err != nil {
		t.Fatal(err)
	}
	exitCard(t, st, "gone")
	if rec := postExit(srv, kid.ID, `{"from":"gone"}`); rec.Code != 403 {
		t.Fatalf("a card reusing the launcher's name answered %d, want 403", rec.Code)
	}
}
