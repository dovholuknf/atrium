package gitsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tests run without a network. The clone comes from a local bare repository through SCM.source,
// which only this package's tests can set. Production always clones from the https URL.

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = CleanEnv("GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// bareSource makes a bare repository with one commit.
func bareSource(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	work := filepath.Join(d, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, work, "add", "f")
	gitT(t, work, "commit", "-q", "-m", "one")
	bare := filepath.Join(d, "src.git")
	gitT(t, d, "clone", "-q", "--bare", work, bare)
	return bare
}

func newSCM(t *testing.T) (*SCM, string) {
	t.Helper()
	root := t.TempDir()
	src := bareSource(t)
	c := &SCM{
		Runner: NewRunner(),
		Root:   func() string { return root },
		source: func(Ref) string { return src },
		HubURL: func(r Ref) (string, error) { return StableHubURL("127.0.0.1:7780", r), nil },
	}
	return c, root
}

func TestCloneMakesTheCloneAtHostOwnerRepo(t *testing.T) {
	c, root := newSCM(t)
	res, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(root, "github", "acme", "widget"))
	got, _ := filepath.EvalSymlinks(res.Path)
	wantR, _ := filepath.EvalSymlinks(want)
	if filepath.ToSlash(got) != filepath.ToSlash(wantR) || res.State != "cloned" {
		t.Fatalf("path %q state %q, want %q cloned", res.Path, res.State, want)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "f")); err != nil {
		t.Fatalf("the clone has no files: %v", err)
	}
	if res.Hub != "hub" {
		t.Fatalf("hub remote = %q (%s)", res.Hub, res.Note)
	}
	if u := strings.TrimSpace(gitT(t, res.Path, "remote", "get-url", "hub")); u != "http://127.0.0.1:7780/git/hub/github/acme/widget.git" {
		t.Fatalf("hub url = %q", u)
	}
	// The scp form is the same repository, so the same place.
	again, err := c.Clone(context.Background(), "git@github.com:acme/widget.git")
	if err != nil || again.State != "existing" || again.Path != res.Path {
		t.Fatalf("second clone: %+v %v", again, err)
	}
}

func TestCloneOfAtriumsOwnPushToOriginFailsOnTheGuardURL(t *testing.T) {
	c, _ := newSCM(t)
	res, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitT(t, res.Path, "config", "remote.origin.pushurl")); got != OriginPushURL {
		t.Fatalf("pushurl = %q", got)
	}
	gitT(t, res.Path, "branch", "x")
	cmd := exec.Command("git", "push", "origin", "x")
	cmd.Dir = res.Path
	cmd.Env = CleanEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a push to origin worked:\n%s", out)
	}
	if !strings.Contains(string(out), "atrium-refused") {
		t.Fatalf("the push failed, but not on the guard URL:\n%s", out)
	}
	// And fetching from origin still works.
	gitT(t, res.Path, "fetch", "origin")
}

func TestFailedCloneAnswersTheExactSentence(t *testing.T) {
	c, root := newSCM(t)
	c.source = func(Ref) string { return filepath.Join(t.TempDir(), "nope.git") }
	_, err := c.Clone(context.Background(), "https://github.com/acme/private")
	if err == nil || err.Error() != "that repo doesn't exist, check it, and if it is private have the operator clone it. Atrium can't." {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(root, "github", "acme", "private")); serr == nil {
		t.Fatal("a failed clone left its folder behind")
	}
}

func TestCloneRefusesBadURLsAndNamesNothingBack(t *testing.T) {
	c, root := newSCM(t)
	for _, u := range []string{
		"file:///etc/passwd",
		"ext::sh -c touch /tmp/x",
		"--upload-pack=touch /tmp/x",
		"-oProxyCommand=x",
		"http://github.com/a/b",
		"ssh://git@github.com/a/b",
		"https://tok:en@github.com/a/b",
		"https://github.com/../b",
		"https://github.com/a/..",
		"https://github.com/a/b/c",
		"https://github.com/-a/b",
		"git@github.com:a/--b",
		"git@github.com:../../b",
		"https://github.com/a%2fb/c",
		"https://github.com:8443/a/b",
		"https://github.com/a/b\nhttps://x/y",
		"",
	} {
		_, err := c.Clone(context.Background(), u)
		if err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("%q: err = %v, want a refusal", u, err)
		}
	}
	if ents, _ := os.ReadDir(root); len(ents) != 0 {
		t.Fatalf("a refused URL made something in the scm folder: %v", ents)
	}
}

