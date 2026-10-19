package api

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card an agent launched with a worktree its inventory does not list, which is every worker launched before cards kept
// an inventory and every worker whose launcher made the worktree itself, would close listing nothing and removing
// nothing. The worktree it works in is the card's, so a close records it first and then frees it under the same rules as
// any listed one: asked about when it holds work nowhere else has.

// adoptWorktree adds the card's own worktree and its branch to the inventory when they are missing. It adopts only a
// linked worktree of an agent-launched card, never a main checkout, a protected branch or a card the operator started.
func (s *Server) adoptWorktree(ctx context.Context, t *store.Task, rows []*store.CardResource) []*store.CardResource {
	wt := strings.TrimSpace(t.Worktree)
	if wt == "" || !slices.Contains(t.Tags, "origin:agent") {
		return rows
	}
	for _, r := range rows {
		if r.Live() && r.Kind == store.ResWorktree && samePathFold(r.Ref, wt) {
			return rows
		}
	}
	if all, err := s.st.List(); err == nil {
		for _, o := range all {
			if o.ID != t.ID && o.ArchivedAt == nil && samePathFold(o.Worktree, wt) {
				return rows
			}
		}
	}
	dir := filepath.FromSlash(wt)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return rows
	}
	gctx, cancel := context.WithTimeout(ctx, closeGitWait)
	defer cancel()
	gitDir, err := gitIn(gctx, dir, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return rows
	}
	common, err := gitIn(gctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return rows
	}
	gitDir, common = strings.TrimSpace(gitDir), strings.TrimSpace(common)
	if samePathFold(gitDir, common) {
		return rows
	}
	repo := filepath.ToSlash(filepath.Dir(filepath.Clean(common)))
	if _, err := s.st.AddResource(t.ID, store.ResWorktree, wt, repo); err != nil {
		return rows
	}
	if b, err := gitIn(gctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		b = strings.TrimSpace(b)
		switch strings.ToLower(b) {
		case "", "main", "master", "claude/main", "hub-main":
		default:
			live := false
			for _, r := range rows {
				live = live || (r.Live() && r.Kind == store.ResBranch && r.Ref == b && samePathFold(r.Detail, repo))
			}
			if !live {
				_, _ = s.st.AddResource(t.ID, store.ResBranch, b, repo)
			}
		}
	}
	if fresh, err := s.st.Resources(t.ID); err == nil {
		return fresh
	}
	return rows
}
