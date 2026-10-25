package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// CLOSING A CARD: the operator's word that the work on a link is over, which frees what the card's inventory lists.
// Design: docs/rnd/card-lifecycle-design.md section 7 and Interview Q4. Item r-finish-pr-review.
//
//	GET  /v1/tasks/{id}/close                          what a close would free, keep and ask
//	POST /v1/tasks/{id}/close {confirm, answers}       the same without confirm, else the close
//
// "close", never "finish": `atrium finish` is an agent reporting its own work over, and one word does not mean two
// things.
//
// IT NEVER REFUSES AND NEVER DECIDES ALONE. A worktree with uncommitted changes, or commits that are on no remote and
// not on the hub, is asked about, and the close waits for an answer per worktree, keyed by the row's seq:
//   - keep: the worktree and its branch stay on disk and on the card. The rest is freed.
//   - stash: the work is pushed to the hub as stash/<card short id>/<branch> (Server.Stash), then removed. A stash
//     that does not land keeps the worktree and says why.
//   - delete: removed as it is.
//
// A clean, pushed worktree is not asked about. Open findings and a running review are warnings, not blocks: the
// review is stopped and its findings stay in its history.
//
// THE ORDER is the runner, then its processes (newest first, so a tunneler goes before its controller), its ports and
// its overlays' folders, then the review, then worktrees, then their branches, then refs. A branch cannot be
// deleted while a worktree has it checked out, so worktrees go before branches. A row that does not free is left live
// with what git said, and the rest carry on. The card then goes to done, with an event saying what went.
//
// What is kept: the card and its transcript, the inventory rows with freed_at, and the review's run folder minus
// src/, renamed aside so a later review of the same head gets the head's own folder (Q3).

// Close answers to an asked worktree.
const (
	CloseKeep   = "keep"
	CloseStash  = "stash"
	CloseDelete = "delete"
)

// closedBy is the `by` of the notified event a close writes on its card. The sweep reads it (sweep.go).
const closedBy = "card-closed"

// ClosedTag is on a card the operator closed. The terminals tab does not list a card that carries it and has no
// session, even with show stopped on: a close freed its folder, so there is nothing to resume.
const ClosedTag = "atrium:closed"

// closeGitWait bounds one git command of a close.
const closeGitWait = time.Minute

// closeItem is one inventory row and what a close does with it.
type closeItem struct {
	Seq    int    `json:"seq"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Detail string `json:"detail,omitempty"`
	// Action is free, ask, keep or freed (gone already).
	Action string `json:"action"`
	Note   string `json:"note,omitempty"`
	Bytes  int64  `json:"bytes"`
	// Err is what freeing it said when it did not go.
	Err string `json:"error,omitempty"`
}

// closeAsk is a worktree that holds work nowhere else has.
type closeAsk struct {
	Seq      int    `json:"seq"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Dirty    int    `json:"dirty"`
	Unpushed int    `json:"unpushed"`
	// Bytes is what the worktree holds on disk now, ignored trees (node_modules) included.
	Bytes int64 `json:"bytes"`
	// Hub is a branch on the hub that is not merged, asked about with keep or delete. Path is the repository and Branch
	// the branch.
	Hub bool `json:"hub,omitempty"`
}

type closePreview struct {
	Card      string      `json:"card"`
	Items     []closeItem `json:"items"`
	Asks      []closeAsk  `json:"asks"`
	Warnings  []string    `json:"warnings"`
	Kept      []string    `json:"kept"`
	DiskBytes int64       `json:"disk_bytes"`
	CanStash  bool        `json:"can_stash"`
	// keepAll ends the card and frees nothing, for a close asked to leave the folder and branches alone.
	keepAll bool
}

