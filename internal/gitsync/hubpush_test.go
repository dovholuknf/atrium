package gitsync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPushToHubPushesAPlainBranchThroughTheForwarder(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	sha := f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	tok, _ := f.cards.Mint("C1")
	out, err := PushToHub(bg, Default, f.work, f.base, tok, "fix/x", "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x is %q, want %q", got, sha)
	}
	if rows := f.pushRows(); rows[len(rows)-1].Card != "C1" {
		t.Fatalf("the push log: %+v", rows[len(rows)-1])
	}
	// A refusal by the hub comes back in the error with the hub's words.
	commit(t, f.work, "m.txt", "m")
	out, err = PushToHub(bg, Default, f.work, f.base, tok, "main", "")
	if err == nil || !strings.Contains(out, "atrium:") {
		t.Fatalf("a card's push of main: %v\n%s", err, out)
	}
}

func TestPushToHubTakesAPlainBranchNameAndNothingElse(t *testing.T) {
	for _, b := range []string{"", "+fix/x", "fix/x:main", ":fix/x", "-f", "--force", "--all", "--mirror", "--tags", "--delete",
		"refs/heads/fix/x", "refs/tags/v1", "tags/v1", "HEAD", "a..b", "fix/x.lock", "fix//x", "fix/x/", "a b", "@{u}", "x@{1}"} {
		if CheckPushBranch(b) == "" {
			t.Errorf("%q was accepted as a plain branch", b)
		}
	}
	for _, b := range []string{"fix/x", "main", "claude/r-hub-remote", "a.b", "v1.2"} {
		if why := CheckPushBranch(b); why != "" {
			t.Errorf("%q refused: %s", b, why)
		}
	}
}

// The push has to go where the forwarder is AFTER every rewrite, not where the config says.
func TestPushToHubRefusesWhenAGlobalPushInsteadOfSendsTheForwarderElsewhere(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	tok, _ := f.cards.Mint("C1")

	// Another server that would take the push, if it were sent there.
	elsewhere := t.TempDir()
	git(t, elsewhere, "init", "-q", "--bare", "-b", "main")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("USERPROFILE", home)
	rewrite := "[url \"" + elsewhere + "/\"]\n\tpushInsteadOf = " + f.srv.URL + "/\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(rewrite), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := git(t, f.work, "remote", "get-url", "--push", "hub"); !strings.HasPrefix(got, elsewhere) {
		t.Fatalf("the test's rewrite did not take: the push url is %q", got)
	}
	before := f.rt.count()
	out, err := PushToHub(bg, Default, f.work, f.base, tok, "fix/x", "")
	if err == nil {
		t.Fatalf("the push was not refused:\n%s", out)
	}
	if !strings.Contains(err.Error(), "not only this room's hub forwarder") {
		t.Fatalf("not refused for the right reason: %v", err)
	}
	if f.rt.count() != before || f.refOn(hubRepo, "refs/heads/fix/x") != "" {
		t.Fatal("something was pushed")
	}
	if got := git(t, elsewhere, "for-each-ref"); got != "" {
		t.Fatalf("the other server got a push:\n%s", got)
	}
}

// A clone whose own `hub` points elsewhere: refused, and the remote atrium adds is atrium-hub.
func TestAHubRemotePointingElsewhereIsLeftAloneAndAtriumHubIsAdded(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	sha := f.branch("fix/x", "x.txt")
	clone := t.TempDir()
	git(t, clone, "init", "-q", "-b", "main")
	git(t, clone, "remote", "add", "hub", "https://example.com/mine/other.git")
	tok, _ := f.cards.Mint("C1")
	git(t, f.work, "remote", "add", "x", clone) // keeps the sha reachable below; the push is from the clone
	git(t, clone, "fetch", "-q", f.work, "fix/x:fix/x")

	out, err := PushToHub(bg, Default, clone, f.base, tok, "fix/x", "")
	if err == nil || !strings.Contains(err.Error(), "hub pushes to https://example.com/mine/other.git") {
		t.Fatalf("a hub that points elsewhere was not refused: %v\n%s", err, out)
	}

	note := ensureRemotes(bg, Default, clone, f.repoURL(hubRepo), false)
	if !strings.Contains(note, "was left alone, so atrium's is `atrium-hub`") {
		t.Fatalf("the report: %q", note)
	}
	if got := git(t, clone, "remote", "get-url", "hub"); got != "https://example.com/mine/other.git" {
		t.Fatalf("the clone's own hub was changed to %s", got)
	}
	if got := git(t, clone, "remote", "get-url", "atrium-hub"); got != f.repoURL(hubRepo) {
		t.Fatalf("atrium-hub is %s", got)
	}
	out, err = PushToHub(bg, Default, clone, f.base, tok, "fix/x", "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x on the hub is %q, want %q", got, sha)
	}
}

