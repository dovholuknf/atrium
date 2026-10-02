package gitsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseURLTakesEveryShapeAndMapsTheHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/openziti/zrok":       "github/openziti/zrok",
		"https://github.com/openziti/zrok.git":   "github/openziti/zrok",
		"https://github.com/openziti/zrok/":      "github/openziti/zrok",
		"https://github.com/openziti/zrok.git/":  "github/openziti/zrok",
		"  https://GitHub.com/OpenZiti/zrok  ":   "github/OpenZiti/zrok",
		"git@github.com:openziti/zrok.git":       "github/openziti/zrok",
		"git@github.com:openziti/zrok":           "github/openziti/zrok",
		"git@github.com:openziti/zrok/":          "github/openziti/zrok",
		"https://gitlab.com/some/thing.git":      "gitlab.com/some/thing",
		"https://Git.Example.ORG/o/.github":      "git.example.org/o/.github",
		"git@git.example.org:o/r.git":            "git.example.org/o/r",
		"https://github.com/o/r.with.dots_and-x": "github/o/r.with.dots_and-x",
	} {
		ref, err := ParseURL(in)
		if err != nil {
			t.Errorf("%q refused: %v", in, err)
			continue
		}
		if ref.Name() != want {
			t.Errorf("%q = %s, want %s", in, ref.Name(), want)
		}
	}
}

func TestParseURLRefusesTheHostileOnes(t *testing.T) {
	long := "https://github.com/o/" + strings.Repeat("a", 5000)
	for name, in := range map[string]string{
		"a token":            "https://ghp_SECRETTOKEN123@github.com/o/r",
		"user and password":  "https://user:hunter2@github.com/o/r.git",
		"x-access-token":     "https://x-access-token:ghs_abc@github.com/o/r",
		"an empty password":  "https://user:@github.com/o/r",
		"scp with a user":    "bob@github.com:o/r.git",
		"dotdot owner":       "https://github.com/../r",
		"dotdot repo":        "https://github.com/o/..",
		"dotdot in the path": "https://github.com/o/../../etc/passwd",
		"encoded dotdot":     "https://github.com/o/%2e%2e",
		"encoded slash":      "https://github.com/o%2fx/r",
		"backslash":          "https://github.com/o\\x/r",
		"backslash scp":      "git@github.com:o\\x/r.git",
		"drive letter":       "C:\\git\\repo",
		"drive letter fwd":   "C:/git/repo",
		"a newline":          "https://github.com/o/r\nhttps://evil.example/x",
		"a carriage return":  "https://github.com/o/r\r",
		"a tab":              "https://github.com/o/\tr",
		"huge":               long,
		"empty":              "",
		"only spaces":        "   ",
		"http":               "http://github.com/o/r",
		"git scheme":         "git://github.com/o/r",
		"ssh scheme":         "ssh://git@github.com/o/r",
		"file scheme":        "file:///etc/passwd",
		"a port":             "https://github.com:8443/o/r",
		"a query":            "https://github.com/o/r?x=1",
		"a fragment":         "https://github.com/o/r#x",
		"too deep":           "https://github.com/o/r/extra",
		"gitlab subgroup":    "https://gitlab.com/g/sub/r",
		"too shallow":        "https://github.com/o",
		"no path":            "https://github.com",
		"an option":          "--upload-pack=touch /tmp/x",
		"a bad host":         "https://-evil.example/o/r",
		"host with dotdot":   "https://a..b/o/r",
		"unicode":            "https://github.com/o/réponse",
		"a space":            "https://github.com/o/a b",
		"windows reserved":   "https://github.com/o/NUL",
		"reserved with ext":  "https://github.com/o/con.txt",
		"trailing dot":       "https://github.com/o/r.",
		"dot git":            "https://github.com/o/.git",
		"owner dot git":      "https://github.com/x.git/r",
		"dot repo":           "https://github.com/o/.",
	} {
		ref, err := ParseURL(in)
		if err == nil {
			t.Errorf("%s: %q was taken as %s", name, in, ref.Name())
			continue
		}
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v is not a refusal", name, err)
		}
		// NEVER ECHOED: a refused URL may hold a token.
		for _, secret := range []string{"SECRETTOKEN", "hunter2", "ghs_abc"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the refusal repeats the secret: %v", name, err)
			}
		}
	}
}

