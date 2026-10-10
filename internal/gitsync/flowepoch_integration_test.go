//go:build integration

package gitsync

import (
	"strings"
	"testing"
)

// NO MERGE IS EVER LOST: work the merger landed after clint fetched is replayed onto the re-signed history, and the
// hub moves claude/main only when everything is on the branch it is moved to.
func TestEpochMoveKeepsWhatLandedAfterClintFetched(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	card := roomCard("sg4", "C1")
	st := x.h.Store()

	// claude/main: main, then A (clint fetches here), then B (lands while clint re-signs).
	git(t, x.work, "switch", "-q", "-C", "cm", "main")
	a := commit(t, x.work, "a.txt", "a")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	b := commit(t, x.work, "b.txt", "b")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	// clint re-signs A, which is the same change under a new sha, and pushes it to main.
	git(t, x.work, "switch", "-q", "-C", "resigned", "main")
	git(t, x.work, "cherry-pick", a)
	aResigned := git(t, x.work, "rev-parse", "HEAD")
	x.must(x.push(operator, hubRepo, "resigned:refs/heads/main"))

	plan, err := st.EpochPlan(bg, hubRepo)
	if err != nil || plan.OldTip != b || plan.Base != aResigned || len(plan.Replay) != 1 || plan.Replay[0] != b || plan.Dropped != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	// With work to replay, main alone is refused.
	if _, _, err := st.EpochMove(bg, hubRepo, 1, "", ""); err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("a move to main with work to replay: %v", err)
	}
	// A branch that lacks B would lose it.
	git(t, x.work, "switch", "-q", "-C", "bad", "resigned")
	commit(t, x.work, "c.txt", "c")
	x.must(x.push(card, hubRepo, "bad:refs/heads/bad"))
	if _, _, err := st.EpochMove(bg, hubRepo, 1, "bad", b); err == nil || !strings.Contains(err.Error(), "lose") {
		t.Fatalf("a branch that lacks the work: %v", err)
	}
	// A branch that is not on the re-signed history is refused.
	if _, _, err := st.EpochMove(bg, hubRepo, 1, "fix/none", b); err == nil {
		t.Fatal("a branch that is not there")
	}
	// The merger replays B on the re-signed history.
	git(t, x.work, "switch", "-q", "-C", "next", "resigned")
	git(t, x.work, "cherry-pick", b)
	next := git(t, x.work, "rev-parse", "HEAD")
	x.must(x.push(card, hubRepo, "next:refs/heads/next"))
	// Something landed since the plan: the move is refused and the merger plans again.
	if _, _, err := st.EpochMove(bg, hubRepo, 1, "next", a); err == nil || !strings.Contains(err.Error(), "plan again") {
		t.Fatalf("a stale plan: %v", err)
	}
	old, got, err := st.EpochMove(bg, hubRepo, 1, "next", b)
	if err != nil || old != b || got != next {
		t.Fatalf("move: %s %s %v", old, got, err)
	}
	if now := x.refOn(hubRepo, ClaudeMainRef); now != next {
		t.Fatalf("claude/main is %s, want %s", now, next)
	}
	if kept := x.refOn(hubRepo, EpochRefPrefix+"1"); kept != b {
		t.Fatalf("the epoch ref is %q, want the old tip %s", kept, b)
	}
	// claude/main is now the re-signed history plus B, which clint has not pushed yet and which is carried over again
	// at the next round. A is not: it is under main.
	if plan, _ = st.EpochPlan(bg, hubRepo); plan.OldTip != next || len(plan.Replay) != 1 || plan.Replay[0] != next {
		t.Fatalf("after the move: %+v", plan)
	}
}

func TestEpochMoveToMainWhenEverythingIsAlreadyThere(t *testing.T) {
	x := newRecv(t)
	x.seedMain(hubRepo)
	st := x.h.Store()
	git(t, x.work, "switch", "-q", "-C", "cm", "main")
	a := commit(t, x.work, "a.txt", "a")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	git(t, x.work, "switch", "-q", "-C", "resigned", "main")
	git(t, x.work, "cherry-pick", a)
	re := git(t, x.work, "rev-parse", "HEAD")
	x.must(x.push(operator, hubRepo, "resigned:refs/heads/main"))
	old, now, err := st.EpochMove(bg, hubRepo, 1, "", "")
	if err != nil || old != a || now != re {
		t.Fatalf("move: %s %s %v", old, now, err)
	}
	if got := x.refOn(hubRepo, ClaudeMainRef); got != re {
		t.Fatalf("claude/main is %s", got)
	}
}
