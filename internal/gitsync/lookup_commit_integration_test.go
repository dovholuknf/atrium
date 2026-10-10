//go:build integration

package gitsync

import "testing"

// A commit a worker pushed is found on the hub by the commit alone, on a claude/*-w branch, as the tip and as an
// ancestor of the tip. No repository is named.
func TestACommitIsFoundOnAnyHubBranchWithNoRepositoryNamed(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	first := x.branch("claude/item-w", "x.txt")
	card := roomCard("sg4", "C1")
	x.must(x.push(card, hubRepo, "claude/item-w:refs/heads/claude/item-w"))
	tip := x.grow("claude/item-w", "y.txt")
	x.must(x.push(card, hubRepo, "claude/item-w:refs/heads/claude/item-w"))

	for name, sha := range map[string]string{"the tip": tip, "a short tip": tip[:8], "an ancestor": first} {
		a := x.h.Lookup(bg, URLQuery{Commit: sha})
		if a.State != URLFound || a.Repo != hubRepo || len(a.Branches) != 1 {
			t.Fatalf("%s: %+v", name, a)
		}
	}
}

// A repository without the object is skipped with one git call: its branches are never searched.
func TestARepositoryWithoutTheCommitIsSkippedAndNotSearched(t *testing.T) {
	x := newRecv(t)
	other := x.makeRepo("github/a/other")
	x.seedMain(hubRepo)
	first := x.branch("claude/item-w", "x.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "claude/item-w:refs/heads/claude/item-w"))
	x.grow("claude/item-w", "y.txt")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "claude/item-w:refs/heads/claude/item-w"))

	var searched []string
	onContains = func(dir string) { searched = append(searched, dir) }
	t.Cleanup(func() { onContains = nil })
	if a := x.h.Lookup(bg, URLQuery{Commit: first}); a.State != URLFound || a.Repo != hubRepo || a.Branch == "" {
		t.Fatalf("%+v", a)
	}
	for _, dir := range searched {
		if dir == other {
			t.Fatalf("searched the repository that has no such commit: %v", searched)
		}
	}
	if len(searched) != 1 {
		t.Fatalf("searched %v, want only the repository that has it", searched)
	}
}

// A commit no hub branch has is not found, and neither is something that is not a commit id.
func TestACommitNoHubBranchHasIsNotFound(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	for _, sha := range []string{"deadbeefdeadbeef", "abc", "--all", ""} {
		if a := x.h.Lookup(bg, URLQuery{Commit: sha}); a.State == URLFound {
			t.Fatalf("%q: %+v", sha, a)
		}
	}
}
