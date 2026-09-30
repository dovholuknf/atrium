package gitsync

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hubFor is a way to reach a served hub without a link: a transport that dials its listener
// whatever host the forwarder names.
func hubFor(s *served) func() (http.RoundTripper, error) {
	addr := strings.TrimPrefix(s.srv.URL, "http://")
	return func() (http.RoundTripper, error) {
		return &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
			},
		}, nil
	}
}

func newSyncer(t *testing.T) (*Syncer, string) {
	root := t.TempDir()
	return &Syncer{Root: func() string { return root }}, root
}

const repo = "github/o/r"

func TestSyncAbsentRunsNothing(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	r := sy.Sync(bg, repo, false, hubFor(s))
	if r.State != StateAbsent {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "github")); !os.IsNotExist(err) {
		t.Fatal("absent created something")
	}
}

func TestSyncInitThenOKThenResigned(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	r := sy.Sync(bg, repo, true, hubFor(s))
	if r.State != StateOK || r.SHA != s.mainSHA {
		t.Fatalf("init: %+v", r)
	}
	clone := filepath.Join(root, "github", "o", "r")
	for _, b := range []string{"claude/main", "hub-main"} {
		if got := git(t, clone, "rev-parse", "refs/heads/"+b); got != s.mainSHA {
			t.Fatalf("%s at %s", b, got)
		}
	}
	// No config was written into the clone by the fetch: no remote, no url.
	cfg, _ := os.ReadFile(filepath.Join(clone, ".git", "config"))
	if strings.Contains(string(cfg), "127.0.0.1") || strings.Contains(string(cfg), "[remote") {
		t.Fatalf("config carries the transport:\n%s", cfg)
	}
	if r := sy.Sync(bg, repo, false, hubFor(s)); r.State != StateOK {
		t.Fatalf("second: %+v", r)
	}

	// A re-sign: an unrelated history under the same branch name.
	work := filepath.Join(filepath.Dir(s.bare), "resign")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, work, "init", "-q", "-b", "claude/main")
	resigned := commit(t, work, "z.txt", "resigned")
	git(t, work, "push", "-q", "-f", s.bare, "claude/main:refs/heads/claude/main")
	r = sy.Sync(bg, repo, false, hubFor(s))
	if r.State != StateOK || r.SHA != resigned {
		t.Fatalf("resign: %+v", r)
	}
	if got := git(t, clone, "rev-parse", "refs/heads/claude/main"); got != resigned {
		t.Fatalf("claude/main at %s", got)
	}
	if st := sy.Last(); len(st) != 1 || st[0].SHA != resigned {
		t.Fatalf("memory: %+v", st)
	}
}

func TestSyncBehindWhenAWorktreeHoldsClaudeMain(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	if r := sy.Sync(bg, repo, true, hubFor(s)); r.State != StateOK {
		t.Fatalf("%+v", r)
	}
	clone := filepath.Join(root, "github", "o", "r")
	wt := filepath.Join(root, "wt")
	git(t, clone, "worktree", "add", "-q", wt, "claude/main")

	work := filepath.Join(filepath.Dir(s.bare), "..", "..", "..", "work")
	next := commit(t, work, "b.txt", "two")
	git(t, work, "push", "-q", s.bare, "claude/main:refs/heads/claude/main")

	r := sy.Sync(bg, repo, false, hubFor(s))
	if r.State != StateBehind || r.SHA != next || r.Detail == "" {
		t.Fatalf("%+v", r)
	}
	if got := git(t, clone, "rev-parse", "refs/heads/claude/main"); got == next {
		t.Fatal("claude/main moved under a worktree")
	}
	// hub-main was free to move and did.
	if got := git(t, clone, "rev-parse", "refs/heads/hub-main"); got != next {
		t.Fatalf("hub-main at %s", got)
	}
}

