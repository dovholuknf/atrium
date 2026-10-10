//go:build integration

package gitsync

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRooms is one attached room whose handler is an httptest server.
type fakeRooms struct {
	srv  *httptest.Server
	name string
	host string
	git  bool
	up   bool
}

func (f *fakeRooms) Attached() []RoomInfo {
	if !f.up {
		return nil
	}
	return []RoomInfo{{Name: f.name, Host: f.host, Git: f.git}}
}

func (f *fakeRooms) Transport(string) http.RoundTripper {
	addr := f.srv.Listener.Addr().String()
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}}
}

type hubFixture struct {
	h        *Hub
	rooms    *fakeRooms
	checkout string
	roomWork string
	syncs    atomic.Int32
	answer   atomic.Value // string state the fake room answers to /v1/git/sync
	requests atomic.Int32
	lastSync atomic.Value // the body of the last sync
	hub      string
}

func newHubFixture(t *testing.T, roomGit bool) *hubFixture {
	t.Helper()
	root := t.TempDir()
	x := &hubFixture{checkout: filepath.Join(root, "checkout"), roomWork: filepath.Join(root, "roomwork"), hub: filepath.Join(root, "hub")}
	for _, d := range []string{x.checkout, x.roomWork, x.hub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, x.checkout, "init", "-q", "-b", "claude/main")
	commit(t, x.checkout, "a.txt", "one")
	git(t, x.roomWork, "init", "-q", "-b", "claude/main")
	commit(t, x.roomWork, "r.txt", "room")
	x.answer.Store("ok")

	roomGitBackend := &Backend{
		Prefix:  "/v1/git",
		Resolve: func(name string) (string, bool) { return filepath.Join(x.roomWork, ".git"), name == "github/o/r" },
		// No hideRefs at all, so claude/main IS advertised and only the hub's own refspec keeps
		// it out.
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/git/sync", func(w http.ResponseWriter, r *http.Request) {
		x.syncs.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b, _ := json.Marshal(body)
		x.lastSync.Store(string(b))
		_ = json.NewEncoder(w).Encode(map[string]string{"state": x.answer.Load().(string), "sha": "abc", "detail": "said so"})
	})
	mux.Handle("/v1/git/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x.requests.Add(1)
		roomGitBackend.ServeHTTP(w, r)
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	x.rooms = &fakeRooms{srv: srv, name: "SG3", git: roomGit, up: true}
	x.h = &Hub{
		Dir:   x.hub,
		Rooms: x.rooms,
		Repos: func() ([]Repo, error) {
			return []Repo{{Name: "github/o/r", Checkout: slash(x.checkout), Branch: "claude/main"}}, nil
		},
		Runner: NewRunner(),
	}
	return x
}

func TestMirrorCreatesTheBareRepositoryAndMovesWithTheBranch(t *testing.T) {
	x := newHubFixture(t, true)
	moved, err := x.h.Mirror(bg)
	if err != nil || len(moved) != 1 {
		t.Fatalf("first mirror: moved=%v err=%v", moved, err)
	}
	bare := x.h.Bare("github/o/r")
	sha1 := git(t, x.checkout, "rev-parse", "claude/main")
	if got := git(t, bare, "rev-parse", "refs/heads/claude/main"); got != sha1 {
		t.Fatalf("bare has %s, checkout has %s", got, sha1)
	}
	moved, err = x.h.Mirror(bg)
	if err != nil || len(moved) != 0 {
		t.Fatalf("second mirror should move nothing: moved=%v err=%v", moved, err)
	}
	// A re-signed claude/main is a non fast-forward move, and the mirror follows it.
	git(t, x.checkout, "reset", "-q", "--hard", "HEAD~0")
	git(t, x.checkout, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"commit", "-q", "--amend", "-m", "re-signed")
	sha2 := git(t, x.checkout, "rev-parse", "claude/main")
	if sha2 == sha1 {
		t.Fatal("the amend did not change the sha")
	}
	moved, err = x.h.Mirror(bg)
	if err != nil || len(moved) != 1 {
		t.Fatalf("re-sign: moved=%v err=%v", moved, err)
	}
	if got := git(t, bare, "rev-parse", "refs/heads/claude/main"); got != sha2 {
		t.Fatalf("bare did not follow the re-sign: %s", got)
	}
	// No server config is written into the bare repository.
	cfg, _ := os.ReadFile(filepath.Join(bare, "config"))
	if strings.Contains(string(cfg), "hideRefs") || strings.Contains(string(cfg), "receivepack") {
		t.Fatalf("server config written into the bare repository:\n%s", cfg)
	}
	if st := x.h.Status(); len(st.Mirror) != 1 || st.Mirror[0].SHA != sha2 {
		t.Fatalf("status = %+v", st)
	}
}

