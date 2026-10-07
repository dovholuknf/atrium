package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

const openURL = "https://github.com/o/r/pull/7"

// openHarness is a PR harness with a recogniser for o/r#7, and a launcher that registers a card and keeps what it
// was sent.
type openHarness struct {
	*prHarness
	launched [][]byte
	refuse   error
	killed   []string
	// defaultRepo is the zendesk row's default repo.
	defaultRepo string
}

func newOpenHarness(t *testing.T, f *fakeForge) *openHarness {
	t.Helper()
	oh := &openHarness{prHarness: newPRHarness(t, f)}
	srv := oh.srv
	srv.st.DefaultReviewsRoot = filepath.ToSlash(t.TempDir())
	srv.PRRunner = &fakePRRunner{st: srv.st}
	srv.Recognise = func(url string) (*store.Resolved, error) {
		switch {
		case strings.Contains(url, "/pull/"):
			return &store.Resolved{Recogniser: "github-pr", URL: url, Host: "github.com", Title: "review o/r#7",
				Prompt: "review it", Tags: []string{"pull-request"},
				Vars: map[string]string{"host": "github.com", "org": "o", "repo": "r", "num": "7"}}, nil
		case strings.Contains(url, "/issues/"):
			return &store.Resolved{Recogniser: "github-issue", URL: url, Kind: "github", Host: "github.com",
				Title: "o/r#3", Prompt: "work on it", Tags: []string{"issue"}, Branch: "issue-3",
				Vars: map[string]string{"host": "github.com", "org": "o", "repo": "r", "num": "3"}}, nil
		case strings.Contains(url, "/tree/"):
			return &store.Resolved{Recogniser: "github-branch", URL: url, Kind: "github", Host: "github.com",
				Tags: []string{"branch"}, Branch: "feat/y",
				Vars: map[string]string{"host": "github.com", "org": "o", "repo": "r", "ref": "feat/y"}}, nil
		case strings.Contains(url, "zendesk"):
			return &store.Resolved{Recogniser: "zendesk-ticket", URL: url, Kind: "zendesk", Host: "acme.zendesk.com",
				Title: "zendesk-9", Prompt: "read the ticket", Tags: []string{"zendesk"}, Branch: "zendesk-9",
				DefaultRepo: oh.defaultRepo,
				Vars:        map[string]string{"host": "acme.zendesk.com", "num": "9"}}, nil
		case strings.HasSuffix(url, "/o/r"):
			return &store.Resolved{Recogniser: "github-repo", URL: url, Kind: "github", Host: "github.com",
				Tags: []string{"r"}, Vars: map[string]string{"host": "github.com", "org": "o", "repo": "r"}}, nil
		}
		return nil, store.ErrNoRecogniser
	}
	srv.Launch = func(body []byte) (*store.Task, error) {
		if oh.refuse != nil {
			return nil, oh.refuse
		}
		oh.launched = append(oh.launched, body)
		var in struct {
			Cwd  string   `json:"cwd"`
			Tags []string `json:"tags"`
		}
		_ = json.Unmarshal(body, &in)
		task := cardIn(t, srv.st, in.Cwd)
		if err := srv.st.SetTags(task.ID, in.Tags); err != nil {
			t.Fatal(err)
		}
		task.Tags = in.Tags
		return task, nil
	}
	srv.Kill = func(id string) error { oh.killed = append(oh.killed, id); return nil }
	return oh
}

