package link

import (
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

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub's store taken pushes: the operator through the board, a room over the link, and the reaches that
// are refused. See docs/rnd/hub-forge-design.md 3.2 and 3.4.

type pushFix struct {
	g    *gitsync.Hub
	log  *gitsync.MemPushLog
	p    *Proxy
	work string
	tmp  string
}

// gitAs is git in dir with extra headers, and the text of a failure.
func gitAs(t *testing.T, dir string, headers []string, args ...string) (string, error) {
	t.Helper()
	var full []string
	for _, h := range headers {
		full = append(full, "-c", "http.extraHeader="+h)
	}
	out, err := gitsync.Default.GitEnv(context.Background(), dir,
		[]string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"}, append(full, args...)...)
	var ge *gitsync.Error
	if errors.As(err, &ge) {
		return out + ge.Stderr, err
	}
	return out, err
}

func newPushFix(t *testing.T) *pushFix {
	t.Helper()
	x := &pushFix{log: &gitsync.MemPushLog{}, tmp: t.TempDir()}
	x.g = &gitsync.Hub{Dir: filepath.Join(x.tmp, "hub"), Runner: gitsync.NewRunner(), PushLog: x.log,
		Repos: func() ([]gitsync.Repo, error) { return nil, nil }}
	x.p = NewProxy(NewHub(Timings{}), nil, "", nil)
	x.p.SetGit(x.g)
	x.work = filepath.Join(x.tmp, "work")
	if err := os.MkdirAll(x.work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, x.work, "init", "-q", "-b", "main")
	x.commit(t, "base.txt", "base")
	return x
}

func (x *pushFix) commit(t *testing.T, file, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(x.work, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, x.work, "add", "-A")
	gitRun(t, x.work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", body)
	return gitRun(t, x.work, "rev-parse", "HEAD")
}

// repo makes an empty repository in the store, as init does for a private repository.
func (x *pushFix) repo(t *testing.T, name string) {
	t.Helper()
	x.g.Store().Forge = func(gitsync.Ref) string { return filepath.Join(x.tmp, "no-such-forge") }
	x.g.Store().Protocols = "file"
	if _, err := x.g.Store().Init(context.Background(), "https://github.com/"+strings.TrimPrefix(name, "github/")); err != nil {
		t.Fatal(err)
	}
}

const boardRepo = "github/o/r"

func (x *pushFix) refOn(t *testing.T, ref string) string {
	t.Helper()
	dir, _ := x.g.Store().Path(boardRepo)
	return gitRun(t, dir, "for-each-ref", "--format=%(objectname)", ref)
}

// ── the operator, through the board ─────────────────────

func TestTheOperatorPushesOnLoopbackThroughTheBoardAndCardHeadersAreDropped(t *testing.T) {
	x := newPushFix(t)
	x.repo(t, boardRepo)
	srv := httptest.NewServer(x.p)
	defer srv.Close()
	url := srv.URL + "/git/hub/" + boardRepo + ".git"

	// The operator's push of main, with card headers it has no business sending.
	out, err := gitAs(t, x.work, []string{"X-Atrium-Card: C1", "X-Atrium-Card-Chain: C1"}, "push", url, "main:refs/heads/main")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if x.refOn(t, "refs/heads/main") == "" {
		t.Fatal("main did not land")
	}
	rows := x.log.Rows()
	if len(rows) != 1 || rows[0].Room != "" || rows[0].Card != "" || !rows[0].Operator() {
		t.Fatalf("the row: %+v", rows)
	}
	// And a fast-forward of it. The operator on loopback is the integration role.
	x.commit(t, "more.txt", "more")
	if out, err := gitAs(t, x.work, nil, "push", url, "main:refs/heads/main"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// A clone reads it back.
	dst := filepath.Join(x.tmp, "clone")
	if out, err := gitAs(t, x.tmp, nil, "clone", "-q", url, dst); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := gitRun(t, dst, "rev-parse", "origin/main"); got != x.refOn(t, "refs/heads/main") {
		t.Fatalf("clone has %s", got)
	}
}

func TestThePushReachesAreTheOperatorsOrRefused(t *testing.T) {
	x := newPushFix(t)
	x.repo(t, boardRepo)
	path := "/git/hub/" + boardRepo + ".git/info/refs?service=git-receive-pack"

	do := func(h http.Handler, host, remote string, hdr map[string]string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://"+host+path, nil)
		req.RemoteAddr = remote
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	loop := "127.0.0.1:5555"
	// The main listener: loopback only.
	if code := do(x.p, "127.0.0.1:7778", loop, nil); code != 200 {
		t.Errorf("loopback = %d", code)
	}
	if code := do(x.p, "127.0.0.1:7778", offLoopback, nil); code != http.StatusForbidden {
		t.Errorf("another machine on the main listener = %d", code)
	}
	if code := do(x.p, "127.0.0.1:7778", loop, map[string]string{"X-Forwarded-For": "203.0.113.9"}); code != http.StatusForbidden {
		t.Errorf("through a proxy header = %d", code)
	}
	if code := do(x.p, "atrium.example.org", loop, nil); code != http.StatusForbidden {
		t.Errorf("a public Host from loopback = %d", code)
	}
	// A zrok PUBLIC share is a 404 for everything, even from a loopback address with a loopback Host.
	pub := edge.MarkReach(x.p, edge.ReachZrokPublic)
	for _, remote := range []string{loop, offLoopback} {
		if code := do(pub, "127.0.0.1:7778", remote, nil); code != http.StatusNotFound {
			t.Errorf("public share from %s = %d", remote, code)
		}
	}
	for _, m := range []string{"GET", "POST"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(m, "http://127.0.0.1:7778/git/hub/"+boardRepo+".git/git-receive-pack", strings.NewReader("0000"))
		req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
		pub.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s on a public share = %d", m, rec.Code)
		}
	}
	// An overlay, and a private share, are the operator's: the network decides who gets there.
	for _, r := range []edge.Reach{edge.ReachOverlay, edge.ReachZrokPrivate} {
		if code := do(edge.MarkReach(x.p, r), "atrium.ziti", offLoopback, nil); code != 200 {
			t.Errorf("%s = %d", r, code)
		}
	}
	// What is not the store is not served here either. (A room's pass-through, /git/room/, is its own route: see
	// git_pass_test.go.)
	for _, p := range []string{"/git/", "/git/hub", "/git/github/o/r.git/info/refs", "/git/room", "/git/room/"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "http://127.0.0.1:7778"+p, nil)
		req.RemoteAddr = loop
		x.p.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d", p, rec.Code)
		}
	}
	// A hub with no git side answers 404.
	bare := NewProxy(NewHub(Timings{}), nil, "", nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://127.0.0.1:7778"+path, nil)
	req.RemoteAddr = loop
	bare.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("no git side = %d", rec.Code)
	}
	// Nothing was written by any of it.
	if x.refOn(t, "refs/heads/main") != "" || len(x.log.Rows()) != 0 {
		t.Fatal("a refused reach changed the store")
	}
}

func TestThePushRefusalsOnTheBoardMoveNothing(t *testing.T) {
	x := newPushFix(t)
	x.repo(t, boardRepo)
	srv := httptest.NewServer(edge.MarkReach(x.p, edge.ReachZrokPublic))
	defer srv.Close()
	out, err := gitAs(t, x.work, nil, "push", srv.URL+"/git/hub/"+boardRepo+".git", "main:refs/heads/main")
	if err == nil {
		t.Fatalf("a push through a public share landed:\n%s", out)
	}
	if x.refOn(t, "refs/heads/main") != "" || len(x.log.Rows()) != 0 {
		t.Fatal("a public share moved the store")
	}
}

// ── release ─────────────────────────────────────────────

func TestTheReleaseRouteIsTheLoopbackOperatorsAndLetsGoOfABranch(t *testing.T) {
	x := newPushFix(t)
	x.repo(t, boardRepo)
	srv := httptest.NewServer(x.p)
	defer srv.Close()
	url := srv.URL + "/git/hub/" + boardRepo + ".git"
	// Seed main as the operator, then a branch through a row the log holds as a card's.
	if out, err := gitAs(t, x.work, nil, "push", url, "main:refs/heads/main"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	gitRun(t, x.work, "branch", "fix/x")
	if out, err := gitAs(t, x.work, nil, "push", url, "fix/x:refs/heads/fix/x"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	_ = x.log.Append(context.Background(), gitsync.PushRow{Repo: boardRepo, Ref: "refs/heads/card", Old: strings.Repeat("0", 40), New: "a", Room: "sg4", Card: "C1"})
	gitRun(t, filepath.Join(x.g.Store().Root(), "github", "o", "r.git"), "update-ref", "refs/heads/card", x.refOn(t, "refs/heads/main"))

	call := func(method, body, remote string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "http://127.0.0.1:7778/_hub/git/release", strings.NewReader(body))
		req.RemoteAddr = remote
		x.p.ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, _ := call("POST", `{"repo":"github/o/r","branch":"card"}`, offLoopback); code != http.StatusForbidden {
		t.Errorf("off loopback = %d", code)
	}
	if code, _ := call("GET", ``, "127.0.0.1:1"); code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", code)
	}
	if code, _ := call("POST", `not json`, "127.0.0.1:1"); code != http.StatusBadRequest {
		t.Errorf("not json = %d", code)
	}
	if code, _ := call("POST", `{"repo":"github/o/r","branch":"main"}`, "127.0.0.1:1"); code != http.StatusBadRequest {
		t.Errorf("main = %d", code)
	}
	if code, _ := call("POST", `{"repo":"..","branch":"x"}`, "127.0.0.1:1"); code != http.StatusBadRequest {
		t.Errorf("a bad repo = %d", code)
	}
	code, out := call("POST", `{"repo":"o/r","branch":"card"}`, "127.0.0.1:1")
	if code != 200 || !strings.Contains(out, `"note":"card was owned by sg4's C1 and is released`) {
		t.Fatalf("release = %d %s", code, out)
	}
	if _, _, ok, _ := x.log.Owner(context.Background(), boardRepo, "refs/heads/card"); ok {
		t.Fatal("the branch is still owned")
	}
	if code, out := call("POST", `{"repo":"o/r","branch":"card"}`, "127.0.0.1:1"); code != 200 || !strings.Contains(out, "no owner") {
		t.Fatalf("a second release = %d %s", code, out)
	}
}

// ── a room, over the link ───────────────────────────────

type roomPush struct {
	*pushFix
	hub *Hub
	fwd *gitsync.Forwarder
	url string
}

// newRoomPush starts a hub that serves the store on the git kind, and a room that dialled it. The room's
// forwarder here is the existing per-command one, which adds no card header: the git client adds them, the way
// the room's stable forwarder will.
func newRoomPush(t *testing.T, roomName string) *roomPush {
	t.Helper()
	x := &roomPush{pushFix: newPushFix(t)}
	x.hub = NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})
	x.g.Rooms = x.hub.GitRooms()
	x.hub.Git = x.g.Backend()
	x.hub.GitStore = x.g.StoreHandler()
	hubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); hubLn.Close() })
	go func() { _ = x.hub.Serve(ctx, hubLn) }()
	room := &Room{Name: roomName, Dial: plain{addr: hubLn.Addr().String()}, Git: true,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond}}
	room.Handler = http.NotFoundHandler()
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return x.hub.Has(strings.ToLower(roomName)) && room.State().Up && room.HubServesGit() })
	f, err := gitsync.NewForwarder(room.GitTransport(), "hub", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	x.fwd = f
	x.url = f.URL + "/git/hub/" + boardRepo + ".git"
	return x
}

