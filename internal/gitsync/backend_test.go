package gitsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var bg = context.Background()

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := Default.Git(bg, dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(out)
}

// commit makes one commit in a work tree and answers its sha.
func commit(t *testing.T, dir, file, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-m", body)
	return git(t, dir, "rev-parse", "HEAD")
}

// served is a bare repository holding claude/main and a hidden room ref, behind a Backend.
type served struct {
	srv          *httptest.Server
	bare         string
	mainSHA      string
	hiddenSHA    string
	visibleClaud string
}

var (
	sharedOnce sync.Once
	shared     *served
	sharedRoot string
)

// newServed is one repository for every read-only test, because git is slow to start on a
// machine with a virus scanner and each fixture costs seconds.
func newServed(t *testing.T) *served {
	t.Helper()
	sharedOnce.Do(func() {
		root, err := os.MkdirTemp("", "gitsync-shared")
		if err != nil {
			t.Fatal(err)
		}
		sharedRoot = root
		shared = buildServed(t, root)
	})
	if shared == nil {
		t.Fatal("the shared fixture failed to build")
	}
	return shared
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared != nil {
		shared.srv.Close()
	}
	if sharedRoot != "" {
		_ = os.RemoveAll(sharedRoot)
	}
	os.Exit(code)
}

// freshServed is a private copy, for a test that changes it.
func freshServed(t *testing.T) *served {
	t.Helper()
	s := buildServed(t, t.TempDir())
	t.Cleanup(s.srv.Close)
	return s
}

func buildServed(t *testing.T, root string) *served {
	t.Helper()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, work, "init", "-q", "-b", "claude/main")
	s := &served{}
	s.mainSHA = commit(t, work, "a.txt", "one")
	git(t, work, "checkout", "-q", "-b", "hidden")
	s.hiddenSHA = commit(t, work, "secret.txt", "hidden")
	git(t, work, "checkout", "-q", "claude/main")

	s.bare = filepath.Join(root, "git", "github", "o", "r.git")
	if err := os.MkdirAll(s.bare, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, s.bare, "init", "-q", "--bare")
	git(t, work, "push", "-q", s.bare, "claude/main:refs/heads/claude/main", "hidden:refs/rooms/x/claude/a")

	b := &Backend{
		Hide: []string{"refs/rooms"},
		Resolve: func(name string) (string, bool) {
			if name == "github/o/r" {
				return s.bare, true
			}
			return "", false
		},
	}
	s.srv = httptest.NewServer(b)
	return s
}

func (s *served) url() string { return s.srv.URL + "/github/o/r.git" }

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestAFetchWorks(t *testing.T) {
	s := newServed(t)
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	git(t, dst, "fetch", "-q", "--no-tags", s.url(), "+refs/heads/claude/main:refs/remotes/hub/claude/main")
	if got := git(t, dst, "rev-parse", "refs/remotes/hub/claude/main"); got != s.mainSHA {
		t.Fatalf("fetched %s, want %s", got, s.mainSHA)
	}
}

func TestAPushToTheServedURLFails(t *testing.T) {
	s := newServed(t)
	dst := t.TempDir()
	git(t, dst, "init", "-q", "-b", "claude/main")
	commit(t, dst, "b.txt", "pushed")
	if _, err := Default.Git(bg, dst, "push", s.url(), "claude/main:refs/heads/pushed"); err == nil {
		t.Fatal("a push was accepted")
	}
	if out := git(t, s.bare, "for-each-ref", "refs/heads/pushed"); out != "" {
		t.Fatalf("the ref arrived anyway: %s", out)
	}
}

func TestAHiddenRefIsNotAdvertised(t *testing.T) {
	s := newServed(t)
	code, body := get(t, s.url()+"/info/refs?service=git-upload-pack")
	if code != 200 {
		t.Fatalf("info/refs = %d %s", code, body)
	}
	if strings.Contains(body, "refs/rooms") || strings.Contains(body, s.hiddenSHA) {
		t.Fatalf("a hidden ref was advertised:\n%s", body)
	}
	if !strings.Contains(body, "refs/heads/claude/main") {
		t.Fatalf("claude/main was not advertised:\n%s", body)
	}
}

// THE CENTRAL TEST. A client that asks for protocol v2 and names a hidden sha. Protocol
// v2 lets a want be any object the server has, so this only holds because the
// Git-Protocol header is dropped and the server answers in v0.
func TestAHiddenShaFetchedByIDFailsWhenTheClientAsksForV2(t *testing.T) {
	s := newServed(t)
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	_, err := Default.Git(bg, dst, "-c", "protocol.version=2", "fetch", "--no-tags", s.url(), s.hiddenSHA)
	if err == nil {
		t.Fatal("a hidden sha was fetched by id")
	}
	if _, err := Default.Git(bg, dst, "cat-file", "-e", s.hiddenSHA); err == nil {
		t.Fatal("the hidden object is in the client")
	}
}