func TestCloneRefusesADestinationThatLeavesTheSCMFolderBySymlink(t *testing.T) {
	c, root := newSCM(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "github")); err != nil {
		t.Skip("no symlinks here: " + err.Error())
	}
	_, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if err == nil || !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("something was made outside the scm folder: %v", ents)
	}
}

func TestCloneWithNoSCMRootRefusesAndSaysHow(t *testing.T) {
	c, _ := newSCM(t)
	c.Root = func() string { return "  " }
	_, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if !errors.Is(err, ErrNoSCMRoot) || !strings.Contains(err.Error(), "git.scm_root") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloneLeavesANonCloneFolderAlone(t *testing.T) {
	c, root := newSCM(t)
	d := filepath.Join(root, "github", "acme", "widget")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "mine"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Clone(context.Background(), "https://github.com/acme/widget"); !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "mine")); err != nil {
		t.Fatal("the operator's file is gone")
	}
}

// operatorClone makes a clone the way an operator would: plain git, nothing of atrium's in it.
func operatorClone(t *testing.T, c *SCM, root string) string {
	t.Helper()
	d := filepath.Join(root, "github", "acme", "widget")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, filepath.Dir(d), "clone", "-q", c.source(Ref{}), d)
	return d
}

func TestAnOperatorsCloneIsUntouchedUntilOneYes(t *testing.T) {
	c, root := newSCM(t)
	d := operatorClone(t, c, root)
	asked := 0
	c.Yes = func(context.Context, string) error { asked++; return ErrAsked }

	if _, err := c.Clone(context.Background(), "https://github.com/acme/widget"); !errors.Is(err, ErrAsked) {
		t.Fatalf("err = %v", err)
	}
	if remotes := strings.TrimSpace(gitT(t, d, "remote")); remotes != "origin" {
		t.Fatalf("remotes before the yes = %q", remotes)
	}
	if out, _ := exec.Command("git", "-C", d, "config", "remote.origin.pushurl").Output(); len(out) != 0 {
		t.Fatalf("pushurl set before the yes: %s", out)
	}

	c.Yes = func(context.Context, string) error { asked++; return nil }
	res, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if err != nil || res.State != "existing" || res.Hub != "hub" {
		t.Fatalf("after the yes: %+v %v", res, err)
	}
	if got := strings.TrimSpace(gitT(t, d, "config", "remote.origin.pushurl")); got != OriginPushURL {
		t.Fatalf("pushurl after the yes = %q", got)
	}
	// ONE yes: it is remembered in the clone, so there is no second question.
	c.Yes = func(context.Context, string) error { t.Fatal("asked a second time"); return nil }
	if _, err := c.Clone(context.Background(), "https://github.com/acme/widget"); err != nil {
		t.Fatal(err)
	}
	if asked != 2 {
		t.Fatalf("asked %d times", asked)
	}
}

func TestADeniedYesChangesNothing(t *testing.T) {
	c, root := newSCM(t)
	d := operatorClone(t, c, root)
	c.Yes = func(context.Context, string) error { return ErrDenied }
	if _, err := c.Clone(context.Background(), "https://github.com/acme/widget"); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if remotes := strings.TrimSpace(gitT(t, d, "remote")); remotes != "origin" {
		t.Fatalf("remotes = %q", remotes)
	}
}

func TestAnExistingHubRemoteElsewhereIsLeftAloneAndAtriumHubIsAdded(t *testing.T) {
	c, root := newSCM(t)
	d := operatorClone(t, c, root)
	gitT(t, d, "remote", "add", "hub", "https://example.org/theirs.git")
	c.Yes = func(context.Context, string) error { return nil }
	res, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	if res.Hub != "atrium-hub" || !strings.Contains(res.Note, "left alone") {
		t.Fatalf("result = %+v", res)
	}
	if u := strings.TrimSpace(gitT(t, d, "remote", "get-url", "hub")); u != "https://example.org/theirs.git" {
		t.Fatalf("the operator's hub was changed to %q", u)
	}
	if u := strings.TrimSpace(gitT(t, d, "remote", "get-url", "atrium-hub")); !strings.HasPrefix(u, "http://127.0.0.1:7780/git/hub/") {
		t.Fatalf("atrium-hub = %q", u)
	}
	// origin stays the forge's: atrium never rewrote its fetch URL.
	if u := strings.TrimSpace(gitT(t, d, "remote", "get-url", "origin")); u != c.source(Ref{}) {
		t.Fatalf("origin = %q", u)
	}
}