func TestARoomPushesToTheHubStoreOverTheLinkAsItselfWithItsCard(t *testing.T) {
	x := newRoomPush(t, "SG4")
	x.repo(t, boardRepo)
	// The operator seeds main, directly through the handler's other door: the board.
	srv := httptest.NewServer(x.p)
	defer srv.Close()
	if out, err := gitAs(t, x.work, nil, "push", srv.URL+"/git/hub/"+boardRepo+".git", "main:refs/heads/main"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	gitRun(t, x.work, "switch", "-q", "-c", "fix/x")
	sha := x.commit(t, "x.txt", "x")

	card := []string{"X-Atrium-Card: C1", "X-Atrium-Card-Chain: C1,C0",
		// What a room cannot do is say it is another room: the hub reads the room off the link.
		"X-Atrium-Room: m1mini", "X-Atrium-Operator: yes"}
	if out, err := gitAs(t, x.work, card, "push", x.url, "fix/x:refs/heads/fix/x"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := x.refOn(t, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x is %s on the hub", got)
	}
	rows := x.log.Rows()
	last := rows[len(rows)-1]
	if !strings.EqualFold(last.Room, "SG4") || last.Room == "m1mini" || last.Card != "C1" || last.Ref != "refs/heads/fix/x" || last.Operator() {
		t.Fatalf("the row: %+v", last)
	}
	// A room cannot move main, which only the operator does.
	out, err := gitAs(t, x.work, card, "push", x.url, "fix/x:refs/heads/main")
	if err == nil || !strings.Contains(out, "only the operator moves main") {
		t.Fatalf("a room moved main: %v\n%s", err, out)
	}
	// A room push with no card is refused with the sentence, and the room's name alone is not an identity.
	out, err = gitAs(t, x.work, nil, "push", x.url, "fix/x:refs/heads/fix/y")
	if err == nil || !strings.Contains(out, "named none") {
		t.Fatalf("a push with no card: %v\n%s", err, out)
	}
	if x.refOn(t, "refs/heads/fix/y") != "" {
		t.Fatal("fix/y landed")
	}
	// Another card, same room, is refused as owned.
	x.commit(t, "x2.txt", "x2")
	out, err = gitAs(t, x.work, []string{"X-Atrium-Card: C2", "X-Atrium-Card-Chain: C2"}, "push", x.url, "fix/x:refs/heads/fix/x")
	if err == nil || !strings.Contains(out, "is owned by") {
		t.Fatalf("another card pushed to an owned branch: %v\n%s", err, out)
	}
	if x.refOn(t, "refs/heads/fix/x") != sha {
		t.Fatal("another card moved the owned branch")
	}
	// And the room fetches the store back over the same link.
	dst := filepath.Join(x.tmp, "clone")
	if out, err := gitAs(t, x.tmp, nil, "clone", "-q", x.url, dst); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := gitRun(t, dst, "rev-parse", "origin/fix/x"); got != sha {
		t.Fatalf("the room's clone has %s", got)
	}
}

func TestTheLinkStillServesTheMirrorsAndNotThroughTheStoreRoute(t *testing.T) {
	x := newRoomPush(t, "SG4")
	// A mirror on the old route; it is not in the store, so the store route never serves it.
	x.g.Repos = func() ([]gitsync.Repo, error) {
		return []gitsync.Repo{{Name: "github/o/m", Checkout: filepath.ToSlash(x.work), Branch: "main"}}, nil
	}
	if _, err := x.g.Mirror(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out, err := gitAs(t, x.tmp, nil, "clone", "-q", x.fwd.URL+"/github/o/m.git", filepath.Join(x.tmp, "m")); err != nil {
		t.Fatalf("the old route no longer serves: %v\n%s", err, out)
	}
	if out, err := gitAs(t, x.tmp, nil, "clone", "-q", x.fwd.URL+"/git/hub/github/o/m.git", filepath.Join(x.tmp, "m2")); err == nil {
		t.Fatalf("a mirror was served by the store route:\n%s", out)
	}
	out, err := gitAs(t, x.work, []string{"X-Atrium-Card: C1"}, "push", x.fwd.URL+"/git/hub/github/o/m.git", "main:refs/heads/z")
	if err == nil {
		t.Fatalf("a push into a mirror landed:\n%s", out)
	}
}

// ── asking a room about a card ──────────────────────────

func TestTheOwnersCardIsAskedThroughTheRelay(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	ctx := relayCtx(t)
	x.proxy.mu.Lock()
	x.proxy.ctl = x.control
	x.proxy.mu.Unlock()

	st, err := x.proxy.GitCards(ctx, "sg4", "s1")
	if err != nil || st.Status != "needs-input" {
		t.Fatalf("s1: %+v %v", st, err)
	}
	if st, err := x.proxy.GitCards(ctx, "sg4", "s2"); err != nil || st.Status != "done" {
		t.Fatalf("s2: %+v %v", st, err)
	}
	if _, err := x.proxy.GitCards(ctx, "sg4", "culled"); !errors.Is(err, gitsync.ErrCardGone) {
		t.Fatalf("a card the room does not have: %v", err)
	}
	if _, err := x.proxy.GitCards(ctx, "atlantis", "s1"); !errors.Is(err, gitsync.ErrRoomUnreachable) {
		t.Fatalf("a room that is not attached: %v", err)
	}
	var none Proxy
	if _, err := none.GitCards(ctx, "sg4", "s1"); !errors.Is(err, gitsync.ErrRoomUnreachable) {
		t.Fatalf("no control: %v", err)
	}
}
