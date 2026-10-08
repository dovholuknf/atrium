package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/cardproc"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE SWEEP: what the inventory still lists that no live card holds. Design: docs/rnd/card-lifecycle-design.md
// section 8. Item r-inventory-sweep.
//
//	GET  /v1/sweep                              the last sweep, or a fresh one when there is none
//	POST /v1/sweep                              sweep now
//	POST /v1/sweep/close {owner, confirm, answers}   close what an owner with no card holds, as a card's close does
//
// It runs once when the room starts and when the board asks. It walks every row not freed yet:
//   - a row of a live card (running, waiting, backlog, shelved) is left alone, except a process that exited.
//   - a row whose thing is gone (its path, its branch, its ref, its review row) is marked freed, with why.
//   - a stash is left alone: it is on the hub, and nothing on a room frees it.
//   - the rest are LEFTOVERS, grouped by owner, of a card that is done or dead and was never closed, or of an owner
//     with no card at all (a card forgotten, an open that died before its card started). The board offers one close
//     per group. NOTHING IS REMOVED HERE: removing is the close, after that press.
//
// THE OTHER DIRECTION: a worktree under the room's own worktree folder (`<scm>/worktrees`) that no inventory row and no
// card names is listed as NOT OWNED, and nothing is offered for it. A provider's worktree folder is not walked: gwt
// keeps its own worktrees there, and those are not atrium's to list.

// sweepPendingGrace is how long an open's pending owner is left alone, so a sweep does not offer what an open in
// flight is still making.
const sweepPendingGrace = 15 * time.Minute

// sweepWorktreeDepth bounds the walk for worktrees: <root>/<host>/<org>/<repo>/<branch>, and a branch with slashes.
const sweepWorktreeDepth = 8