func (oh *openHarness) open(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	rec := post(t, oh.h, "/v1/open", map[string]any{"url": url, "why": "look"})
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestOpenMakesTheWorktreeTheRowAndTheCardAndTiesThem(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	oh.checkout(t, "o", "r")
	code, out := oh.open(t, openURL)
	if code != 201 || out["created"] != true || out["key"] != "github.com/o/r/7" {
		t.Fatalf("%d %v", code, out)
	}
	path, _ := out["worktree"].(string)
	if !strings.HasSuffix(path, "/o/r/feat-x") {
		t.Fatalf("worktree %q", path)
	}
	if got := git(t, path, "log", "-1", "--format=%s"); got != "pr" {
		t.Errorf("the worktree is not at the PR head: %q", got)
	}
	if len(oh.launched) != 1 {
		t.Fatalf("launched %d", len(oh.launched))
	}
	var sent struct {
		Cwd, Prompt, Title, Why, Branch string
		Tags                            []string
		SourceURL                       string `json:"source_url"`
	}
	_ = json.Unmarshal(oh.launched[0], &sent)
	if sent.Cwd != path || sent.Prompt != "review it" || sent.Title != "review o/r#7" || sent.Why != "look" ||
		sent.Branch != "feat/x" || sent.SourceURL != openURL ||
		strings.Join(sent.Tags, ",") != "pull-request,pr,pr:o/r#7,link:github.com/o/r/7" {
		t.Errorf("the launch was %s", oh.launched[0])
	}
	row, err := oh.srv.st.PRByID(out["pr"].(string))
	if err != nil || row.WalkerTask != out["card"] {
		t.Fatalf("the row's walker is not the card: %v %+v", err, row)
	}
	// The card owns what the open made, and its disk was measured.
	rec := httpDo(t, oh.h, "GET", "/v1/tasks/"+out["card"].(string)+"/resources")
	var inv struct {
		Resources []store.CardResource `json:"resources"`
		Disk      int64                `json:"disk_bytes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	kinds := []string{}
	for _, r := range inv.Resources {
		kinds = append(kinds, r.Kind+" "+r.Ref)
	}
	want := "worktree " + path + ",ref refs/atrium/pr/7,branch feat/x,review " + row.ID
	if rec.Code != 200 || strings.Join(kinds, ",") != want {
		t.Errorf("the inventory is %d %v, want %s", rec.Code, kinds, want)
	}
	if inv.Disk <= 0 {
		t.Errorf("the disk was not measured: %d", inv.Disk)
	}
}

// httpDo is a request with no body through the handler.
func httpDo(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestOpenTwiceAnswersTheLiveCard(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	oh.checkout(t, "o", "r")
	_, first := oh.open(t, openURL)
	code, again := oh.open(t, openURL)
	if code != 200 || again["created"] != false || again["card"] != first["card"] || again["pr"] != first["pr"] {
		t.Fatalf("%d %v, first %v", code, again, first)
	}
	if len(oh.launched) != 1 {
		t.Errorf("a second open launched again: %d", len(oh.launched))
	}
}

func TestOpenUndoesTheWorktreeAndTheRowWhenTheCardDoesNotStart(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	repo := oh.checkout(t, "o", "r")
	oh.refuse = errors.New("no runner called ghost")
	code, out := oh.open(t, openURL)
	if code != 400 || out["step"] != "card" || !strings.Contains(out["error"].(string), "no runner called ghost") {
		t.Fatalf("%d %v", code, out)
	}
	dest := filepath.Join(oh.wt, "o", "r", "feat-x")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("the worktree was left behind: %v", err)
	}
	if got := git(t, repo, "branch", "--list", "feat/x"); got != "" {
		t.Errorf("the branch it made was left behind: %q", got)
	}
	rows, _ := oh.srv.st.PRs(store.PRFilter{})
	if len(rows) != 0 {
		t.Errorf("the row is still in the index: %+v", rows[0])
	}
	// What it made is in no card's live inventory.
	if disk, _ := oh.srv.st.CardDisk(); len(disk) != 0 {
		t.Errorf("a rolled back open left live inventory: %v", disk)
	}
	// Nothing half made is in the way of the next try.
	oh.refuse = nil
	if code, out := oh.open(t, openURL); code != 201 {
		t.Fatalf("the retry: %d %v", code, out)
	}
}

func TestOpenRefusesALinkNothingKnowsAndOneThatNamesNoWork(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	if code, out := oh.open(t, "https://nowhere.example/x"); code != 422 || out["code"] != "no_recogniser" {
		t.Errorf("unknown: %d %v", code, out)
	}
	if code, out := oh.open(t, "https://github.com/o/r"); code != 422 || out["code"] != "not_openable" {
		t.Errorf("repo page: %d %v", code, out)
	}
	if len(oh.launched) != 0 {
		t.Errorf("a refusal launched")
	}
}

func TestOpenNeedsNoProviderRow(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{pr: forge.PR{HeadRef: "main", FromFork: true}})
	if err := oh.srv.st.DeleteProvider("github"); err != nil {
		t.Fatal(err)
	}
	scm := filepath.Join(t.TempDir(), "scm")
	clone := filepath.Join(scm, "github.com", "o", "r")
	mkDir(t, filepath.Dir(clone))
	git(t, scm, "clone", "-q", oh.up, clone)
	oh.srv.SCMClone = func(context.Context, string) (gitsync.SCMResult, error) {
		return gitsync.SCMResult{Path: clone, State: "existing"}, nil
	}
	code, out := oh.open(t, openURL)
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	want := filepath.ToSlash(filepath.Join(scm, "worktrees", "github.com", "o", "r", "pr-7"))
	if out["worktree"] != want {
		t.Fatalf("worktree %v, want %s", out["worktree"], want)
	}
}