func TestMirrorReportsAMissingBranch(t *testing.T) {
	x := newHubFixture(t, true)
	x.h.Repos = func() ([]Repo, error) {
		return []Repo{{Name: "github/o/r", Checkout: slash(x.checkout), Branch: "main"}}, nil
	}
	if _, err := x.h.Mirror(bg); err == nil || !strings.Contains(err.Error(), "no branch main") {
		t.Fatalf("err = %v", err)
	}
}

func TestOnlyTheListedNamesAreServed(t *testing.T) {
	x := newHubFixture(t, true)
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(x.h.Backend())
	defer srv.Close()
	if code, _ := get(t, srv.URL+"/github/o/r.git/info/refs?service=git-upload-pack"); code != 200 {
		t.Fatalf("listed name = %d", code)
	}
	if code, _ := get(t, srv.URL+"/github/o/other.git/info/refs?service=git-upload-pack"); code != 404 {
		t.Fatalf("unlisted name = %d", code)
	}
}

func TestCollectBringsNewBranchesPrunesDeletedAndNeverCollectsClaudeMain(t *testing.T) {
	x := newHubFixture(t, true)
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	git(t, x.roomWork, "checkout", "-q", "-b", "claude/a")
	shaA := commit(t, x.roomWork, "a.txt", "worker a")
	git(t, x.roomWork, "checkout", "-q", "claude/main")
	headsBefore := git(t, x.checkout, "for-each-ref", "refs/heads")

	res, err := x.h.Collect(bg, "SG3")
	if err != nil || len(res.Repos) != 1 || res.Repos[0].Error != "" {
		t.Fatalf("collect: %+v err=%v", res, err)
	}
	bare := x.h.Bare("github/o/r")
	if got := git(t, bare, "rev-parse", "refs/rooms/sg3/claude/a"); got != shaA {
		t.Fatalf("bare refs/rooms/sg3/claude/a = %s", got)
	}
	if got := git(t, x.checkout, "rev-parse", "refs/remotes/sg3/claude/a"); got != shaA {
		t.Fatalf("checkout refs/remotes/sg3/claude/a = %s", got)
	}
	if !res.Repos[0].Moved || !res.Repos[0].Delivered {
		t.Fatalf("first collect should have moved and delivered: %+v", res.Repos[0])
	}
	// claude/main on the room is never collected, though this room advertised it.
	if out := git(t, bare, "for-each-ref", "refs/rooms/sg3/claude/main"); out != "" {
		t.Fatalf("claude/main was collected: %s", out)
	}
	if out := git(t, x.checkout, "for-each-ref", "refs/remotes/sg3/claude/main"); out != "" {
		t.Fatalf("claude/main was delivered: %s", out)
	}
	// Never a branch under refs/heads in the checkout.
	if got := git(t, x.checkout, "for-each-ref", "refs/heads"); got != headsBefore {
		t.Fatalf("refs/heads in the checkout changed:\n%s\nwas\n%s", got, headsBefore)
	}

	// A collect that moved nothing does not touch the checkout, and FETCH_HEAD is never written.
	res, err = x.h.Collect(bg, "SG3")
	if err != nil || res.Repos[0].Moved || res.Repos[0].Delivered {
		t.Fatalf("second collect: %+v err=%v", res.Repos[0], err)
	}
	for _, p := range []string{filepath.Join(x.checkout, ".git", "FETCH_HEAD"), filepath.Join(x.h.Bare("github/o/r"), "FETCH_HEAD")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s was written", p)
		}
	}

	// A hub killed between the collect and the delivery leaves the checkout short. The next
	// collect notices, though it moved nothing.
	git(t, x.checkout, "update-ref", "-d", "refs/remotes/sg3/claude/a")
	res, err = x.h.Collect(bg, "SG3")
	if err != nil || res.Repos[0].Moved || !res.Repos[0].Delivered {
		t.Fatalf("repair collect: %+v err=%v", res.Repos[0], err)
	}
	if got := git(t, x.checkout, "rev-parse", "refs/remotes/sg3/claude/a"); got != shaA {
		t.Fatalf("not repaired: %s", got)
	}

	// The branch is deleted on the room, and pruned from both.
	git(t, x.roomWork, "branch", "-q", "-D", "claude/a")
	res, err = x.h.Collect(bg, "SG3")
	if err != nil || !res.Repos[0].Moved || !res.Repos[0].Delivered {
		t.Fatalf("prune collect: %+v err=%v", res.Repos[0], err)
	}
	if out := git(t, bare, "for-each-ref", "refs/rooms/sg3"); out != "" {
		t.Fatalf("not pruned from the bare repository: %s", out)
	}
	if out := git(t, x.checkout, "for-each-ref", "refs/remotes/sg3"); out != "" {
		t.Fatalf("not pruned from the checkout: %s", out)
	}
}

