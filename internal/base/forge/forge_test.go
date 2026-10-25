package forge

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestPickByHost(t *testing.T) {
	entries := []Entry{
		{Host: "git.corp.example", Forge: "github", Cmd: "ghe"},
		{Host: "off.example", Forge: "none"},
	}
	for _, c := range []struct{ host, kind, cmd string }{
		{"github.com", GitHub, ""},
		{"GitHub.com", GitHub, ""},
		{"bitbucket.org", Bitbucket, ""},
		{"git.corp.example", GitHub, "ghe"},
	} {
		kind, cmd, err := Pick(c.host, entries)
		if err != nil || kind != c.kind || cmd != c.cmd {
			t.Errorf("%s: %q %q %v", c.host, kind, cmd, err)
		}
	}
	// A provider with an empty forge infers it from its own host.
	if kind, _, err := Pick("github.com", []Entry{{Host: "github.com"}}); err != nil || kind != GitHub {
		t.Errorf("inferred: %q %v", kind, err)
	}
}

func TestNoForgeSentence(t *testing.T) {
	for _, host := range []string{"gitlab.example.com", "off.example"} {
		_, _, err := Pick(host, []Entry{{Host: "off.example", Forge: "none"}})
		var nf *NoForgeError
		if !errors.As(err, &nf) || nf.Code() != "no_forge" {
			t.Fatalf("%s: %v", host, err)
		}
		for _, want := range []string{"no_forge", host, "provider", "forge"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q lacks %q", err, want)
			}
		}
	}
}

func TestUnbuiltForgeIsNoForge(t *testing.T) {
	_, err := For("gitlab.example.com", []Entry{{Host: "gitlab.example.com", Forge: GitLab}}, nil)
	var nf *NoForgeError
	if !errors.As(err, &nf) || !strings.Contains(err.Error(), "gitlab") {
		t.Fatalf("%v", err)
	}
}

func TestAccessErrorSentences(t *testing.T) {
	run := func(err error) Runner {
		return func(context.Context, Cmd) ([]byte, error) { return nil, err }
	}
	g := newGitHub("", run(&exec.Error{Name: "gh", Err: exec.ErrNotFound}))
	_, err := g.Diff(context.Background(), Ref{Host: "github.com", Org: "o", Repo: "r", Number: 1})
	var ae *AccessError
	if !errors.As(err, &ae) || !ae.NotInstalled || ae.Tool != "gh" || ae.Host != "github.com" {
		t.Fatalf("%#v", err)
	}
	if !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), "gh auth login --hostname github.com") {
		t.Errorf("%s", err)
	}

	g = newGitHub("", run(errors.New("gh pr: To get started with GitHub CLI, please run:  gh auth login")))
	_, err = g.View(context.Background(), Ref{Host: "ghe.example", Org: "o", Repo: "r", Number: 1})
	if !errors.As(err, &ae) || ae.NotInstalled || ae.Host != "ghe.example" || ae.Detail == "" {
		t.Fatalf("%#v", err)
	}
	if !strings.Contains(err.Error(), "not logged in for ghe.example") ||
		!strings.Contains(err.Error(), "gh auth login --hostname ghe.example") {
		t.Errorf("%s", err)
	}

	// Anything else passes through untouched.
	other := errors.New("gh pr: GraphQL: Could not resolve to a PullRequest")
	if _, err = newGitHub("", run(other)).Head(context.Background(), Ref{Org: "o", Repo: "r", Number: 1}); err != other {
		t.Errorf("%v", err)
	}
}

func TestGitHubArgvAndParse(t *testing.T) {
	var got []Cmd
	run := func(_ context.Context, c Cmd) ([]byte, error) {
		got = append(got, c)
		if c.Args[1] == "diff" {
			return []byte("diff --git a b\n"), nil
		}
		return []byte(`{"title":"t","author":{"login":"ekoby"},"headRefOid":"abc1234","headRefName":"feat",` +
			`"baseRefName":"main","isCrossRepository":true,"files":[{"path":"a.c","additions":1,"deletions":2}]}`), nil
	}
	g := newGitHub("ghw", run)
	ref := Ref{Host: "ghe.example", Org: "o", Repo: "r", Number: 7}
	v, err := g.View(context.Background(), ref)
	if err != nil || v.Head != "abc1234" || v.HeadRef != "feat" || v.BaseRef != "main" || !v.FromFork ||
		v.Author != "ekoby" || len(v.Files) != 1 || v.Files[0].Deletions != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	if h, err := g.Head(context.Background(), ref); err != nil || h != "abc1234" {
		t.Fatalf("%q %v", h, err)
	}
	if got[0].Name != "ghw" || got[0].Args[4] != "ghe.example/o/r" {
		t.Errorf("%+v", got[0])
	}
	if d, err := g.Diff(context.Background(), ref); err != nil || !strings.HasPrefix(string(d), "diff") {
		t.Fatalf("%q %v", d, err)
	}
	fs := g.FetchSpec(ref)
	if fs.Remote != "https://ghe.example/o/r.git" || fs.Refspec != "pull/7/head" {
		t.Errorf("%+v", fs)
	}
	if u := g.PRURL(Ref{Org: "o", Repo: "r", Number: 7}); u != "https://github.com/o/r/pull/7" {
		t.Errorf("%s", u)
	}
}