func TestParseNameAcceptsShortAndFull(t *testing.T) {
	for in, want := range map[string]string{
		"openziti/zrok":              "github/openziti/zrok",
		"github/openziti/zrok":       "github/openziti/zrok",
		"github.com/openziti/zrok":   "github/openziti/zrok",
		"gitlab.com/o/r":             "gitlab.com/o/r",
		"openziti/zrok.git":          "github/openziti/zrok",
		"GITHUB/openziti/zrok":       "github/openziti/zrok",
		"github/dovholuknf/atrium.x": "github/dovholuknf/atrium.x",
	} {
		ref, err := ParseName(in)
		if err != nil || ref.Name() != want {
			t.Errorf("%q = %v, %v, want %s", in, ref, err, want)
		}
	}
	for _, in := range []string{"", "zrok", "a/b/c/d", "../x", "a/../b", "a\\b/c", "C:/a/b", "a//b", "/a/b", "a/b/",
		"github/o/NUL", "a/b%2e/c", "a/b\x00/c", "github/o/.git", strings.Repeat("a/", 200) + "b"} {
		if ref, err := ParseName(in); err == nil {
			t.Errorf("%q was taken as %s", in, ref.Name())
		}
	}
}

// forge is a local bare repository standing in for a forge: no network.
type forge struct {
	dir  string
	work string
}

func newForge(t *testing.T, branch string) *forge {
	t.Helper()
	root := t.TempDir()
	f := &forge{dir: filepath.Join(root, "forge.git"), work: filepath.Join(root, "work")}
	git(t, "", "init", "-q", "--bare", "-b", branch, f.dir)
	if err := os.MkdirAll(f.work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, f.work, "init", "-q", "-b", branch)
	return f
}

func (f *forge) push(t *testing.T, branch, file, body string, at int64) string {
	t.Helper()
	commitAt(t, f.work, file, body, at)
	git(t, f.work, "push", "-q", f.dir, "HEAD:refs/heads/"+branch)
	return git(t, f.work, "rev-parse", "HEAD")
}

// commitAt is commit with a committer time, so the order of branches and `main.at` are fixed.
func commitAt(t *testing.T, dir, file, body string, at int64) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	if at < 1_000_000_000 {
		at += 1_600_000_000
	}
	date := strconv.FormatInt(at, 10) + " +0000"
	if _, err := Default.GitEnv(bg, dir, []string{"GIT_COMMITTER_DATE=" + date, "GIT_AUTHOR_DATE=" + date},
		"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", body); err != nil {
		t.Fatal(err)
	}
}

type storeFixture struct {
	h     *Hub
	s     *Store
	root  string
	forge *forge
}

// newStore is a hub with an empty store whose "forge" is a local repository whose default branch is
// `branch`.
func newStore(t *testing.T, branch string) *storeFixture {
	t.Helper()
	root := t.TempDir()
	x := &storeFixture{root: root, forge: newForge(t, branch)}
	x.h = &Hub{Dir: root, Runner: NewRunner(), Repos: func() ([]Repo, error) { return nil, nil }}
	x.s = x.h.Store()
	x.s.Protocols = "file"
	x.s.Forge = func(Ref) string { return x.forge.dir }
	return x
}

const repoURL = "https://github.com/o/r"

