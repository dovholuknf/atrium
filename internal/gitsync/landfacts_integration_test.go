//go:build integration && git

package gitsync

import (
	"os"
	"path/filepath"
	"testing"
)

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

	f, err := st.LandFacts(bg, hubRepo, "fix/x", "", "f-x", forgeAsk(st))
	if err != nil || f.SHA != work || f.Landed || f.Pushed {
		t.Fatalf("pushed to the hub only: %+v %v", f, err)
	}
	if len(f.Paths) != 1 || f.Paths[0] != "x.go" {
		t.Fatalf("paths = %v", f.Paths)
	}
	if f, _ := st.LandFacts(bg, hubRepo, "fix/nowhere", "", "f-x", forgeAsk(st)); f.SHA != "" {
		t.Fatalf("no branch: %+v", f)
	}

	// The merger fast-forwards claude/main onto the tip.
	git(t, x.work, "switch", "-q", "-C", "cm", "fix/x")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	f, err = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x", forgeAsk(st))
	if err != nil || !f.Landed || f.LandedSHA != work || f.Pushed {
		t.Fatalf("after the fast-forward: %+v %v", f, err)
	}
	if f.PushedVia != "origin" && f.PushedVia != "mirror" {
		t.Fatalf("pushed says nothing of where it looked: %+v", f)
	}

	// A commit on top of it: the landed sha is still the tip it had.
	git(t, x.work, "commit", "-q", "--allow-empty", "-m", "later")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	if f, _ = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x", forgeAsk(st)); !f.Landed || f.LandedSHA != work {
		t.Fatalf("a later commit moved the landed sha: %+v", f)
	}

	// Clint pushes: the forge's main holds it.
	git(t, x.work, "push", "-q", forge, "cm:refs/heads/main")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/main"))
	f, err = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x", forgeAsk(st))
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

// forgeAsk is how a pass asks the forge, forced, as a mirror pass would.
func forgeAsk(st *Store) func() ForgeMain {
	return func() ForgeMain { return st.ForgeMainTip(bg, hubRepo, true) }
}

// MOST ITEMS LAND BY CHERRY-PICK AND ARE RE-SIGNED ONTO ORIGIN'S MAIN: new shas, the same patches. Both are found, and the
// sha recorded is the one on claude/main, and on main, that carries the work.
func TestLandFactsSeeThroughACherryPickAndAResign(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	work := x.branch("fix/x", "x.go")
	other := x.branch("fix/other", "o.go")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x", "fix/other:refs/heads/fix/other"))
	st := x.h.Store()
	forge := t.TempDir()
	git(t, forge, "init", "-q", "--bare", "-b", "main")
	st.Protocols = "file"
	st.Forge = func(Ref) string { return forge }

	// The merger cherry-picks it onto claude/main.
	git(t, x.work, "switch", "-q", "-C", "cm", "main")
	git(t, x.work, "cherry-pick", work)
	picked := git(t, x.work, "rev-parse", "HEAD")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	f, err := st.LandFacts(bg, hubRepo, "fix/x", "", "f-x", forgeAsk(st))
	if err != nil || !f.Landed || f.LandedSHA != picked || f.Pushed {
		t.Fatalf("after the cherry-pick (picked %s): %+v %v", picked, f, err)
	}
	if o, _ := st.LandFacts(bg, hubRepo, "fix/other", "", "f-o", forgeAsk(st)); o.SHA != other || o.Landed {
		t.Fatalf("an unrelated branch landed: %+v", o)
	}

	// Clint re-signs it onto origin's main: the same patch, a new sha, and the message differs.
	git(t, x.work, "switch", "-q", "-C", "resigned", "main")
	git(t, x.work, "cherry-pick", work)
	git(t, x.work, "commit", "-q", "--amend", "-m", "re-signed", "--date", "2020-01-01T00:00:00Z")
	resigned := git(t, x.work, "rev-parse", "HEAD")
	if resigned == picked || resigned == work {
		t.Fatal("the re-sign kept a sha")
	}
	git(t, x.work, "push", "-q", forge, "resigned:refs/heads/main")
	x.must(x.push(operator, hubRepo, "resigned:refs/heads/main"))
	f, err = st.LandFacts(bg, hubRepo, "fix/x", "", "f-x", forgeAsk(st))
	if err != nil || !f.Pushed || f.PushedSHA != resigned || f.PushedVia != "origin" || f.LandedSHA != picked {
		t.Fatalf("after the re-sign (resigned %s): %+v %v", resigned, f, err)
	}
}

// THE FORGE IS ASKED ONCE, then remembered, unless something says to look again.
func TestForgeMainTipIsRememberedUntilForced(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	st := x.h.Store()
	forge := t.TempDir()
	git(t, forge, "init", "-q", "--bare", "-b", "main")
	st.Protocols = "file"
	asked := 0
	st.Forge = func(Ref) string { asked++; return forge }
	if fm := st.ForgeMainTip(bg, hubRepo, false); fm.Tip != "" || fm.Why == "" {
		t.Fatalf("a forge with no main: %+v", fm)
	}
	st.ForgeMainTip(bg, hubRepo, false)
	st.ForgeMainTip(bg, hubRepo, false)
	if asked != 1 {
		t.Fatalf("asked %d times in a row", asked)
	}
	st.ForgeMainTip(bg, hubRepo, true)
	if asked != 2 {
		t.Fatalf("a forced ask was not made: %d", asked)
	}
}

