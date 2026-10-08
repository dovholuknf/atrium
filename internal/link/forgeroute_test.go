package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// fakeForge is the hub's forge with no CLI: it answers what it is set to, and records the asks.
type fakeForge struct {
	mu     sync.Mutex
	kind   string
	pr     forge.PR
	err    error
	remote string
	views  int
}

func (f *fakeForge) Kind() string { return f.kind }
func (f *fakeForge) View(_ context.Context, _ forge.Ref) (*forge.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.views++
	if f.err != nil {
		return nil, f.err
	}
	pr := f.pr
	return &pr, nil
}
func (f *fakeForge) Diff(context.Context, forge.Ref) ([]byte, error) {
	return []byte("diff --git a b"), f.err
}
func (f *fakeForge) Head(context.Context, forge.Ref) (string, error) { return f.pr.Head, f.err }
func (f *fakeForge) FetchSpec(ref forge.Ref) forge.FetchSpec {
	return forge.FetchSpec{Remote: f.remote, Refspec: "refs/pull/7/head"}
}
func (f *fakeForge) PRURL(forge.Ref) string { return "https://github.com/o/r/pull/7" }
func (f *fakeForge) Issue(context.Context, forge.Ref) (*forge.Issue, error) {
	return &forge.Issue{Title: "an issue", Body: "b"}, f.err
}

// liveGrowls is a growl store that holds what was raised until it is ended, so openIn sees it.
type liveGrowls struct {
	fakeGrowls
}

func (l *liveGrowls) Live() ([]GrowlRow, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []GrowlRow
	for _, r := range l.raised {
		gone := false
		for _, e := range l.ended {
			gone = gone || e == r.ID
		}
		if !gone {
			out = append(out, r)
		}
	}
	return out, nil
}

// forgeHub is a storeProxy with the forge wired and a growl store that records the alert.
type forgeHub struct {
	*storeProxy
	ff     *fakeForge
	growls *liveGrowls
	hosts  []string
}

func newForgeHub(t *testing.T) *forgeHub {
	t.Helper()
	x := &forgeHub{storeProxy: newStoreProxy(t), growls: &liveGrowls{}}
	x.ff = &fakeForge{kind: forge.GitHub, remote: x.forge, pr: forge.PR{Title: "t", Head: "h", BaseRef: "master"}}
	x.p.SetForge(&memSettings{m: map[string]string{}}, func(context.Context, forge.Cmd) ([]byte, error) {
		return nil, errors.New("no CLI in a test")
	})
	x.p.forgeSide().forgeOf = func(host string) (forge.Forge, error) {
		x.hosts = append(x.hosts, host)
		if host != "github.com" {
			return nil, &forge.NoForgeError{Host: host}
		}
		return x.ff, nil
	}
	x.p.mu.Lock()
	x.p.growl = NewGrowler(x.growls)
	x.p.mu.Unlock()
	return x
}

// ask is a room's forge question as serveGit hands it on, with the room the hello named.
func (x *forgeHub) ask(path string, a forge.HubAsk) (int, string) {
	raw, _ := json.Marshal(a)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://hub"+path, strings.NewReader(string(raw)))
	req.Header.Set(ForgeRoomHeader, "alpha")
	x.p.serveForge(rec, req)
	return rec.Code, rec.Body.String()
}

var prAsk = forge.HubAsk{Host: "github.com", Org: "o", Repo: "r", Number: 7}

// THE HUB FETCHES THE HEAD INTO ITS STORE under refs/atrium/pr/<N>, so the room reads it as any other ref.
func TestTheHubFetchesAPullRequestHeadIntoItsStore(t *testing.T) {
	x := newForgeHub(t)
	work := filepath.Join(filepath.Dir(x.forge), "work")
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("pr"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "-A")
	gitRun(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "pr")
	gitRun(t, work, "push", "-q", x.forge, "HEAD:refs/pull/7/head")
	head := gitRun(t, work, "rev-parse", "HEAD")

	a := prAsk
	a.Fetch, a.Diff = true, true
	code, body := x.ask(forge.HubPRPath, a)
	if code != 200 {
		t.Fatalf("answer = %d %s", code, body)
	}
	var ans forge.HubPR
	if err := json.Unmarshal([]byte(body), &ans); err != nil {
		t.Fatal(err)
	}
	if ans.Kind != forge.GitHub || ans.Store != "github/o/r" || ans.Ref != "refs/atrium/pr/7" || ans.PR.Title != "t" ||
		string(ans.Diff) != "diff --git a b" || ans.URL != "https://github.com/o/r/pull/7" {
		t.Fatalf("answer = %+v", ans)
	}
	dir, _ := x.g.Store().Path("github/o/r")
	if got := gitRun(t, dir, "rev-parse", "refs/atrium/pr/7"); got != head {
		t.Fatalf("refs/atrium/pr/7 = %s, want %s", got, head)
	}
	if len(x.growls.raised) != 0 {
		t.Fatalf("an alert for a fetch that worked: %+v", x.growls.raised)
	}
}

