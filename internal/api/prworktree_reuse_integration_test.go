//go:build integration

package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// slowForge reads a pull request only once release is closed, so a test can show what ran while the forge was being read.
type slowForge struct {
	fakeForge
	release chan struct{}
}

func (f *slowForge) View(ctx context.Context, r forge.Ref) (*forge.PR, error) {
	select {
	case <-f.release:
	case <-time.After(5 * time.Second):
	}
	return f.fakeForge.View(ctx, r)
}

// headForge is a forge that answers the PR and names its head in the upstream repo.
func headForge(up string) forge.Forge {
	return &fakeForge{pr: forge.PR{HeadRef: "feat/x"}, spec: forge.FetchSpec{Remote: up, Refspec: "pull/7/head"}}
}

// reuseHarness is a room with the repo already under its scm folder, and counters on what a PR worktree does with it.
type reuseHarness struct {
	*prHarness
	scm     string
	mu      sync.Mutex
	finds   int
	fetches []string
}

func newReuseHarness(t *testing.T, forgeOf func(up string) forge.Forge, onFind func()) *reuseHarness {
	t.Helper()
	hn := newPRHarness(t, &fakeForge{})
	// No provider is passed to prWorktree: the repo is the scm clone, which the room already has.
	rh := &reuseHarness{prHarness: hn, scm: filepath.Join(t.TempDir(), "scm", "github.com", "o", "r")}
	mkDir(t, filepath.Dir(rh.scm))
	git(t, filepath.Dir(rh.scm), "clone", "-q", hn.up, rh.scm)
	f := forgeOf(hn.up)
	hn.srv.PRForge = func(string) (forge.Forge, error) { return f, nil }
	hn.srv.SCMHas = func(string) bool { return true }
	hn.srv.SCMClone = func(_ context.Context, url string) (gitsync.SCMResult, error) {
		rh.mu.Lock()
		rh.finds++
		rh.mu.Unlock()
		if onFind != nil {
			onFind()
		}
		return gitsync.SCMResult{Path: filepath.ToSlash(rh.scm), State: "existing"}, nil
	}
	inner := hn.srv.PRFetch
	hn.srv.PRFetch = func(ctx context.Context, dir string, spec forge.FetchSpec, dst string) error {
		rh.mu.Lock()
		rh.fetches = append(rh.fetches, spec.Refspec+" -> "+dst)
		rh.mu.Unlock()
		return inner(ctx, dir, spec, dst)
	}
	return rh
}

func (rh *reuseHarness) make(t *testing.T, ctx context.Context) prWorktreeResult {
	t.Helper()
	res, status, err := rh.srv.prWorktree(ctx, nil, prWorktreeRequest{Host: "github.com", Org: "o", Repo: "r", Number: 7})
	if err != nil {
		t.Fatalf("%d %v", status, err)
	}
	return res
}

// A repo the room already has under its scm folder is not cloned: the clone is found, the one ref is fetched, and the
// worktree is added. That is what gwt does.
func TestPRWorktreeOnAnExistingCloneFetchesOneRefAndClonesNothing(t *testing.T) {
	rh := newReuseHarness(t, headForge, nil)
	res := rh.make(t, context.Background())
	if res.Existed || res.Branch != "feat/x" {
		t.Fatalf("%+v", res)
	}
	if got := git(t, res.Path, "log", "-1", "--format=%s"); got != "pr" {
		t.Errorf("the worktree is not at the PR head: %q", got)
	}
	if rh.finds != 1 {
		t.Errorf("the clone was asked for %d times, want 1", rh.finds)
	}
	if len(rh.fetches) != 1 || rh.fetches[0] != "pull/7/head -> refs/atrium/pr/7" {
		t.Errorf("fetches %v, want the one head ref", rh.fetches)
	}
	// nothing else of the repo came: the only ref added is the head
	if got := git(t, rh.scm, "for-each-ref", "--format=%(refname)", "refs/atrium/"); got != "refs/atrium/pr/7" {
		t.Errorf("refs under atrium: %q", got)
	}
	if _, err := os.Stat(filepath.Join(rh.scm, ".git")); err != nil {
		t.Errorf("the clone is gone: %v", err)
	}
}

// With the clone already there, finding it runs while the forge is read: the forge here does not answer until the
// clone has been asked for, so a path that found the clone only after the read would wait it out.
func TestPRWorktreeFindsTheCloneWhileTheForgeIsRead(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	rh := newReuseHarness(t, func(up string) forge.Forge {
		return &slowForge{fakeForge: fakeForge{pr: forge.PR{HeadRef: "feat/x"},
			spec: forge.FetchSpec{Remote: up, Refspec: "pull/7/head"}}, release: release}
	}, func() { once.Do(func() { close(release) }) })

	start := time.Now()
	res := rh.make(t, context.Background())
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("the clone was found only after the forge answered: %v", took)
	}
	if res.Branch != "feat/x" {
		t.Errorf("%+v", res)
	}
}

// A repo the room has to clone is still cloned only after the forge answers, so a forge that refuses clones nothing.
func TestPRWorktreeWithoutACloneWaitsForTheForgeBeforeCloning(t *testing.T) {
	rh := newReuseHarness(t, func(string) forge.Forge {
		return &fakeForge{err: &forge.AccessError{Tool: "gh", Host: "github.com", NotInstalled: true}}
	}, nil)
	rh.srv.SCMHas = func(string) bool { return false }
	if _, _, err := rh.srv.prWorktree(context.Background(), nil,
		prWorktreeRequest{Host: "github.com", Org: "o", Repo: "r", Number: 7}); err == nil {
		t.Fatal("a refused forge made a worktree")
	}
	if rh.finds != 0 {
		t.Errorf("a repo with no clone was cloned before the forge answered: %d", rh.finds)
	}
}

// Every step is said as it starts, so a slow one is never silent, and in order.
func TestPRWorktreeSaysEachStep(t *testing.T) {
	rh := newReuseHarness(t, headForge, nil)
	var said []string
	steps := &opSteps{label: "test", last: time.Now(), say: func(s string) { said = append(said, s) }}
	rh.make(t, withOpSteps(context.Background(), steps))
	want := []string{"reading pull request 7", "finding the clone of o/r", "fetching the head of pull request 7", "making the worktree"}
	if len(said) != len(want) {
		t.Fatalf("said %q, want %q", said, want)
	}
	for i := range want {
		if !strings.HasPrefix(said[i], want[i]) {
			t.Errorf("step %d said %q, want %q", i, said[i], want[i])
		}
	}
}
