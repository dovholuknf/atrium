//go:build integration

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// stdoutOf runs f with stdout captured.
func stdoutOf(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	was := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	ferr := f()
	w.Close()
	os.Stdout = was
	return <-done, ferr
}

// openBoard answers POST /v1/open as the open verb does: opened for a pull request and a ticket, not_openable for a
// repo page, a step refusal for a refused link. POST /v1/recognise answers the repo page's resolution.
func openBoard(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = append(*seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("X-Atrium-Room")+" "+string(b))
		var in struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(b, &in)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/open" && strings.Contains(in.URL, "/pull/"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"github.com/o/r/7","card":"beta~c1","pr":"beta~pr_1","worktree":"/wt/o/r/pr-7",` +
				`"created":true,"room":"beta","title":"review o/r#7"}`))
		case r.URL.Path == "/v1/open" && strings.Contains(in.URL, "/tickets/"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"zendesk:acme.zendesk.com/9","kind":"support","card":"c2","repo":"github.com/o/z",` +
				`"worktree":"/wt/o/z/zendesk-9","created":true}`))
		case r.URL.Path == "/v1/open" && strings.HasSuffix(in.URL, "/o/r"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"code":"not_openable","error":"names no piece of work","step":"recognise"}`))
		case r.URL.Path == "/v1/open":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"worktree_failed","error":"gh is not logged in","step":"worktree"}`))
		case r.URL.Path == "/v1/recognise":
			_, _ = w.Write([]byte(`{"recogniser":"github-repo","label":"a repository on github","title":"o/r repo"}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestOpenPrintsTheCardAndWhereItIsOnTheBoard(t *testing.T) {
	var seen []string
	srv := openBoard(t, &seen)
	defer srv.Close()
	out, err := stdoutOf(t, func() error {
		return openLink("https://github.com/o/r/pull/7", openOpts{boardURL: srv.URL, why: "look", room: "beta"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"opened: github.com/o/r/7 on beta", "beta~c1", "/wt/o/r/pr-7", srv.URL + "/#term=beta~c1"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "POST /v1/open beta ") || !strings.Contains(seen[0], `"why":"look"`) {
		t.Errorf("the board saw %v", seen)
	}
}

func TestOpenATicketSendsTheRepoAndPrintsWhereItOpened(t *testing.T) {
	var seen []string
	srv := openBoard(t, &seen)
	defer srv.Close()
	out, err := stdoutOf(t, func() error {
		return openLink("https://acme.zendesk.com/agent/tickets/9", openOpts{boardURL: srv.URL, repo: "github.com/o/z"})
	})
	if err != nil || !strings.Contains(out, "support") || !strings.Contains(out, "github.com/o/z") {
		t.Errorf("a ticket: %v\n%s", err, out)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], `"repo":"github.com/o/z"`) {
		t.Errorf("the board saw %v", seen)
	}
}

func TestOpenShowsALinkThatNamesNoWorkAndSaysWhyARefusalStopped(t *testing.T) {
	var seen []string
	srv := openBoard(t, &seen)
	defer srv.Close()
	out, err := stdoutOf(t, func() error {
		return openLink("https://github.com/o/r", openOpts{boardURL: srv.URL})
	})
	if err != nil || !strings.Contains(out, "names no piece of work") || !strings.Contains(out, "o/r repo") {
		t.Errorf("a repo page: %v\n%s", err, out)
	}
	_, err = stdoutOf(t, func() error {
		return openLink("https://bitbucket.org/o/r/x", openOpts{boardURL: srv.URL})
	})
	if err == nil || err.Error() != "atrium refused at the worktree step: gh is not logged in" {
		t.Errorf("a refusal: %v", err)
	}
}
