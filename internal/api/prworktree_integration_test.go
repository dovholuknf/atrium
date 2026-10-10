//go:build integration

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

type fakeForge struct {
	pr   forge.PR
	err  error
	spec forge.FetchSpec
}

func (f *fakeForge) Kind() string { return forge.GitHub }

func (f *fakeForge) View(context.Context, forge.Ref) (*forge.PR, error) {
	if f.err != nil {
		return nil, f.err
	}
	p := f.pr
	return &p, nil
}

func (f *fakeForge) Diff(context.Context, forge.Ref) ([]byte, error) { return nil, nil }

func (f *fakeForge) Head(context.Context, forge.Ref) (string, error) { return "", nil }

func (f *fakeForge) FetchSpec(forge.Ref) forge.FetchSpec { return f.spec }

func (f *fakeForge) PRURL(forge.Ref) string { return "" }

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t",
		"-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// upstream is a local repo with one commit and refs/pull/7/head one commit further on.
func upstream(t *testing.T) string {
	t.Helper()
	up := filepath.Join(t.TempDir(), "up")
	mkDir(t, up)
	git(t, up, "init", "-q", "-b", "main")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, up, "checkout", "-q", "-b", "tmp")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "pr")
	git(t, up, "update-ref", "refs/pull/7/head", "HEAD")
	git(t, up, "checkout", "-q", "main")
	git(t, up, "branch", "-q", "-D", "tmp")
	return up
}

type prHarness struct {
	srv    *Server
	h      http.Handler
	wt     string
	root   string
	up     string
	clones int
}

func newPRHarness(t *testing.T, f *fakeForge) *prHarness {
	t.Helper()
	base := t.TempDir()
	hn := &prHarness{wt: filepath.Join(base, "wt"), root: filepath.Join(base, "git"), up: upstream(t)}
	mkDir(t, hn.root)
	srv, st, h := serverFor(t)
	hn.srv, hn.h = srv, h
	if _, err := st.SaveProvider(store.Provider{
		Name: "github", Root: filepath.ToSlash(hn.root), Enabled: true,
		Worktrees: true, WorktreeRoot: filepath.ToSlash(hn.wt),
	}); err != nil {
		t.Fatal(err)
	}
	f.spec = forge.FetchSpec{Remote: hn.up, Refspec: "pull/7/head"}
	srv.PRForge = func(string) (forge.Forge, error) { return f, nil }
	srv.PRFetch = func(ctx context.Context, dir string, spec forge.FetchSpec, dst string) error {
		cmd := exec.CommandContext(ctx, "git", "-c", "protocol.file.allow=always", "fetch", spec.Remote,
			"+"+spec.Refspec+":"+dst)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return errors.New(string(out))
		}
		return nil
	}
	return hn
}

// checkout puts a clone of upstream where the provider expects one.
func (hn *prHarness) checkout(t *testing.T, org, repo string) string {
	dir := filepath.Join(hn.root, org, repo)
	mkDir(t, filepath.Dir(dir))
	git(t, hn.root, "clone", "-q", hn.up, dir)
	return dir
}

func (hn *prHarness) ask(t *testing.T) (int, map[string]any) {
	rec := post(t, hn.h, "/v1/providers/github/pr-worktree", map[string]any{"org": "o", "repo": "r", "number": 7})
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestPRWorktreeSameRepoUsesTheRealBranch(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	hn.checkout(t, "o", "r")
	code, out := hn.ask(t)
	if code != 200 || out["branch"] != "feat/x" || out["existed"] != false {
		t.Fatalf("%d %v", code, out)
	}
	path := out["path"].(string)
	if !strings.HasSuffix(path, "/o/r/feat-x") {
		t.Errorf("path %s", path)
	}
	if got := git(t, path, "log", "-1", "--format=%s"); got != "pr" {
		t.Errorf("the worktree is not at the PR head: %q", got)
	}
	if got := git(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/x" {
		t.Errorf("branch %q", got)
	}
}

func TestPRWorktreeForkUsesPRNumber(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "main", FromFork: true}})
	hn.checkout(t, "o", "r")
	code, out := hn.ask(t)
	if code != 200 || out["branch"] != "pr-7" {
		t.Fatalf("%d %v", code, out)
	}
	if got := git(t, out["path"].(string), "rev-parse", "--abbrev-ref", "HEAD"); got != "pr-7" {
		t.Errorf("branch %q", got)
	}
}

