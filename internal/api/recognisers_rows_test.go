package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// loadShippedRows PUTs every row of scripts/recognisers/<file> through the real
// route, the same loop load.ps1 runs, and returns how many it saved.
func loadShippedRows(t *testing.T, s *Server, file string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "recognisers", file))
	if err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("%s is not a json array: %v", file, err)
	}
	for _, row := range rows {
		var id struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(row, &id); err != nil || id.ID == "" {
			t.Fatalf("%s has a row with no id", file)
		}
		if w := putRecogniser(t, s, id.ID, string(row)); w.Code != http.StatusOK {
			t.Fatalf("%s: PUT %s answered %d: %s", file, id.ID, w.Code, w.Body.String())
		}
	}
	return len(rows)
}

// recogniseOver asks POST /v1/recognise, with the store's own match and fill
// standing in for the daemon, which would also run a fetch. A fetch needs `gh`
// and a network, and none of what is checked here depends on it.
func recogniseOver(t *testing.T, s *Server, st *store.Store, url string) (int, store.Resolved) {
	t.Helper()
	s.Recognise = func(u string) (*store.Resolved, error) {
		r, vars, err := st.MatchRecogniser(u)
		if err != nil {
			return nil, err
		}
		return r.Fill(vars), nil
	}
	body, _ := json.Marshal(map[string]string{"url": url})
	w := httptest.NewRecorder()
	s.recognise(w, httptest.NewRequest("POST", "/v1/recognise", strings.NewReader(string(body))))
	var out store.Resolved
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, out
}

func TestTheShippedGitHubRowsResolvePullRequestsAndIssues(t *testing.T) {
	s, st := recogniserServer(t)
	if n := loadShippedRows(t, s, "github.json"); n != 4 {
		t.Fatalf("github.json holds %d rows, wanted 4", n)
	}

	cases := []struct {
		url, row, org, repo, num string
	}{
		{"https://github.com/openziti/ziti/pull/4211", "github-pull-request", "openziti", "ziti", "4211"},
		{"https://github.com/openziti/ziti/pull/4211/files", "github-pull-request", "openziti", "ziti", "4211"},
		{"https://github.com/openziti/ziti/issues/12", "github-issue", "openziti", "ziti", "12"},
	}
	for _, c := range cases {
		code, got := recogniseOver(t, s, st, c.url)
		if code != http.StatusOK {
			t.Fatalf("%s answered %d", c.url, code)
		}
		if got.Recogniser != c.row || got.Host != "github.com" || got.Org != c.org ||
			got.Repo != c.repo || got.Vars["num"] != c.num {
			t.Fatalf("%s resolved to %+v", c.url, got)
		}
	}

	_, pr := recogniseOver(t, s, st, "https://github.com/openziti/ziti/pull/4211")
	if pr.Kind != "github" || pr.Window != "pull-requests" ||
		pr.Cwd != "D:/worktrees/github/openziti/ziti/pr-4211" ||
		strings.Join(pr.Tags, ",") != "pull-request,ziti" {
		t.Fatalf("pull request fields: %+v", pr)
	}
	// Nothing fetched {title} or {headRefName}, so they stay as visible holes.
	if len(pr.Missing) == 0 {
		t.Fatalf("a row with a fetch and no fetch run should report holes: %+v", pr)
	}
	_, is := recogniseOver(t, s, st, "https://github.com/openziti/ziti/issues/12")
	if is.Cwd != "D:/worktrees/github/openziti/ziti/issue-12" || is.Branch != "issue-12" {
		t.Fatalf("issue fields: %+v", is)
	}
}

func TestTheShippedBitbucketRowResolvesAPullRequest(t *testing.T) {
	s, st := recogniserServer(t)
	if n := loadShippedRows(t, s, "bitbucket.json"); n != 3 {
		t.Fatalf("bitbucket.json holds %d rows, wanted 3", n)
	}
	code, got := recogniseOver(t, s, st, "https://bitbucket.org/acme/widgets/pull-requests/77")
	if code != http.StatusOK {
		t.Fatalf("answered %d", code)
	}
	if got.Recogniser != "bitbucket-pull-request" || got.Kind != "bitbucket" || got.Host != "bitbucket.org" ||
		got.Org != "acme" || got.Repo != "widgets" || got.Title != "acme/widgets PR 77" ||
		got.Cwd != "D:/worktrees/bitbucket/acme/widgets/pr-77" || got.Window != "pull-requests" {
		t.Fatalf("resolved to %+v", got)
	}
	// The branch is the forge fetch's, through the hub, and this test runs no fetch.
	if strings.Join(got.Missing, ",") != "headRefName" {
		t.Fatalf("only the branch the forge fetch fills should be a hole: %v", got.Missing)
	}
	if _, again := recogniseOver(t, s, st, "https://bitbucket.org/acme/widgets/pull-requests/77/diff"); again.Vars["num"] != "77" {
		t.Fatalf("a trailing path should still be the same pull request: %+v", again)
	}
}

