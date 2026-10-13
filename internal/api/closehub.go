package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE HUB BRANCHES OF A CLOSE: a claude/* branch a card pushed to the hub is an inventory row (store.ResHubBranch), and
// closing the card deletes it on the hub the way its worktree goes. The delete is a push (Server.HubDelete) and the hub
// takes it only for a branch the push log says that card pushed, so a branch another card owns stays and says why.
//
// A delete that fails (the hub is unreachable, the branch is not the card's) is reported and the row stays live. It
// never stops the rest of the close.

// freeHubBranches deletes each live hub-branch row of the card on the hub. dir is where the push runs from, so it is
// called before the card's worktrees are removed.
func (s *Server) freeHubBranches(t *store.Task, rows []*store.CardResource, asked map[int]closeAsk,
	answers map[string]string, steps *closeSteps, failed func(*store.CardResource, error),
	freed func(*store.CardResource), warn func(string, ...any)) {
	var mine []*store.CardResource
	for _, r := range rows {
		if !r.Live() || r.Kind != store.ResHubBranch {
			continue
		}
		// A branch that is not merged was asked about, and keep leaves it on the hub and the card.
		if _, isAsked := asked[r.Seq]; isAsked && answers[strconv.Itoa(r.Seq)] == CloseKeep {
			continue
		}
		mine = append(mine, r)
	}
	if len(mine) == 0 {
		return
	}
	steps.begin("hubbranches", fmt.Sprintf("deleting %d %s on the hub", len(mine), plural(len(mine), "branch", "branches")))
	for _, r := range mine {
		if s.HubDelete == nil {
			err := errors.New("this room has no hub remote to delete from")
			failed(r, err)
			warn("the hub branch %s was not deleted: %v", r.Ref, err)
			steps.problem("%v", err)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*closeGitWait)
		err := s.HubDelete(ctx, t.ID, t.Worktree, r.Detail, r.Ref)
		cancel()
		if err != nil {
			failed(r, err)
			warn("the hub branch %s was not deleted: %v", r.Ref, err)
			steps.problem("hub branch %s: %v", r.Ref, err)
			continue
		}
		freed(r)
	}
}

// hubBranchMerged says whether a branch is an ancestor of the integration branch, as the card's own clone sees it. A
// branch or a clone that cannot be read is NOT merged, so the close asks rather than deletes.
func hubBranchMerged(ctx context.Context, dir, branch string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, closeGitWait)
	defer cancel()
	for _, base := range []string{"refs/heads/claude/main", "refs/remotes/hub/claude/main", "refs/heads/hub-main"} {
		if _, err := gitIn(ctx, dir, "rev-parse", "--verify", "--quiet", base); err != nil {
			continue
		}
		if _, err := gitIn(ctx, dir, "merge-base", "--is-ancestor", "refs/heads/"+branch, base); err == nil {
			return true
		}
	}
	return false
}

// hubBranchNote is what a preview says of a hub-branch row.
func hubBranchNote(r *store.CardResource) string {
	return "deleted on the hub " + strconv.Quote(r.Detail) + ", which takes it only if this card pushed it"
}
