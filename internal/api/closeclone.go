package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE CLONE A CARD MADE: an open that found no checkout of the repo cloned one into the scm folder (repoCheckout), and
// the card that got the worktree owns that clone too (store.ResClone). Closing the card deletes it once its own
// worktree and branches are gone, so a pasted link leaves nothing behind (u-005, decision 4).
//
// A CLONE IS SHARED BY WHATEVER CAME AFTER IT. Another card's worktree may hang off it, and somebody may have worked in
// it. So it goes only when atrium made it, nothing else hangs off it, and it holds nothing that is not on a remote: no
// changed files, no stash, no commit on a local branch that no remote has. Otherwise it is kept, the reason is said,
// and the row stays live for the sweep to list.

// cloneKeepReason is why a clone must stay, or "" when it may go. mineWT are the card's own worktrees of it and
// mineBranch its own branches, which the close removes first and so do not count.
func cloneKeepReason(ctx context.Context, dir string, mineWT, mineBranch []string) string {
	ctx, cancel := context.WithTimeout(ctx, closeGitWait)
	defer cancel()
	if out, err := gitIn(ctx, dir, "config", "--local", "atrium.clone"); err != nil || strings.TrimSpace(out) != "made" {
		return "atrium did not make it, so it is not atrium's to delete"
	}
	if top, err := gitIn(ctx, dir, "rev-parse", "--show-toplevel"); err != nil ||
		!sameRealDir(filepath.FromSlash(strings.TrimSpace(top)), filepath.FromSlash(dir)) {
		return "git does not answer for it as a clone"
	}
	others := 0
	for _, wt := range worktreesOf(dir) {
		if _, err := os.Stat(filepath.FromSlash(wt.Path)); err != nil {
			continue
		}
		mine := false
		for _, m := range mineWT {
			if sameRealDir(filepath.FromSlash(wt.Path), filepath.FromSlash(m)) {
				mine = true
			}
		}
		if !mine {
			others++
		}
	}
	if others > 0 {
		return fmt.Sprintf("%d other %s off it", others, plural(others, "worktree hangs", "worktrees hang"))
	}
	if out, err := gitIn(ctx, dir, "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		return "it has changed files"
	}
	if _, err := gitIn(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/stash"); err == nil {
		return "it has a stash"
	}
	args := []string{"rev-list", "--count"}
	for _, b := range mineBranch {
		args = append(args, "--exclude=refs/heads/"+b)
	}
	args = append(args, "--branches", "--not", "--remotes", "--glob=refs/atrium/*")
	out, err := gitIn(ctx, dir, args...)
	if err != nil {
		return "git could not say what in it is on no remote"
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(out)); n > 0 {
		return fmt.Sprintf("%d %s in it on no remote", n, plural(n, "commit is", "commits are"))
	}
	return ""
}

// sameRealDir is eqPath after resolving links, because git reports the real path: /private/var for a /var folder on a Mac,
// and the long name for an 8.3 short one on Windows.
func sameRealDir(a, b string) bool {
	if eqPath(a, b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && eqPath(ra, rb)
}

// cardOwnOf is the card's live worktree paths and branch names in the repository at dir.
func cardOwnOf(rows []*store.CardResource, dir string) (wts, branches []string) {
	for _, r := range rows {
		if !r.Live() || !eqPath(filepath.FromSlash(r.Detail), filepath.FromSlash(dir)) {
			continue
		}
		switch r.Kind {
		case store.ResWorktree:
			wts = append(wts, r.Ref)
		case store.ResBranch:
			branches = append(branches, r.Ref)
		}
	}
	return wts, branches
}

// freeClones deletes each clone the card made, after its worktrees and branches are gone. One that must stay is said
// and left live. It never stops the rest of the close.
func (s *Server) freeClones(clones []*store.CardResource, steps *closeSteps, failed func(*store.CardResource, error),
	freed func(*store.CardResource), warn func(string, ...any)) {
	for _, r := range clones {
		dir := filepath.FromSlash(r.Ref)
		if _, err := os.Stat(dir); err != nil {
			freed(r)
			continue
		}
		key := "clone:" + strconv.Itoa(r.Seq)
		after, _ := s.st.Resources(r.Card)
		wts, branches := cardOwnOf(after, r.Ref)
		if why := cloneKeepReason(context.Background(), r.Ref, wts, branches); why != "" || len(wts) > 0 {
			if why == "" {
				why = "the card's own worktree of it was kept"
			}
			steps.begin(key, "keeping the clone "+r.Ref)
			warn("the clone %s was kept: %s", r.Ref, why)
			continue
		}
		n := dirSize(context.Background(), dir)
		if n > 0 {
			_ = s.st.SetResourceBytes(r.Card, r.Seq, n)
		}
		steps.begin(key, "deleting the clone "+r.Ref+sizeNote(n))
		if err := removeAllSoon(dir); err != nil {
			failed(r, err)
			warn("the clone %s did not delete: %v", r.Ref, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
	}
}