// HEAD is not under refs/, so hiding refs/rooms alone would advertise a HEAD that points at a
// room's branch.
func TestHeadPointingAtAHiddenRefIsNotAdvertised(t *testing.T) {
	x := newHubFixture(t, true)
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	git(t, x.roomWork, "checkout", "-q", "-b", "claude/a")
	shaA := commit(t, x.roomWork, "a.txt", "worker a")
	git(t, x.roomWork, "checkout", "-q", "claude/main")
	if _, err := x.h.Collect(bg, "sg3"); err != nil {
		t.Fatal(err)
	}
	bare := x.h.Bare("github/o/r")
	git(t, bare, "symbolic-ref", "HEAD", "refs/rooms/sg3/claude/a")
	// Hidden by name: not advertised as HEAD whatever it points at.
	srv := httptest.NewServer(x.h.Backend())
	_, body0 := get(t, srv.URL+"/github/o/r.git/info/refs?service=git-upload-pack")
	srv.Close()
	if strings.Contains(body0, shaA) || strings.Contains(body0, " HEAD\x00") || strings.Contains(body0, " HEAD ") {
		t.Fatalf("HEAD was advertised:\n%s", body0)
	}
	// And the next mirror points it back at the integration branch, so its name does not
	// leak through the symref capability either.
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	srv = httptest.NewServer(x.h.Backend())
	defer srv.Close()
	code, body := get(t, srv.URL+"/github/o/r.git/info/refs?service=git-upload-pack")
	if code != 200 {
		t.Fatalf("info/refs = %d", code)
	}
	if strings.Contains(body, shaA) || strings.Contains(body, " HEAD\x00") || strings.Contains(body, "refs/rooms") {
		t.Fatalf("a hidden ref was advertised through HEAD:\n%s", body)
	}
}

func TestAStaleLockUnderAnOwnedRefIsRemovedAndOneElsewhereIsLeft(t *testing.T) {
	x := newHubFixture(t, true)
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	git(t, x.roomWork, "checkout", "-q", "-b", "claude/a")
	commit(t, x.roomWork, "a.txt", "worker a")
	git(t, x.roomWork, "checkout", "-q", "claude/main")

	old := time.Now().Add(-CommandBound - time.Minute)
	mk := func(p string, mod time.Time) string {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ownedBare := mk(filepath.Join(x.h.Bare("github/o/r"), "refs", "rooms", "sg3", "claude", "a.lock"), old)
	ownedCk := mk(filepath.Join(x.checkout, ".git", "refs", "remotes", "sg3", "claude", "a.lock"), old)
	elsewhere := mk(filepath.Join(x.checkout, ".git", "refs", "heads", "somebody.lock"), old)
	otherRoom := mk(filepath.Join(x.checkout, ".git", "refs", "remotes", "m1mini", "claude", "z.lock"), old)

	if _, err := x.h.Collect(bg, "SG3"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{ownedBare, ownedCk} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("stale lock %s was not removed", p)
		}
	}
	for _, p := range []string{elsewhere, otherRoom} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("lock %s outside what this side owns was removed", p)
		}
	}
	// A young lock is a command that may still be running, and is left.
	young := mk(filepath.Join(x.h.Bare("github/o/r"), "refs", "rooms", "sg3", "claude", "young.lock"), time.Now())
	if _, err := x.h.Collect(bg, "SG3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("a young lock was removed")
	}
}

