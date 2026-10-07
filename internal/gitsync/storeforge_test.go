package gitsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A forge's branch fetched into the store on an ask, and served by the store's route. The forge is a local bare
// repository (store_test.go's forge), and the route is the real handler with a real git (receive_test.go's newRecv).

type forgeFix struct {
	*recvFix
	forge *forge
	main  string
}

const forgeRepo = "github/o/r"

// newForgeFix is a hub whose store reads its forge from a local repository with main and, when branch is set, that
// branch too. The store does not hold the repository yet.
func newForgeFix(t *testing.T) *forgeFix {
	t.Helper()
	f := newForge(t, "main")
	x := &forgeFix{forge: f}
	x.main = f.push(t, "main", "a.txt", "one", 1000)
	x.recvFix = newRecv(t, func(h *Hub) {
		s := h.Store()
		s.Protocols = "file"
		s.Forge = func(ref Ref) string {
			if ref.Name() == forgeRepo {
				return f.dir
			}
			return filepath.Join(filepath.Dir(f.dir), "no-such.git")
		}
	})
	return x
}

// forgeBranch puts a branch on the forge, off its main, and answers its tip.
func (x *forgeFix) forgeBranch(t *testing.T, b, file string) string {
	t.Helper()
	git(t, x.forge.work, "switch", "-q", "-C", b, "main")
	sha := x.forge.push(t, b, file, b+file, 2000)
	git(t, x.forge.work, "switch", "-q", "main")
	return sha
}

func (x *forgeFix) lookup(branch string) URLAnswer {
	return x.h.Lookup(bg, URLQuery{Repo: "o/r", Branch: branch, Base: x.srv.URL})
}

// fetchHead fetches one name from the store's route into a fresh repository as a room's card, and answers FETCH_HEAD.
func (x *forgeFix) fetchHead(t *testing.T, name string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	if out, err := x.run(dir, roomCard("sg4", "c9"), "fetch", "-q", x.url(forgeRepo), name); err != nil {
		return out, err
	}
	return git(t, dir, "rev-parse", "FETCH_HEAD"), nil
}

// stale forgets when the route last refreshed, so the next fetch asks the forge again.
func (x *forgeFix) stale() {
	s := x.h.Store()
	s.forge.mu.Lock()
	s.forge.at = nil
	s.forge.mu.Unlock()
}

func TestABranchOnlyTheForgeHasIsFoundAndFetchableThroughTheHub(t *testing.T) {
	x := newForgeFix(t)
	if _, err := x.h.Store().Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	tip := x.forgeBranch(t, "mfa-posture-tests", "m.txt")

	a := x.lookup("mfa-posture-tests")
	if a.State != URLFound || len(a.Branches) != 1 || len(a.Branches[0].Sources) != 1 {
		t.Fatalf("answer = %+v", a)
	}
	src := a.Branches[0].Sources[0]
	if src.Source != "hub" || !src.Forge || src.SHA != tip || src.URL != x.url(forgeRepo) {
		t.Fatalf("source = %+v, want the hub's URL at %s from the forge", src, tip)
	}
	if !strings.Contains(a.Note, "git fetch hub mfa-posture-tests") || !strings.Contains(a.Note, "forge/mfa-posture-tests") {
		t.Fatalf("note = %q", a.Note)
	}
	if !strings.Contains(a.Text(), "git fetch "+x.url(forgeRepo)+" mfa-posture-tests") {
		t.Fatalf("text = %q", a.Text())
	}
	// ITS OWN NAMESPACE: refs/forge/, and never refs/heads.
	if got := x.refOn(forgeRepo, ForgeRefPrefix+"mfa-posture-tests"); got != tip {
		t.Fatalf("refs/forge = %q, want %s", got, tip)
	}
	if got := x.refOn(forgeRepo, headsPrefix+"mfa-posture-tests"); got != "" {
		t.Fatalf("a forge branch was put under refs/heads: %s", got)
	}

	// THE ROUTE SERVES IT BY ITS PLAIN NAME, and by forge/<name>.
	for _, name := range []string{"mfa-posture-tests", "forge/mfa-posture-tests"} {
		got, err := x.fetchHead(t, name)
		if err != nil || got != tip {
			t.Fatalf("fetch %s = %q, %v, want %s", name, got, err, tip)
		}
	}

	// A COLLEAGUE'S NEW COMMIT arrives on the next fetch through the route, and on the next ask.
	git(t, x.forge.work, "switch", "-q", "mfa-posture-tests")
	next := x.forge.push(t, "mfa-posture-tests", "m2.txt", "more", 3000)
	git(t, x.forge.work, "switch", "-q", "main")
	x.stale()
	if got, err := x.fetchHead(t, "mfa-posture-tests"); err != nil || got != next {
		t.Fatalf("fetch after a forge commit = %q, %v, want %s", got, err, next)
	}
	// The forge force-pushes it, and the copy follows: refs/forge/ is nobody's work but the forge's.
	git(t, x.forge.work, "switch", "-q", "-C", "mfa-posture-tests", "main")
	commitAt(t, x.forge.work, "m3.txt", "rewritten", 4000)
	git(t, x.forge.work, "push", "-q", x.forge.dir, "+HEAD:refs/heads/mfa-posture-tests")
	again := git(t, x.forge.work, "rev-parse", "HEAD")
	git(t, x.forge.work, "switch", "-q", "main")
	if a := x.lookup("mfa-posture-tests"); a.State != URLFound || a.Branches[0].Sources[0].SHA != again {
		t.Fatalf("the ask did not refresh: %+v, want %s", a, again)
	}
	// main is the seed's, and nothing else moved.
	if got := x.refOn(forgeRepo, MainRef); got != x.main {
		t.Fatalf("main = %s, want %s", got, x.main)
	}
}

