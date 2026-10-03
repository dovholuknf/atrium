package gitsync

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestServedHideIsAPureFunctionOfTheLiveBranches(t *testing.T) {
	base := []string{"HEAD", "refs", "!refs/heads/claude/", "refs/heads/claude/main"}
	cases := []struct {
		name string
		live []string
		want []string
	}{
		{"none", nil, base},
		{"one", []string{"fix/x"}, append(append([]string{}, base...), "!refs/heads/fix/x")},
		{"sorted and once each", []string{"b", "a", "b"}, append(append([]string{}, base...), "!refs/heads/a", "!refs/heads/b")},
		{"claude/main is never served", []string{"claude/main"}, base},
		// A card working in the clone's own checkout on its default branch is not served that branch: the operator's
		// unpushed work may be there.
		{"the clone's own branches are never served", []string{"main", "master", "hub-main", "claude/main", "fix/x"},
			append(append([]string{}, base...), "!refs/heads/fix/x")},
		{"not a branch name", []string{"", "refs/stash", "refs/heads/x", "-x", "a b", "a..b", "a:b", "a*", "a/", "/a", "x.lock", "a//b", "a/.b", "@{u}", "x\ny", "!refs/heads/y", "a\"b"}, base},
	}
	for _, c := range cases {
		if got := ServedHide(c.live); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// roomRepo is a clone-like repository holding a served branch, a live card's branch, and everything a reader
// must not get: stash, notes, an unserved branch, claude/main.
type roomRepo struct {
	srv                                        *httptest.Server
	dir                                        string
	w1, live, secret, stash, notes, claudeMain string

	mu   sync.Mutex
	card []string
}

func (r *roomRepo) setLive(b ...string) { r.mu.Lock(); r.card = b; r.mu.Unlock() }

func newRoomRepo(t *testing.T) *roomRepo {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &roomRepo{dir: filepath.Join(work, ".git")}
	git(t, work, "init", "-q", "-b", "claude/main")
	r.claudeMain = commit(t, work, "a.txt", "one")
	git(t, work, "checkout", "-q", "-b", "claude/w1")
	r.w1 = commit(t, work, "w.txt", "worker")
	git(t, work, "checkout", "-q", "-b", "fix/live", "claude/main")
	r.live = commit(t, work, "l.txt", "live")
	git(t, work, "checkout", "-q", "-b", "secret/x", "claude/main")
	r.secret = commit(t, work, "s.txt", "secret")
	git(t, work, "checkout", "-q", "claude/main")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "stash")
	r.stash = git(t, work, "rev-parse", "refs/stash")
	git(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "notes", "add", "-m", "private note", r.claudeMain)
	r.notes = git(t, work, "rev-parse", "refs/notes/commits")

	b := &Backend{
		Resolve: func(name string) (string, bool) { return r.dir, name == "github/o/r" },
		HideFor: func(_, dir string) []string {
			r.mu.Lock()
			defer r.mu.Unlock()
			return ServedHide(r.card, DefaultBranches(nil, dir)...)
		},
	}
	r.srv = httptest.NewServer(b)
	t.Cleanup(r.srv.Close)
	return r
}

func (r *roomRepo) url() string { return r.srv.URL + "/github/o/r.git" }

// advertised is what the room lists, ref name to sha.
func (r *roomRepo) advertised(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, l := range strings.Split(git(t, t.TempDir(), "ls-remote", r.url()), "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			out[f[1]] = f[0]
		}
	}
	return out
}

// want posts a bare upload-pack request for one sha, which is what a reader that was not shown the ref would
// send, and answers the body.
func (r *roomRepo) want(t *testing.T, sha string) string {
	t.Helper()
	body := new(bytes.Buffer)
	body.Write(pktLine("want " + sha + "\n"))
	body.WriteString("0000")
	body.Write(pktLine("done\n"))
	req, _ := http.NewRequest("POST", r.url()+"/git-upload-pack", body)
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return fmt.Sprintf("%d %s", resp.StatusCode, b)
}

func TestTheRoomAdvertisesClaudeBranchesAndLiveCardsAndNothingElse(t *testing.T) {
	r := newRoomRepo(t)
	got := r.advertised(t)
	if got["refs/heads/claude/w1"] != r.w1 {
		t.Fatalf("claude/w1 is not served: %v", got)
	}
	for ref := range got {
		if ref != "refs/heads/claude/w1" {
			t.Errorf("%s is advertised with no live card", ref)
		}
	}
	r.setLive("fix/live")
	got = r.advertised(t)
	if got["refs/heads/fix/live"] != r.live || got["refs/heads/claude/w1"] != r.w1 || len(got) != 2 {
		t.Fatalf("with a live card: %v", got)
	}
	// The card ends, and the very next request does not serve it.
	r.setLive()
	if _, ok := r.advertised(t)["refs/heads/fix/live"]; ok {
		t.Fatal("a branch whose card ended is still served")
	}
}