func TestEnsureRemotesAddsHubFollowsAPortChangeAndGuardsOriginOnAtriumMadeClones(t *testing.T) {
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare", "-b", "main")
	mk := func() string {
		c := t.TempDir()
		git(t, c, "init", "-q", "-b", "main")
		git(t, c, "remote", "add", "origin", remote)
		commit(t, c, "a.txt", "a")
		return c
	}
	u1 := "http://127.0.0.1:7777/git/hub/github/o/r.git"
	u2 := "http://127.0.0.1:50001/git/hub/github/o/r.git"

	made := mk()
	if note := ensureRemotes(bg, Default, made, u1, true); note != "" {
		t.Fatalf("a made clone had something to say: %q", note)
	}
	if got := git(t, made, "remote", "get-url", "hub"); got != u1 {
		t.Fatalf("hub is %s", got)
	}
	if got := git(t, made, "config", "remote.origin.pushurl"); got != OriginPushURL {
		t.Fatalf("origin pushurl is %q", got)
	}
	// A script on the clone pushing to origin fails on the pushurl, and the forge is untouched.
	out, err := Default.Git(bg, made, "push", "origin", "main")
	if err == nil {
		t.Fatalf("git push origin worked on an atrium-made clone:\n%s", out)
	}
	if e := err.(*Error); !strings.Contains(e.Stderr, "atrium-refused") {
		t.Fatalf("failed, but not on the pushurl: %s", e.Stderr)
	}
	if got := git(t, remote, "for-each-ref"); got != "" {
		t.Fatalf("the forge got a push:\n%s", got)
	}
	// The agent port moved: atrium's own remote follows, and it is not "elsewhere".
	if note := ensureRemotes(bg, Default, made, u2, true); note != "" {
		t.Fatalf("a port change was reported: %q", note)
	}
	if got := git(t, made, "remote", "get-url", "hub"); got != u2 {
		t.Fatalf("hub is %s after the port moved", got)
	}

	// An operator's clone: hub added, origin NOT touched, and the report says why.
	theirs := mk()
	note := ensureRemotes(bg, Default, theirs, u1, false)
	if !strings.Contains(note, "origin is not guarded") || !strings.Contains(note, "operator's yes") {
		t.Fatalf("the report: %q", note)
	}
	if out, err := Default.Git(bg, theirs, "config", "--get", "remote.origin.pushurl"); err == nil {
		t.Fatalf("an existing clone's origin was changed: %s", out)
	}
	if got := git(t, theirs, "remote", "get-url", "hub"); got != u1 {
		t.Fatalf("hub is %s", got)
	}
}

func TestEnsureRemotesLeavesARoomGitCloneAloneAndSilent(t *testing.T) {
	// room-git.ps1 init makes the clone by push: no origin, and atrium.clone=made.
	c := t.TempDir()
	git(t, c, "init", "-q", "-b", "main")
	git(t, c, "config", cfgMade, "made")
	if note := ensureRemotes(bg, Default, c, "http://127.0.0.1:7777/git/hub/github/o/r.git", false); note != "" {
		t.Fatalf("a room-git clone had something to say: %q", note)
	}
	if out, err := Default.Git(bg, c, "config", "--get", "remote.origin.pushurl"); err == nil {
		t.Fatalf("a pushurl was made for an origin that is not there: %s", out)
	}
	// With an origin and the marker, it is guarded like any clone atrium made.
	git(t, c, "remote", "add", "origin", t.TempDir())
	if note := ensureRemotes(bg, Default, c, "http://127.0.0.1:7777/git/hub/github/o/r.git", false); note != "" {
		t.Fatalf("a marked clone with an origin had something to say: %q", note)
	}
	if got := git(t, c, "config", "remote.origin.pushurl"); got != OriginPushURL {
		t.Fatalf("origin pushurl is %q", got)
	}
}

func TestSyncAddsTheHubRemoteAndGuardsTheOriginOfACloneItMade(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	sy.HubRemote = func(c string) string { return "http://127.0.0.1:7777/git/hub/" + c + ".git" }
	r := sy.Sync(bg, repo, true, hubFor(s))
	if r.State != StateOK {
		t.Fatalf("%+v", r)
	}
	clone := filepath.Join(root, "github", "o", "r")
	if got := git(t, clone, "remote", "get-url", "hub"); got != "http://127.0.0.1:7777/git/hub/github/o/r.git" {
		t.Fatalf("hub is %s", got)
	}
	if got := git(t, clone, "config", "remote.origin.pushurl"); got != OriginPushURL {
		t.Fatalf("origin pushurl %q", got)
	}
	// Syncing again, now an existing clone, keeps what it made and says nothing.
	r = sy.Sync(bg, repo, false, hubFor(s))
	if r.State != StateOK || r.Remote != "" {
		t.Fatalf("%+v", r)
	}
}