func TestTheShippedSupportRowsDefaultToZiti(t *testing.T) {
	s, st := recogniserServer(t)
	if n := loadShippedRows(t, s, "support.json"); n != 2 {
		t.Fatalf("support.json holds %d rows, wanted 2", n)
	}
	cases := []struct{ url, row, num, cwd string }{
		{"https://netfoundry.zendesk.com/agent/tickets/15925", "zendesk-ticket", "15925",
			"D:/worktrees/github/openziti/ziti/zendesk-15925"},
		{"https://openziti.discourse.group/t/one-client-with-wrong-clock-caused-whole-network-down/6158/6",
			"discourse-topic", "6158", "D:/worktrees/github/openziti/ziti/discourse-6158"},
		{"https://openziti.discourse.group/t/6158", "discourse-topic", "6158",
			"D:/worktrees/github/openziti/ziti/discourse-6158"},
		{"https://openziti.discourse.group/t/6158/6", "discourse-topic", "6158",
			"D:/worktrees/github/openziti/ziti/discourse-6158"},
	}
	for _, c := range cases {
		code, got := recogniseOver(t, s, st, c.url)
		if code != http.StatusOK || got.Recogniser != c.row || got.Vars["num"] != c.num || got.Cwd != c.cwd {
			t.Fatalf("%s answered %d: %+v", c.url, code, got)
		}
	}
	for _, url := range []string{
		"https://netfoundry.zendesk.com/agent/tickets/15925x",
		"https://other.zendesk.com/agent/tickets/15925",
		"https://openziti.discourse.group/t/../6158",
		"https://openziti.discourse.group/c/general/5",
	} {
		if code, got := recogniseOver(t, s, st, url); code != http.StatusNotFound {
			t.Fatalf("%s answered %d by %q, wanted 404", url, code, got.Recogniser)
		}
	}
}

// Near misses. An issue is not a pull request, extra path IN FRONT of the
// marker is a different thing, and letters glued to the number are not a number.
// None may be answered by the pull request rows.
func TestTheShippedRowsDoNotAnswerNearMisses(t *testing.T) {
	s, st := recogniserServer(t)
	loadShippedRows(t, s, "github.json")
	loadShippedRows(t, s, "bitbucket.json")

	for _, url := range []string{
		"https://bitbucket.org/acme/widgets/extra/pull-requests/77",
		"https://bitbucket.org/acme/widgets/pull-requests/77x",
		"https://bitbucket.org/acme/widgets/pull-requests/",
		"https://github.com/openziti/ziti/extra/pull/4211",
		"https://github.com/openziti/ziti/pull/4211x",
		"https://github.com/openziti/ziti/pull/",
		// a dot segment would walk the worktree path out of its root
		"https://github.com/../ziti/pull/1",
		"https://github.com/openziti/../pull/1",
		"https://github.com/openziti/..",
		"https://github.com/../..",
		"https://bitbucket.org/../widgets/pull-requests/77",
		"https://bitbucket.org/acme/./pull-requests/77",
		"https://github.com/openziti/ziti/tree/../x",
		"https://github.com/openziti/ziti/tree/main/../..",
		"https://bitbucket.org/acme/widgets/branch/./x",
	} {
		if code, got := recogniseOver(t, s, st, url); code != http.StatusNotFound {
			t.Fatalf("%s answered %d by %q, wanted 404", url, code, got.Recogniser)
		}
	}

	// And an issue url is the issue row's, never the pull request row's.
	_, got := recogniseOver(t, s, st, "https://github.com/openziti/ziti/issues/12")
	if got.Recogniser == "github-pull-request" {
		t.Fatal("an issue was answered as a pull request")
	}
	if _, got := recogniseOver(t, s, st, "https://bitbucket.org/acme/widgets/issues/77"); got.Recogniser != "bitbucket-issue" {
		t.Fatalf("a bitbucket issue: %+v", got)
	}
	for url, want := range map[string]string{
		"https://github.com/openziti/ziti/tree/feature/x":     "D:/worktrees/github/openziti/ziti/feature/x",
		"https://bitbucket.org/acme/widgets/branch/feature/x": "D:/worktrees/bitbucket/acme/widgets/feature/x",
	} {
		if _, got := recogniseOver(t, s, st, url); got.Cwd != want || got.Branch != "feature/x" {
			t.Fatalf("%s: %+v", url, got)
		}
	}
	// The shrug at the bottom takes a bare repository and nothing longer.
	if _, repo := recogniseOver(t, s, st, "https://github.com/openziti/ziti"); repo.Recogniser != "github-repo" {
		t.Fatalf("a bare repository: %+v", repo)
	}
}