func TestInitSeedsMainEvenWhenTheForgeCallsItMaster(t *testing.T) {
	x := newStore(t, "master")
	want := x.forge.push(t, "master", "a.txt", "one", 1000)
	x.forge.push(t, "feature", "b.txt", "two", 2000)

	res, err := x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !res.Seeded || res.Main != want || res.Repo != "github/o/r" {
		t.Fatalf("result = %+v, want main %s", res, want)
	}
	dir, _ := x.s.Path("o/r")
	if dir != filepath.Join(x.root, "git", "github", "o", "r.git") {
		t.Fatalf("path = %s", dir)
	}
	if got := git(t, dir, "rev-parse", "refs/heads/main"); got != want {
		t.Fatalf("main = %s, want %s", got, want)
	}
	if got := git(t, dir, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
		t.Fatalf("HEAD = %s", got)
	}
	// ONLY THE DEFAULT BRANCH: the forge's other branches are not fetched, and master is not kept.
	if refs := git(t, dir, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/main" {
		t.Fatalf("refs = %q", refs)
	}
	if !x.s.Exists("o/r") || !x.s.Exists("github/o/r") || x.s.Exists("o/other") {
		t.Fatal("Exists is wrong")
	}
}

func TestASecondInitIsANoOpAndNeverFetchesAgain(t *testing.T) {
	x := newStore(t, "master")
	first := x.forge.push(t, "master", "a.txt", "one", 1000)
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	// The forge moves on, and the forge is then broken. Neither may matter.
	x.forge.push(t, "master", "a.txt", "two", 2000)
	var asked int
	x.s.Forge = func(Ref) string { asked++; return x.forge.dir }
	for _, in := range []string{repoURL, repoURL + ".git", "git@github.com:o/r.git", repoURL + "/"} {
		res, err := x.s.Init(bg, in)
		if err != nil {
			t.Fatal(err)
		}
		if res.Created || res.Seeded || res.Main != first || !strings.Contains(res.Note, "already there") {
			t.Fatalf("second init = %+v", res)
		}
	}
	if asked != 0 {
		t.Fatalf("the forge was asked %d times by a second init", asked)
	}
	dir, _ := x.s.Path("o/r")
	if got := git(t, dir, "rev-parse", "refs/heads/main"); got != first {
		t.Fatalf("main moved to %s", got)
	}
}

func TestAnUnreachableForgeLeavesAnEmptyRepoAndASentence(t *testing.T) {
	x := newStore(t, "main")
	x.s.Forge = func(Ref) string { return filepath.Join(x.root, "no", "such.git") }
	res, err := x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Seeded || res.Main != "" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.Note, "does not exist on the forge or is private") || !strings.Contains(res.Note, PushMain) {
		t.Fatalf("note = %q", res.Note)
	}
	// A repository that is not there is not a network error, so it is not told to rerun.
	if strings.Contains(res.Note, "run init again") {
		t.Fatalf("note tells a missing repository to rerun: %q", res.Note)
	}
	dir, _ := x.s.Path("o/r")
	if git(t, dir, "symbolic-ref", "HEAD") != "refs/heads/main" || git(t, dir, "for-each-ref") != "" {
		t.Fatal("not an empty repository with HEAD on main")
	}
}