// `git remote get-url --push` prints only the first push url, and git pushes to every one.
func TestPushToHubRefusesASecondPushURLOnHub(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("fix/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	var mu sync.Mutex
	var other []string
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		other = append(other, r.URL.String())
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer evil.Close()
	git(t, f.work, "config", "--add", "remote.hub.pushurl", f.repoURL(hubRepo))
	git(t, f.work, "config", "--add", "remote.hub.pushurl", evil.URL+"/evil.git")
	if got := git(t, f.work, "remote", "get-url", "--push", "hub"); !strings.HasPrefix(got, f.base) {
		t.Fatalf("the first push url is not the forwarder's, so this proves nothing: %s", got)
	}
	tok, _ := f.cards.Mint("C1")
	before := f.rt.count()
	if out, err := PushToHub(bg, Default, f.work, f.base, tok, "fix/x", ""); err == nil {
		t.Fatalf("a second pushurl was not refused:\n%s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(other) != 0 || f.rt.count() != before || f.refOn(hubRepo, "refs/heads/fix/x") != "" {
		t.Fatalf("something was pushed: other=%v", other)
	}
}

// remote.<name>.proxy and http.<url>.proxy beat http.proxy, so a clone could put its own listener between the card's
// token and the forwarder. Both are cleared, and the push still lands.
func TestPushToHubIgnoresAProxyTheClonesConfigSets(t *testing.T) {
	for _, key := range []string{"remote.hub.proxy", "http.proxy", "http.<base>.proxy", "http.<push>.proxy"} {
		t.Run(key, func(t *testing.T) {
			f := newRemote(t)
			f.seedMain(hubRepo)
			sha := f.branch("fix/x", "x.txt")
			git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
			var mu sync.Mutex
			var seen []http.Header
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, r.Header.Clone())
				mu.Unlock()
				http.Error(w, "a proxy", http.StatusBadGateway)
			}))
			defer proxy.Close()
			k := strings.NewReplacer("<base>", f.base, "<push>", f.repoURL(hubRepo)).Replace(key)
			git(t, f.work, "config", k, proxy.URL)
			tok, _ := f.cards.Mint("C1")
			out, err := PushToHub(bg, Default, f.work, f.base, tok, "fix/x", "")
			mu.Lock()
			defer mu.Unlock()
			if len(seen) != 0 {
				t.Fatalf("the clone's proxy was used: %v", seen[0])
			}
			if err != nil || f.refOn(hubRepo, "refs/heads/fix/x") != sha {
				t.Fatalf("the push did not land: %v\n%s", err, out)
			}
		})
	}
}

// A CLONE MADE BY HAND, with no hub remote and no origin, at <root>/<host>/<owner>/<repo>: pushed from a worktree of
// it to the forwarder's URL for the repository its place names, and nothing is added to its config.
func TestAHandMadeCloneWithNoHubRemoteIsPushedByTheURLItsPlaceNames(t *testing.T) {
	f := newRemote(t)
	f.seedMain(hubRepo)
	sha := f.branch("fix/x", "x.txt")
	root := t.TempDir()
	clone := filepath.Join(root, "github.com", "o", "r")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "init", "-q", "-b", "main")
	git(t, clone, "fetch", "-q", f.work, "fix/x:fix/x")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, clone, "worktree", "add", "-q", wt, "fix/x")
	cfgBefore, _ := os.ReadFile(filepath.Join(clone, ".git", "config"))
	tok, _ := f.cards.Mint("C1")

	// No name: refused, with the sentence saying why.
	if out, err := PushToHub(bg, Default, wt, f.base, tok, "fix/x", ""); err == nil || !strings.Contains(err.Error(), "no hub remote") {
		t.Fatalf("a clone with no hub remote and no name: %v\n%s", err, out)
	}
	name := HubNameOf(bg, Default, wt, filepath.Join(root, "elsewhere"), root)
	if name != hubRepo {
		t.Fatalf("HubNameOf is %q, want %s", name, hubRepo)
	}
	out, err := PushToHub(bg, Default, wt, f.base, tok, "fix/x", name)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.refOn(hubRepo, "refs/heads/fix/x"); got != sha {
		t.Fatalf("fix/x on the hub is %q, want %q", got, sha)
	}
	if rows := f.pushRows(); rows[len(rows)-1].Card != "C1" {
		t.Fatalf("the push log: %+v", rows[len(rows)-1])
	}
	if cfgAfter, _ := os.ReadFile(filepath.Join(clone, ".git", "config")); string(cfgAfter) != string(cfgBefore) {
		t.Fatalf("the clone's config was changed:\n%s", cfgAfter)
	}
	// A name the hub does not take is refused before git runs.
	if _, err := PushToHub(bg, Default, wt, f.base, tok, "fix/x", "../x"); err == nil {
		t.Fatal("a bad name was pushed to")
	}
}

func TestHubNameOfReadsOriginFirstAndSaysNothingOutsideARoot(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "github", "o", "r")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "init", "-q", "-b", "main")
	git(t, clone, "remote", "add", "origin", "https://github.com/other/thing.git")
	if got := HubNameOf(bg, Default, clone, root); got != "github/other/thing" {
		t.Fatalf("with an origin: %q", got)
	}
	loose := t.TempDir()
	git(t, loose, "init", "-q", "-b", "main")
	if got := HubNameOf(bg, Default, loose, root); got != "" {
		t.Fatalf("a clone outside every root: %q", got)
	}
	deep := filepath.Join(root, "github", "o", "r", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, deep, "init", "-q", "-b", "main")
	if got := HubNameOf(bg, Default, deep, root); got != "" {
		t.Fatalf("a clone four deep: %q", got)
	}
}
