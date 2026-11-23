package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// moveGitPack is A's half of carrying the branch: a git bundle of the card's own commits, from the merge base with
// origin, or the whole branch with full. Only once frozen, so the tree is final.
func (d *Daemon) moveGitPack(w http.ResponseWriter, _ *http.Request, in *moveAsk) {
	t := d.moveCard(w, in)
	if t == nil {
		return
	}
	if f, _ := d.st.Frozen(t.ID); f == nil {
		small.WriteJSONErr(w, http.StatusConflict, store.ErrNotFrozen)
		return
	}
	if _, err := os.Stat(filepath.Join(t.Worktree, ".git")); err != nil {
		hublink.WriteJSONCode(w, http.StatusOK, map[string]any{"ok": true, "skipped": "not a git worktree"})
		return
	}
	branch, err := moveGit(t.Worktree, gitexec.RevParseAbbrevRef{Rev: "HEAD"})
	branch = strings.TrimSpace(branch)
	if err != nil || branch == "" || branch == "HEAD" {
		small.WriteJSONErr(w, http.StatusConflict, errors.New("the card is not on a branch"))
		return
	}
	tip, err := moveGit(t.Worktree, gitexec.RevParse{Rev: "HEAD"})
	if err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	tip = strings.TrimSpace(tip)
	origin, _ := moveGit(t.Worktree, gitexec.RemoteGetURL{Name: "origin"})
	base := ""
	if !in.Full {
		for _, ref := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/main", "refs/remotes/origin/master"} {
			if out, err := moveGit(t.Worktree, gitexec.MergeBase{A: "HEAD", B: ref}); err == nil {
				base = strings.TrimSpace(out)
				break
			}
		}
	}
	ans := map[string]any{"ok": true, "branch": branch, "tip": tip, "base": base, "origin": strings.TrimSpace(origin)}
	if base == tip {
		hublink.WriteJSONCode(w, http.StatusOK, ans)
		return
	}
	dir, err := os.MkdirTemp("", "atrium-gitpack-")
	if err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	defer os.RemoveAll(dir)
	file := filepath.ToSlash(filepath.Join(dir, "card.bundle"))
	revs := []string{branch}
	if base != "" {
		revs = append(revs, "^"+base)
	}
	if _, err := moveGit(t.Worktree, gitexec.BundleRevs{File: file, Revs: revs}); err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	budget := int64(maxMoveBundle)
	data, err := readCapped(file, &budget)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errTooLarge) {
			code = http.StatusRequestEntityTooLarge
		}
		small.WriteJSONErr(w, code, fmt.Errorf("git bundle: %w", err))
		return
	}
	ans["git"] = data
	hublink.WriteJSONCode(w, http.StatusOK, ans)
}

// moveWorktree is B's half: the branch lands in this room's clone of the repo and gets a worktree at this room's
// layout. The answer is the successor's cwd.
func (d *Daemon) moveWorktree(w http.ResponseWriter, r *http.Request, in *moveAsk) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	path, created, branchCreated, status, err := d.ap.MoveWorktree(ctx, in.Repo, strings.TrimSpace(in.Branch), strings.TrimSpace(in.Base), in.Git)
	if err != nil {
		small.WriteJSONErr(w, status, err)
		return
	}
	hublink.WriteJSONCode(w, http.StatusOK, map[string]any{"ok": true, "cwd": path, "created": created, "branch_created": branchCreated})
}

// moveUnworktree removes a worktree moveWorktree made, on an undo. With branch, the branch goes too. Refused while a live card sits in it.
func (d *Daemon) moveUnworktree(w http.ResponseWriter, r *http.Request, in *moveAsk) {
	cwd := strings.TrimSpace(in.Cwd)
	if cwd == "" {
		small.WriteJSONErr(w, http.StatusBadRequest, errors.New("say which worktree"))
		return
	}
	if _, err := os.Stat(cwd); err != nil {
		hublink.WriteJSONCode(w, http.StatusOK, map[string]any{"ok": true, "gone": true})
		return
	}
	if ts, err := d.st.List(); err == nil {
		for _, t := range ts {
			if t.Status != store.StatusDone && t.Status != store.StatusDead && samePath(t.Worktree, cwd) {
				small.WriteJSONErr(w, http.StatusConflict, fmt.Errorf("card %s is still in %s", t.ID, cwd))
				return
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := d.ap.RemoveMoveWorktree(ctx, cwd, strings.TrimSpace(in.Branch)); err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	hublink.WriteJSONCode(w, http.StatusOK, map[string]any{"ok": true})
}