func TestPRWorktreeSecondCallIsTheAnswer(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	hn.checkout(t, "o", "r")
	_, first := hn.ask(t)
	code, second := hn.ask(t)
	if code != 200 || second["existed"] != true || real(second["path"]) != real(first["path"]) {
		t.Fatalf("%d %v then %v", code, first, second)
	}
}

func TestPRWorktreeWithNoCheckoutGoesThroughTheClonePath(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	scm := filepath.Join(t.TempDir(), "scm", "o", "r")
	hn.srv.SCMClone = func(_ context.Context, url string) (gitsync.SCMResult, error) {
		hn.clones++
		if url != "https://github.com/o/r" {
			t.Errorf("clone url %q", url)
		}
		if _, err := os.Stat(filepath.Join(scm, ".git")); err != nil {
			mkDir(t, filepath.Dir(scm))
			git(t, filepath.Dir(scm), "clone", "-q", hn.up, scm)
		}
		return gitsync.SCMResult{Path: filepath.ToSlash(scm), State: "cloned"}, nil
	}
	code, out := hn.ask(t)
	if code != 200 || hn.clones != 1 {
		t.Fatalf("%d %v clones=%d", code, out, hn.clones)
	}
	path := out["path"].(string)
	if !strings.HasPrefix(path, filepath.ToSlash(hn.wt)) {
		t.Errorf("the worktree is not under the worktree root: %s", path)
	}
	if got := git(t, path, "log", "-1", "--format=%s"); got != "pr" {
		t.Errorf("head %q", got)
	}
	// And it is idempotent through the clone too.
	if _, again := hn.ask(t); again["existed"] != true {
		t.Errorf("second call: %v", again)
	}
}

func TestPRWorktreeCloneFailureIsTheExactSentence(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat/x"}})
	hn.srv.SCMClone = func(context.Context, string) (gitsync.SCMResult, error) {
		return gitsync.SCMResult{}, errors.New(gitsync.CloneFailed)
	}
	code, out := hn.ask(t)
	if code != http.StatusBadRequest || out["error"] != gitsync.CloneFailed {
		t.Fatalf("%d %v", code, out)
	}
	if _, err := os.Stat(hn.wt); err == nil {
		t.Errorf("a failed clone left a worktree folder")
	}
}

func TestPRWorktreeAccessErrorIsUnchangedAndNothingIsCloned(t *testing.T) {
	ae := &forge.AccessError{Tool: "gh", Host: "github.com", NotInstalled: true}
	hn := newPRHarness(t, &fakeForge{err: ae})
	hn.srv.SCMClone = func(context.Context, string) (gitsync.SCMResult, error) {
		hn.clones++
		return gitsync.SCMResult{}, errors.New("must not clone")
	}
	code, out := hn.ask(t)
	if code != http.StatusBadRequest || out["error"] != ae.Error() || hn.clones != 0 {
		t.Fatalf("%d %v clones=%d", code, out, hn.clones)
	}
}