func TestStashNotesAnUnservedBranchAndClaudeMainAreRefusedToAFetch(t *testing.T) {
	r := newRoomRepo(t)
	r.setLive("fix/live")
	for _, c := range []struct{ name, ref string }{
		{"stash", "refs/stash"},
		{"notes", "refs/notes/commits"},
		{"an unserved branch", "refs/heads/secret/x"},
		{"claude/main", "refs/heads/claude/main"},
	} {
		dst := t.TempDir()
		git(t, dst, "init", "-q")
		if out, err := Default.Git(bg, dst, "fetch", r.url(), c.ref); err == nil {
			t.Errorf("%s: the fetch of %s went through: %s", c.name, c.ref, out)
		}
		if out := git(t, dst, "for-each-ref"); out != "" || git(t, dst, "count-objects", "-v") == "" {
			t.Errorf("%s: refs were written: %s", c.name, out)
		}
	}
	// And the served ones are taken.
	dst := t.TempDir()
	git(t, dst, "init", "-q")
	git(t, dst, "fetch", "-q", r.url(), "refs/heads/claude/w1", "refs/heads/fix/live")
}

func TestAWantByShaOutsideTheServedSetIsRefusedByTheRoomsGit(t *testing.T) {
	r := newRoomRepo(t)
	r.setLive("fix/live")
	// A served tip is taken. Anything the room did not advertise is not, though the sha is real. (A commit that is an
	// ancestor of a served tip is taken too, by stateless HTTP's own rule, and gives away nothing the tip's history
	// does not.)
	if got := r.want(t, r.w1); strings.Contains(got, "ERR") {
		t.Fatalf("a served tip was refused: %s", got)
	}
	for name, sha := range map[string]string{
		"stash": r.stash, "notes": r.notes, "an unserved branch": r.secret,
	} {
		if got := r.want(t, sha); !strings.Contains(got, "ERR") || strings.Contains(got, "PACK") {
			t.Errorf("%s: %s", name, got)
		}
	}
}

func TestTheRoomsUploadPackSpeaksV0AndOffersNoFilterOrAnySha(t *testing.T) {
	r := newRoomRepo(t)
	req, _ := http.NewRequest("GET", r.url()+"/info/refs?service=git-upload-pack", nil)
	req.Header.Set("Git-Protocol", "version=2")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	adv := string(b)
	if strings.Contains(adv, "version 2") {
		t.Fatalf("the room spoke v2: %q", adv)
	}
	for _, cap := range []string{"filter", "allow-tip-sha1-in-want", "allow-reachable-sha1-in-want", "shallow"} {
		if cap == "shallow" {
			continue // shallow is refused by the hub before it gets here
		}
		if strings.Contains(adv, cap) {
			t.Errorf("the room offers %s: %q", cap, adv)
		}
	}
}

func TestACardIsMatchedToItsRepositoryOnTheFullNameWhenItRecordedOne(t *testing.T) {
	for _, c := range []struct {
		name, host, org, repo string
		want                  bool
	}{
		{"github/o/r", "", "", "r", true},
		{"github/o/r", "github", "o", "r", true},
		{"github/o/r", "github.com", "O", "R", true},
		{"o/r", "", "o", "r", true},
		// A card that recorded no org or host matches on the folder name alone.
		{"github/o/r", "", "", "r", true},
		{"github/o/r", "", "", "other", false},
		{"github/o/r", "", "x", "r", false},
		{"github/o/r", "gitlab", "o", "r", false},
		{"github/o/r", "github", "x", "r", false},
		{"github/o/r", "", "", "", false},
		{"../r", "", "", "r", false},
	} {
		if got := RepoMatches(c.name, c.host, c.org, c.repo); got != c.want {
			t.Errorf("%+v = %v", c, got)
		}
	}
}

func TestAClonesDefaultBranchWhateverItIsCalledIsNeverServedToALiveCard(t *testing.T) {
	// The pure half.
	got := ServedHide([]string{"develop", "trunk", "fix/x"}, "develop", "trunk")
	want := []string{"HEAD", "refs", "!refs/heads/claude/", "refs/heads/claude/main", "!refs/heads/fix/x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}

	// Through a real clone: origin/HEAD points at develop, and init.defaultBranch is trunk.
	r := newRoomRepo(t)
	work := filepath.Dir(r.dir)
	git(t, work, "branch", "develop", "claude/main")
	git(t, work, "branch", "trunk", "claude/main")
	r.setLive("develop", "trunk", "fix/live")
	// Neither is a default yet, so a card on either would be served: this is what the test is about.
	got2 := r.advertised(t)
	if got2["refs/heads/develop"] == "" || got2["refs/heads/trunk"] == "" {
		t.Fatalf("a live card on develop and trunk is served until they are the defaults: %v", got2)
	}
	git(t, work, "update-ref", "refs/remotes/origin/develop", "claude/main")
	git(t, work, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	got2 = r.advertised(t)
	if _, ok := got2["refs/heads/develop"]; ok {
		t.Fatalf("origin/HEAD's branch is served: %v", got2)
	}
	if got2["refs/heads/trunk"] == "" {
		t.Fatalf("trunk went with it: %v", got2)
	}
	git(t, work, "config", "init.defaultBranch", "trunk")
	got2 = r.advertised(t)
	if _, ok := got2["refs/heads/trunk"]; ok {
		t.Fatalf("init.defaultBranch's branch is served: %v", got2)
	}
	if got2["refs/heads/fix/live"] != r.live {
		t.Fatalf("the card's own branch stopped being served: %v", got2)
	}
}
