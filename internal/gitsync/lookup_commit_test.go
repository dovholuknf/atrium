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
