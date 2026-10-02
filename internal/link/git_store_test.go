package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

type fakeGitSettings struct {
	mu    sync.Mutex
	store string
	on    bool
}

func (f *fakeGitSettings) GitStorePath(dir string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.store == "" {
		return filepath.Join(dir, "git"), nil
	}
	return f.store, nil
}

func (f *fakeGitSettings) SetGitStore(d string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d != "" && !filepath.IsAbs(d) {
		return os.ErrInvalid
	}
	f.store = d
	return nil
}
func (f *fakeGitSettings) GitCreateOnPush() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on, nil
}
func (f *fakeGitSettings) SetGitCreateOnPush(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.on = on
	return nil
}

// storeProxy is a proxy over a hub git side whose "forge" is a local bare repository with a commit on
// master.
type storeProxy struct {
	p     *Proxy
	g     *gitsync.Hub
	dir   string
	forge string
	sha   string
	set   *fakeGitSettings
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitsync.Default.Git(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(out)
}

func newStoreProxy(t *testing.T) *storeProxy {
	t.Helper()
	x := &storeProxy{p: NewProxy(NewHub(Timings{}), nil, "", nil), dir: t.TempDir(), set: &fakeGitSettings{}}
	root := t.TempDir()
	x.forge = filepath.Join(root, "forge.git")
	work := filepath.Join(root, "work")
	gitRun(t, "", "init", "-q", "--bare", "-b", "master", x.forge)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "-A")
	gitRun(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "one")
	gitRun(t, work, "push", "-q", x.forge, "HEAD:refs/heads/master")
	x.sha = gitRun(t, work, "rev-parse", "HEAD")

	x.g = &gitsync.Hub{Dir: x.dir, Rooms: NewHub(Timings{}).GitRooms(), Runner: gitsync.NewRunner(),
		Repos: func() ([]gitsync.Repo, error) { return nil, nil }}
	x.g.Store().Protocols = "file"
	x.g.Store().Forge = func(gitsync.Ref) string { return x.forge }
	x.p.SetGit(x.g)
	x.p.SetGitSettings(x.set, x.dir)
	return x
}

// call makes one request, from loopback unless `remote` says otherwise.
func (x *storeProxy) call(method, path, body, remote string) (int, string) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "http://127.0.0.1:7778"+path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	if remote != "" {
		req.RemoteAddr = remote
	}
	x.p.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

const offLoopback = "192.0.2.7:5555"

func TestTheStoreRoutesAre404UntilWired(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	for _, c := range [][2]string{{"POST", "/_hub/git/init"}, {"GET", "/_hub/git/repos"}, {"GET", "/_hub/git/settings"}} {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d", c[0], c[1], rec.Code)
		}
	}
	// A hub with git but no settings store: only the settings route is missing.
	x := newStoreProxy(t)
	x.p.SetGitSettings(nil, "")
	if code, _ := x.call("GET", "/_hub/git/settings", "", ""); code != http.StatusNotFound {
		t.Errorf("settings without a store = %d", code)
	}
	if code, _ := x.call("GET", "/_hub/git/repos", "", ""); code != http.StatusOK {
		t.Errorf("repos = %d", code)
	}
}

func TestInitIsLoopbackOperatorOnlyAndAPostAndBounded(t *testing.T) {
	x := newStoreProxy(t)
	body := `{"url":"https://github.com/o/r"}`
	if code, out := x.call("POST", "/_hub/git/init", body, offLoopback); code != http.StatusForbidden || !strings.Contains(out, "machine the hub runs on") {
		t.Fatalf("off loopback = %d %s", code, out)
	}
	// Through a proxy on the machine, which is not the operator either.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:7778/_hub/git/init", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	x.p.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("via a proxy = %d", rec.Code)
	}
	// And a share that keeps its public Host, from loopback.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "https://atrium.example.org/_hub/git/init", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	x.p.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("via a share with a public host = %d", rec.Code)
	}
	if entries, _ := os.ReadDir(filepath.Join(x.dir, "git")); len(entries) != 0 {
		t.Fatalf("a refused init made %v", entries)
	}
	for _, m := range []string{"GET", "PUT", "DELETE"} {
		if code, _ := x.call(m, "/_hub/git/init", body, ""); code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", m, code)
		}
	}
	// A body past the limit is not read to the end, so its url is never seen.
	huge := `{"url":"https://github.com/o/r","pad":"` + strings.Repeat("x", 8<<10) + `"}`
	if code, _ := x.call("POST", "/_hub/git/init", huge, ""); code != http.StatusBadRequest {
		t.Errorf("a huge body = %d, want 400", code)
	}
	if code, _ := x.call("POST", "/_hub/git/init", `not json`, ""); code != http.StatusBadRequest {
		t.Errorf("not json = %d", code)
	}
	if code, _ := x.call("POST", "/_hub/git/init", `{"url":"`+strings.Repeat("a", 3000)+`"}`, ""); code != http.StatusBadRequest {
		t.Errorf("a long url = %d", code)
	}
}

