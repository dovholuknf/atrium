package forge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// hubFake stands for the hub's forge route: it records what each call asked and answers through JSON, as the link
// does.
type hubFake struct {
	asked []HubAsk
	paths []string
	pr    HubPR
	issue HubIssue
	repo  HubRepo
	err   error
}

func (h *hubFake) call(_ context.Context, path string, in, out any) error {
	h.paths = append(h.paths, path)
	h.asked = append(h.asked, in.(HubAsk))
	if h.err != nil {
		return h.err
	}
	var ans any
	switch path {
	case HubPRPath:
		ans = h.pr
	case HubIssuePath:
		ans = h.issue
	case HubRepoPath:
		ans = h.repo
	}
	b, err := json.Marshal(ans)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func TestTheRemoteAsksTheHubAndNamesTheHeadInItsStore(t *testing.T) {
	h := &hubFake{pr: HubPR{Kind: GitHub, PR: &PR{Title: "t", Head: "abc", BaseRef: "main"}, Diff: []byte("d"),
		URL: "https://github.com/o/r/pull/7", Store: "github/o/r", Ref: "refs/atrium/pr/7"}}
	r := NewRemote(h.call)
	ref := Ref{Host: "github.com", Org: "o", Repo: "r", Number: 7}
	if r.Kind() != Hub {
		t.Fatalf("Kind before an answer = %q", r.Kind())
	}
	// Before View, FetchSpec and PRURL are built from the ref alone and run nothing.
	if spec := r.FetchSpec(ref); spec != (FetchSpec{Hub: "github/o/r", Refspec: "refs/atrium/pr/7"}) {
		t.Fatalf("FetchSpec before View = %+v", spec)
	}
	if u := r.PRURL(Ref{Host: "bitbucket.org", Org: "o", Repo: "r", Number: 2}); u != "https://bitbucket.org/o/r/pull-requests/2" {
		t.Fatalf("PRURL for bitbucket = %s", u)
	}
	if len(h.asked) != 0 {
		t.Fatalf("a call before View: %+v", h.asked)
	}

	pr, err := r.View(context.Background(), ref)
	if err != nil || pr.Title != "t" {
		t.Fatalf("View = %+v, %v", pr, err)
	}
	if a := h.asked[0]; !a.Fetch || a.Diff || a.HeadOnly || a.Host != "github.com" || a.Number != 7 {
		t.Fatalf("View asked %+v", a)
	}
	if r.Kind() != GitHub {
		t.Fatalf("Kind after an answer = %q", r.Kind())
	}
	h.pr.Store, h.pr.Ref = "github/o/r", "refs/atrium/pr/7x"
	if d, err := r.Diff(context.Background(), ref); err != nil || string(d) != "d" {
		t.Fatalf("Diff = %q, %v", d, err)
	}
	if a := h.asked[1]; !a.Diff || a.Fetch {
		t.Fatalf("Diff asked %+v", a)
	}
	// The latest answer is what FetchSpec names.
	if spec := r.FetchSpec(ref); spec.Refspec != "refs/atrium/pr/7x" || spec.Remote != "" {
		t.Fatalf("FetchSpec = %+v", spec)
	}
	h.pr.PR.Head = "def"
	h.pr.Ref = "refs/other"
	if head, err := r.Head(context.Background(), ref); err != nil || head != "def" {
		t.Fatalf("Head = %q, %v", head, err)
	}
	if a := h.asked[2]; !a.HeadOnly {
		t.Fatalf("Head asked %+v", a)
	}
	// A head check is not a view: it moves nothing FetchSpec names.
	if spec := r.FetchSpec(ref); spec.Refspec != "refs/atrium/pr/7x" {
		t.Fatalf("FetchSpec after Head = %+v", spec)
	}
	if u := r.PRURL(ref); u != "https://github.com/o/r/pull/7" {
		t.Fatalf("PRURL = %s", u)
	}
	if _, err := r.Peek(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if a := h.asked[3]; a.Fetch || a.Diff || a.HeadOnly {
		t.Fatalf("Peek asked %+v", a)
	}
}

func TestTheRemoteReadsIssuesAndRepos(t *testing.T) {
	h := &hubFake{issue: HubIssue{Kind: GitHub, Issue: &Issue{Title: "bug", Body: "b"}}, repo: HubRepo{Store: "github/o/r"}}
	r := NewRemote(h.call)
	var _ IssueReader = r
	is, err := r.Issue(context.Background(), Ref{Host: "github.com", Org: "o", Repo: "r", Number: 4})
	if err != nil || is.Title != "bug" || h.paths[0] != HubIssuePath || h.asked[0].Number != 4 {
		t.Fatalf("Issue = %+v, %v, asked %v %+v", is, err, h.paths, h.asked)
	}
	name, err := r.Repo(context.Background(), "github.com", "o", "r")
	if err != nil || name != "github/o/r" || h.paths[1] != HubRepoPath {
		t.Fatalf("Repo = %q, %v", name, err)
	}
	h.repo.Store = ""
	if _, err := r.Repo(context.Background(), "github.com", "o", "r"); err == nil {
		t.Fatal("a hub that names no store was taken")
	}
	h.issue.Issue = nil
	if _, err := r.Issue(context.Background(), Ref{Number: 4}); err == nil {
		t.Fatal("an empty issue answer was taken")
	}
}

func TestTheRemoteHandsBackTheHubsRefusal(t *testing.T) {
	refusal := &HubError{Message: "the hub's gh is not logged in", Code: CodeAccess, Tool: "gh", Host: "github.com"}
	r := NewRemote((&hubFake{err: refusal}).call)
	_, err := r.View(context.Background(), Ref{Host: "github.com", Org: "o", Repo: "r", Number: 1})
	var he *HubError
	if !errors.As(err, &he) || he.Code != CodeAccess || err.Error() != refusal.Message {
		t.Fatalf("err = %v", err)
	}
	r = NewRemote((&hubFake{}).call)
	if _, err := r.View(context.Background(), Ref{Number: 1}); err == nil {
		t.Fatal("an answer with no PR was taken")
	}
}

func TestStoreNameSpellsGitHubShort(t *testing.T) {
	for in, want := range map[[3]string]string{
		{"github.com", "o", "r"}:    "github/o/r",
		{"", "o", "r"}:              "github/o/r",
		{"Bitbucket.org", "o", "r"}: "bitbucket.org/o/r",
		{" git.corp.x ", "o", "r"}:  "git.corp.x/o/r",
	} {
		if got := StoreName(in[0], in[1], in[2]); got != want {
			t.Errorf("%q = %s, want %s", in, got, want)
		}
	}
	if PRRef(12) != "refs/atrium/pr/12" {
		t.Fatal(PRRef(12))
	}
}