// A BUILDER'S COMMIT THE HUB NEVER HELD, OR ONE PICKED WITH A CONFLICT RESOLVED, STILL LANDS BY THE ITEM'S CHANGELOG ENTRY.
func TestLandFactsFindALandingByTheItemsChangelogEntry(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	st := x.h.Store()
	forge := t.TempDir()
	git(t, forge, "init", "-q", "--bare", "-b", "main")
	st.Protocols = "file"
	st.Forge = func(Ref) string { return forge }

	// Nothing on claude/main names the item yet, and the builder's sha is one the mirror never had.
	f, err := st.LandFacts(bg, hubRepo, "", strings40('b'), "f-cl", forgeAsk(st))
	if err != nil || f.Landed {
		t.Fatalf("before: %+v %v", f, err)
	}
	git(t, x.work, "switch", "-q", "-C", "cm")
	if err := os.MkdirAll(filepath.Join(x.work, "changelog", "board"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x.work, "changelog", "board", "2026-10-15-f-cl.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, x.work, "add", "-A")
	git(t, x.work, "commit", "-q", "-m", "land f-cl")
	picked := git(t, x.work, "rev-parse", "HEAD")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	f, err = st.LandFacts(bg, hubRepo, "", strings40('b'), "f-cl", forgeAsk(st))
	if err != nil || !f.Landed || f.LandedSHA != picked || f.Pushed {
		t.Fatalf("landed: %+v %v", f, err)
	}
	if len(f.Paths) == 0 {
		t.Fatalf("no paths: %+v", f)
	}
	// The builder named no sha at all.
	if f, _ = st.LandFacts(bg, hubRepo, "", "-", "f-cl", forgeAsk(st)); !f.Landed || f.LandedSHA != picked {
		t.Fatalf("no sha: %+v", f)
	}
	git(t, x.work, "push", "-q", forge, "cm:refs/heads/main")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/main"))
	if f, _ = st.LandFacts(bg, hubRepo, "", "-", "f-cl", forgeAsk(st)); !f.Pushed || f.PushedSHA != picked {
		t.Fatalf("pushed: %+v", f)
	}
}

// EVERY ITEM AT ONE TIP IS READ FROM ONE `git log`, and a pass where no tip moved starts no git at all. A log per item kept
// twenty git children alive on the live hub.
func TestLandFactsReadEveryItemAtOneTipFromOneLog(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	st := x.h.Store()
	git(t, x.work, "switch", "-q", "-C", "cm")
	ids := []string{"n-1", "n-2", "n-3", "n-4", "n-5", "n-6"}
	shas := map[string]string{}
	for _, id := range ids {
		dir := filepath.Join(x.work, "changelog", "board")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "2026-10-24-"+id+".md"), []byte(id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(x.work, id+".go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, x.work, "add", "-A")
		git(t, x.work, "commit", "-q", "-m", "land "+id)
		shas[id] = git(t, x.work, "rev-parse", "HEAD")
	}
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	dir, err := st.bareFor(hubRepo)
	if err != nil || dir == "" {
		t.Fatalf("no repository: %q %v", dir, err)
	}
	tip := git(t, x.work, "rev-parse", "cm")
	run := x.h.runner()

	before := run.Spawned.Load()
	for _, id := range ids {
		if at, err := st.addedChangelog(bg, dir, tip, id); err != nil || at != shas[id] {
			t.Fatalf("%s: %q %v", id, at, err)
		}
	}
	if n := run.Spawned.Load() - before; n != 1 {
		t.Fatalf("%d items at one tip ran %d git commands, want 1", len(ids), n)
	}

	pass := func() {
		for _, id := range ids {
			f, err := st.LandFacts(bg, hubRepo, "", "-", id, nil)
			if err != nil || !f.Landed || f.LandedSHA != shas[id] || f.Pushed {
				t.Fatalf("%s: %+v %v", id, f, err)
			}
			if len(f.Paths) != 2 || f.Paths[0] != "changelog/board/2026-10-24-"+id+".md" || f.Paths[1] != id+".go" {
				t.Fatalf("%s paths: %v", id, f.Paths)
			}
		}
	}
	pass()
	before = run.Spawned.Load()
	pass()
	if n := run.Spawned.Load() - before; n != 0 {
		t.Fatalf("a pass where no tip moved ran %d git commands", n)
	}

	x.must(x.push(operator, hubRepo, "cm:refs/heads/main"))
	if f, _ := st.LandFacts(bg, hubRepo, "", "-", "n-3", nil); !f.Pushed || f.PushedSHA != shas["n-3"] {
		t.Fatalf("main moved and was not read again: %+v", f)
	}
}
