//go:build integration

package gitsync

import "testing"

// A branch is merged when claude/main has its change, by ancestry or by patch: a cherry-pick, a rebase and a
// re-sign all give the same change a new sha.
func TestItemFactsSeeThroughACherryPick(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	card := roomCard("sg4", "C1")
	work := x.branch("fix/x", "x.txt")
	other := x.branch("fix/other", "o.txt")
	x.must(x.push(card, hubRepo, "fix/x:refs/heads/fix/x", "fix/other:refs/heads/fix/other"))
	st := x.h.Store()

	f, err := st.ItemFacts(bg, hubRepo, "fix/x", "")
	if err != nil || f.SHA != work || !f.Implemented || f.Merged || f.PushedByClint {
		t.Fatalf("pushed only: %+v %v", f, err)
	}
	// A short sha names the same commit.
	if f, err = st.ItemFacts(bg, hubRepo, "", work[:9]); err != nil || f.SHA != work || !f.Implemented {
		t.Fatalf("short sha: %+v %v", f, err)
	}
	if f, _ := st.ItemFacts(bg, hubRepo, "fix/nowhere", ""); f.Implemented || f.SHA != "" {
		t.Fatalf("no branch: %+v", f)
	}
	if f, err := st.ItemFacts(bg, "github/o/none", "fix/x", ""); err != nil || f.Implemented {
		t.Fatalf("no repository: %+v %v", f, err)
	}

	// The merger lands the change as a cherry-pick: another sha, the same patch.
	git(t, x.work, "switch", "-q", "-C", "cm", "main")
	git(t, x.work, "cherry-pick", work)
	picked := git(t, x.work, "rev-parse", "HEAD")
	if picked == work {
		t.Fatal("the cherry-pick kept the sha")
	}
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	f, _ = st.ItemFacts(bg, hubRepo, "fix/x", "")
	if !f.Merged || f.PushedByClint {
		t.Fatalf("after the merge: %+v", f)
	}
	if o, _ := st.ItemFacts(bg, hubRepo, "fix/other", ""); o.SHA != other || o.Merged {
		t.Fatalf("an unrelated branch is merged: %+v", o)
	}
	// A check on the picked commit covers the item, one on main does not.
	main := x.refOn(hubRepo, MainRef)
	if ok, err := st.CoveredBy(bg, hubRepo, picked, work); err != nil || !ok {
		t.Fatalf("a check on the merge does not cover the item: %v %v", ok, err)
	}
	if ok, _ := st.CoveredBy(bg, hubRepo, main, work); ok {
		t.Fatal("main, which lacks the change, covers the item")
	}

	// Clint pushes it, re-signed: main now has it.
	x.must(x.push(operator, hubRepo, "cm:refs/heads/main"))
	if f, _ = st.ItemFacts(bg, hubRepo, "fix/x", ""); !f.PushedByClint {
		t.Fatalf("after clint's push: %+v", f)
	}
}
