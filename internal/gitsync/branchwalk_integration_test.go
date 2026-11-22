//go:build integration && git

package gitsync

import (
	"errors"
	"strings"
	"testing"
)

// A HUB BRANCH IS WALKED AGAINST MAIN with no card: its own commits are in the patch, a branch the hub does not list is
// refused, and so is a repository it does not hold.
func TestBranchWalkIsAHubBranchAgainstMain(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	t.Parallel()
	x := newRecv(t)
	x.seedMain(hubRepo)
	x.branch("claude/secfix-v0.5.2", "fix.go")
	x.grow("claude/secfix-v0.5.2", "more.go")
	x.must(x.push(roomCard("claude-room-e", "C1"), hubRepo, "claude/secfix-v0.5.2:refs/heads/claude/secfix-v0.5.2"))
	st := x.h.Store()

	w, err := st.BranchWalk(bg, hubRepo, "claude/secfix-v0.5.2")
	if err != nil {
		t.Fatal(err)
	}
	if w.Base != "main" || w.Branch != "claude/secfix-v0.5.2" || !ValidSHA(w.Head) || !ValidSHA(w.MergeBase) {
		t.Errorf("walked %+v", w)
	}
	for _, f := range []string{"b/fix.go", "b/more.go"} {
		if !strings.Contains(w.Diff, f) {
			t.Errorf("the patch has no %s:\n%s", f, w.Diff)
		}
	}
	for _, b := range []string{"claude/nope", "refs/heads/main", "--all", "main..x"} {
		if _, err := st.BranchWalk(bg, hubRepo, b); !errors.Is(err, ErrRefused) {
			t.Errorf("%q: %v", b, err)
		}
	}
	if _, err := st.BranchWalk(bg, "github/o/nowhere", "main"); err == nil {
		t.Error("a repository the hub does not hold was walked")
	}
}
