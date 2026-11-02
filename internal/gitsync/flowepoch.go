package gitsync

import (
	"context"
	"fmt"
	"strings"
)

// The reset of claude/main onto the commits clint re-signed and pushed, without losing what landed in between.
//
// THE HUB DOES THE MOVE, AND ONLY AFTER CHECKING. The pre-receive hook holds every pushed ref to a fast-forward, and
// the reset is the one move that is not, so it is not a push: the merger replays on a branch of its own, and the hub
// moves claude/main to that branch when it has checked that
//
//	- claude/main is still where the plan said (nothing landed since, or the merger plans again),
//	- the branch has main, the re-signed history, under it, and
//	- every commit claude/main has that main lacks has its change on the branch.
//
// The old tip is written to refs/atrium/epochs/<n> first, so no commit is ever unreachable.

// EpochRefPrefix is where each epoch's old claude/main tip is kept in the hub's repository.
const EpochRefPrefix = "refs/atrium/epochs/"

// EpochPlan is what a reset has to carry over.
type EpochPlan struct {
	// OldTip is claude/main now, Base is main (the re-signed history clint pushed).
	OldTip string `json:"old_tip"`
	Base   string `json:"base"`
	// Replay is the commits of claude/main that main lacks by patch, oldest first: the work that landed after clint's
	// fetch. Empty means main already has everything and claude/main can simply become main.
	Replay []string `json:"replay"`
	// Dropped is how many commits of claude/main main has by patch, the re-signed copies.
	Dropped int `json:"dropped"`
}

// EpochPlan reads the plan for the repository `name`. A repository without claude/main or main has none, and that is
// an error: there is nothing to reset.
func (s *Store) EpochPlan(ctx context.Context, name string) (EpochPlan, error) {
	var p EpochPlan
	dir, err := s.bareFor(name)
	if err != nil {
		return p, err
	}
	if dir == "" {
		return p, refuse("the hub holds no repository %s", name)
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	return s.epochPlan(ctx, dir)
}

func (s *Store) epochPlan(ctx context.Context, dir string) (EpochPlan, error) {
	var p EpochPlan
	for ref, into := range map[string]*string{ClaudeMainRef: &p.OldTip, MainRef: &p.Base} {
		out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ref+"^{commit}")
		if err != nil {
			return p, refuse("the repository has no %s, so there is nothing to reset", trimHead(ref))
		}
		*into = strings.TrimSpace(out)
	}
	out, err := s.git(ctx, dir, "cherry", p.Base, p.OldTip)
	if err != nil {
		return p, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && f[0] == "+":
			p.Replay = append(p.Replay, f[1])
		case len(f) == 2 && f[0] == "-":
			p.Dropped++
		}
	}
	return p, nil
}

// EpochMove moves claude/main of the repository `name` to the tip of `branch`, as epoch n, when the checks above
// hold. An empty branch means main itself, which is only allowed when the plan has nothing to replay. It answers the
// tip claude/main was and is. expectOld is the claude/main the plan was made against.
func (s *Store) EpochMove(ctx context.Context, name string, n int64, branch, expectOld string) (oldTip, newTip string, err error) {
	ref, err := ParseName(name)
	if err != nil {
		return "", "", err
	}
	dir, err := s.bareFor(name)
	if err != nil {
		return "", "", err
	}
	if dir == "" {
		return "", "", refuse("the hub holds no repository %s", name)
	}
	if branch != "" {
		if why := CheckPushBranch(branch); why != "" {
			return "", "", refuse("%s", why)
		}
	}
	// THE REPOSITORY'S LOCK, the one every push holds, so no push lands between the checks and the move.
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()

	plan, err := s.epochPlan(ctx, dir)
	if err != nil {
		return "", "", err
	}
	if expectOld != "" && expectOld != plan.OldTip {
		return "", "", refuse("claude/main moved since the plan (it was %s and is %s). plan again", short(expectOld), short(plan.OldTip))
	}
	next := plan.Base
	if branch != "" {
		tip, ok, err := s.tipOf(ctx, dir, branch)
		if err != nil {
			return "", "", err
		}
		if !ok {
			return "", "", refuse("the hub has no branch %s to move claude/main to", branch)
		}
		next = tip
	} else if len(plan.Replay) > 0 {
		return "", "", refuse("%d commits of claude/main are not in main, so replay them on a branch and give it", len(plan.Replay))
	}
	if next != plan.Base {
		if ok, err := s.isAncestor(ctx, dir, plan.Base, next); err != nil || !ok {
			if err != nil {
				return "", "", err
			}
			return "", "", refuse("%s does not have main under it, so it is not on the re-signed history", branch)
		}
	}
	out, err := s.git(ctx, dir, "cherry", next, plan.OldTip)
	if err != nil {
		return "", "", err
	}
	lost := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+") {
			lost++
		}
	}
	if lost > 0 {
		return "", "", refuse("%d commits of claude/main are not on %s, and the move would lose them", lost, nameOr(branch, "main"))
	}
	if _, err := s.git(ctx, dir, "update-ref", "-m", fmt.Sprintf("epoch %d", n), fmt.Sprintf("%s%d", EpochRefPrefix, n), plan.OldTip); err != nil {
		return "", "", err
	}
	if _, err := s.git(ctx, dir, "update-ref", "-m", fmt.Sprintf("epoch %d", n), ClaudeMainRef, next, plan.OldTip); err != nil {
		return "", "", err
	}
	return plan.OldTip, next, nil
}

func nameOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
