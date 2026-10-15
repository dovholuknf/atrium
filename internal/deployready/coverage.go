package deployready

import (
	"context"
	"fmt"
	"strings"
)

// WHAT THE VERDICTS SAY OF ONE RANGE (item lifecycle L4). `Check` answers for the integration branch against an installed
// build. The item lifecycle's `reviewed` step asks the same question of an item's own commits, and it must be the SAME
// answer: a verdict trailer is trusted under the same rules (a verdict commit touches review files alone), matched to a
// commit by the same patch id, and a hold wins the same way. Nothing here is a second matcher. See
// docs/rnd/item-lifecycle-design.md section 3, `reviewed`.

// Coverage is how much of a range the verdicts cover.
type Coverage struct {
	Base string `json:"base"`
	Tip  string `json:"tip"`
	// Total is the commits a reviewer reads: every commit of the range that carries a change, less the verdict commits
	// themselves, which are the review and not the work.
	Total int `json:"total"`
	// Covered is how many of them have a verdict that clears them: hub-ok, and room-ok too where the commit also runs in the
	// room. A hold clears nothing.
	Covered int `json:"covered"`
	// Held are the commits a verdict holds.
	Held []string `json:"held,omitempty"`
	// Uncovered are the commits no verdict clears, held ones included, oldest first.
	Uncovered []string `json:"uncovered,omitempty"`
	// TipCovered is that the newest commit of the range is cleared, which is what a verdict "at the tip" means.
	TipCovered bool `json:"tip_covered"`
	// Notes are verdicts that were ignored, and why.
	Notes []string `json:"notes,omitempty"`
}

// Line is the sentence for a coverage: "covers 1 of 2 commits".
func (c Coverage) Line() string {
	switch {
	case c.Total == 0:
		return "no commits to review"
	case len(c.Held) > 0:
		return fmt.Sprintf("held on %d of %d commit%s", len(c.Held), c.Total, plural(c.Total))
	}
	return fmt.Sprintf("covers %d of %d commit%s", c.Covered, c.Total, plural(c.Total))
}

// ErrNotBehind is `from` not being an ancestor of `to`: the branch was rebased or reset, so there is no count of commits since.
var ErrNotBehind = fmt.Errorf("that commit is not an ancestor of the tip")

// Since counts the commits after `from` up to `to` that are the work: the verdict commits are left out, because a review
// landing on the branch after a test is not a change to what was tested. ErrNotBehind says `from` is no longer on the
// branch's line, which the caller shows as stale with no count.
func (c *Checker) Since(ctx context.Context, from, to string) (int, error) {
	if from == to {
		return 0, nil
	}
	if _, err := c.Git.Git(ctx, c.Dir, "merge-base", "--is-ancestor", from, to); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, ErrNotBehind
	}
	cands, err := c.candidates(ctx, from, to)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range cands {
		if !allReview(k.paths) {
			n++
		}
	}
	return n, nil
}

// Coverage reads base..tip and says which commits a verdict clears. Verdicts are read from the Checker's integration branch
// and from alsoOn, which are refs of the same repository (an item's own branch, as `refs/heads/<name>`).
func (c *Checker) Coverage(ctx context.Context, base, tip string, alsoOn ...string) (Coverage, error) {
	cov := Coverage{Base: base, Tip: tip}
	cands, err := c.candidates(ctx, base, tip)
	if err != nil {
		return cov, err
	}
	refs := []string{"refs/heads/" + c.Branch}
	for _, r := range alsoOn {
		if r = strings.TrimSpace(r); r != "" && r != refs[0] {
			refs = append(refs, r)
		}
	}
	verdicts, notes, _, err := c.verdictsOn(ctx, refs...)
	if err != nil {
		return cov, err
	}
	cov.Notes = notes
	var read []candidate
	for _, k := range cands {
		if !allReview(k.paths) {
			read = append(read, k)
		}
	}
	cov.Total = len(read)
	for i, k := range read {
		d := verdicts[k.patch]
		if k.patch == "" {
			d = dims{}
		}
		cleared := d.hub == "ok" && (!anyRoomSide(k.paths) || d.room == "ok")
		switch {
		case d.hub == "hold" || (anyRoomSide(k.paths) && d.room == "hold"):
			cov.Held = append(cov.Held, k.sha)
			cov.Uncovered = append(cov.Uncovered, k.sha)
		case cleared:
			cov.Covered++
		default:
			cov.Uncovered = append(cov.Uncovered, k.sha)
		}
		if i == len(read)-1 {
			cov.TipCovered = cleared
		}
	}
	return cov, nil
}
