//go:build integration && git

package gitsync

import (
	"os"
	"path/filepath"
	"testing"
)

// An idle mirror pass runs no git, and a move of the checkout's origin/main is still carried.
func TestAnIdleMirrorPassRunsNoGit(t *testing.T) {
	x := newStore(t, "main")
	ck := filepath.Join(t.TempDir(), "ck")
	if err := os.MkdirAll(ck, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, ck, "init", "-q", "-b", "claude/main")
	git(t, ck, "update-ref", "refs/remotes/origin/main", commit(t, ck, "a.txt", "one"))
	x.h.Repos = func() ([]Repo, error) { return []Repo{{Name: "github/o/r", Checkout: ck, Branch: "claude/main"}}, nil }
	for i := 0; i < 2; i++ {
		if _, err := x.h.mirrorPass(bg, true); err != nil {
			t.Fatal(err)
		}
	}
	before := x.h.Runner.Spawned.Load()
	if _, err := x.h.mirrorPass(bg, true); err != nil {
		t.Fatal(err)
	}
	if n := x.h.Runner.Spawned.Load() - before; n != 0 {
		t.Fatalf("an idle pass started %d git processes, want 0", n)
	}
	sha := commit(t, ck, "b.txt", "two")
	git(t, ck, "update-ref", "refs/remotes/origin/main", sha)
	if _, err := x.h.mirrorPass(bg, true); err != nil {
		t.Fatal(err)
	}
	if got := git(t, x.h.Bare("github/o/r"), "rev-parse", ForgeRefPrefix+"main"); got != sha {
		t.Fatalf("the forge's main is %s, want %s", got, sha)
	}
}

// The store's ref print moves with a branch made on the hub's disk, in a reftable repository too.
func TestTheStoresRefPrintMovesWithABranch(t *testing.T) {
	x := newStore(t, "main")
	x.forge.push(t, "main", "a.txt", "one", 1000)
	if _, err := x.s.Init(bg, repoURL); err != nil {
		t.Fatal(err)
	}
	a := x.s.refsPrint()
	if b := x.s.refsPrint(); a != b {
		t.Fatal("the print moved with nothing changed")
	}
	dir, _ := x.s.Path("o/r")
	git(t, dir, "branch", "feature", "main")
	if x.s.refsPrint() == a {
		t.Fatal("the print did not move with a new branch")
	}
}
