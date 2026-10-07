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

// openBoard answers POST /v1/open as the open verb does: opened for a pull request, not_a_pr for an issue, a step
// refusal for a refused link. POST /v1/recognise answers the issue's resolution.
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
		case r.URL.Path == "/v1/open" && strings.Contains(in.URL, "/issues/"):
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"code":"not_a_pr","error":"only a pull request","step":"recognise"}`))
		case r.URL.Path == "/v1/open":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"worktree_failed","error":"gh is not logged in","step":"worktree"}`))
		case r.URL.Path == "/v1/recognise":
			_, _ = w.Write([]byte(`{"recogniser":"github-issue","label":"github issue","title":"o/r issue 3"}`))
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

func TestOpenShowsALinkThatIsNotAPRAndSaysWhyARefusalStopped(t *testing.T) {
	var seen []string
	srv := openBoard(t, &seen)
	defer srv.Close()
	out, err := stdoutOf(t, func() error {
		return openLink("https://github.com/o/r/issues/3", openOpts{boardURL: srv.URL})
	})
	if err != nil || !strings.Contains(out, "only a pull request opens as a card") || !strings.Contains(out, "o/r issue 3") {
		t.Errorf("an issue: %v\n%s", err, out)
	}
	_, err = stdoutOf(t, func() error {
		return openLink("https://bitbucket.org/o/r/x", openOpts{boardURL: srv.URL})
	})
	if err == nil || err.Error() != "atrium refused at the worktree step: gh is not logged in" {
		t.Errorf("a refusal: %v", err)
	}
}
