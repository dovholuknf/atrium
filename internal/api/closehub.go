package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// hubBranchMerged says whether a branch is an ancestor of the integration branch, as the card's own clone sees it, or
// for a pull request's card of the forge's main, since that work lands there and never on claude/main. The tip is the
// local branch, else the hub's copy of it as last fetched. A branch or a clone that cannot be read is NOT merged, so
// the close asks rather than deletes.
func hubBranchMerged(ctx context.Context, dir, branch string, pr bool) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, closeGitWait)
	defer cancel()
	tips := []string{"refs/heads/" + branch, "refs/remotes/hub/" + branch, "refs/remotes/atrium-hub/" + branch}
	bases := []string{"refs/heads/claude/main", "refs/remotes/hub/claude/main", "refs/remotes/atrium-hub/claude/main",
		"refs/heads/hub-main"}
	if pr {
		bases = append(bases, "refs/remotes/origin/HEAD", "refs/remotes/origin/main", "refs/remotes/origin/master")
	}
	have := commitRefs(ctx, dir, append(slices.Clone(tips), bases...))
	tip := ""
	for _, ref := range tips {
		if have[ref] {
			tip = ref
			break
		}
	}
	if tip == "" {
		return false
	}
	for _, base := range bases {
		if !have[base] {
			continue
		}
		if _, err := gitIn(ctx, dir, "merge-base", "--is-ancestor", tip, base); err == nil {
			return true
		}
	}
	return false
}

// commitRefs says which of the named refs exist and are commits, in one git call. A clone that cannot be read has none.
func commitRefs(ctx context.Context, dir string, names []string) map[string]bool {
	out, err := gitIn(ctx, dir, append([]string{"for-each-ref", "--format=%(refname) %(objecttype)"}, names...)...)
	have := map[string]bool{}
	if err != nil {
		return have
	}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == "commit" && slices.Contains(names, f[0]) {
			have[f[0]] = true
		}
	}
	return have
}

// hubBranchNote is what a preview says of a hub-branch row.
func hubBranchNote(r *store.CardResource) string {
	return "deleted on the hub " + strconv.Quote(r.Detail) + ", which takes it only if this card pushed it"
}