// THE FORGE IS PICKED BY HOST on the hub, and a host it has no forge for is said so, not run.
func TestTheHubPicksTheForgeByHost(t *testing.T) {
	x := newForgeHub(t)
	a := prAsk
	a.Host = "GitLab.Example.com"
	code, body := x.ask(forge.HubPRPath, a)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, `"code":"no_forge"`) {
		t.Fatalf("answer = %d %s", code, body)
	}
	if len(x.hosts) != 1 || x.hosts[0] != "gitlab.example.com" {
		t.Fatalf("hosts asked = %v", x.hosts)
	}
	if code, body := x.ask(forge.HubIssuePath, prAsk); code != 200 || !strings.Contains(body, `"title":"an issue"`) {
		t.Fatalf("issue = %d %s", code, body)
	}
}

func TestTheHubRefusesABadQuestion(t *testing.T) {
	x := newForgeHub(t)
	for why, a := range map[string]forge.HubAsk{
		"no host":       {Org: "o", Repo: "r", Number: 1},
		"an option org": {Host: "github.com", Org: "-o", Repo: "r", Number: 1},
		"dotdot repo":   {Host: "github.com", Org: "o", Repo: "..", Number: 1},
		"a slash":       {Host: "github.com", Org: "o/x", Repo: "r", Number: 1},
		"no number":     {Host: "github.com", Org: "o", Repo: "r"},
		"a bad host":    {Host: "github.com/x", Org: "o", Repo: "r", Number: 1},
	} {
		if code, body := x.ask(forge.HubPRPath, a); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", why, code, body)
		}
	}
	if x.ff.views != 0 {
		t.Fatalf("the forge was run %d times for bad questions", x.ff.views)
	}
}

// A HUB CLI THAT IS NOT LOGGED IN IS THE HUB'S ALERT: one growler, hung on the room that asked, the sentence names the
// hub, and the next answer that works ends it.
func TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess(t *testing.T) {
	x := newForgeHub(t)
	x.ff.err = &forge.AccessError{Tool: "gh", Host: "github.com", Detail: "not logged in"}
	for i := 0; i < 3; i++ {
		code, body := x.ask(forge.HubPRPath, prAsk)
		if code != http.StatusBadGateway || !strings.Contains(body, `"code":"access"`) || !strings.Contains(body, "on the hub") {
			t.Fatalf("answer = %d %s", code, body)
		}
	}
	if len(x.growls.raised) != 1 {
		t.Fatalf("raised = %+v, want one", x.growls.raised)
	}
	g := x.growls.raised[0]
	if g.Room != "alpha" || !strings.HasPrefix(g.ID, "forge|gh@github.com|") || !strings.Contains(g.Body, "gh auth login") {
		t.Fatalf("alert = %+v", g)
	}
	x.ff.err = nil
	if code, body := x.ask(forge.HubPRPath, prAsk); code != 200 {
		t.Fatalf("answer = %d %s", code, body)
	}
	if len(x.growls.ended) != 1 || x.growls.ended[0] != g.ID {
		t.Fatalf("ended = %v, want %s", x.growls.ended, g.ID)
	}
	// A new failure is a new alert under its own id.
	x.ff.err = &forge.AccessError{Tool: "gh", Host: "github.com"}
	x.ask(forge.HubPRPath, prAsk)
	if len(x.growls.raised) != 2 || x.growls.raised[1].ID == g.ID {
		t.Fatalf("raised = %+v", x.growls.raised)
	}
}

// A HEAD FETCH THE FORGE REFUSED A CREDENTIAL FOR raises the same alert (fix-up c). Any other fetch failure is said
// and raises nothing.
func TestAnAuthFailureFetchingTheHeadRaisesTheAlert(t *testing.T) {
	x := newForgeHub(t)
	f := x.p.forgeSide()
	code, he := x.p.forgeRefusal(f, "beta", prAsk, &forgeAccess{kind: forge.GitHub,
		err: &gitsync.FetchError{Auth: true, Msg: "the hub could not read the head"}})
	if code != http.StatusBadGateway || he.Code != forge.CodeAccess || he.Tool != "gh" ||
		!strings.Contains(he.Message, "the hub could not read the head") || !strings.Contains(he.Message, "on the hub") {
		t.Fatalf("refusal = %d %+v", code, he)
	}
	if len(x.growls.raised) != 1 || x.growls.raised[0].Room != "beta" {
		t.Fatalf("raised = %+v", x.growls.raised)
	}
	_, he = x.p.forgeRefusal(f, "beta", prAsk, &forgeAccess{kind: forge.GitHub,
		err: &gitsync.FetchError{Msg: "couldn't find remote ref"}})
	if he.Code != forge.CodeFetch || len(x.growls.raised) != 1 {
		t.Fatalf("a plain fetch failure: %+v, raised %+v", he, x.growls.raised)
	}
}