func TestSyncBehindWhenHubMainIsCheckedOutDirty(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	if r := sy.Sync(bg, repo, true, hubFor(s)); r.State != StateOK {
		t.Fatalf("%+v", r)
	}
	clone := filepath.Join(root, "github", "o", "r")
	git(t, clone, "checkout", "-q", "hub-main")
	if err := os.WriteFile(filepath.Join(clone, "a.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(filepath.Dir(s.bare), "..", "..", "..", "work")
	next := commit(t, work, "a.txt", "changed upstream")
	git(t, work, "push", "-q", s.bare, "claude/main:refs/heads/claude/main")

	r := sy.Sync(bg, repo, false, hubFor(s))
	if r.State != StateBehind || r.SHA != next {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "a.txt")); string(b) != "dirty" {
		t.Fatalf("work tree change lost: %q", b)
	}
	// And a clean checkout of hub-main is reset to it.
	git(t, clone, "checkout", "-q", "--", "a.txt")
	if r := sy.Sync(bg, repo, false, hubFor(s)); r.State != StateOK {
		t.Fatalf("clean: %+v", r)
	}
	if got := git(t, clone, "rev-parse", "HEAD"); got != next {
		t.Fatalf("HEAD at %s", got)
	}
}

func TestSyncFailedWhenTheHubDidNotSayGit(t *testing.T) {
	sy, root := newSyncer(t)
	if err := os.MkdirAll(filepath.Join(root, "github", "o", "r"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, filepath.Join(root, "github", "o", "r"), "init", "-q")
	r := sy.Sync(bg, repo, false, func() (http.RoundTripper, error) {
		return nil, errors.New("this hub predates git sync")
	})
	if r.State != StateFailed || !strings.Contains(r.Detail, "predates") {
		t.Fatalf("%+v", r)
	}
	if r := sy.Sync(bg, repo, false, nil); r.State != StateFailed {
		t.Fatalf("nil: %+v", r)
	}
}

func TestSyncFailedWhenTheFetchFails(t *testing.T) {
	s := freshServed(t)
	sy, _ := newSyncer(t)
	r := sy.Sync(bg, "github/o/unknown", true, hubFor(s))
	if r.State != StateFailed || r.Detail == "" || r.SHA != "" {
		t.Fatalf("%+v", r)
	}
}

// The room's handler, on a server of its own.
func roomServer(t *testing.T, s *served) (*httptest.Server, string) {
	sy, root := newSyncer(t)
	h := &RoomHandler{Syncer: sy, Hub: hubFor(s)}
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	return srv, root
}

func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b bytes.Buffer
	_, _ = b.ReadFrom(res.Body)
	return res.StatusCode, b.String()
}

func TestSyncRequestCannotNameASourceBranch(t *testing.T) {
	s := freshServed(t)
	srv, root := roomServer(t, s)
	for _, body := range []string{
		`{"name":"github/o/r","init":true,"branch":"claude/ui"}`,
		`{"name":"github/o/r","init":true,"ref":"refs/heads/claude/ui"}`,
		`{"name":"github/o/r","init":true,"url":"http://example.com/x.git"}`,
	} {
		if code, out := post(t, srv.URL+"/v1/git/sync", body); code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", body, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "github")); !os.IsNotExist(err) {
		t.Fatal("a refused request still ran")
	}
	code, out := post(t, srv.URL+"/v1/git/sync", `{"name":"github/o/r","init":true}`)
	if code != 200 || !strings.Contains(out, `"ok"`) {
		t.Fatalf("%d %s", code, out)
	}
	code, out = get(t, srv.URL+"/v1/git/status")
	if code != 200 || !strings.Contains(out, `"state":"ok"`) {
		t.Fatalf("status %d %s", code, out)
	}
}

func TestStatusBeforeAnySync(t *testing.T) {
	s := freshServed(t)
	srv, _ := roomServer(t, s)
	if _, out := get(t, srv.URL+"/v1/git/status"); !strings.Contains(out, `"none"`) {
		t.Fatal(out)
	}
}

func TestTheRoomServesClaudeBranchesExceptMainAndNothingElse(t *testing.T) {
	s := freshServed(t)
	srv, root := roomServer(t, s)
	if code, out := post(t, srv.URL+"/v1/git/sync", `{"name":"github/o/r","init":true}`); code != 200 {
		t.Fatal(out)
	}
	clone := filepath.Join(root, "github", "o", "r")
	git(t, clone, "checkout", "-q", "-b", "claude/w1", "claude/main")
	w1 := commit(t, clone, "w.txt", "worker")
	git(t, clone, "checkout", "-q", "-b", "other", "claude/main")
	other := commit(t, clone, "o.txt", "private")
	git(t, clone, "checkout", "-q", "claude/w1")

	url := srv.URL + "/v1/git/github/o/r.git"
	refs := git(t, clone, "ls-remote", url)
	if !strings.Contains(refs, "refs/heads/claude/w1") {
		t.Fatalf("claude/w1 not offered:\n%s", refs)
	}
	for _, bad := range []string{"refs/heads/claude/main", "refs/heads/other", "refs/heads/hub-main", "refs/remotes/hub", "HEAD"} {
		if strings.Contains(refs, bad) {
			t.Fatalf("%s is offered:\n%s", bad, refs)
		}
	}

	// A hidden sha by id, the client asking for v2, must fail.
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	if _, err := Default.Git(bg, dst, "-c", "protocol.version=2", "fetch", "--no-tags", url, other); err == nil {
		t.Fatal("a hidden sha was fetched by id")
	}
	if _, err := Default.Git(bg, dst, "-c", "protocol.version=2", "fetch", "--no-tags", url, "+refs/heads/claude/w1:refs/heads/w1"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dst, "rev-parse", "w1"); got != w1 {
		t.Fatalf("got %s", got)
	}

	// The dumb protocol, and a push.
	for _, p := range []string{"/info/refs", "/HEAD", "/objects/info/packs"} {
		if code, _ := get(t, url+p); code != 403 && code != 404 {
			t.Fatalf("GET %s: %d", p, code)
		}
	}
	if _, err := Default.Git(bg, clone, "push", url, "claude/w1:refs/heads/x"); err == nil {
		t.Fatal("a push was accepted")
	}
	// Serving wrote no policy into the clone.
	cfg, _ := os.ReadFile(filepath.Join(clone, ".git", "config"))
	if strings.Contains(string(cfg), "hideRefs") || strings.Contains(string(cfg), "receivepack") {
		t.Fatalf("policy in config:\n%s", cfg)
	}
	// An unknown clone is a 404.
	if code, _ := get(t, srv.URL+"/v1/git/github/o/nope.git/info/refs?service=git-upload-pack"); code != 404 {
		t.Fatalf("unknown: %d", code)
	}
}

// Only a repository the hub has synced is served. A clone that is on disk but was never synced,
// or whose sync was refused, is a 404 even to the hub.
func TestTheRoomServesOnlyWhatTheHubHasSynced(t *testing.T) {
	s := freshServed(t)
	srv, root := roomServer(t, s)
	url := srv.URL + "/v1/git/github/o/r.git/info/refs?service=git-upload-pack"

	// A clone that exists (somebody's own checkout) and was never synced.
	clone := filepath.Join(root, "github", "o", "r")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "init", "-q", "-b", "claude/private")
	commit(t, clone, "p.txt", "private")
	if code, _ := get(t, url); code != 404 {
		t.Fatalf("an unsynced clone answered %d", code)
	}

	// A sync the room refused (another repository's origin) does not make it served.
	git(t, clone, "remote", "add", "origin", "https://github.com/someone/else.git")
	post(t, srv.URL+"/v1/git/sync", `{"name":"github/o/r"}`)
	if code, _ := get(t, url); code != 404 {
		t.Fatalf("a refused clone answered %d", code)
	}

	// Once the hub has synced it, it is served.
	git(t, clone, "remote", "set-url", "origin", "git@github.com:o/r.git")
	if code, out := post(t, srv.URL+"/v1/git/sync", `{"name":"github/o/r"}`); code != 200 || !strings.Contains(out, `"ok"`) {
		t.Fatalf("%d %s", code, out)
	}
	if code, _ := get(t, url); code != 200 {
		t.Fatalf("a synced clone answered %d", code)
	}
	// And a name the hub never asked about is 404 whatever is on disk.
	other := filepath.Join(root, "github", "o", "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q")
	if code, _ := get(t, srv.URL+"/v1/git/github/o/other.git/info/refs?service=git-upload-pack"); code != 404 {
		t.Fatalf("a name never synced answered %d", code)
	}
}