// sweepLeftover is one owner's rows that no live card holds.
type sweepLeftover struct {
	Owner  string `json:"owner"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
	// NoCard is an owner with no card: close it through POST /v1/sweep/close.
	NoCard bool `json:"no_card,omitempty"`
	// Closed is a card that was closed and kept these on purpose.
	Closed    bool                  `json:"closed,omitempty"`
	Rows      []*store.CardResource `json:"rows"`
	DiskBytes int64                 `json:"disk_bytes"`
}

// sweepFreed is a row the sweep found gone and marked freed.
type sweepFreed struct {
	Owner string `json:"owner"`
	Seq   int    `json:"seq"`
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Why   string `json:"why"`
}

// sweepNotOwned is a worktree nothing owns.
type sweepNotOwned struct {
	Path string `json:"path"`
}

type sweepReport struct {
	SweptAt   string          `json:"swept_at"`
	Leftovers []sweepLeftover `json:"leftovers"`
	Freed     []sweepFreed    `json:"freed"`
	NotOwned  []sweepNotOwned `json:"not_owned"`
	// Root is the worktree folder walked for not owned ones, "" when the room has no scm folder.
	Root     string   `json:"root,omitempty"`
	Warnings []string `json:"warnings"`
}

// sweepState is the last report, and one sweep at a time.
type sweepState struct {
	mu   sync.Mutex
	last *sweepReport
}

// GET /v1/sweep
func (s *Server) getSweep(w http.ResponseWriter, r *http.Request) {
	s.sweep.mu.Lock()
	last := s.sweep.last
	s.sweep.mu.Unlock()
	if last == nil {
		last = s.Sweep(r.Context())
	}
	writeJSON(w, http.StatusOK, last)
}

// POST /v1/sweep
func (s *Server) postSweep(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Sweep(r.Context()))
}

// POST /v1/sweep/close: the close of a card, for an owner the sweep listed. A card that exists is closed as itself.
func (s *Server) postSweepClose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Owner string `json:"owner"`
		closeRequest
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	in.Owner = strings.TrimSpace(in.Owner)
	if in.Owner == "" {
		prError(w, http.StatusBadRequest, "bad_request", "whose: owner", nil)
		return
	}
	if t, err := s.st.Get(in.Owner); err == nil {
		s.closeFlow(w, r, t, false, in.closeRequest)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.fail(w, err)
		return
	}
	rows, err := s.st.Resources(in.Owner)
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(rows) == 0 {
		writeErr(w, http.StatusNotFound, errors.New("nothing is held by "+in.Owner))
		return
	}
	s.closeFlow(w, r, &store.Task{ID: in.Owner, Status: store.StatusDead}, true, in.closeRequest)
}

// sweepLive is whether a card is still at work, or may come back to it, so its rows are its own business.
func sweepLive(t *store.Task) bool {
	switch t.Status {
	case store.StatusDone, store.StatusDead:
		return false
	}
	return true
}

// Sweep walks the inventory and the room's worktree folder, marks what is gone, and keeps the answer for GET.
func (s *Server) Sweep(ctx context.Context) *sweepReport {
	s.sweep.mu.Lock()
	defer s.sweep.mu.Unlock()
	rep := &sweepReport{SweptAt: time.Now().UTC().Format(time.RFC3339), Leftovers: []sweepLeftover{},
		Freed: []sweepFreed{}, NotOwned: []sweepNotOwned{}, Warnings: []string{}}
	warn := func(msg string) { rep.Warnings = append(rep.Warnings, msg) }

	rows, err := s.st.LiveResources()
	if err != nil {
		warn("the inventory did not read: " + err.Error())
	}
	owned := map[string]bool{}
	byOwner := map[string]*sweepLeftover{}
	var order []string
	cards := map[string]*store.Task{}
	missing := map[string]bool{}
	for _, r := range rows {
		if r.Kind == store.ResWorktree {
			owned[sweepKey(r.Ref)] = true
		}
		// A stash is on the hub, and nothing on a room frees it.
		if r.Kind == store.ResStash {
			continue
		}
		t, known := cards[r.Card]
		if !known && !missing[r.Card] {
			got, err := s.st.Get(r.Card)
			switch {
			case err == nil:
				t, cards[r.Card] = got, got
			case errors.Is(err, sql.ErrNoRows):
				missing[r.Card] = true
			default:
				warn("the card " + r.Card + " did not read: " + err.Error())
				continue
			}
		}
		// A live card's rows are its own, except a process that exited: that one is freed whoever holds it.
		if t != nil && sweepLive(t) && r.Kind != store.ResProc {
			continue
		}
		if t == nil && strings.HasPrefix(r.Card, "pending:") && sweepYoung(r.MadeAt) {
			continue
		}
		if why := s.sweepGone(ctx, r); why != "" {
			if err := s.st.FreeResource(r.Card, r.Seq, why); err != nil {
				warn("a gone " + r.Kind + " was not marked freed: " + err.Error())
			} else {
				rep.Freed = append(rep.Freed, sweepFreed{Owner: r.Card, Seq: r.Seq, Kind: r.Kind, Ref: r.Ref, Why: why})
				continue
			}
		}
		if t != nil && sweepLive(t) {
			continue
		}
		g := byOwner[r.Card]
		if g == nil {
			g = &sweepLeftover{Owner: r.Card, NoCard: t == nil, Rows: []*store.CardResource{}}
			if t != nil {
				g.Title, g.Status, g.Closed = t.Title, t.Status, s.wasClosed(t.ID)
			}
			byOwner[r.Card] = g
			order = append(order, r.Card)
		}
		g.Rows = append(g.Rows, r)
		if r.Bytes > 0 {
			g.DiskBytes += r.Bytes
		}
	}
	for _, o := range order {
		rep.Leftovers = append(rep.Leftovers, *byOwner[o])
	}

	// The other direction.
	if all, err := s.st.List(); err == nil {
		for _, t := range all {
			if t.Worktree != "" {
				owned[sweepKey(t.Worktree)] = true
			}
		}
	}
	root := ""
	set, _ := s.st.Setting(gitsync.SettingSCMRoot)
	if scm := gitsync.EffectiveSCMRoot(set); strings.TrimSpace(scm) != "" {
		root = filepath.ToSlash(filepath.Join(filepath.FromSlash(strings.TrimSpace(scm)), "worktrees"))
	}
	rep.Root = root
	if root != "" {
		for _, p := range findWorktrees(ctx, root) {
			if !owned[sweepKey(p)] {
				rep.NotOwned = append(rep.NotOwned, sweepNotOwned{Path: p})
			}
		}
	}
	s.sweep.last = rep
	if n := len(rep.Freed); n > 0 {
		log.Printf("[atrium api] sweep: %d gone rows marked freed", n)
	}
	return rep
}

// sweepYoung is whether a row was made within the pending grace.
func sweepYoung(madeAt string) bool {
	at, err := time.Parse(store.TimeFormat, madeAt)
	return err == nil && time.Since(at) < sweepPendingGrace
}

// sweepGone says why a row's thing is no longer there, or "" when it is, or when that cannot be told.
func (s *Server) sweepGone(ctx context.Context, r *store.CardResource) string {
	switch r.Kind {
	case store.ResWorktree, store.ResDir:
		if _, err := os.Stat(filepath.FromSlash(r.Ref)); errors.Is(err, os.ErrNotExist) {
			return "gone from disk, found by the sweep"
		}
	case store.ResBranch:
		if r.Detail != "" && isCheckout(filepath.FromSlash(r.Detail)) && !branchStillThere(r.Detail, r.Ref) {
			return "the branch is gone, found by the sweep"
		}
	case store.ResRef:
		if r.Detail != "" && isCheckout(filepath.FromSlash(r.Detail)) {
			cctx, cancel := context.WithTimeout(ctx, closeGitWait)
			_, err := gitIn(cctx, r.Detail, "rev-parse", "--verify", "--quiet", r.Ref)
			cancel()
			if err != nil {
				return "the ref is gone, found by the sweep"
			}
		}
	case store.ResOverlay, store.ResIdentity:
		if _, err := os.Stat(filepath.FromSlash(r.Ref)); errors.Is(err, os.ErrNotExist) {
			return "gone from disk, found by the sweep"
		}
	case store.ResProc:
		pid, err := strconv.Atoi(r.Ref)
		if err != nil {
			return "not a pid, found by the sweep"
		}
		at, err := cardproc.StartTime(pid)
		if errors.Is(err, cardproc.ErrGone) {
			return "the process exited, found by the sweep"
		}
		if err == nil && at != r.Detail {
			return "the pid is another process now, found by the sweep"
		}
	case store.ResReview:
		p, err := s.st.PRByID(r.Ref)
		if errors.Is(err, sql.ErrNoRows) {
			return "the review row is gone, found by the sweep"
		}
		if err == nil && p.Archived != "" {
			return "the review was archived, found by the sweep"
		}
	}
	return ""
}

// wasClosed is whether a card was closed, so what it still holds was kept on purpose.
func (s *Server) wasClosed(id string) bool {
	evs, err := s.st.Events(id, 0)
	if err != nil {
		return false
	}
	for _, e := range evs {
		if e.Kind != store.EventNotified {
			continue
		}
		var p struct {
			By string `json:"by"`
		}
		if json.Unmarshal(e.Payload, &p) == nil && p.By == closedBy {
			return true
		}
	}
	return false
}

// sweepKey is a path as the sweep compares it: slashes, cleaned, and without case, since the rooms this runs on
// mostly have a file system that ignores it.
func sweepKey(p string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(p)))))
}

// findWorktrees is every linked worktree under root: a directory whose .git is a file. It does not go into one it
// found, and goes no deeper than sweepWorktreeDepth.
func findWorktrees(ctx context.Context, root string) []string {
	base := filepath.FromSlash(root)
	var out []string
	_ = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(base, p)
		if rel != "." && strings.Count(filepath.ToSlash(rel), "/") >= sweepWorktreeDepth {
			return fs.SkipDir
		}
		if fi, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			if fi.Mode().IsRegular() {
				out = append(out, filepath.ToSlash(p))
			}
			// A worktree, or a whole checkout: either way not walked into.
			if rel != "." {
				return fs.SkipDir
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}