type closeRequest struct {
	Confirm bool `json:"confirm"`
	// Keep ends the card and frees nothing: the folder, the branch and any hub branch stay.
	Keep bool `json:"keep,omitempty"`
	// Answers is keep, stash or delete by the worktree row's seq.
	Answers map[string]string `json:"answers"`
	// Progress is the id the board sends so each step of the close is broadcast as a close-progress event with it.
	Progress string `json:"progress,omitempty"`
}

type closeStash struct {
	Branch string `json:"branch"`
	Repo   string `json:"repo,omitempty"`
	// Expires is the day the hub removes the stash (2006-01-02), empty when the hub keeps stashes for ever.
	Expires string `json:"expires,omitempty"`
}

// stashExpiry is the date a stash made now is removed on, or "" for never. See internal/gitsync/stashexpiry.go.
func (s *Server) stashExpiry(now time.Time) string {
	days := 30
	if s.StashRetentionDays != nil {
		days = s.StashRetentionDays()
	}
	if days <= 0 {
		return ""
	}
	return now.AddDate(0, 0, days).UTC().Format("2006-01-02")
}

type closeResult struct {
	Card    string       `json:"card"`
	Closed  bool         `json:"closed"`
	Items   []closeItem  `json:"items"`
	Stashes []closeStash `json:"stashes"`
	// Left counts the rows still live: kept on purpose or not freed.
	Left     int      `json:"left"`
	Warnings []string `json:"warnings"`
}

// closing holds the cards a close is running on, so a second press does not race the first.
var closing sync.Map

// GET /v1/tasks/{id}/close
func (s *Server) getClose(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	pv, err := s.closePreview(r.Context(), t)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// POST /v1/tasks/{id}/close
func (s *Server) postClose(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in closeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	s.closeFlow(w, r, t, false, in)
}

// closeFlow is a close after the card is found: the preview without confirm, else the asks checked and the close.
// orphan is an owner with no card (a forgotten card, an open that died half way), which has only its inventory.
func (s *Server) closeFlow(w http.ResponseWriter, r *http.Request, t *store.Task, orphan bool, in closeRequest) {
	for k, v := range in.Answers {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case CloseKeep, CloseStash, CloseDelete:
			in.Answers[k] = strings.ToLower(strings.TrimSpace(v))
		default:
			prError(w, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("worktree %s: %q is not keep, stash or delete", k, v), nil)
			return
		}
	}
	if _, busy := closing.LoadOrStore(t.ID, true); busy {
		prError(w, http.StatusConflict, "closing", "that card is being closed already", nil)
		return
	}
	defer closing.Delete(t.ID)

	pv, err := s.closePreview(r.Context(), t)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !in.Confirm {
		writeJSON(w, http.StatusOK, pv)
		return
	}
	var missing []string
	for _, a := range pv.Asks {
		if in.Keep {
			break
		}
		ans := in.Answers[strconv.Itoa(a.Seq)]
		if ans == "" {
			missing = append(missing, a.Path)
		}
		if ans == CloseStash && a.Hub {
			prError(w, http.StatusBadRequest, "bad_request", "a branch on the hub is kept or deleted, not stashed: "+a.Path, nil)
			return
		}
		if ans == CloseStash && !pv.CanStash {
			prError(w, http.StatusBadRequest, "no_stash", "this room cannot stash to a hub. keep or delete "+a.Path, nil)
			return
		}
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "needs_answer",
			"error":   "say keep, stash or delete for " + strings.Join(missing, ", "),
			"preview": pv})
		return
	}
	pv.keepAll = in.Keep
	steps := s.newCloseSteps(strings.TrimSpace(in.Progress), t.ID)
	writeJSON(w, http.StatusOK, s.closeCard(t, pv, in.Answers, orphan, steps))
}

// cardRunning is whether a card's session may still be going.
func cardRunning(t *store.Task) bool {
	switch t.Status {
	case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
		return true
	}
	return false
}

