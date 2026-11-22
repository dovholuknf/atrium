//go:build integration && git

package gitsync

import "testing"

func TestDeleteFromHubTakesABranchWhoseTipMovedAfterTheRoomFetchedIt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	t.Parallel()
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("claude/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	tok, _ := f.cards.Mint("C1")
	if out, err := PushToHub(bg, Default, f.work, f.base, tok, "claude/x", ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	commit(t, f.work, "y.txt", "y")
	if out, err := PushToHub(bg, Default, f.work, f.base, tok, "claude/x", ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	git(t, f.work, "update-ref", "refs/remotes/hub/claude/x", "HEAD~1")
	if out, err := DeleteFromHub(bg, Default, f.work, f.base, tok, "claude/x", ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.refOn(hubRepo, "refs/heads/claude/x"); got != "" {
		t.Fatalf("claude/x is still at %q", got)
	}
}

func TestDeleteFromHubTakesABranchTheHubAlreadyDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	t.Parallel()
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("claude/x", "x.txt")
	git(t, f.work, "remote", "add", "hub", f.repoURL(hubRepo))
	tok, _ := f.cards.Mint("C1")
	if out, err := PushToHub(bg, Default, f.work, f.base, tok, "claude/x", ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for i := range 2 {
		if out, err := DeleteFromHub(bg, Default, f.work, f.base, tok, "claude/x", ""); err != nil {
			t.Fatalf("delete %d: %v\n%s", i, err, out)
		}
	}
}

func TestFetchFromHubBringsTheBranchIntoTheHubRemoteRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	t.Parallel()
	f := newRemote(t)
	f.seedMain(hubRepo)
	f.branch("claude/x", "x.txt")
	tok, _ := f.cards.Mint("C1")
	if out, err := PushToHub(bg, Default, f.work, f.base, tok, "claude/x", hubRepo); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	tip := git(t, f.work, "rev-parse", "refs/heads/claude/x")
	git(t, f.work, "update-ref", "-d", "refs/remotes/hub/claude/x")
	_ = FetchFromHub(bg, Default, f.work, f.base, tok, "claude/x", hubRepo)
	if got := git(t, f.work, "rev-parse", "refs/remotes/hub/claude/x"); got != tip {
		t.Fatalf("hub/claude/x is %s, want %s", got, tip)
	}
}
