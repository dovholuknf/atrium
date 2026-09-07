package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func recogniserServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st), st
}

func putRecogniser(t *testing.T, s *Server, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("PUT", "/v1/recognisers/"+id, strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.saveRecogniser(w, r)
	return w
}

// A pattern that does not compile is the CALLER's mistake and the message names
// the position in the expression. Reported as a 500 it would read as "atrium is
// broken" and the position would be buried.
func TestSavingABadPatternIsA400ThatSaysWhy(t *testing.T) {
	s, _ := recogniserServer(t)
	w := putRecogniser(t, s, "bad", `{"pattern":"(?P<org>[^/]+","enabled":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, wanted 400", w.Code)
	}
	var e struct{ Error string }
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Error, "regular expression") {
		t.Fatalf("the refusal does not say what is wrong: %q", e.Error)
	}
}

// A url no row wants is an ANSWER, not a failure. The board says "nothing here
// knows what that is yet" and points at where rows are written, which it can
// only do if it can tell this apart from a broken request.
func TestAnUnrecognisedURLIsA404(t *testing.T) {
	s, _ := recogniserServer(t)
	s.Recognise = func(url string) (*store.Resolved, error) { return nil, store.ErrNoRecogniser }

	r := httptest.NewRequest("POST", "/v1/recognise", strings.NewReader(`{"url":"https://x.invalid/y"}`))
	w := httptest.NewRecorder()
	s.recognise(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d, wanted 404", w.Code)
	}
}

// RECOGNISING STARTS NOTHING. This is the separation the whole design rests on,
// and it is the reason a browser can be pointed at this endpoint: the worst a
// url can do here is fill some text boxes in.
func TestRecognisingNeverLaunches(t *testing.T) {
	s, st := recogniserServer(t)
	launched := 0
	s.Launch = func(body []byte) (*store.Task, error) {
		launched++
		return nil, nil
	}
	if _, err := st.SaveRecogniser(store.Recogniser{
		ID: "pr", Enabled: true, Pattern: `^https://x/(?P<repo>\w+)$`, Cwd: "/tmp/{repo}",
	}); err != nil {
		t.Fatal(err)
	}
	s.Recognise = func(url string) (*store.Resolved, error) {
		r, vars, err := st.MatchRecogniser(url)
		if err != nil {
			return nil, err
		}
		return r.Fill(vars), nil
	}

	r := httptest.NewRequest("POST", "/v1/recognise", strings.NewReader(`{"url":"https://x/ziti"}`))
	w := httptest.NewRecorder()
	s.recognise(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if launched != 0 {
		t.Fatal("recognising a url started a runner. it fills a form in and stops")
	}
	var got store.Resolved
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Cwd != "/tmp/ziti" {
		t.Fatalf("cwd: %q", got.Cwd)
	}
}

// Deleting the thing that recognised the link does not make the work not work.
func TestDeletingARecogniserLeavesTheCardsItFilled(t *testing.T) {
	s, st := recogniserServer(t)
	if _, err := st.SaveRecogniser(store.Recogniser{
		ID: "pr", Enabled: true, Pattern: `^https://x/(?P<repo>\w+)$`,
	}); err != nil {
		t.Fatal(err)
	}
	task, _, err := st.Register(store.Observed{
		WireName: "w", Worktree: "/tmp/ziti", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("DELETE", "/v1/recognisers/pr", nil)
	r.SetPathValue("id", "pr")
	w := httptest.NewRecorder()
	s.deleteRecogniser(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	if _, err := st.Get(task.ID); err != nil {
		t.Fatalf("the card went with the row: %v", err)
	}
}

// A daemon that never wired the hook says so rather than pretending the url is
// unknown, which would send somebody off to write a row that already exists.
func TestRecogniseWithoutADaemonSaysSo(t *testing.T) {
	s, _ := recogniserServer(t)
	r := httptest.NewRequest("POST", "/v1/recognise", strings.NewReader(`{"url":"https://x/y"}`))
	w := httptest.NewRecorder()
	s.recognise(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, wanted 501", w.Code)
	}
}