func TestCleanLocksRemovesOnlyOldOwnedOnes(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	mk := func(rel string, age time.Time) string {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, age, age); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldOwned := mk("refs/remotes/hub/claude/main.lock", old)
	oldHead := mk("refs/heads/claude/main.lock", old)
	oldHub := mk("refs/heads/hub-main.lock", old)
	fresh := mk("refs/remotes/hub/other.lock", time.Now())
	elsewhere := mk("refs/heads/feature.lock", old)
	packed := mk("packed-refs.lock", old)

	removed, left := CleanLocks(dir, RoomOwned, 10*time.Minute)
	if len(removed) != 3 {
		t.Fatalf("removed %v", removed)
	}
	for _, p := range []string{oldOwned, oldHead, oldHub} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived", p)
		}
	}
	for _, p := range []string{fresh, elsewhere, packed} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed", p)
		}
	}
	if len(left) != 2 || !strings.Contains(left[0]+left[1], "File exists") {
		t.Fatalf("left %v", left)
	}
	// A hub-style owner lists the whole of refs and packed-refs.
	if removed, left := CleanLocks(dir, []string{"refs", "packed-refs"}, 10*time.Minute); len(removed) != 2 || len(left) != 0 {
		t.Fatalf("removed %v left %v", removed, left)
	}
}

func TestSyncClearsAStaleOwnedLockBeforeFetching(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	if r := sy.Sync(bg, repo, true, hubFor(s)); r.State != StateOK {
		t.Fatalf("%+v", r)
	}
	clone := filepath.Join(root, "github", "o", "r")
	lock := filepath.Join(clone, ".git", "refs", "remotes", "hub", "claude", "main.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(lock, old, old)
	work := filepath.Join(filepath.Dir(s.bare), "..", "..", "..", "work")
	commit(t, work, "c.txt", "three")
	git(t, work, "push", "-q", s.bare, "claude/main:refs/heads/claude/main")
	if r := sy.Sync(bg, repo, false, hubFor(s)); r.State != StateOK {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("stale lock left")
	}
}

func TestCheckGit(t *testing.T) {
	v, err := Default.CheckGit(bg)
	if err != nil {
		t.Skipf("this machine's git is not usable for sync: %v", err)
	}
	if !strings.HasPrefix(v, "git version ") {
		t.Fatal(v)
	}
}