// closePreview reads the inventory and the worktrees it names. Nothing is changed.
func (s *Server) closePreview(ctx context.Context, t *store.Task) (*closePreview, error) {
	rows, err := s.st.Resources(t.ID)
	if err != nil {
		return nil, err
	}
	rows = s.adoptWorktree(ctx, t, rows)
	pv := &closePreview{Card: t.ID, Items: []closeItem{}, Asks: []closeAsk{}, Warnings: []string{},
		DiskBytes: diskOf(rows), CanStash: s.Stash != nil,
		Kept: []string{"the card and its transcript", "this list, with what freeing each row said"}}
	if cardRunning(t) {
		pv.Warnings = append(pv.Warnings, "the session is still running. it is stopped first")
	}
	// A pull request's card: its hub branches count as merged once the forge's main has them (closehub.go).
	prCard := len(s.walkedOutside(t.ID, nil)) > 0
	for _, r := range rows {
		it := closeItem{Seq: r.Seq, Kind: r.Kind, Ref: r.Ref, Detail: r.Detail, Action: "free", Bytes: r.Bytes,
			Err: r.FreedErr}
		if !r.Live() {
			it.Action = "freed"
			pv.Items = append(pv.Items, it)
			continue
		}
		switch r.Kind {
		case store.ResWorktree:
			if _, err := os.Stat(filepath.FromSlash(r.Ref)); err != nil {
				it.Note = "already gone from disk"
				break
			}
			// Measured now, ignored trees included: a stored size can predate the install that made 0.4 GB of
			// node_modules, and the dialog must not say a small number for a big delete.
			mctx, mcancel := context.WithTimeout(ctx, measureWait)
			if n := dirSize(mctx, filepath.FromSlash(r.Ref)); n > 0 {
				it.Bytes = n
				pv.DiskBytes += n - max(r.Bytes, 0)
			}
			mcancel()
			dirty, unpushed, branch := worktreeWork(ctx, r.Ref)
			if dirty > 0 || unpushed > 0 {
				it.Action = "ask"
				pv.Asks = append(pv.Asks, closeAsk{Seq: r.Seq, Path: r.Ref, Branch: branch, Dirty: dirty,
					Unpushed: unpushed, Bytes: it.Bytes})
			}
		case store.ResBranch:
			it.Note = "deleted with its worktree, kept when the worktree is kept"
		case store.ResReview:
			it.Note = "archived. its findings, walk and bundle stay, the source copy goes"
			if p, err := s.st.PRByID(r.Ref); err == nil {
				pv.reviewNotes(p)
			}
		case store.ResStash:
			it.Action, it.Note = "keep", "on the hub. nothing here frees it"
		case store.ResClone:
			if _, err := os.Stat(filepath.FromSlash(r.Ref)); err != nil {
				it.Note = "already gone from disk"
				break
			}
			wts, branches := cardOwnOf(rows, r.Ref)
			if why := cloneKeepReason(ctx, r.Ref, wts, branches); why != "" {
				it.Action, it.Note = "keep", "kept: "+why
				pv.Kept = append(pv.Kept, "the clone it made, "+r.Ref+": "+why)
				break
			}
			it.Note = "the clone this card's open made, deleted after its worktree. kept if its worktree is"
		case store.ResHubBranch:
			it.Note = hubBranchNote(r)
			if !hubBranchMerged(ctx, t.Worktree, r.Ref, prCard) {
				it.Action, it.Note = "ask", "not merged into claude/main, so it is asked about: keep or delete"
				if prCard {
					it.Note = "not merged into claude/main or the forge's main, so it is asked about: keep or delete"
				}
				pv.Asks = append(pv.Asks, closeAsk{Seq: r.Seq, Path: r.Detail + " " + r.Ref, Branch: r.Ref, Hub: true})
			}
		case store.ResDir:
			if s.inScratch(r.Ref) == "" {
				it.Action, it.Note = "keep", "a folder outside the scratch folder is not freed by a close. the sweep lists it"
				break
			}
			it.Note = "the card's scratch folder, deleted with what is in it"
		case store.ResProc:
			if !procStillIt(r) {
				it.Note = "already exited"
				break
			}
			it.Note = "stopped, with every process it started"
		case store.ResPort:
			it.Note = "released"
		case store.ResOverlay:
			if s.inCards(r.Ref) == "" {
				it.Action, it.Note = "keep", "outside the cards folder, so a close does not delete it. the sweep lists it"
				break
			}
			it.Note = "its tunnelers and controller stopped, then its folder deleted, PKI and identities with it"
		case store.ResIdentity:
			it.Note = "deleted with its overlay's folder"
		}
		pv.Items = append(pv.Items, it)
	}
	// A review the card walks that its inventory does not name (one started before cards kept an inventory) is
	// archived with it too, as seq 0.
	for _, p := range s.walkedOutside(t.ID, rows) {
		pv.Items = append(pv.Items, closeItem{Kind: store.ResReview, Ref: p.ID, Action: "free", Bytes: -1,
			Note: "the review it walks. archived, its findings, walk and bundle stay"})
		pv.reviewNotes(p)
	}
	return pv, nil
}

