package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// prSyncCommits bounds the commits an answer lists.
const prSyncCommits = 50

// POST /v1/tasks/{id}/pr-sync: atrium_git_sync for a card a pull request's link opened. The head comes through the
// card's forge, which on a room with a hub is the hub's store, so the room needs no forge login. A refusal is a 409.
func (s *Server) prSync(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), makeWorktreeDeadline)
	defer cancel()
	res, err := s.syncPRCard(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// syncPRCard fetches the card's pull request head and moves the card's branch to it: a fast-forward, or a reset when
// the pull request was force-pushed. It refuses a worktree with changes, and a branch with commits the last fetched
// head does not have, so nothing of the card's own is moved off its branch.
func (s *Server) syncPRCard(ctx context.Context, id string) (gitsync.PRSyncResult, error) {
	res := gitsync.PRSyncResult{Card: id}
	t, err := s.st.Get(id)
	if err != nil {
		return res, fmt.Errorf("no card %s on this room", id)
	}
	k := linkKind{}
	if s.Recognise != nil && strings.TrimSpace(t.URL) != "" {
		if got, err := s.Recognise(t.URL); err == nil {
			k = classify(got)
		}
	}
	if k.kind != linkPR || k.num <= 0 {
		return res, errors.New("this card was not opened from a pull request's link, so there is no pull request " +
			"to fetch. atrium_git_sync with a room syncs claude/main")
	}
	if t.Worktree == "" {
		return res, errors.New("this card has no worktree to bring up to date")
	}
	if s.PRForge == nil {
		return res, errors.New("this room has no forge to ask about pull requests")
	}
	res.PR = fmt.Sprintf("%s/%s/%s#%d", k.host, k.org, k.repo, k.num)
	dir := t.Worktree
	branch, err := gitIn(ctx, dir, gitexec.RevParseAbbrevRef{Rev: "HEAD"})
	if err != nil {
		return res, err
	}
	res.Branch = strings.TrimSpace(branch)
	if res.Branch == "" || res.Branch == "HEAD" {
		return res, errors.New(dir + " is on no branch, so there is no branch to move. switch to the pull request's branch first")
	}
	dirty, err := gitIn(ctx, dir, gitexec.Status{Untracked: "no"})
	if err != nil {
		return res, err
	}
	if d := strings.TrimSpace(dirty); d != "" {
		return res, errors.New("the worktree has local changes, so nothing was fetched or moved. commit or stash " +
			"them first:\n" + tailLines(d))
	}

	ref := forge.Ref{Host: k.host, Org: k.org, Repo: k.repo, Number: k.num}
	f, err := s.PRForge(k.host)
	if err != nil {
		return res, err
	}
	if fr, ok := f.(interface{ Forget(forge.Ref) }); ok {
		fr.Forget(ref)
	}
	if _, err := f.View(ctx, ref); err != nil {
		return res, err
	}
	forgeCmd := ""
	if p := s.providerByHost(k.host); p != nil {
		forgeCmd = p.ForgeCmd
	}
	last := revOf(ctx, dir, prWorktreeRef(k.num))
	if err := s.prHeadFetch(f.Kind(), forgeCmd)(ctx, dir, f.FetchSpec(ref), prWorktreeRef(k.num)); err != nil {
		return res, errors.New("could not fetch the head of pull request " + strconv.Itoa(k.num) + ": " + err.Error())
	}
	res.Old, res.New = revOf(ctx, dir, "HEAD"), revOf(ctx, dir, prWorktreeRef(k.num))
	if res.Old == "" || res.New == "" {
		return res, errors.New("git could not name the branch's commit or the fetched head")
	}
	if res.Old == res.New {
		res.State = "current"
		res.Text = fmt.Sprintf("%s is already at the head of pull request %d (%s). nothing new", res.Branch, k.num, short7(res.New))
		return res, nil
	}
	res.State = "fast-forward"
	if !ancestor(ctx, dir, res.Old, res.New) {
		base := last
		if base == "" {
			base = res.Old
		}
		if !ancestor(ctx, dir, res.Old, base) {
			return res, fmt.Errorf("%s has commits that are not in pull request %d, so it was not moved. push them "+
				"or move them to another branch first. the head is fetched to %s", res.Branch, k.num, prWorktreeRef(k.num))
		}
		res.State = "reset"
		if n, err := gitIn(ctx, dir, gitexec.RevListCount{Revs: []string{res.New + ".." + res.Old}}); err == nil {
			res.Dropped, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	if _, err := gitIn(ctx, dir, gitexec.SwitchReset{Branch: res.Branch, Start: res.New}); err != nil {
		return res, err
	}
	if out, err := gitIn(ctx, dir, gitexec.Log{Revs: []string{res.Old + ".." + res.New}, Format: "%h %s", MaxCount: prSyncCommits}); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				res.Commits = append(res.Commits, l)
			}
		}
	}
	news := fmt.Sprintf("%d new commit(s)", len(res.Commits))
	if len(res.Commits) > 0 {
		news += ":\n" + strings.Join(res.Commits, "\n")
	}
	if res.State == "reset" {
		res.Text = fmt.Sprintf("pull request %d was force-pushed. %s is reset from %s to %s, and %d commit(s) it had are "+
			"not in the new head (git reflog still has them). %s", k.num, res.Branch, short7(res.Old), short7(res.New), res.Dropped, news)
		return res, nil
	}
	res.Text = fmt.Sprintf("%s fast-forwarded from %s to %s. %s", res.Branch, short7(res.Old), short7(res.New), news)
	return res, nil
}

// revOf is the commit rev names in dir, or "".
func revOf(ctx context.Context, dir, rev string) string {
	out, err := gitIn(ctx, dir, gitexec.RevParseVerify{Rev: rev + "^{commit}", Quiet: true})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func ancestor(ctx context.Context, dir, commit, of string) bool {
	_, err := gitIn(ctx, dir, gitexec.IsAncestor{Commit: commit, Of: of})
	return err == nil
}

func short7(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