func TestAFirstAskMakesTheRepositoryFromItsForge(t *testing.T) {
	x := newForgeFix(t)
	tip := x.forgeBranch(t, "feature", "f.txt")
	// A typo makes nothing.
	if a := x.lookup("no-such-branch"); a.State != URLNotFound || x.h.Store().Exists(forgeRepo) {
		t.Fatalf("a miss: %+v, exists=%v", a, x.h.Store().Exists(forgeRepo))
	}
	a := x.lookup("feature")
	if a.State != URLFound || a.Repo != forgeRepo || a.Branches[0].Sources[0].SHA != tip {
		t.Fatalf("answer = %+v", a)
	}
	if !x.h.Store().Exists(forgeRepo) || x.refOn(forgeRepo, MainRef) != x.main {
		t.Fatal("the repository was not made with main seeded")
	}
	if got, err := x.fetchHead(t, "feature"); err != nil || got != tip {
		t.Fatalf("fetch = %q, %v", got, err)
	}
}

func TestAPushedBranchOfTheSameNameIsNotShadowed(t *testing.T) {
	x := newForgeFix(t)
	if _, err := x.h.Store().Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	forgeTip := x.forgeBranch(t, "fix/x", "forge.txt")
	if a := x.lookup("fix/x"); a.State != URLFound || !a.Branches[0].Sources[0].Forge {
		t.Fatalf("answer = %+v", a)
	}

	// A room's card pushes its own fix/x: the forge's copy did not take the name.
	pushed := x.branch("fix/x", "room.txt")
	x.must(x.push(roomCard("sg4", "c1"), forgeRepo, "fix/x:refs/heads/fix/x"))
	if got := x.refOn(forgeRepo, headsPrefix+"fix/x"); got != pushed {
		t.Fatalf("refs/heads/fix/x = %q, want the push %s", got, pushed)
	}

	a := x.lookup("fix/x")
	src := a.Branches[0].Sources[0]
	if a.State != URLFound || src.Forge || src.SHA != pushed || strings.Contains(a.Note, "from the forge") {
		t.Fatalf("answer = %+v, want the pushed branch alone", a)
	}
	x.stale()
	if got, err := x.fetchHead(t, "fix/x"); err != nil || got != pushed {
		t.Fatalf("plain fetch = %q, %v, want the push %s", got, err, pushed)
	}
	if got, err := x.fetchHead(t, "forge/fix/x"); err != nil || got != forgeTip {
		t.Fatalf("forge fetch = %q, %v, want %s", got, err, forgeTip)
	}
}

func TestAMissingBranchIsStillNotFound(t *testing.T) {
	x := newForgeFix(t)
	if _, err := x.h.Store().Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	a := x.lookup("nope")
	if a.State != URLNotFound || !strings.Contains(a.Note, "the forge has no branch `nope` either") {
		t.Fatalf("answer = %+v", a)
	}
	if refs := x.refsOn(forgeRepo); strings.Contains(refs, ForgeRefPrefix) {
		t.Fatalf("refs = %s", refs)
	}
	// A branch name that is no ref is never given to git as one.
	for _, bad := range []string{"-upload-pack=x", "a..b", "a b", "x:y"} {
		if a := x.lookup(bad); a.State == URLFound {
			t.Errorf("%q was found", bad)
		}
	}
}