// reviewNotes adds what closing a review warns about and keeps.
func (pv *closePreview) reviewNotes(p *store.PRReview) {
	switch p.State {
	case store.PRQueued, store.PRFetching, store.PRRunning:
		pv.Warnings = append(pv.Warnings, "the review is still running. it is stopped")
	}
	if _, wc := readPRCounts(p.RunDir); wc.Open+wc.Accepted > 0 {
		pv.Warnings = append(pv.Warnings, fmt.Sprintf(
			"%d findings are open or accepted and not posted. they stay in the review's history", wc.Open+wc.Accepted))
	}
	pv.Kept = append(pv.Kept, "the review's findings and walk, in "+closedRunDir(p.RunDir, "<when>"))
}

// walkedOutside is the reviews a card walks that are not archived and not in its inventory.
func (s *Server) walkedOutside(card string, rows []*store.CardResource) []*store.PRReview {
	walked, err := s.st.PRsWalkedBy(card)
	if err != nil {
		return nil
	}
	listed := map[string]bool{}
	for _, r := range rows {
		if r.Kind == store.ResReview {
			listed[r.Ref] = true
		}
	}
	var out []*store.PRReview
	for _, p := range walked {
		if !listed[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// worktreeWork is how much of a worktree is held nowhere else: files git status lists, and commits on HEAD that no
// remote-tracking branch and no fetched PR ref has. branch is the checked out branch, "" when detached.
func worktreeWork(ctx context.Context, dir string) (dirty, unpushed int, branch string) {
	ctx, cancel := context.WithTimeout(ctx, closeGitWait)
	defer cancel()
	if out, err := gitIn(ctx, dir, "status", "--porcelain"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if strings.TrimSpace(l) != "" {
				dirty++
			}
		}
	}
	if out, err := gitIn(ctx, dir, "rev-list", "--count", "HEAD", "--not", "--remotes",
		"--glob=refs/atrium/*"); err == nil {
		unpushed, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	if out, err := gitIn(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		branch = strings.TrimSpace(out)
	}
	return dirty, unpushed, branch
}

// gitIn runs git in dir and answers its output, and its last lines as the error when it fails.
func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = filepath.FromSlash(dir)
	nowindow.Hide(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(tailLines(string(out)))
		if msg == "" {
			msg = err.Error()
		}
		return string(out), errors.New("git " + args[0] + ": " + msg)
	}
	return string(out), nil
}

// closeCard frees the inventory in the close's order and moves the card to done. An orphan has no card to move.
func (s *Server) closeCard(t *store.Task, pv *closePreview, answers map[string]string, orphan bool,
	steps *closeSteps) *closeResult {
	res := &closeResult{Card: t.ID, Stashes: []closeStash{}, Warnings: []string{}}
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }

	// 1. the runner
	if cardRunning(t) && s.Kill != nil {
		steps.begin("session", "stopping the session")
		if err := s.Kill(t.ID); err != nil {
			warn("the session did not stop: %v. a worktree it holds open may not remove", err)
			steps.problem("%v", err)
		}
	}

	rows, err := s.st.Resources(t.ID)
	if err != nil {
		warn("the inventory did not read: %v", err)
	}
	failed := func(r *store.CardResource, err error) {
		log.Printf("[atrium api] close %s: %s %s: %v", t.ID, r.Kind, r.Ref, err)
		if e := s.st.ResourceFailed(r.Card, r.Seq, err.Error()); e != nil {
			log.Printf("[atrium api] close %s: %v", t.ID, e)
		}
	}
	freed := func(r *store.CardResource) {
		if e := s.st.FreeResource(r.Card, r.Seq, ""); e != nil {
			log.Printf("[atrium api] close %s: %v", t.ID, e)
		}
	}
	of := func(kind string) []*store.CardResource {
		var out []*store.CardResource
		if pv.keepAll {
			return out
		}
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].Live() && rows[i].Kind == kind {
				out = append(out, rows[i])
			}
		}
		return out
	}
	asked := map[int]closeAsk{}
	for _, a := range pv.Asks {
		asked[a.Seq] = a
	}

	// 1b. its processes, so nothing holds the worktree, then its ports
	if n := len(of(store.ResProc)); n > 0 {
		steps.begin("processes", fmt.Sprintf("stopping %d %s", n, plural(n, "process", "processes")))
	}
	for _, r := range of(store.ResProc) {
		if procStillIt(r) {
			pid, _ := strconv.Atoi(r.Ref)
			if err := cardproc.StopTree(pid); err != nil {
				failed(r, err)
				warn("process %s did not stop: %v", r.Ref, err)
				steps.problem("process %s: %v", r.Ref, err)
				continue
			}
		}
		freed(r)
	}
	if n := len(of(store.ResPort)); n > 0 {
		steps.begin("ports", fmt.Sprintf("releasing %d %s", n, plural(n, "port", "ports")))
	}
	for _, r := range of(store.ResPort) {
		freed(r)
	}

	// 1c. overlays, once their tunnelers and quickstart are stopped above: the folder, its PKI and its identities
	for _, r := range of(store.ResOverlay) {
		home := s.inCards(r.Ref)
		if home == "" {
			continue
		}
		steps.begin("overlay:"+strconv.Itoa(r.Seq), "deleting the overlay "+r.Ref)
		if err := removeAllSoon(home); err != nil {
			failed(r, err)
			warn("%s did not delete: %v", r.Ref, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
		s.freeIdentitiesOf(t.ID, rows, home)
	}

	// 2. the review
	outside := s.walkedOutside(t.ID, rows)
	if pv.keepAll {
		outside = nil
	}
	if len(outside)+len(of(store.ResReview)) > 0 {
		steps.begin("review", "archiving the review")
	}
	for _, p := range outside {
		if err := s.closeReview(p.ID); err != nil {
			warn("the review %s did not archive: %v", p.ID, err)
			steps.problem("%v", err)
		}
	}
	for _, r := range of(store.ResReview) {
		if err := s.closeReview(r.Ref); err != nil {
			failed(r, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
	}

	// 2b. the card's branches on the hub, pushed from its worktree, so before the worktree goes
	if !pv.keepAll {
		s.freeHubBranches(t, rows, asked, answers, steps, failed, freed, warn)
	}

	// 3. worktrees. A kept one keeps its branch: repo and branch name.
	keptBranch := map[string]bool{}
	for _, r := range of(store.ResWorktree) {
		a, isAsked := asked[r.Seq]
		ans := answers[strconv.Itoa(r.Seq)]
		key := "worktree:" + strconv.Itoa(r.Seq)
		if isAsked && ans == CloseKeep {
			steps.begin(key, "keeping "+r.Ref)
			keptBranch[r.Detail+"\x00"+a.Branch] = true
			continue
		}
		if isAsked && ans == CloseStash {
			steps.begin(key, "stashing "+r.Ref+" to the hub")
			ctx, cancel := context.WithTimeout(context.Background(), 2*closeGitWait)
			hubBranch, hubRepo, err := s.Stash(ctx, t.ID, r.Ref, a.Branch)
			cancel()
			if err != nil {
				warn("%s was kept: the stash did not land: %v", r.Ref, err)
				failed(r, fmt.Errorf("kept, the stash did not land: %w", err))
				steps.problem("kept, the stash did not land: %v", err)
				ans = CloseKeep
			} else {
				res.Stashes = append(res.Stashes, closeStash{Branch: hubBranch, Repo: hubRepo, Expires: s.stashExpiry(time.Now())})
				if _, err := s.st.AddResource(t.ID, store.ResStash, hubBranch, hubRepo); err != nil {
					warn("the stash %s is on the hub, but the card did not record it: %v", hubBranch, err)
				}
			}
		}
		if isAsked && ans == CloseKeep {
			keptBranch[r.Detail+"\x00"+a.Branch] = true
			continue
		}
		// Measured now, ignored trees included, so the row says what was freed and not what an early look saw.
		n := dirSize(context.Background(), filepath.FromSlash(r.Ref))
		if n > 0 {
			_ = s.st.SetResourceBytes(r.Card, r.Seq, n)
		}
		if !(isAsked && ans == CloseStash) {
			steps.begin(key, "removing "+r.Ref+sizeNote(n))
		}
		if err := removeWorktree(r.Detail, r.Ref); err != nil {
			failed(r, err)
			keptBranch[r.Detail+"\x00"+a.Branch] = true
			warn("%s did not remove: %v", r.Ref, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
	}

	// 4. branches, 5. refs
	if n := len(of(store.ResBranch)); n > 0 {
		steps.begin("branches", fmt.Sprintf("deleting %d %s", n, plural(n, "branch", "branches")))
	}
	for _, r := range of(store.ResBranch) {
		if keptBranch[r.Detail+"\x00"+r.Ref] {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), closeGitWait)
		_, err := gitIn(ctx, r.Detail, "branch", "-D", "--", r.Ref)
		cancel()
		if err != nil && branchStillThere(r.Detail, r.Ref) {
			failed(r, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
	}
	if n := len(of(store.ResRef)); n > 0 {
		steps.begin("refs", fmt.Sprintf("deleting %d %s", n, plural(n, "ref", "refs")))
	}
	for _, r := range of(store.ResRef) {
		ctx, cancel := context.WithTimeout(context.Background(), closeGitWait)
		_, err := gitIn(ctx, r.Detail, "update-ref", "-d", r.Ref)
		cancel()
		if err != nil {
			failed(r, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
	}

	// 5a. the clone the card's open made, once nothing of the card hangs off it
	s.freeClones(of(store.ResClone), steps, failed, freed, warn)

	// 5b. scratch folders, only under the scratch root
	for _, r := range of(store.ResDir) {
		dir := s.inScratch(r.Ref)
		if dir == "" {
			continue
		}
		steps.begin("scratch:"+strconv.Itoa(r.Seq), "removing the scratch folder "+r.Ref)
		if err := os.RemoveAll(dir); err != nil {
			failed(r, err)
			warn("%s did not remove: %v", r.Ref, err)
			steps.problem("%v", err)
			continue
		}
		freed(r)
	}

	// 6. the card
	steps.begin("card", "marking the card done")
	defer steps.end("")
	after, _ := s.st.Resources(t.ID)
	res.Items = []closeItem{}
	for _, r := range after {
		it := closeItem{Seq: r.Seq, Kind: r.Kind, Ref: r.Ref, Detail: r.Detail, Action: "freed", Bytes: r.Bytes,
			Err: r.FreedErr}
		if r.Live() {
			it.Action = "keep"
			res.Left++
		}
		res.Items = append(res.Items, it)
	}
	res.Closed = true
	if orphan {
		return res
	}
	// A close the operator made is the clean up a "clean up when done" mark asks for, so the mark goes before the card
	// reaches done and no board offers the close a second time. See cleanupWhenDone in js/sharing.js.
	// The closed mark goes on in the same write. The terminals tab leaves a card with it out of the list.
	// The tags as they are now: a close walks resources for a long while, and the card may have been tagged since t was read.
	now := t.Tags
	if cur, err := s.st.Get(t.ID); err == nil {
		now = cur.Tags
	}
	kept := withoutCleanupTags(now)
	if !slices.Contains(kept, ClosedTag) {
		kept = append(kept, ClosedTag)
	}
	if err := s.st.SetTags(t.ID, kept); err != nil {
		warn("the closed mark did not go on: %v", err)
	}
	if t.Status != store.StatusPaused {
		if err := s.st.SetStatusBecause(t.ID, store.StatusDone, "closed"); err != nil {
			warn("the card did not move to done: %v", err)
		}
	}
	// A notified event with its own by, as a deploy hold's lift is: the event table takes a fixed set of kinds.
	if err := s.st.AppendEvent(t.ID, store.EventNotified, map[string]any{"by": closedBy, "left": res.Left,
		"stashes": res.Stashes, "warnings": res.Warnings}); err != nil {
		log.Printf("[atrium api] close %s: the event: %v", t.ID, err)
	}
	if nt, err := s.st.Get(t.ID); err == nil {
		s.PublishTask(nt)
	}
	return res
}

// reclaimInventory frees what a reclaimed worker owns, which is a close without its questions: a worktree that holds
// work nowhere else has is KEPT, never deleted and never asked about, because the launcher said the work was merged
// and deployed and did not say this. Best effort, like the rest of a reclaim: a failure is logged and the cull stands.
func (s *Server) reclaimInventory(ctx context.Context, t *store.Task) {
	pv, err := s.closePreview(ctx, t)
	if err != nil {
		log.Printf("[atrium api] reclaim %s: the inventory did not read: %v", t.ID, err)
		return
	}
	live := false
	for _, it := range pv.Items {
		if it.Action != "freed" {
			live = true
		}
	}
	if !live {
		return
	}
	if _, busy := closing.LoadOrStore(t.ID, true); busy {
		return
	}
	defer closing.Delete(t.ID)
	answers := map[string]string{}
	for _, a := range pv.Asks {
		answers[strconv.Itoa(a.Seq)] = CloseKeep
	}
	s.closeCard(t, pv, answers, false, nil)
}

// POST /v1/tasks/{id}/cleanup-offer claims the one offer of a "clean up when done" close: it swaps cleanup:when-done
// for cleanup:offered in one step and answers 200 to the single caller that swapped it, 409 to every other, since the
// mark was already gone. Boards show the close only on a 200, so two open boards never both offer it.
func (s *Server) postCleanupOffer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	swapped, err := s.st.SwapTag(id, "cleanup:when-done", "cleanup:offered")
	if err != nil {
		s.fail(w, err)
		return
	}
	if !swapped {
		prError(w, http.StatusConflict, "already_offered", "the clean up was already offered, or the card does not carry it", nil)
		return
	}
	if nt, err := s.st.Get(id); err == nil {
		s.PublishTask(nt)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// withoutCleanupTags is tags with the board's two clean up marks taken off: cleanup:when-done, set when a link is
// opened, and cleanup:offered, which the board swaps it for when it shows the close.
func withoutCleanupTags(tags []string) []string {
	kept := make([]string, 0, len(tags))
	for _, g := range tags {
		if g != "cleanup:when-done" && g != "cleanup:offered" {
			kept = append(kept, g)
		}
	}
	return kept
}

// procStillIt is whether a proc row's pid is still the process recorded: running, with the same start time.
func procStillIt(r *store.CardResource) bool {
	pid, err := strconv.Atoi(r.Ref)
	if err != nil {
		return false
	}
	at, err := cardproc.StartTime(pid)
	return err == nil && at == r.Detail
}

// branchStillThere says whether a branch is in a repo, so deleting one already gone is not a failure.
func branchStillThere(repo, branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), closeGitWait)
	defer cancel()
	_, err := gitIn(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// removeWorktree removes a worktree the inventory names, through git and only through git, so a path that is not a
// worktree of repo is never deleted. A worktree already gone is pruned. A Windows file a stopped session still held
// is tried again briefly.
func removeWorktree(repo, path string) error {
	if strings.TrimSpace(repo) == "" {
		return errors.New("the inventory does not say which repository it belongs to")
	}
	if _, err := os.Stat(filepath.FromSlash(path)); errors.Is(err, os.ErrNotExist) {
		ctx, cancel := context.WithTimeout(context.Background(), closeGitWait)
		defer cancel()
		_, _ = gitIn(ctx, repo, "worktree", "prune")
		return nil
	}
	var err error
	for try := 0; try < 3; try++ {
		if try > 0 {
			time.Sleep(time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), closeGitWait)
		_, err = gitIn(ctx, repo, "worktree", "remove", "--force", "--force", filepath.FromSlash(path))
		cancel()
		if err == nil || !worktreeListed(repo, path) {
			err = nil
			break
		}
	}
	if err != nil {
		return err
	}
	// Git is done with it. What is still at the path is not freed until it is gone.
	return removeLeftover(repo, path)
}

// closeReview stops a review that is going, sets its run folder aside without its source copy, and archives the
// row. A row already gone is closed.
func (s *Server) closeReview(id string) error {
	p, err := s.st.PRByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	prOpMu.Lock()
	if _, err := s.st.MovePR(p.ID, []string{store.PRQueued, store.PRFetching, store.PRRunning},
		store.PRAborted, "", "the card was closed"); err == nil {
		s.prRunner().Abort(p.ID)
	} else if !errors.Is(err, store.ErrPRState) && !errors.Is(err, sql.ErrNoRows) {
		prOpMu.Unlock()
		return err
	}
	prOpMu.Unlock()
	if p.Archived == "" {
		if err := s.setRunAside(p); err != nil {
			log.Printf("[atrium api] close: review %s: %v", p.ID, err)
		}
		if _, err := s.st.ArchivePR(p.ID); err != nil {
			return err
		}
	}
	s.PublishPR(p.ID)
	return nil
}

// closedRunDir is the name a closed review's run folder is set aside under.
func closedRunDir(runDir, when string) string {
	return strings.TrimRight(runDir, "/") + "-closed-" + when
}

// setRunAside renames a review's run folder to its closed name and removes src/, the copy of the PR's code. Only a
// folder under the reviews root is touched.
func (s *Server) setRunAside(p *store.PRReview) error {
	if p.RunDir == "" {
		return nil
	}
	root := filepath.FromSlash(s.st.ReviewsRoot())
	dir, err := safepath.Contained(root, filepath.FromSlash(p.RunDir))
	if err != nil {
		return fmt.Errorf("%s is outside the reviews root, so it is left as it is", p.RunDir)
	}
	if rootAbs, err := filepath.Abs(root); err == nil && dir == rootAbs {
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	to := closedRunDir(dir, time.Now().UTC().Format("20060102-150405"))
	if err := os.Rename(dir, to); err != nil {
		return err
	}
	if err := s.st.SetPRRunDir(p.ID, filepath.ToSlash(to)); err != nil {
		_ = os.Rename(to, dir)
		return err
	}
	return os.RemoveAll(filepath.Join(to, "src"))
}
