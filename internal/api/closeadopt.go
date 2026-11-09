package api

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/store"
)

// A card an agent launched with a worktree its inventory does not list, which is every worker launched before cards kept
// an inventory and every worker whose launcher made the worktree itself, would close listing nothing and removing
// nothing. The worktree it works in is the card's, so a close records it first and then frees it under the same rules as
// any listed one: asked about when it holds work nowhere else has.

// adoptWorktree adds the card's own worktree and its branch to the inventory when they are missing. It adopts only a
// linked worktree of an agent-launched card, never a main checkout, a protected branch or a card the operator started.
func (s *Server) adoptWorktree(ctx context.Context, t *store.Task, rows []*store.CardResource) []*store.CardResource {
	if !slices.Contains(t.Tags, "origin:agent") {
		return rows
	}
	return s.adoptLinked(ctx, t, rows)
}

// adoptLinked is adoptWorktree for any card, for a close asked to remove the card's worktree as well.
func (s *Server) adoptLinked(ctx context.Context, t *store.Task, rows []*store.CardResource) []*store.CardResource {
	wt := strings.TrimSpace(t.Worktree)
	if wt == "" {
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
	gitDir, err := gitIn(gctx, dir, gitexec.RevParseGitDirs{Absolute: true, GitDir: true})
	if err != nil {
		return rows
	}
	common, err := gitIn(gctx, dir, gitexec.RevParseGitDirs{Absolute: true, Common: true})
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
	if b, err := gitIn(gctx, dir, gitexec.SymbolicRef{Name: "HEAD", Short: true}); err == nil {
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

// closeForeign is the card's folder when its inventory does not list it. Atrium did not make it, so a close keeps it
// unless asked to remove it as well. Why says why removing it is not offered.
type closeForeign struct {
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Main     bool   `json:"main,omitempty"`
	Dirty    int    `json:"dirty"`
	Unpushed int    `json:"unpushed"`
	Why      string `json:"why,omitempty"`
}

// foreignWorktree describes the card's folder for a close whose inventory does not list it, or nil when it does.
func (s *Server) foreignWorktree(ctx context.Context, t *store.Task, rows []*store.CardResource) *closeForeign {
	wt := strings.TrimSpace(t.Worktree)
	if wt == "" {
		return nil
	}
	for _, r := range rows {
		if r.Live() && r.Kind == store.ResWorktree && samePathFold(r.Ref, wt) {
			return nil
		}
	}
	f := &closeForeign{Path: wt, Branch: strings.TrimSpace(t.Branch)}
	dir := filepath.FromSlash(wt)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		f.Why = "it is not on disk"
		return f
	}
	gctx, cancel := context.WithTimeout(ctx, closeGitWait)
	defer cancel()
	gitDir, err := gitIn(gctx, dir, gitexec.RevParseGitDirs{Absolute: true, GitDir: true})
	common, err2 := gitIn(gctx, dir, gitexec.RevParseGitDirs{Absolute: true, Common: true})
	if err != nil || err2 != nil {
		f.Why = "it is not a git worktree"
		return f
	}
	gitDir, common = strings.TrimSpace(gitDir), strings.TrimSpace(common)
	f.Repo = filepath.ToSlash(filepath.Dir(filepath.Clean(common)))
	f.Dirty, f.Unpushed, f.Branch = worktreeWork(ctx, wt)
	if samePathFold(gitDir, common) {
		f.Main, f.Why = true, "it is the repository's main checkout"
		return f
	}
	// adoptLinked never takes a protected branch, so the tick names only the folder.
	switch strings.ToLower(f.Branch) {
	case "main", "master", "claude/main", "hub-main":
		f.Branch = ""
	}
	if all, err := s.st.List(); err == nil {
		for _, o := range all {
			if o.ID != t.ID && o.ArchivedAt == nil && samePathFold(o.Worktree, wt) {
				f.Why = "another open card works in it"
				return f
			}
		}
	}
	return f
}