// THE OPERATOR'S CHECK ANSWERS AND RAISES NOTHING, since no room is waiting on it.
func TestTheHubForgeCheckAnswersAndRaisesNothing(t *testing.T) {
	x := newForgeHub(t)
	code, body := x.call(http.MethodPost, "/_hub/forge/check", `{"forges":[{"tool":"gh"},{"tool":"curl"}]}`, "")
	if code != 200 || !strings.Contains(body, `"state":"logged_out"`) || !strings.Contains(body, `"state":"unknown"`) ||
		!strings.Contains(body, "gh auth login --hostname github.com") {
		t.Fatalf("check = %d %s", code, body)
	}
	if len(x.growls.raised) != 0 {
		t.Fatalf("the check raised %+v", x.growls.raised)
	}
}

// THE HUB'S ENTRIES PER HOST are the operator's, held to a host, a forge and a command name.
func TestTheHubForgeEntries(t *testing.T) {
	x := newForgeHub(t)
	if code, body := x.call(http.MethodPut, "/_hub/forge", `{"entries":[{"Host":"Git.Corp.X","Forge":"github","Cmd":"ghe"}]}`, ""); code != 200 {
		t.Fatalf("put = %d %s", code, body)
	}
	if tool, cmd := x.p.forgeSide().toolOf(forge.GitHub, "git.corp.x"); tool != "gh" || cmd != "ghe" {
		t.Fatalf("toolOf = %s %s", tool, cmd)
	}
	for _, bad := range []string{
		`{"entries":[{"Host":"a b","Forge":"github"}]}`,
		`{"entries":[{"Host":"x.y","Forge":"svn"}]}`,
		`{"entries":[{"Host":"x.y","Forge":"github","Cmd":"/usr/bin/gh"}]}`,
		`{"entries":[{"Host":"x.y","Forge":"github","Cmd":"gh --x"}]}`,
	} {
		if code, _ := x.call(http.MethodPut, "/_hub/forge", bad, ""); code != http.StatusBadRequest {
			t.Errorf("%s = %d", bad, code)
		}
	}
	if code, body := x.call(http.MethodGet, "/_hub/forge", "", ""); code != 200 || !strings.Contains(body, "ghe") {
		t.Fatalf("get = %d %s", code, body)
	}
}

// A ROOM ASKS OVER ITS LINK: the hub names the asking room from the hello, and a refusal reaches the room as a
// HubError with the hub's sentence.
func TestARoomAsksTheHubOverItsLink(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0})
	defer x.done()
	ff := &fakeForge{kind: forge.GitHub, pr: forge.PR{Title: "over the link", Head: "h"}}
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	x.proxy.forgeSide().forgeOf = func(string) (forge.Forge, error) { return ff, nil }
	growls := &liveGrowls{}
	x.proxy.mu.Lock()
	x.proxy.growl = NewGrowler(growls)
	x.proxy.mu.Unlock()

	r := forge.NewRemote(x.rooms["alpha"].room.Forge)
	ref := forge.Ref{Host: "github.com", Org: "o", Repo: "r", Number: 7}
	pr, err := r.Peek(context.Background(), ref)
	if err != nil || pr.Title != "over the link" {
		t.Fatalf("Peek = %+v, %v", pr, err)
	}
	ff.err = &forge.AccessError{Tool: "gh", Host: "github.com"}
	_, err = r.Peek(context.Background(), ref)
	var he *forge.HubError
	if !errors.As(err, &he) || he.Code != forge.CodeAccess {
		t.Fatalf("err = %v", err)
	}
	if len(growls.raised) != 1 || growls.raised[0].Room != "alpha" {
		t.Fatalf("raised = %+v", growls.raised)
	}
}

// A HUB WITH NO FORGE WIRED answers 404, which the room says in a sentence and does not fall back on.
func TestARoomWhoseHubRunsNoForgeIsToldSo(t *testing.T) {
	x := newClaimHub(t, map[string]int{"alpha": 0})
	defer x.done()
	var out forge.HubPR
	err := x.rooms["alpha"].room.Forge(context.Background(), forge.HubPRPath, prAsk, &out)
	if err == nil || !strings.Contains(err.Error(), "update the hub") {
		t.Fatalf("err = %v", err)
	}
}

// A PASTE'S RECOGNISE AND ITS OPEN ASK THE FORGE ONCE, not twice: the open takes the recognise's read, once.
func TestAPeekIsTakenOnceByTheFetchThatFollowsIt(t *testing.T) {
	x := newForgeHub(t)
	if code, body := x.ask(forge.HubPRPath, prAsk); code != 200 {
		t.Fatalf("peek = %d %s", code, body)
	}
	a := prAsk
	a.Fetch = true
	// no head in the forge's remote, so the fetch itself fails, but the read must not have been asked for again
	x.ask(forge.HubPRPath, a)
	if x.ff.views != 1 {
		t.Fatalf("the forge was read %d times for a peek and the open after it", x.ff.views)
	}
	x.ask(forge.HubPRPath, a)
	if x.ff.views != 2 {
		t.Fatalf("a second open reused a read it should not have: %d", x.ff.views)
	}
}