func TestABranchTheForgeDroppedIsDroppedFromTheStore(t *testing.T) {
	x := newForgeFix(t)
	if _, err := x.h.Store().Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	x.forgeBranch(t, "gone", "g.txt")
	if a := x.lookup("gone"); a.State != URLFound {
		t.Fatalf("answer = %+v", a)
	}
	git(t, x.forge.dir, "update-ref", "-d", "refs/heads/gone")
	if a := x.lookup("gone"); a.State != URLNotFound {
		t.Fatalf("answer = %+v", a)
	}
	if got := x.refOn(forgeRepo, ForgeRefPrefix+"gone"); got != "" {
		t.Fatalf("refs/forge/gone = %s", got)
	}
}

func TestAPrivateRepositoryWithNoCredentialSaysSo(t *testing.T) {
	x := newForgeFix(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="forge"`)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
	}))
	defer srv.Close()
	s := x.h.Store()
	s.Protocols = "http"
	s.Forge = func(Ref) string { return srv.URL + "/o/r.git" }

	// Not held yet, and held (made empty, as init makes a private one).
	for _, held := range []bool{false, true} {
		if held {
			x.makeRepo(forgeRepo)
		}
		a := x.lookup("mfa")
		if a.State != URLNoCredential || !strings.Contains(a.Note, "the hub has no credential for "+forgeRepo) {
			t.Fatalf("held=%v: answer = %+v", held, a)
		}
		if strings.Contains(a.Text(), "not found") {
			t.Fatalf("held=%v: text = %q", held, a.Text())
		}
	}

	// WITH A HELPER the hub has a login, and the sentence says the login cannot read it.
	x.h.ForgeHelper = func(host string) string {
		if host != "github.com" {
			t.Errorf("host = %q", host)
		}
		return "!false"
	}
	if a := x.lookup("mfa"); a.State != URLNoCredential || !strings.Contains(a.Note, "login cannot read") {
		t.Fatalf("with a helper: %+v", a)
	}
}

func TestOneFetchPerBranchAtATime(t *testing.T) {
	var fs forgeState
	var runs atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	got := make([]string, 4)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = fs.once(bg, "k", func() (string, error) {
				runs.Add(1)
				<-release
				return "sha", nil
			})
		}()
	}
	// Every caller is waiting on the one run before it is let go.
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if runs.Load() != 1 {
		t.Fatalf("runs = %d", runs.Load())
	}
	for _, g := range got {
		if g != "sha" {
			t.Fatalf("got = %q", got)
		}
	}
	// A caller that gives up does not wait for the run.
	block := make(chan struct{})
	defer close(block)
	go fs.once(bg, "slow", func() (string, error) { <-block; return "", nil })
	for {
		fs.mu.Lock()
		_, in := fs.flights["slow"]
		fs.mu.Unlock()
		if in {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := fs.once(ctx, "slow", func() (string, error) { return "", nil }); err == nil {
		t.Fatal("a cancelled wait answered")
	}
}

func TestForgeAdvertAddsPlainNamesThatNoBranchHas(t *testing.T) {
	in := advert(
		sha1h+" refs/heads/main",
		sha2h+" refs/heads/Fix",
		sha1h+" refs/forge/fix",
		sha2h+" refs/forge/mfa",
		sha1h+" refs/forge/main",
	)
	got, ok := readAdvert(forgeAdvert(in), nil)
	if !ok {
		t.Fatal("the advertisement does not read")
	}
	if got["mfa"] != sha2h || got["main"] != sha1h || got["Fix"] != sha2h {
		t.Fatalf("%v", got)
	}
	// `fix` differs from the pushed `Fix` only in case, which NTFS would make one.
	if _, ok := got["fix"]; ok {
		t.Fatalf("a case twin was added: %v", got)
	}
	plain := advert(sha1h + " refs/heads/main")
	if string(forgeAdvert(plain)) != string(plain) {
		t.Fatal("an advertisement with no forge branch was changed")
	}
	if junk := []byte("not an advert refs/forge/x"); string(forgeAdvert(junk)) != string(junk) {
		t.Fatal("junk was changed")
	}
}
