package forge

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

const bbPRJSON = `{"title":"fix it","author":{"display_name":"Ekoby K","nickname":"ekoby"},
"source":{"branch":{"name":"feat"},"commit":{"hash":"abc123def456"},"repository":{"full_name":"%s"}},
"destination":{"branch":{"name":"main"},"commit":{"hash":"fff"},"repository":{"full_name":"o/r"}}}`

const bbStatJSON = `{"values":[{"lines_added":3,"lines_removed":1,"new":{"path":"a.c"},"old":{"path":"a.c"}},
{"lines_added":0,"lines_removed":9,"new":null,"old":{"path":"gone.c"}}]}`

func bbRunner(source string, got *[]Cmd) Runner {
	return func(_ context.Context, c Cmd) ([]byte, error) {
		*got = append(*got, c)
		p := c.Args[1]
		switch {
		case strings.HasSuffix(p, "/diff"):
			return []byte("diff --git a/a.c b/a.c\n"), nil
		case strings.Contains(p, "/diffstat"):
			return []byte(bbStatJSON), nil
		}
		return []byte(strings.Replace(bbPRJSON, "%s", source, 1)), nil
	}
}

func TestBitbucketViewSameRepo(t *testing.T) {
	var got []Cmd
	b := newBitbucket("", bbRunner("o/r", &got))
	ref := Ref{Host: "bitbucket.org", Org: "o", Repo: "r", Number: 7}
	v, err := b.View(context.Background(), ref)
	if err != nil || v.Head != "abc123def456" || v.HeadRef != "feat" || v.BaseRef != "main" || v.FromFork ||
		v.Author != "ekoby" || v.Title != "fix it" {
		t.Fatalf("%+v %v", v, err)
	}
	if len(v.Files) != 2 || v.Files[0].Path != "a.c" || v.Files[0].Additions != 3 || v.Files[1].Path != "gone.c" ||
		v.Files[1].Deletions != 9 {
		t.Errorf("%+v", v.Files)
	}
	if got[0].Name != "bb" || got[0].Args[0] != "api" || got[0].Args[1] != "/repositories/o/r/pullrequests/7" {
		t.Errorf("%+v", got[0])
	}
	fs := b.FetchSpec(ref)
	if fs.Remote != "https://bitbucket.org/o/r.git" || fs.Refspec != "feat" {
		t.Errorf("%+v", fs)
	}
	if u := b.PRURL(ref); u != "https://bitbucket.org/o/r/pull-requests/7" {
		t.Errorf("%s", u)
	}
	if h, err := b.Head(context.Background(), ref); err != nil || h != "abc123def456" {
		t.Fatalf("%q %v", h, err)
	}
	if d, err := b.Diff(context.Background(), ref); err != nil || !strings.HasPrefix(string(d), "diff") {
		t.Fatalf("%q %v", d, err)
	}
}

func TestBitbucketForkPR(t *testing.T) {
	var got []Cmd
	b := newBitbucket("bbw", bbRunner("someone/r-fork", &got))
	ref := Ref{Org: "o", Repo: "r", Number: 9}
	v, err := b.View(context.Background(), ref)
	if err != nil || !v.FromFork {
		t.Fatalf("%+v %v", v, err)
	}
	if fs := b.FetchSpec(ref); fs.Remote != "https://bitbucket.org/someone/r-fork.git" || fs.Refspec != "feat" {
		t.Errorf("%+v", fs)
	}
	if got[0].Name != "bbw" {
		t.Errorf("%+v", got[0])
	}
	// No View yet: the destination, and no refspec to fetch.
	if fs := b.FetchSpec(Ref{Org: "o", Repo: "r", Number: 10}); fs.Refspec != "" || !strings.HasSuffix(fs.Remote, "/o/r.git") {
		t.Errorf("%+v", fs)
	}
}

func TestBitbucketAccessErrors(t *testing.T) {
	ref := Ref{Org: "o", Repo: "r", Number: 1}
	run := func(err error) Runner { return func(context.Context, Cmd) ([]byte, error) { return nil, err } }

	_, err := newBitbucket("", run(&exec.Error{Name: "bb", Err: exec.ErrNotFound})).Diff(context.Background(), ref)
	var ae *AccessError
	if !errors.As(err, &ae) || !ae.NotInstalled || ae.Tool != "bb" || ae.Host != "bitbucket.org" ||
		!strings.Contains(err.Error(), "bb auth login") || strings.Contains(err.Error(), "--hostname") {
		t.Fatalf("%v", err)
	}
	_, err = newBitbucket("", run(errors.New("bb: not logged in, run bb auth login"))).View(context.Background(), ref)
	if !errors.As(err, &ae) || ae.NotInstalled || !strings.Contains(err.Error(), "not logged in for bitbucket.org") {
		t.Fatalf("%v", err)
	}
	other := errors.New("bb: 404 not found")
	if _, err = newBitbucket("", run(other)).Head(context.Background(), ref); err != other {
		t.Errorf("%v", err)
	}
}

func TestBitbucketBadOutput(t *testing.T) {
	run := func(context.Context, Cmd) ([]byte, error) { return []byte("<html>"), nil }
	if _, err := newBitbucket("", run).Head(context.Background(), Ref{Org: "o", Repo: "r", Number: 1}); err == nil {
		t.Error("want an error")
	}
}