// The same, past the client's own check: a raw v0 request that wants the hidden sha.
func TestARawWantOfAHiddenShaIsRefused(t *testing.T) {
	s := newServed(t)
	line := "want " + s.hiddenSHA + " no-progress\n"
	body := pkt(line) + "0000" + pkt("done\n")
	req, _ := http.NewRequest("POST", s.url()+"/git-upload-pack", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	req.Header.Set("Git-Protocol", "version=2")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if strings.Contains(string(b), "PACK") {
		t.Fatalf("a pack came back for a hidden sha")
	}
}

func pkt(s string) string {
	const hex = "0123456789abcdef"
	n := len(s) + 4
	return string([]byte{hex[n>>12&15], hex[n>>8&15], hex[n>>4&15], hex[n&15]}) + s
}

func TestTheDumbProtocolIsOff(t *testing.T) {
	s := newServed(t)
	loose := git(t, s.bare, "rev-parse", "refs/rooms/x/claude/a")
	for _, p := range []string{
		"/objects/info/packs",
		"/HEAD",
		"/info/refs",
		"/refs/rooms/x/claude/a",
		"/objects/" + loose[:2] + "/" + loose[2:],
		"/config",
	} {
		code, _ := get(t, s.url()+p)
		if code != 403 && code != 404 {
			t.Errorf("GET %s = %d, want 403 or 404", p, code)
		}
	}
}

func TestReceivePackIsRefused(t *testing.T) {
	s := newServed(t)
	code, _ := get(t, s.url()+"/info/refs?service=git-receive-pack")
	if code != 403 {
		t.Fatalf("receive-pack advertisement = %d", code)
	}
	res, err := http.Post(s.url()+"/git-receive-pack", "application/x-git-receive-pack-request", strings.NewReader("0000"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("receive-pack POST = %d", res.StatusCode)
	}
}

func TestNamesNotOnTheListOrMalformedAre404(t *testing.T) {
	s := newServed(t)
	for _, p := range []string{
		"/github/o/other.git/info/refs?service=git-upload-pack",
		"/github/o/r/info/refs?service=git-upload-pack",
		"/github/o/../o/r.git/info/refs?service=git-upload-pack",
		"/../github/o/r.git/info/refs?service=git-upload-pack",
		"/github/o/%2e%2e/o/r.git/info/refs?service=git-upload-pack",
		"/github/%2E%2E/o/r.git/info/refs?service=git-upload-pack",
		"/C:/github/o/r.git/info/refs?service=git-upload-pack",
		"/c%3A/x.git/info/refs?service=git-upload-pack",
		"/github%5Co%5Cr.git/info/refs?service=git-upload-pack",
		"/github\\o\\r.git/info/refs?service=git-upload-pack",
	} {
		req, err := http.NewRequest("GET", s.srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Built by hand so nothing normalises the path first.
		req.URL.Opaque = "//" + strings.TrimPrefix(s.srv.URL, "http://") + strings.SplitN(p, "?", 2)[0]
		req.URL.RawQuery = "service=git-upload-pack"
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Errorf("GET %s = %d, want 404", p, res.StatusCode)
		}
	}
	if !ValidName("github/o/r") || ValidName("a/../b") || ValidName("a\\b") || ValidName("c:/x") || ValidName("") {
		t.Fatal("ValidName is wrong")
	}
}

func TestNothingIsWrittenIntoTheRepositoryConfig(t *testing.T) {
	s := newServed(t)
	before, _ := os.ReadFile(filepath.Join(s.bare, "config"))
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	git(t, dst, "fetch", "-q", "--no-tags", s.url(), "+refs/heads/claude/main:refs/remotes/hub/claude/main")
	after, _ := os.ReadFile(filepath.Join(s.bare, "config"))
	if string(before) != string(after) {
		t.Fatalf("the served repository's config changed:\n%s", after)
	}
	if strings.Contains(string(after), "hideRefs") || strings.Contains(string(after), "receivepack") {
		t.Fatalf("server config was written into the repository:\n%s", after)
	}
}

// A GIT_DIR in the parent environment must not reach any git we run, nor the CGI child.
func TestAGitDirInTheParentEnvironmentDoesNotLeak(t *testing.T) {
	decoy := t.TempDir()
	t.Setenv("GIT_DIR", filepath.Join(decoy, "nope"))
	t.Setenv("GIT_WORK_TREE", decoy)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy, "idx"))
	for _, kv := range CleanEnv() {
		if strings.HasPrefix(strings.ToUpper(kv), "GIT_DIR=") ||
			strings.HasPrefix(strings.ToUpper(kv), "GIT_WORK_TREE=") ||
			strings.HasPrefix(strings.ToUpper(kv), "GIT_INDEX_FILE=") {
			t.Fatalf("CleanEnv kept %s", kv)
		}
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("git init went somewhere else: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decoy, "nope")); err == nil {
		t.Fatal("git acted on the GIT_DIR from the parent environment")
	}
	// And the served side.
	s := newServed(t)
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	git(t, dst, "fetch", "-q", "--no-tags", s.url(), "+refs/heads/claude/main:refs/remotes/hub/claude/main")
}

func TestStopCancelsAndWaitsForGitChildren(t *testing.T) {
	r := NewRunner()
	done := make(chan error, 1)
	go func() {
		// A fetch from a listener that never answers.
		_, err := r.Git(bg, t.TempDir(), "ls-remote", "http://127.0.0.1:1/x.git")
		done <- err
	}()
	if !r.Stop(5e9) {
		t.Fatal("children did not exit within the bound")
	}
	<-done
	if _, err := r.Git(bg, "", "version"); err != ErrStopped {
		t.Fatalf("a stopped runner ran a command: %v", err)
	}
}