func TestInitAnswersTheExactJSONAndRefusesTheHostile(t *testing.T) {
	x := newStoreProxy(t)
	code, out := x.call("POST", "/_hub/git/init", `{"url":"https://github.com/o/r"}`, "")
	want := `{"repo":"github/o/r","created":true,"seeded":true,"main":"` + x.sha + `","note":"created, and main seeded from the forge's default branch"}`
	if code != http.StatusOK || out != want {
		t.Fatalf("init = %d\n got %s\nwant %s", code, out, want)
	}
	// The second is the no-op, in the same shape.
	code, out = x.call("POST", "/_hub/git/init", `{"url":"https://github.com/o/r.git"}`, "")
	want = `{"repo":"github/o/r","created":false,"seeded":false,"main":"` + x.sha + `","note":"already there, main left alone"}`
	if code != http.StatusOK || out != want {
		t.Fatalf("second init = %d\n got %s\nwant %s", code, out, want)
	}
	for url, wantCode := range map[string]int{
		"https://ghp_TOPSECRET@github.com/o/r2": http.StatusBadRequest,
		"https://github.com/../x":               http.StatusBadRequest,
		"https://github.com/O/R":                http.StatusConflict,
		"":                                      http.StatusBadRequest,
	} {
		code, out := x.call("POST", "/_hub/git/init", `{"url":"`+url+`"}`, "")
		if code != wantCode {
			t.Errorf("%q = %d %s, want %d", url, code, out, wantCode)
		}
		if strings.Contains(out, "TOPSECRET") || strings.Contains(out, x.dir) {
			t.Errorf("the refusal repeats a secret or a path: %s", out)
		}
	}
}

func TestReposIsOpenLikeGrowlsAndExactlyShaped(t *testing.T) {
	x := newStoreProxy(t)
	// Empty: exactly this, from anywhere.
	if code, out := x.call("GET", "/_hub/git/repos", "", offLoopback); code != http.StatusOK || out != `{"repos":[]}` {
		t.Fatalf("empty = %d %s", code, out)
	}
	if code, _ := x.call("POST", "/_hub/git/repos", `{}`, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", code)
	}
	if code, _ := x.call("PUT", "/_hub/git/repos", `{}`, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("PUT = %d", code)
	}
	if code, _ := x.call("POST", "/_hub/git/init", `{"url":"https://github.com/o/r"}`, ""); code != http.StatusOK {
		t.Fatal(code)
	}
	code, out := x.call("GET", "/_hub/git/repos", "", offLoopback)
	// main.at is the time of the forge's commit, which the test did not pin, so it is checked apart.
	if code != http.StatusOK || !strings.HasPrefix(out, `{"repos":[{"host":"github","owner":"o","repo":"r",`+
		`"url":"git@hub.atrium:o/r.git","path":"/git/hub/github/o/r.git","main":{"sha":"`+x.sha+`","at":"`) ||
		!strings.HasSuffix(out, `"},"branches":[]}]}`) {
		t.Fatalf("seeded = %d %s", code, out)
	}
	if strings.Contains(out, x.dir) || strings.Contains(out, filepath.ToSlash(x.dir)) {
		t.Fatalf("the answer names a directory on the hub's disk: %s", out)
	}
	// An empty repo of another host.
	x.g.Store().Forge = func(gitsync.Ref) string { return filepath.Join(x.dir, "none.git") }
	if code, _ := x.call("POST", "/_hub/git/init", `{"url":"https://gitlab.com/g/p"}`, ""); code != http.StatusOK {
		t.Fatal(code)
	}
	_, out = x.call("GET", "/_hub/git/repos", "", "")
	if !strings.Contains(out, `{"host":"gitlab.com","owner":"g","repo":"p","url":"git@hub.atrium:gitlab.com/g/p.git",`+
		`"path":"/git/hub/gitlab.com/g/p.git","main":{"sha":"","at":null},"branches":[]}`) {
		t.Fatalf("empty repo = %s", out)
	}
}

func TestSettingsAreLoopbackOnlyAndRoundTrip(t *testing.T) {
	x := newStoreProxy(t)
	alt := filepath.Join(t.TempDir(), "forge")
	for _, m := range []string{"GET", "PUT"} {
		if code, out := x.call(m, "/_hub/git/settings", `{"create_on_push":true}`, offLoopback); code != http.StatusForbidden || strings.Contains(out, x.dir) {
			t.Errorf("%s off loopback = %d %s", m, code, out)
		}
	}
	if x.set.on {
		t.Fatal("a refused PUT wrote")
	}
	if code, _ := x.call("POST", "/_hub/git/settings", `{}`, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", code)
	}
	code, out := x.call("GET", "/_hub/git/settings", "", "")
	if code != http.StatusOK || out != `{"create_on_push":false,"store":"`+jsonPath(filepath.Join(x.dir, "git"))+`"}` {
		t.Fatalf("defaults = %d %s", code, out)
	}
	code, out = x.call("PUT", "/_hub/git/settings", `{"create_on_push":true}`, "")
	if code != http.StatusOK || !strings.Contains(out, `"create_on_push":true`) || !x.set.on || x.set.store != "" {
		t.Fatalf("PUT on = %d %s", code, out)
	}
	code, out = x.call("PUT", "/_hub/git/settings", `{"store":"`+jsonPath(alt)+`"}`, "")
	if code != http.StatusOK || !strings.Contains(out, jsonPath(alt)) || !x.set.on {
		t.Fatalf("PUT store = %d %s", code, out)
	}
	for _, bad := range []string{`{}`, `not json`, `{"store":"relative"}`} {
		if code, _ := x.call("PUT", "/_hub/git/settings", bad, ""); code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d", bad, code)
		}
	}
	if x.set.store != alt {
		t.Fatalf("a refused PUT changed the store: %s", x.set.store)
	}
	if code, out = x.call("PUT", "/_hub/git/settings", `{"store":"","create_on_push":false}`, ""); code != http.StatusOK ||
		!strings.Contains(out, `"create_on_push":false`) || x.set.store != "" {
		t.Fatalf("reset = %d %s", code, out)
	}
}

func jsonPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }
