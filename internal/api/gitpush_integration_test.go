//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// git.push defaults to hub, reads back what was written, and refuses a value it does not know rather than storing
// something that would read as `none` and look like it took.
func TestGitPushSetting(t *testing.T) {
	srv, st, _ := fileServer(t)
	if got := settingsGet(t, srv)["git_push"]; got != "hub" {
		t.Fatalf("default = %v, want hub", got)
	}
	if rec := settingsPost(t, srv, `{"git_push":"none"}`); rec.Code != 200 {
		t.Fatalf("setting none answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["git_push"]; got != "none" || st.GitPush() != "none" {
		t.Fatalf("read back %v / %s", got, st.GitPush())
	}
	for _, bad := range []string{`{"git_push":"all"}`, `{"git_push":"origin"}`, `{"git_push":"Hub x"}`} {
		if rec := settingsPost(t, srv, bad); rec.Code != 400 {
			t.Fatalf("%s answered %d, want 400", bad, rec.Code)
		}
	}
	if st.GitPush() != "none" {
		t.Fatal("a refused value changed the setting")
	}
	if rec := settingsPost(t, srv, `{"git_push":"hub"}`); rec.Code != 200 || st.GitPush() != "hub" {
		t.Fatalf("hub: %d %s", rec.Code, st.GitPush())
	}
	// Empty is the default.
	settingsPost(t, srv, `{"git_push":"none"}`)
	if rec := settingsPost(t, srv, `{"git_push":""}`); rec.Code != 200 || st.GitPush() != "hub" {
		t.Fatalf("empty: %d %s", rec.Code, st.GitPush())
	}
}

// A damaged row closes the door: only hub, or nothing, lets a card push.
func TestAnUnreadableGitPushValueReadsAsNone(t *testing.T) {
	_, st, _ := fileServer(t)
	if err := st.SetSetting("git_push", "banana"); err != nil {
		t.Fatal(err)
	}
	if st.GitPush() != "none" {
		t.Fatalf("banana reads as %s", st.GitPush())
	}
}

func TestTheGitPushRouteTakesABranchAndNothingElse(t *testing.T) {
	srv, st, dir := fileServer(t)
	id := cardIn(t, st, dir).ID
	var got [2]string
	srv.GitPush = func(id, branch string) (any, error) {
		got = [2]string{id, branch}
		return map[string]string{"ok": "1"}, nil
	}
	h := srv.Handler()
	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/git-push", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := post(`{"branch":"fix/x"}`); c != 200 || got != [2]string{id, "fix/x"} {
		t.Fatalf("%d %v", c, got)
	}
	for _, bad := range []string{`{"branch":"fix/x","force":true}`, `{"branch":"fix/x","remote":"origin"}`, `nope`} {
		if c := post(bad); c != 400 {
			t.Fatalf("%s answered %d", bad, c)
		}
	}
}