func TestARoomThatDidNotSayGitIsNeverAskedNorDialled(t *testing.T) {
	x := newHubFixture(t, false)
	res, err := x.h.Sync(bg, "sg3", "", false)
	if err != nil || len(res) != 1 || res[0].State != "unsupported" {
		t.Fatalf("sync = %+v err=%v", res, err)
	}
	if _, err := x.h.Collect(bg, "sg3"); err == nil {
		t.Fatal("a collect from a room that never said Git was tried")
	}
	x.h.Attached(bg, "sg3")
	time.Sleep(300 * time.Millisecond)
	if x.syncs.Load() != 0 || x.requests.Load() != 0 {
		t.Fatalf("the room was contacted: syncs=%d requests=%d", x.syncs.Load(), x.requests.Load())
	}
}

func TestSyncAsksTheRoomAndRecordsItsAnswer(t *testing.T) {
	x := newHubFixture(t, true)
	x.answer.Store("behind")
	res, err := x.h.Sync(bg, "sg3", "github/o/r", true)
	if err != nil || len(res) != 1 || res[0].State != "behind" || res[0].Detail != "said so" {
		t.Fatalf("sync = %+v err=%v", res, err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(x.lastSync.Load().(string)), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "github/o/r" || body["init"] != true {
		t.Fatalf("the room was sent %v", body)
	}
	if _, err := x.h.Sync(bg, "sg3", "github/o/nope", false); err == nil {
		t.Fatal("a name not in git_repos was accepted")
	}
	if _, err := x.h.Sync(bg, "nowhere", "", false); err == nil {
		t.Fatal("a room that is not attached was accepted")
	}
	st := x.h.Status().Rooms["sg3"]
	if st.State != "behind" || len(st.Repos) != 1 || st.Repos[0].Name != "github/o/r" {
		t.Fatalf("status = %+v", st)
	}
	raw, _ := json.Marshal(st)
	if !strings.Contains(string(raw), `"state":"behind"`) || !strings.Contains(string(raw), `"repos":[`) {
		t.Fatalf("shape = %s", raw)
	}
}

// The room on the hub's own machine shares the checkout's disk, so it is skipped, and the
// status says why.
func TestTheRoomOnTheHubsOwnMachineIsSkipped(t *testing.T) {
	x := newHubFixture(t, true)
	x.rooms.host = "Sg4"
	x.h.SelfHost = "sg4"
	res, err := x.h.Sync(bg, "sg3", "", true)
	if err != nil || len(res) != 1 || res[0].State != "skipped" || res[0].Detail != SameMachine {
		t.Fatalf("sync = %+v err=%v", res, err)
	}
	if _, err := x.h.Collect(bg, "sg3"); err == nil || err.Error() != SameMachine {
		t.Fatalf("collect err = %v", err)
	}
	x.h.Attached(bg, "sg3")
	time.Sleep(300 * time.Millisecond)
	if x.syncs.Load() != 0 || x.requests.Load() != 0 {
		t.Fatalf("the room was contacted: syncs=%d requests=%d", x.syncs.Load(), x.requests.Load())
	}
	if st := x.h.Status().Rooms["sg3"]; st == nil || st.Repos[0].Detail != SameMachine {
		t.Fatalf("status = %+v", st)
	}
}

// Another machine is not skipped. (A hub of its own: the attach above left a goroutine that read SelfHost, and
// changing it under that goroutine is a race.)
func TestTheRoomOnAnotherMachineIsNotSkipped(t *testing.T) {
	x := newHubFixture(t, true)
	x.rooms.host = "Sg4"
	x.h.SelfHost = "elsewhere"
	x.answer.Store("ok")
	if _, err := x.h.Sync(bg, "sg3", "", false); err != nil || x.syncs.Load() != 1 {
		t.Fatalf("a different host was skipped: syncs=%d err=%v", x.syncs.Load(), err)
	}
}

func TestRoomNamesInRefsAreLowercasedAndCheckedByGit(t *testing.T) {
	h := &Hub{Runner: NewRunner()}
	if k, err := h.RoomKey(bg, "SG3"); err != nil || k != "sg3" {
		t.Fatalf("key = %q err=%v", k, err)
	}
	for _, bad := range []string{"", "a b", "a..b", "a:b", "a~b", "a\\b", "a[b", "x.lock/"} {
		if _, err := h.RoomKey(bg, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestOnlyTheIntegrationBranchMayBeMirrored(t *testing.T) {
	ok := `[{"name":"github/o/r","checkout":"` + slash(t.TempDir()) + `","branch":"claude/main"},
	        {"name":"github/o/s","checkout":"` + slash(t.TempDir()) + `"}]`
	repos, err := ParseRepos(ok)
	if err != nil || len(repos) != 2 || repos[1].Branch != "claude/main" {
		t.Fatalf("repos = %+v err=%v", repos, err)
	}
	dir := slash(t.TempDir())
	for _, branch := range []string{"claude/ui", "claude/f-019b", "hub-main", "develop", "refs/heads/claude/main", "../main"} {
		_, err := ParseRepos(`[{"name":"github/o/r","checkout":"` + dir + `","branch":"` + branch + `"}]`)
		if err == nil {
			t.Errorf("branch %q was accepted", branch)
			continue
		}
		if !strings.Contains(err.Error(), "integration branch") || !strings.Contains(err.Error(), "claude/main or main") {
			t.Errorf("the refusal for %q does not say why: %v", branch, err)
		}
	}
	for _, bad := range []string{
		`[{"name":"../x","checkout":"` + dir + `","branch":"main"}]`,
		`[{"name":"github/o/r","checkout":"relative","branch":"main"}]`,
		`[{"name":"github/o/r","checkout":"` + dir + `","branch":"main"},{"name":"github/o/r","checkout":"` + dir + `","branch":"main"}]`,
		`not json`,
	} {
		if _, err := ParseRepos(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	if repos, err := ParseRepos("  "); err != nil || repos != nil {
		t.Fatalf("empty = %v %v", repos, err)
	}
}

// A hub stopped in the middle of a collect leaves every ref at its old sha or its new one.
func TestAHubStoppedMidFetchLeavesEveryRefWhole(t *testing.T) {
	x := newHubFixture(t, true)
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	git(t, x.roomWork, "checkout", "-q", "-b", "claude/a")
	shaA := commit(t, x.roomWork, "a.txt", "worker a")
	git(t, x.roomWork, "checkout", "-q", "claude/main")
	if _, err := x.h.Collect(bg, "sg3"); err != nil {
		t.Fatal(err)
	}
	// The room moves on, and then stalls its upload-pack, so the next collect hangs.
	git(t, x.roomWork, "checkout", "-q", "claude/a")
	shaB := commit(t, x.roomWork, "b.txt", "worker a again")
	git(t, x.roomWork, "checkout", "-q", "claude/main")
	stall := make(chan struct{})
	old := x.rooms.srv.Config.Handler
	x.rooms.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "git-upload-pack") && r.Method == http.MethodPost {
			<-stall
			return
		}
		old.ServeHTTP(w, r)
	})
	defer close(stall)
	done := make(chan struct{})
	go func() { _, _ = x.h.Collect(bg, "sg3"); close(done) }()
	time.Sleep(3 * time.Second)
	if !x.h.runner().Stop(15 * time.Second) {
		t.Fatal("git children did not stop within the bound")
	}
	<-done
	for _, dir := range []string{x.h.Bare("github/o/r"), x.checkout} {
		ref := "refs/rooms/sg3/claude/a"
		if dir == x.checkout {
			ref = "refs/remotes/sg3/claude/a"
		}
		got := git(t, dir, "rev-parse", ref)
		if got != shaA && got != shaB {
			t.Fatalf("%s in %s is %s, neither the old nor the new sha", ref, dir, got)
		}
		if out, err := Default.Git(bg, dir, "fsck", "--connectivity-only", "--no-progress"); err != nil {
			t.Fatalf("fsck %s: %v %s", dir, err, out)
		}
	}
}