// real resolves symlinks, since git reports /private/var where the test made /var.
func real(v any) string {
	p, _ := v.(string)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestPRWorktreeRaisesAndClearsTheForgeAlert(t *testing.T) {
	ae := &forge.AccessError{Tool: "gh", Host: "github.com"}
	hn := newPRHarness(t, &fakeForge{err: ae})
	var raised error
	hn.srv.ForgeFailed = func(kind string, err error) bool { raised = err; return true }
	hn.srv.ForgeWorked = func(string, string) { t.Errorf("cleared on a failure") }
	hn.ask(t)
	if raised != ae {
		t.Fatalf("raised %v", raised)
	}

	hn = newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat"}})
	hn.checkout(t, "o", "r")
	var kind, host string
	hn.srv.ForgeWorked = func(k, h string) { kind, host = k, h }
	hn.ask(t)
	if kind != forge.GitHub || host != "github.com" {
		t.Fatalf("worked %q %q", kind, host)
	}
}

func TestCredentialHelperIsTheForgeCLIsOwn(t *testing.T) {
	if got := credentialHelper(forge.GitHub, ""); got != "!gh auth git-credential" {
		t.Errorf("%q", got)
	}
	if got := credentialHelper(forge.GitHub, "ghw"); got != "!ghw auth git-credential" {
		t.Errorf("%q", got)
	}
	if got := credentialHelper(forge.Bitbucket, ""); got != "" {
		t.Errorf("%q", got)
	}
	spec := forge.FetchSpec{Remote: "https://github.com/o/r.git", Refspec: "pull/7/head"}
	args := strings.Join(fetchArgs(spec, "refs/atrium/pr/7", "!gh auth git-credential"), " ")
	want := "-c credential.helper= -c credential.helper=!gh auth git-credential fetch --no-tags -- " +
		"https://github.com/o/r.git +pull/7/head:refs/atrium/pr/7"
	if !strings.Contains(args, want) || !strings.Contains(args, "protocol.https.allow=always") {
		t.Errorf("%s", args)
	}
	if strings.Contains(strings.Join(fetchArgs(spec, "x", ""), " "), "credential") {
		t.Errorf("a forge with no helper changed the credentials")
	}
}

// A failed fetch still answers the one sentence, and the helper reaches the default fetch from the provider.
func TestPRWorktreeFetchFailureSentence(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat"}})
	hn.checkout(t, "o", "r")
	hn.srv.PRFetch = func(context.Context, string, forge.FetchSpec, string) error {
		return errors.New("fatal: could not read Username")
	}
	code, out := hn.ask(t)
	if code != http.StatusBadRequest || out["error"] != "could not fetch the head of pull request 7: fatal: could not read Username" {
		t.Fatalf("%d %v", code, out)
	}
}

// A HEAD IN THE HUB'S STORE is fetched over the room's loopback, http alone and with no credential helper, even when
// the forge has one.
func TestAHubHeadIsFetchedWithNoCredential(t *testing.T) {
	spec := forge.FetchSpec{Remote: "http://127.0.0.1:9/tok/git/hub/github/o/r.git", Refspec: "refs/atrium/pr/7",
		Hub: "github/o/r"}
	args := strings.Join(fetchArgs(spec, "refs/atrium/pr/7", "!gh auth git-credential"), " ")
	if strings.Contains(args, "credential") || strings.Contains(args, "https.allow") ||
		!strings.Contains(args, "protocol.http.allow=always") ||
		!strings.HasSuffix(args, "-- "+spec.Remote+" +refs/atrium/pr/7:refs/atrium/pr/7") {
		t.Fatalf("%s", args)
	}
}

// A ROOM WITH A HUB reads the head through the hub's store and never from the forge, and says why when it cannot.
func TestPRWorktreeWithAHubReadsTheHeadFromTheHubsStore(t *testing.T) {
	f := &fakeForge{pr: forge.PR{HeadRef: "feat"}}
	hn := newPRHarness(t, f)
	hn.checkout(t, "o", "r")
	f.spec = forge.FetchSpec{Hub: "github/o/r", Refspec: "refs/atrium/pr/7"}
	hn.srv.PRFetch = nil
	var asked []string
	hn.srv.HubSource = func(_ context.Context, name string) (string, func(), error) {
		asked = append(asked, name)
		return "", nil, errors.New("the hub cannot be asked right now")
	}
	code, out := hn.ask(t)
	if code != http.StatusBadRequest || !strings.Contains(real(out["error"]), "the hub cannot be asked right now") {
		t.Fatalf("%d %v", code, out)
	}
	if len(asked) != 1 || asked[0] != "github/o/r" {
		t.Fatalf("asked %v", asked)
	}
}

// THE HUB PLACES BY HOST, so a provider with another name on this room still takes the PR.
func TestPRWorktreeFindsTheProviderByHost(t *testing.T) {
	hn := newPRHarness(t, &fakeForge{pr: forge.PR{HeadRef: "feat"}})
	hn.checkout(t, "o", "r")
	rec := post(t, hn.h, "/v1/providers/gh/pr-worktree",
		map[string]any{"host": "github.com", "org": "o", "repo": "r", "number": 7})
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = post(t, hn.h, "/v1/providers/gh/pr-worktree",
		map[string]any{"host": "bitbucket.org", "org": "o", "repo": "r", "number": 7})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another host = %d %s", rec.Code, rec.Body.String())
	}
}
