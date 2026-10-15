//go:build integration && git

package gitsync

import "testing"

// A BRANCH FAST-FORWARDED ONTO claude/main IS LANDED AT ITS OWN TIP, and it is pushed only when the forge's main holds it.
func TestLandFactsFollowABranchFromPushedToLandedToPushed(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	card := roomCard("sg4", "C1")
	work := x.branch("fix/x", "x.go")
	x.must(x.push(card, hubRepo, "fix/x:refs/heads/fix/x"))
	st := x.h.Store()

	forge := t.TempDir()
	git(t, forge, "init", "-q", "--bare", "-b", "main")
	st.Protocols = "file"
	st.Forge = func(Ref) string { return forge }

	f, err := st.LandFacts(bg, hubRepo, "fix/x", "", "f-x")
	if err != nil || f.SHA != work || f.Landed || f.Pushed {
		t.Fatalf("pushed to the hub only: %+v %v", f, err)
	}
	if len(f.Paths) != 1 || f.Paths[0] != "x.go" {
		t.Fatalf("paths = %v", f.Paths)
	}
	if f, _ := st.LandFacts(bg, hubRepo, "fix/nowhere", "", "f-x"); f.SHA != "" {
		t.Fatalf("no branch: %+v", f)
	}

	// The merger fast-forwards claude/main onto the tip.
	git(t, x.work, "switch", "-q", "-C", "cm", "fix/x")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	f, err = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x")
	if err != nil || !f.Landed || f.LandedSHA != work || f.Pushed {
		t.Fatalf("after the fast-forward: %+v %v", f, err)
	}
	if f.PushedVia != "origin" && f.PushedVia != "mirror" {
		t.Fatalf("pushed says nothing of where it looked: %+v", f)
	}

	// A commit on top of it: the landed sha is still the tip it had.
	git(t, x.work, "commit", "-q", "--allow-empty", "-m", "later")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	if f, _ = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x"); !f.Landed || f.LandedSHA != work {
		t.Fatalf("a later commit moved the landed sha: %+v", f)
	}

	// Clint pushes: the forge's main holds it.
	git(t, x.work, "push", "-q", forge, "cm:refs/heads/main")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/main"))
	f, err = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x")
	if err != nil || !f.Pushed || f.PushedVia != "origin" {
		t.Fatalf("after the push: %+v %v", f, err)
	}

	// A build that holds the landed sha, and one that does not.
	tip := git(t, x.work, "rev-parse", "HEAD")
	if c, known, err := st.Contains(bg, hubRepo, tip, work); err != nil || !known || !c {
		t.Fatalf("a newer build: %v %v %v", c, known, err)
	}
	if c, known, _ := st.Contains(bg, hubRepo, strings40('a'), work); known || c {
		t.Fatalf("a build the mirror has never seen: %v %v", c, known)
	}
}

func strings40(c byte) string {
	b := make([]byte, 40)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
