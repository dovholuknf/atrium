//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/deployready"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

type readyRepo struct {
	t   *testing.T
	dir string
}

func newReadyRepo(t *testing.T) *readyRepo {
	t.Helper()
	r := &readyRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "claude/main")
	return r
}

func (r *readyRepo) git(args ...string) string {
	r.t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.dir
	cmd.Env = gitsync.CleanEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *readyRepo) commit(msg string, files map[string]string) string {
	r.t.Helper()
	for k, v := range files {
		p := filepath.Join(r.dir, filepath.FromSlash(k))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

type spawned struct {
	script, tip string
	finish      chan int
}

// readyProxy is a hub whose integration checkout is r and whose installed build is `installed`.
func readyProxy(t *testing.T, r *readyRepo, installed string, script string) (*Proxy, *[]spawned) {
	t.Helper()
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	g := &gitsync.Hub{Runner: gitsync.NewRunner(), Repos: func() ([]gitsync.Repo, error) {
		return []gitsync.Repo{{Name: "github/x/y", Checkout: r.dir, Branch: "claude/main"}}, nil
	}}
	p.SetGit(g)
	p.SetLaunchCaps(&fakeSettings{m: map[string]string{SettingDeployScript: script}})
	p.SetDeployReady()
	st := p.deployReady()
	st.installed = func(context.Context, *Proxy) (string, error) { return installed, nil }
	var started []spawned
	var mu sync.Mutex
	st.spawn = func(script, tip string) (*deployProc, error) {
		s := spawned{script: script, tip: tip, finish: make(chan int, 1)}
		mu.Lock()
		started = append(started, s)
		mu.Unlock()
		return &deployProc{Pid: 4242, Wait: func() (int, error) { return <-s.finish, nil }}, nil
	}
	return p, &started
}

func loopbackReq(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Host = "127.0.0.1:7778"
	return r
}

func getReady(t *testing.T, p *Proxy) deployReadyView {
	t.Helper()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-ready?fresh=1", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var v deployReadyView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body)
	}
	return v
}

func postDeploy(p *Proxy, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, r)
	return rec
}

func scriptFile(t *testing.T) string {
	t.Helper()
	s := filepath.Join(t.TempDir(), "deploy-ready.ps1")
	if err := os.WriteFile(s, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDeployReadyIs404WhenNotWired(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-ready", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestDeployReadyNamesWhatBlocksAndClearsWhenReviewed(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("change the daemon", map[string]string{"internal/daemon/d.go": "d"})
	r.commit("docs", map[string]string{"docs/x.md": "x"})
	p, _ := readyProxy(t, r, base, scriptFile(t))

	v := getReady(t, p)
	if v.State != deployready.StateBlocked || v.Ready {
		t.Fatalf("view = %+v", v.Report)
	}
	if len(v.Blocking) != 1 || v.Blocking[0].SHA != code || v.Blocking[0].Subject != "change the daemon" {
		t.Fatalf("blocking = %+v", v.Blocking)
	}
	if v.Deploy.State != "idle" {
		t.Fatalf("deploy = %+v", v.Deploy)
	}

	r.commit("Review of daemon\n\nAtrium-Verdict: hub-ok "+code+"\nAtrium-Verdict: room-ok "+code,
		map[string]string{"docs/backlog/rt/r-new-review-d.md": "ok"})
	v = getReady(t, p)
	if !v.Ready || v.State != deployready.StateReady || v.Script == "" {
		t.Fatalf("view = %+v script %q", v.Report, v.Script)
	}
}

func TestDeployClickRefusals(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	p, started := readyProxy(t, r, base, scriptFile(t))
	body := `{"tip":"` + code + `"}`

	// Not from this machine.
	req := loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", body)
	req.RemoteAddr = "192.0.2.9:4000"
	if rec := postDeploy(p, req); rec.Code != http.StatusForbidden {
		t.Fatalf("remote = %d %s", rec.Code, rec.Body)
	}
	// Through a proxy that kept loopback addresses.
	req = loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", body)
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	if rec := postDeploy(p, req); rec.Code != http.StatusForbidden {
		t.Fatalf("proxied = %d %s", rec.Code, rec.Body)
	}
	// Wrong verb.
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-ready/deploy", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET deploy = %d", rec.Code)
	}
	// A tip that is not a full sha is never handed to a script.
	for _, tip := range []string{"", "abc", "HEAD", code[:12], code + "; calc"} {
		b, _ := json.Marshal(map[string]string{"tip": tip})
		if rec := postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", string(b))); rec.Code != http.StatusBadRequest {
			t.Fatalf("tip %q = %d %s", tip, rec.Code, rec.Body)
		}
	}
	// Not ready, with the reason in the answer.
	rec = postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", body))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not ready") ||
		!strings.Contains(rec.Body.String(), code[:8]) {
		t.Fatalf("unready = %d %s", rec.Code, rec.Body)
	}
	if len(*started) != 0 {
		t.Fatal("a script was started for a deploy that was not ready")
	}
}