func TestWithNoHubForwarderTheCloneStillWorksAndSaysSo(t *testing.T) {
	c, _ := newSCM(t)
	c.HubURL = nil
	res, err := c.Clone(context.Background(), "https://github.com/acme/widget")
	if err != nil || res.Hub != "" || !strings.Contains(res.Note, "hub remote was not added") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestHTTPSURLIsBuiltFromTheCheckedParts(t *testing.T) {
	r, err := ParseURL("git@github.com:acme/widget.git")
	if err != nil {
		t.Fatal(err)
	}
	if got := httpsURL(r); got != "https://github.com/acme/widget.git" {
		t.Fatal(got)
	}
	r, _ = ParseURL("https://git.example.org/a/b")
	if got := httpsURL(r); got != "https://git.example.org/a/b.git" {
		t.Fatal(got)
	}
}

func TestCredentialHelperIsScopedToTheCheckedHostAndOnlyForListedHosts(t *testing.T) {
	c, _ := newSCM(t)
	c.CredentialHelper = func() string { return "store" }
	gh, _ := ParseURL("https://github.com/a/b")
	other, _ := ParseURL("https://collector.example/a/b")

	if got := strings.Join(c.credentialArgs(gh), " "); got != "-c credential.https://github.com.helper=store" {
		t.Fatalf("github args = %q", got)
	}
	if got := c.credentialArgs(other); got != nil {
		t.Fatalf("a host not in git.credential_hosts got %v", got)
	}
	c.CredentialHosts = func() string { return "git.example.org, Collector.Example" }
	if got := strings.Join(c.credentialArgs(other), " "); got != "-c credential.https://collector.example.helper=store" {
		t.Fatalf("listed host args = %q", got)
	}
	if got := c.credentialArgs(gh); got != nil {
		t.Fatalf("github was not listed but got %v", got)
	}
	c.CredentialHelper = func() string { return "" }
	if got := c.credentialArgs(other); got != nil {
		t.Fatalf("no helper still gave %v", got)
	}
}

// A helper for another host is never called: the clone is of a stub https server that answers 401,
// the helper writes a marker file when git runs it, and the marker must not exist afterwards.
func TestAHelperIsNeverCalledForAHostNotListed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	script := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+filepath.ToSlash(marker)+"'\necho username=u\necho password=p\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		hosts  string
		called bool
	}{
		// The clone's host is "github" -> github.com, which is listed, but the stub is at 127.0.0.1:
		// the scoped key is for github.com, so the stub is never asked either.
		{"default list", "", false},
		{"another list", "example.org", false},
	} {
		os.Remove(marker)
		c, _ := newSCM(t)
		c.source = func(Ref) string { return srv.URL + "/a/b.git" }
		c.extra = []string{"-c", "http.sslVerify=false"}
		c.CredentialHelper = func() string { return "!" + filepath.ToSlash(script) }
		c.CredentialHosts = func() string { return tc.hosts }
		if _, err := c.Clone(context.Background(), "https://github.com/acme/widget"); err == nil {
			t.Fatalf("%s: a 401 stub cloned", tc.name)
		}
		if _, err := os.Stat(marker); (err == nil) != tc.called {
			t.Errorf("%s: helper called = %v, want %v", tc.name, err == nil, tc.called)
		}
	}
}

func TestStableHubURLNeverNamesAllInterfaces(t *testing.T) {
	r := Ref{Host: "github", Owner: "a", Repo: "b"}
	for _, addr := range []string{"0.0.0.0:7782", ":7782", "[::]:7782", "127.0.0.1:7782"} {
		if got := StableHubURL(addr, r); got != "http://127.0.0.1:7782/git/hub/github/a/b.git" {
			t.Errorf("%s -> %s", addr, got)
		}
	}
}