func TestAPrivateForgeThatAsksForACredentialIsToldApart(t *testing.T) {
	x := newStore(t, "main")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="forge"`)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
	}))
	defer srv.Close()
	x.s.Protocols = "http"
	x.s.Forge = func(Ref) string { return srv.URL + "/o/r.git" }
	res, err := x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Seeded || !strings.Contains(res.Note, "does not exist on the forge or is private") || !strings.Contains(res.Note, PushMain) {
		t.Fatalf("result = %+v", res)
	}
}

func TestANetworkErrorSaysToRerunAndTheRerunSeeds(t *testing.T) {
	x := newStore(t, "master")
	want := x.forge.push(t, "master", "a.txt", "one", 1000)
	// Nothing listens on port 1.
	x.s.Protocols = "http"
	x.s.Forge = func(Ref) string { return "http://127.0.0.1:1/o/r.git" }
	res, err := x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Seeded || !res.Created || res.Main != "" ||
		!strings.Contains(res.Note, "could not be reached") || !strings.Contains(res.Note, PushMain) ||
		!strings.Contains(res.Note, "run init again") {
		t.Fatalf("result = %+v", res)
	}

	// The network is back: the rerun SEEDS, since main is still absent.
	x.s.Protocols = "file"
	x.s.Forge = func(Ref) string { return x.forge.dir }
	res, err = x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !res.Seeded || res.Main != want {
		t.Fatalf("rerun = %+v", res)
	}
	// And now it is the no-op.
	x.forge.push(t, "master", "a.txt", "two", 2000)
	res, _ = x.s.Init(bg, repoURL)
	if res.Seeded || res.Main != want || !strings.Contains(res.Note, "already there") {
		t.Fatalf("third = %+v", res)
	}
}

func TestAnEmptyRepoThatTheOperatorPushedMainToIsLeftAlone(t *testing.T) {
	x := newStore(t, "master")
	x.s.Forge = func(Ref) string { return filepath.Join(x.root, "no", "such.git") }
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	dir, _ := x.s.Path("o/r")
	pushed := x.forge.push(t, "master", "a.txt", "operator", 1000)
	git(t, x.forge.work, "push", "-q", dir, "HEAD:refs/heads/main")
	// The forge is reachable now, with other history. The operator's main must stand.
	x.forge.push(t, "master", "a.txt", "forge", 2000)
	x.s.Forge = func(Ref) string { return x.forge.dir }
	res, err := x.s.Init(bg, repoURL)
	if err != nil || res.Seeded || res.Main != pushed {
		t.Fatalf("init over a pushed main = %+v, %v", res, err)
	}
}

func TestAnEmptyForgeRepositoryMakesAnEmptyRepo(t *testing.T) {
	x := newStore(t, "main")
	res, err := x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Seeded || res.Main != "" || !strings.Contains(res.Note, PushMain) {
		t.Fatalf("result = %+v", res)
	}
}

// Nothing of the URL, the forge or a credential is written into the new repository.
func TestTheNewRepoHoldsNoURLOrCredential(t *testing.T) {
	x := newStore(t, "master")
	x.forge.push(t, "master", "a.txt", "one", 1000)
	if _, err := x.s.Init(bg, "git@github.com:o/r.git"); err != nil {
		t.Fatal(err)
	}
	dir, _ := x.s.Path("o/r")
	cfg, err := os.ReadFile(filepath.Join(dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"url", "remote", x.forge.dir, "github.com", "credential", "http", "origin", "receivepack", "extraheader"} {
		if strings.Contains(strings.ToLower(string(cfg)), strings.ToLower(bad)) {
			t.Errorf("the config holds %q:\n%s", bad, cfg)
		}
	}
	// Nor anywhere else a name could land, and git agrees there is no remote.
	if out := git(t, dir, "remote"); out != "" {
		t.Errorf("remotes = %q", out)
	}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(p, "objects") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), x.forge.dir) {
			t.Errorf("%s holds the forge's address", p)
		}
		return nil
	})
}

// No receive-pack and no hook: the bare repository is ready for them and has neither.
func TestTheNewRepoIsReadyForReceiveAndHasNoHook(t *testing.T) {
	x := newStore(t, "main")
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	dir, _ := x.s.Path("o/r")
	if out, err := Default.Git(bg, dir, "config", "--get", "http.receivepack"); err == nil {
		t.Fatalf("http.receivepack is set: %q", out)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "hooks"))
	for _, e := range ents {
		t.Errorf("a hook file: %s", e.Name())
	}
	if git(t, dir, "rev-parse", "--is-bare-repository") != "true" {
		t.Fatal("not bare")
	}
}

// The operator's own git config is not read: a global insteadOf must not redirect the seed.
func TestTheSeedIgnoresTheOperatorsGlobalGitConfig(t *testing.T) {
	x := newStore(t, "master")
	want := x.forge.push(t, "master", "a.txt", "one", 1000)
	home := t.TempDir()
	cfg := "[url \"" + filepath.ToSlash(filepath.Join(x.root, "nowhere")) + "/\"]\n\tinsteadOf = " + filepath.ToSlash(filepath.Dir(x.forge.dir)) + "/\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	res, err := x.s.Init(bg, repoURL)
	if err != nil || res.Main != want {
		t.Fatalf("a global insteadOf was obeyed: %+v, %v", res, err)
	}
	env := strings.Join(x.s.env(), "\n")
	for _, must := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_ALLOW_PROTOCOL="} {
		if !strings.Contains(env, must) {
			t.Errorf("env lacks %s", must)
		}
	}
	x2 := newStore(t, "x")
	x2.s.Protocols = ""
	if !strings.Contains(strings.Join(x2.s.env(), "\n"), "GIT_ALLOW_PROTOCOL=https") {
		t.Error("the default is not https alone")
	}
}

func TestCaseCollisionsAreRefusedAndSaidSo(t *testing.T) {
	x := newStore(t, "main")
	if _, err := x.s.Init(bg, "https://github.com/foo/bar"); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"https://github.com/Foo/Bar", "https://github.com/foo/BAR", "https://github.com/FOO/bar.git"} {
		_, err := x.s.Init(bg, in)
		if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "github/foo/bar") ||
			!strings.Contains(err.Error(), "ignores case") {
			t.Errorf("%s: %v", in, err)
		}
	}
	// The same name again is the no-op, not a collision.
	if res, err := x.s.Init(bg, "https://github.com/foo/bar"); err != nil || res.Created {
		t.Fatalf("same name: %+v %v", res, err)
	}
	// And nothing differently cased was made.
	ents, _ := os.ReadDir(filepath.Join(x.root, "git", "github"))
	if len(ents) != 1 || ents[0].Name() != "foo" {
		t.Fatalf("owners = %v", ents)
	}
	// An owner that differs only in case, with a different repository, collides too.
	if _, err := x.s.Init(bg, "https://github.com/FOO/other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("owner case: %v", err)
	}
	// A different owner is fine.
	if _, err := x.s.Init(bg, "https://github.com/foo2/bar"); err != nil {
		t.Fatal(err)
	}
}

func TestInitRefusesWhatIsAlreadyThereAndIsNotARepository(t *testing.T) {
	x := newStore(t, "main")
	d := filepath.Join(x.root, "git", "github", "o", "r.git")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := x.s.Init(bg, repoURL)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "keep.txt")); string(b) != "mine" {
		t.Fatal("the directory was touched")
	}
	if x.s.Exists("o/r") {
		t.Fatal("Exists says yes")
	}
}

func TestInitRemovesWhatItMadeWhenGitFails(t *testing.T) {
	x := newStore(t, "main")
	x.h.Runner = NewRunner()
	x.h.Runner.Stop(time.Second)
	if _, err := x.s.Init(bg, repoURL); err == nil {
		t.Fatal("init worked with a stopped runner")
	}
	if _, err := os.Stat(filepath.Join(x.root, "git", "github", "o", "r.git")); err == nil {
		t.Fatal("a half-made repository was left")
	}
}

func TestReftableOnlyWhereGitHasItAndTheProbeWorks(t *testing.T) {
	realVer, err := Default.Git(bg, "", "--version")
	if err != nil {
		t.Fatal(err)
	}
	realOK := false
	if m := verRe.FindStringSubmatch(realVer); m != nil {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		realOK = maj > 2 || (maj == 2 && min >= 45)
	}
	format := func(t *testing.T, x *storeFixture) string {
		t.Helper()
		if _, err := x.s.Init(bg, repoURL); err != nil {
			t.Fatal(err)
		}
		dir, _ := x.s.Path("o/r")
		if _, err := os.Stat(filepath.Join(dir, "reftable")); err == nil {
			return "reftable"
		}
		return "files"
	}
	cases := []struct {
		name    string
		version string
		probe   func(context.Context) bool
		want    string
	}{
		{"old git", "git version 2.44.1", nil, "files"},
		{"old git, probe would pass", "git version 2.44.1", func(context.Context) bool { return true }, "files"},
		{"new git, probe fails", "git version 2.45.0", func(context.Context) bool { return false }, "files"},
		{"unreadable version", "git version banana", func(context.Context) bool { return true }, "files"},
		{"windows build", "git version 2.31.0.windows.1", func(context.Context) bool { return true }, "files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newStore(t, "main")
			x.s.GitVersion = func(context.Context) (string, error) { return c.version, nil }
			x.s.Probe = c.probe
			if got := format(t, x); got != c.want {
				t.Fatalf("format = %s, want %s", got, c.want)
			}
		})
	}
	t.Run("version error", func(t *testing.T) {
		x := newStore(t, "main")
		x.s.GitVersion = func(context.Context) (string, error) { return "", errors.New("no") }
		if got := format(t, x); got != "files" {
			t.Fatal(got)
		}
	})
	if !realOK {
		t.Skip("this git is older than 2.45")
	}
	t.Run("new git, real probe", func(t *testing.T) {
		x := newStore(t, "main")
		if got := format(t, x); got != "reftable" {
			t.Fatalf("format = %s on %s", got, realVer)
		}
	})
	t.Run("new git, reftable repo still seeds and is read", func(t *testing.T) {
		x := newStore(t, "master")
		want := x.forge.push(t, "master", "a.txt", "one", 1000)
		res, err := x.s.Init(bg, repoURL)
		if err != nil || res.Main != want || !res.Seeded {
			t.Fatalf("%+v %v", res, err)
		}
		v, err := x.s.View(bg)
		if err != nil || len(v) != 1 || v[0].Main.SHA != want {
			t.Fatalf("view = %+v %v", v, err)
		}
	})
}

// The git_repos mirrors live under the same directory by default, and the store leaves them alone.
func TestTheStoreAndTheGitReposMirrorsCoexist(t *testing.T) {
	x := newStore(t, "master")
	x.forge.push(t, "master", "a.txt", "one", 1000)
	ck := filepath.Join(t.TempDir(), "checkout")
	if err := os.MkdirAll(ck, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, ck, "init", "-q", "-b", "claude/main")
	mirrored := commit(t, ck, "m.txt", "mirror")
	x.h.Repos = func() ([]Repo, error) {
		return []Repo{{Name: "github/o/atrium", Checkout: filepath.ToSlash(ck), Branch: "claude/main"}}, nil
	}
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	mirror := x.h.Bare("github/o/atrium")

	// THE MIRROR IS IN THE STORE'S DIRECTORY AND NOT IN THE STORE.
	if _, err := os.Stat(filepath.Join(mirror, "HEAD")); err != nil {
		t.Fatal(err)
	}
	if got, _ := x.s.List(); len(got) != 0 {
		t.Fatalf("the store lists the mirror: %v", got)
	}
	if v, _ := x.s.View(bg); len(v) != 0 {
		t.Fatalf("the board lists the mirror: %v", v)
	}
	if x.s.Exists("o/atrium") {
		t.Fatal("Exists is true for a mirror")
	}

	// The store makes its own beside it, and the mirror still serves and still mirrors.
	if _, err := x.s.Init(bg, "https://github.com/o/other"); err != nil {
		t.Fatal(err)
	}
	if got, _ := x.s.List(); len(got) != 1 || got[0].Ref.Name() != "github/o/other" {
		t.Fatalf("list = %v", got)
	}
	// NO SERVING ROUTE IN THIS ITEM: the link's git handler still resolves the git_repos names and
	// nothing the store made.
	if _, ok := x.h.Backend().Resolve("github/o/other"); ok {
		t.Fatal("the link's git handler serves a store repository")
	}
	if dir, ok := x.h.Backend().Resolve("github/o/atrium"); !ok || dir != mirror {
		t.Fatalf("the link's git handler lost the mirror: %q %v", dir, ok)
	}
	commit(t, ck, "m2.txt", "mirror two")
	moved, err := x.h.Mirror(bg)
	if err != nil || len(moved) != 1 {
		t.Fatalf("mirror after a store init: %v %v", moved, err)
	}
	if got := git(t, mirror, "symbolic-ref", "HEAD"); got != "refs/heads/claude/main" {
		t.Fatalf("the mirror's HEAD = %s", got)
	}
	_ = mirrored

	// Only an init by the operator takes the mirror into the store, and it changes nothing in it.
	before := git(t, mirror, "for-each-ref")
	res, err := x.s.Init(bg, "https://github.com/o/atrium")
	if err != nil || res.Created || res.Seeded || !strings.Contains(res.Note, "nothing in it was changed") {
		t.Fatalf("init of a mirror = %+v %v", res, err)
	}
	if after := git(t, mirror, "for-each-ref"); after != before {
		t.Fatalf("the mirror's refs changed:\n%s\n%s", before, after)
	}
	if git(t, mirror, "symbolic-ref", "HEAD") != "refs/heads/claude/main" {
		t.Fatal("HEAD moved")
	}
	if got, _ := x.s.List(); len(got) != 2 {
		t.Fatalf("after taking it: %v", got)
	}
	// And the mirror keeps working.
	commit(t, ck, "m3.txt", "mirror three")
	if moved, err := x.h.Mirror(bg); err != nil || len(moved) != 1 {
		t.Fatalf("mirror after being taken: %v %v", moved, err)
	}
}

func TestTheStoreRootIsTheSettingAndTheDefaultIsUnderTheHubDir(t *testing.T) {
	x := newStore(t, "main")
	if x.s.Root() != filepath.Join(x.root, "git") {
		t.Fatalf("default root = %s", x.s.Root())
	}
	alt := filepath.Join(t.TempDir(), "elsewhere")
	x.h.StoreRoot = func() string { return alt }
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(alt, "github", "o", "r.git", Marker)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(x.root, "git")); err == nil {
		t.Fatal("the default directory was made")
	}
	x.h.StoreRoot = func() string { return "" }
	if x.s.Root() != filepath.Join(x.root, "git") {
		t.Fatal("an empty setting is not the default")
	}
}

func TestInitInParallelMakesItOnce(t *testing.T) {
	x := newStore(t, "master")
	x.forge.push(t, "master", "a.txt", "one", 1000)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := x.s.Init(bg, repoURL)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if res.Created {
				created++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("created %d times", created)
	}
}

func TestViewIsTheExactShapeTheBoardReads(t *testing.T) {
	x := newStore(t, "master")
	empty := func() []byte {
		raw, _ := json.Marshal(map[string]any{"repos": mustView(t, x.s)})
		return raw
	}
	if got := string(empty()); got != `{"repos":[]}` {
		t.Fatalf("empty store = %s", got)
	}

	sha := x.forge.push(t, "master", "a.txt", "one", 1_700_000_000)
	if _, err := x.s.Init(bg, "https://github.com/openziti/zrok"); err != nil {
		t.Fatal(err)
	}
	// A repository on another forge, and an empty one.
	x.s.Forge = func(Ref) string { return filepath.Join(x.root, "none.git") }
	if _, err := x.s.Init(bg, "https://gitlab.com/grp/proj.git"); err != nil {
		t.Fatal(err)
	}

	dir, _ := x.s.Path("openziti/zrok")
	older := x.forge.push(t, "fix/a", "b.txt", "older", 1_700_000_100)
	newer := x.forge.push(t, "fix/b", "c.txt", "newer", 1_700_000_200)
	git(t, dir, "fetch", "-q", "--no-tags", x.forge.dir, "+refs/heads/fix/a:refs/heads/fix/a", "+refs/heads/fix/b:refs/heads/fix/b")
	git(t, dir, "update-ref", "refs/heads/claude/main", sha)

	raw, _ := json.Marshal(map[string]any{"repos": mustView(t, x.s)})
	want := `{"repos":[` +
		`{"host":"github","owner":"openziti","repo":"zrok","url":"git@hub.atrium:openziti/zrok.git",` +
		`"path":"/git/hub/github/openziti/zrok.git",` +
		`"main":{"sha":"` + sha + `","at":"2023-11-14T22:13:20Z"},` +
		`"branches":[` +
		`{"name":"fix/b","sha":"` + newer + `","room":"","card":"","at":"2023-11-14T22:16:40Z","released":false},` +
		`{"name":"fix/a","sha":"` + older + `","room":"","card":"","at":"2023-11-14T22:15:00Z","released":false}]},` +
		`{"host":"gitlab.com","owner":"grp","repo":"proj","url":"git@hub.atrium:gitlab.com/grp/proj.git",` +
		`"path":"/git/hub/gitlab.com/grp/proj.git","main":{"sha":"","at":null},"branches":[]}]}`
	if string(raw) != want {
		t.Fatalf("shape:\n got %s\nwant %s", raw, want)
	}
	// NEVER A PATH ON THE HUB'S DISK.
	if strings.Contains(string(raw), x.root) || strings.Contains(string(raw), filepath.ToSlash(x.root)) {
		t.Fatalf("the answer holds the hub's directory: %s", raw)
	}

	// main.at is a function that can be swapped, which the next item does.
	x.s.MainAt = func(_ context.Context, name string, tip Tip) *time.Time {
		at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
		return &at
	}
	if v := mustView(t, x.s); *v[0].Main.At != "2030-01-02T03:04:05Z" {
		t.Fatalf("MainAt was not used: %v", *v[0].Main.At)
	}
	x.s.MainAt = func(context.Context, string, Tip) *time.Time { return nil }
	if v := mustView(t, x.s); v[0].Main.At != nil || v[0].Main.SHA != sha {
		t.Fatalf("a nil MainAt: %+v", v[0].Main)
	}
}

func mustView(t *testing.T, s *Store) []RepoView {
	t.Helper()
	v, err := s.View(bg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// An answer or an error never names a directory on the hub's disk.
func TestNoAnswerNamesAPathOnTheHubsDisk(t *testing.T) {
	x := newStore(t, "main")
	x.s.Forge = func(Ref) string { return filepath.Join(x.root, "no", "such.git") }
	res, err := x.s.Init(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), x.root) {
		t.Fatalf("answer = %s", raw)
	}
	// The scrub itself.
	got := x.s.scrub("fatal: " + filepath.Join(x.root, "git", "github") + " and " + filepath.ToSlash(x.root) + "/x")
	if strings.Contains(got, x.root) || !strings.Contains(got, "<hub>") {
		t.Fatalf("scrub = %s", got)
	}
}

func TestInitRefusesAHostileURLBeforeTouchingTheDisk(t *testing.T) {
	x := newStore(t, "main")
	for _, in := range []string{"https://tok@github.com/o/r", "https://github.com/../x", "https://github.com/o/r\nx", ""} {
		if _, err := x.s.Init(bg, in); !errors.Is(err, ErrRefused) {
			t.Errorf("%q: %v", in, err)
		}
	}
	if _, err := os.Stat(filepath.Join(x.root, "git")); err == nil {
		t.Fatal("a refused URL made the store directory")
	}
}