func TestDeployClickStartsOneScriptWithTheTipItWasShown(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	r.commit("Review\n\nAtrium-Verdict: hub-ok "+code, map[string]string{"docs/backlog/rt/r-new-review-a.md": "ok"})
	tip := r.git("rev-parse", "HEAD")
	script := scriptFile(t)
	p, started := readyProxy(t, r, base, script)

	// The branch moves after the board drew its line.
	stale := `{"tip":"` + code + `"}`
	rec := postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", stale))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "moved") {
		t.Fatalf("stale = %d %s", rec.Code, rec.Body)
	}
	if len(*started) != 0 {
		t.Fatal("started on a stale tip")
	}

	rec = postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", `{"tip":"`+tip+`"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("click = %d %s", rec.Code, rec.Body)
	}
	if len(*started) != 1 || (*started)[0].tip != tip || (*started)[0].script != script {
		t.Fatalf("started = %+v", *started)
	}

	// A second click while it runs.
	rec = postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", `{"tip":"`+tip+`"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already running") {
		t.Fatalf("second = %d %s", rec.Code, rec.Body)
	}
	if len(*started) != 1 {
		t.Fatal("a second script was started")
	}
	if v := getReady(t, p); v.Deploy.State != "running" || v.Deploy.Tip != tip {
		t.Fatalf("deploy = %+v", v.Deploy)
	}

	(*started)[0].finish <- 3
	deadline := time.Now().Add(5 * time.Second)
	for {
		v := getReady(t, p)
		if v.Deploy.State == "finished" {
			if v.Deploy.Exit == nil || *v.Deploy.Exit != 3 {
				t.Fatalf("deploy = %+v", v.Deploy)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never finished: %+v", v.Deploy)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDeployClickWithNoScriptSaysSo(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	r.commit("Review\n\nAtrium-Verdict: hub-ok "+code, map[string]string{"docs/backlog/rt/r-new-review-a.md": "ok"})
	tip := r.git("rev-parse", "HEAD")
	p, started := readyProxy(t, r, base, filepath.Join(t.TempDir(), "missing.ps1"))
	rec := postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", `{"tip":"`+tip+`"}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), SettingDeployScript) {
		t.Fatalf("no script = %d %s", rec.Code, rec.Body)
	}
	if len(*started) != 0 {
		t.Fatal("started with no script")
	}
}

func TestDeployReadyUnknownWhenTheInstalledBuildCannotBeRead(t *testing.T) {
	r := newReadyRepo(t)
	r.commit("base", map[string]string{"internal/a.go": "a"})
	p, _ := readyProxy(t, r, "", scriptFile(t))
	v := getReady(t, p)
	if v.State != deployready.StateUnknown || v.Ready || v.Error == "" {
		t.Fatalf("view = %+v", v.Report)
	}
}

func TestDeployReadyTickTellsAWatchingBoardOnlyWhenTheAnswerMoves(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	p, _ := readyProxy(t, r, base, scriptFile(t))

	// Nobody watching: nothing is read and nothing is sent.
	p.deployReadyTick(context.Background())
	if p.deployReady().haveRep {
		t.Fatal("the tick read git with nobody watching")
	}

	s := p.feeds.add("")
	defer p.feeds.drop(s)
	heard := func() bool {
		timeout := time.After(500 * time.Millisecond)
		for {
			select {
			case e := <-s.ch:
				if e.Kind == deployReadyEvent {
					return true
				}
			case <-timeout:
				return false
			}
		}
	}
	p.deployReadyTick(context.Background())
	if !heard() {
		t.Fatal("the first answer was not announced")
	}
	p.deployReadyTick(context.Background())
	if heard() {
		t.Fatal("an unchanged answer was announced again")
	}
	r.commit("Review\n\nAtrium-Verdict: hub-ok "+code, map[string]string{"docs/backlog/rt/r-new-review-a.md": "ok"})
	p.deployReadyTick(context.Background())
	if !heard() {
		t.Fatal("a verdict landing was not announced")
	}
}

// slowGit never answers until its context ends, which is what a git on a huge history looks like from outside.
type slowGit struct {
	mu    sync.Mutex
	calls int
}

func (g *slowGit) Git(ctx context.Context, _ string, _ ...string) (string, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

func (g *slowGit) GitInput(ctx context.Context, dir string, _ []byte, a ...string) (string, error) {
	return g.Git(ctx, dir, a...)
}

func TestDeployReadyAnswersUnknownAtTheBoundAndNeverMakesASecondCallerWait(t *testing.T) {
	old := deployReadyBound
	deployReadyBound = 400 * time.Millisecond
	defer func() { deployReadyBound = old }()

	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	p, _ := readyProxy(t, r, base, scriptFile(t))
	st := p.deployReady()
	slow := &slowGit{}
	st.checker = &deployready.Checker{Git: slow, Dir: r.dir, Branch: "claude/main"}

	first := make(chan deployReadyView, 1)
	go func() { first <- getReady(t, p) }()
	// Wait until the first caller is inside git.
	deadline := time.Now().Add(2 * time.Second)
	for {
		slow.mu.Lock()
		n := slow.calls
		slow.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first caller never reached git")
		}
		time.Sleep(5 * time.Millisecond)
	}

	start := time.Now()
	second := getReady(t, p)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("the second caller waited %s", d)
	}
	if second.State != deployready.StateUnknown || second.Ready || second.Error == "" {
		t.Fatalf("second = %+v", second.Report)
	}

	select {
	case v := <-first:
		if v.State != deployready.StateUnknown || v.Ready || !strings.Contains(v.Error, "timed out") {
			t.Fatalf("first = %+v", v.Report)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first caller was not answered at the bound")
	}

	// The give-up is not reused: the next ask reads again at once.
	st.mu.Lock()
	inflight, reuse := st.inflight, !st.lastAt.IsZero()
	st.mu.Unlock()
	if inflight || reuse {
		t.Fatalf("inflight = %v, reusable = %v", inflight, reuse)
	}
}

func TestDeployReadyHandsBackTheLastAnswerStaleWhileANewerReadRuns(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	p, _ := readyProxy(t, r, base, scriptFile(t))
	if v := getReady(t, p); v.State != deployready.StateBlocked {
		t.Fatalf("warm-up = %+v", v.Report)
	}
	st := p.deployReady()
	st.mu.Lock()
	st.inflight = true
	st.mu.Unlock()

	v := getReady(t, p)
	if !v.Stale || v.Ready || v.State != deployready.StateBlocked {
		t.Fatalf("view = %+v", v.Report)
	}
	rec := postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy",
		`{"tip":"`+strings.Repeat("a", 40)+`"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("a click during a read = %d %s", rec.Code, rec.Body)
	}
}

func TestDeployReadyGetThatRunsThePassTellsWatchingBoards(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	p, _ := readyProxy(t, r, base, scriptFile(t))
	s := p.feeds.add("")
	defer p.feeds.drop(s)
	getReady(t, p)
	timeout := time.After(time.Second)
	for {
		select {
		case e := <-s.ch:
			if e.Kind == deployReadyEvent {
				return
			}
		case <-timeout:
			t.Fatal("a GET that ran the pass announced nothing")
		}
	}
}

func TestParseVersionCommit(t *testing.T) {
	out := "atrium dev\ncommit 7a38c1f2deadbeef0123456789abcdef01234567 (modified)\nboard  0123abcd\nplatform windows/amd64\n"
	if got := parseVersionCommit(out); got != "7a38c1f2deadbeef0123456789abcdef01234567" {
		t.Fatalf("got %q", got)
	}
	if got := parseVersionCommit("atrium dev\nboard 0123abcd\n"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestDeployEnvDropsAtriumVariables(t *testing.T) {
	in := []string{"PATH=/bin", "ATRIUM_NEW_BUILD=other.exe", "atrium_location=private", "ATRIUMX=1", "HOME=/h",
		"GIT_TERMINAL_PROMPT=0"}
	got := deployEnv(in)
	want := []string{"PATH=/bin", "ATRIUMX=1", "HOME=/h", "GIT_TERMINAL_PROMPT=0"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("deployEnv = %v, want %v", got, want)
	}
}

func TestDeployQueueEndpointListsWhatTheHubIsMissing(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a/a.go": "package a\n"})
	landed := r.commit("landed", map[string]string{"internal/a/a.go": "package a // 2\n",
		"changelog/fabric/2026-10-04-f-landed.md": "x\n"})
	p, _ := readyProxy(t, r, base, scriptFile(t))
	p.SetRunningCommit(base)

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-queue", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var q deployready.Queue
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body)
	}
	if len(q.Entries) != 1 || q.Entries[0].SHA != landed || q.Entries[0].Item != "f-landed" || q.Entries[0].Needs != "hub" {
		t.Fatalf("queue = %+v", q)
	}

	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-queue?format=md", ""))
	if !strings.Contains(rec.Body.String(), "| f-landed | hub |") {
		t.Fatalf("markdown:\n%s", rec.Body)
	}

	// With no running commit set, the installed binary's commit stands in for it.
	p.SetRunningCommit("")
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-queue", ""))
	if !strings.Contains(rec.Body.String(), landed) {
		t.Fatalf("fallback: %s", rec.Body)
	}
}

func TestDeployQueueIs404WhenNotWired(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/deploy-queue", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET = %d", rec.Code)
	}
}
