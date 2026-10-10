//go:build integration

package gitsync

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// prHead pushes a commit to the fake forge under refs/pull/<n>/head, as GitHub keeps a pull request's head.
func (f *forge) prHead(t *testing.T, n, file, body string, at int64) string {
	t.Helper()
	commitAt(t, f.work, file, body, at)
	git(t, f.work, "push", "-q", f.dir, "HEAD:refs/pull/"+n+"/head")
	return git(t, f.work, "rev-parse", "HEAD")
}

func TestFetchPRPutsTheHeadUnderTheAtriumRef(t *testing.T) {
	x := newStore(t, "main")
	main := x.forge.push(t, "main", "a.txt", "one", 1000)
	name, err := x.s.Hold(bg, repoURL)
	if err != nil || name != "github/o/r" {
		t.Fatalf("Hold = %q, %v", name, err)
	}
	head := x.forge.prHead(t, "7", "b.txt", "pr", 2000)

	got, err := x.s.FetchPR(bg, name, x.forge.dir, "refs/pull/7/head", PRRefPrefix+"7", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != head {
		t.Fatalf("FetchPR = %s, want %s", got, head)
	}
	dir, _ := x.s.Path(name)
	if sha := git(t, dir, "rev-parse", PRRefPrefix+"7"); sha != head {
		t.Fatalf("%s7 = %s, want %s", PRRefPrefix, sha, head)
	}
	// MAIN IS LEFT ALONE when it is already there: the base is only fetched into an empty main.
	if sha := git(t, dir, "rev-parse", MainRef); sha != main {
		t.Fatalf("main = %s, want %s", sha, main)
	}
}

func TestFetchPRFillsAnEmptyMainFromTheBase(t *testing.T) {
	x := newStore(t, "main")
	// The forge is empty when the store first holds it, as a private repository looks to a seed with no login.
	name, err := x.s.Hold(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := x.s.Path(name)
	if x.s.tip(bg, dir) != "" {
		t.Fatal("main is not empty")
	}
	main := x.forge.push(t, "main", "a.txt", "one", 1000)
	x.forge.prHead(t, "3", "b.txt", "pr", 2000)
	if _, err := x.s.FetchPR(bg, name, x.forge.dir, "refs/pull/3/head", PRRefPrefix+"3", "main", ""); err != nil {
		t.Fatal(err)
	}
	if sha := git(t, dir, "rev-parse", MainRef); sha != main {
		t.Fatalf("main = %s, want %s", sha, main)
	}
}

func TestFetchPRRefusesWhatIsNotAPullRequestHead(t *testing.T) {
	x := newStore(t, "main")
	x.forge.push(t, "main", "a.txt", "one", 1000)
	name, err := x.s.Hold(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	for why, c := range map[string][2]string{
		"a branch as dst":     {"refs/pull/1/head", MainRef},
		"a dotdot dst":        {"refs/pull/1/head", PRRefPrefix + "../heads/main"},
		"an option refspec":   {"--upload-pack=x", PRRefPrefix + "1"},
		"a refspec with dst":  {"refs/pull/1/head:refs/heads/main", PRRefPrefix + "1"},
		"an empty refspec":    {" ", PRRefPrefix + "1"},
		"a refspec with a sp": {"refs/pull/1/head refs/heads/x", PRRefPrefix + "1"},
	} {
		if _, err := x.s.FetchPR(bg, name, x.forge.dir, c[0], c[1], "", ""); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: err = %v, want a refusal", why, err)
		}
	}
	if _, err := x.s.FetchPR(bg, "github/o/other", x.forge.dir, "refs/pull/1/head", PRRefPrefix+"1", "", ""); !errors.Is(err, ErrRefused) {
		t.Errorf("a repository the store does not hold: err = %v", err)
	}
}

func TestFetchPRSaysAMissingHeadIsNotAnAuthFailure(t *testing.T) {
	x := newStore(t, "main")
	x.forge.push(t, "main", "a.txt", "one", 1000)
	name, err := x.s.Hold(bg, repoURL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = x.s.FetchPR(bg, name, x.forge.dir, "refs/pull/99/head", PRRefPrefix+"99", "", "")
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Auth {
		t.Fatalf("err = %#v, want a FetchError that is not Auth", err)
	}
	if strings.Contains(fe.Msg, x.root) {
		t.Fatalf("the sentence names the hub's path: %s", fe.Msg)
	}
}

func TestFetchFailedMarksAMissingCredential(t *testing.T) {
	x := newStore(t, "main")
	ref, _ := ParseName("github/o/r")
	for _, stderr := range []string{
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		"remote: Repository not found.\nfatal: repository 'https://github.com/o/r/' not found",
		"fatal: Authentication failed for 'https://github.com/o/r/'",
		"error: RPC failed; HTTP 403 curl 22 The requested URL returned error: 403",
	} {
		err := x.s.fetchFailed(ref, &Error{Stderr: stderr, Err: errors.New("exit status 128")})
		var fe *FetchError
		if !errors.As(err, &fe) || !fe.Auth {
			t.Errorf("%q: err = %#v, want Auth", stderr, err)
		}
	}
	err := x.s.fetchFailed(ref, &Error{Stderr: "fatal: couldn't find remote ref refs/pull/9/head", Err: errors.New("exit status 128")})
	var fe *FetchError
	if !errors.As(err, &fe) || fe.Auth || !strings.Contains(fe.Msg, "couldn't find remote ref") {
		t.Errorf("a missing ref: err = %#v", err)
	}
}

func TestPRFetchArgsSetTheHelperForThisCommandOnly(t *testing.T) {
	args := prFetchArgs("https://github.com/o/r", "!gh auth git-credential", []string{"+a:b"})
	// The configured helpers are reset first, then the CLI's own is the only one.
	i := slices.Index(args, "credential.helper=")
	j := slices.Index(args, "credential.helper=!gh auth git-credential")
	if i < 0 || j != i+2 || slices.Index(args, "fetch") < j {
		t.Fatalf("args = %q", args)
	}
	if slices.Index(args, "--") > slices.Index(args, "https://github.com/o/r") {
		t.Fatalf("the remote is not after --: %q", args)
	}
	for _, a := range prFetchArgs("x", "", []string{"+a:b"}) {
		if strings.HasPrefix(a, "credential.helper") {
			t.Fatalf("no helper asked, and one is set: %q", a)
		}
	}
}
