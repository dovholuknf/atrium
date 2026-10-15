package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// CLOSING BY WORKTREE: the call gwt's Remove-Worktree makes before it deletes a folder. Item g-close-by-cwd.
//
//	POST /v1/cards/archive {cwd, wait, work}
//
// Every live card whose cwd is that folder is closed through the same close a board press runs (closeCard), so the
// worktree, branch, review and runner go the way they do for POST /v1/tasks/{id}/close. A card is matched on its own
// worktree and on any worktree row of its inventory.
//
// NO DIALOG, SO NO ASK IS LEFT OPEN. A worktree holding work nowhere else has is stashed to the hub when the room can,
// and kept otherwise, never deleted, unless `work` says keep, stash or delete. The caller is about to delete the folder
// itself, which is why the default is the cautious one.
//
// wait true answers once every close has finished. Without it the answer is 202 at once, naming the cards, and the
// closes run behind it.

type archiveRequest struct {
	Cwd  string `json:"cwd"`
	Wait bool   `json:"wait"`
	// Work is what a worktree holding work nowhere else has gets: keep, stash or delete. Empty is stash, else keep.
	Work string `json:"work,omitempty"`
}

type archiveResult struct {
	Cwd    string            `json:"cwd"`
	Wait   bool              `json:"wait"`
	Cards  []string          `json:"cards"`
	Closed []*closeResult    `json:"closed,omitempty"`
	Failed map[string]string `json:"failed,omitempty"`
}

// samePathFold is whether two folders are the same one: slashes, a trailing separator and, where the filesystem
// ignores case, case.
func samePathFold(a, b string) bool {
	na, nb := normCwd(a), normCwd(b)
	if na == "" || nb == "" {
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(na, nb)
	}
	return na == nb
}

// normCwd is a folder with forward slashes, one separator between parts and none at the end. "/" stays "/".
func normCwd(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" {
		return ""
	}
	// the \\?\ prefix a Windows tool may add
	p = strings.TrimPrefix(p, "//?/")
	for strings.Contains(p[1:], "//") {
		p = p[:1] + strings.ReplaceAll(p[1:], "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	return p
}

// cardsAt is the live cards whose cwd is dir, not yet closed.
func (s *Server) cardsAt(dir string) ([]*store.Task, error) {
	all, err := s.st.List()
	if err != nil {
		return nil, err
	}
	var out []*store.Task
	for _, t := range all {
		if slices.Contains(t.Tags, ClosedTag) {
			continue
		}
		hit := samePathFold(t.Worktree, dir)
		if !hit {
			if rows, err := s.st.Resources(t.ID); err == nil {
				for _, r := range rows {
					if r.Kind == store.ResWorktree && r.Live() && samePathFold(r.Ref, dir) {
						hit = true
						break
					}
				}
			}
		}
		if hit {
			out = append(out, t)
		}
	}
	return out, nil
}

// closeUnasked is a close with no one to ask: every asked worktree gets work, or the cautious default.
func (s *Server) closeUnasked(t *store.Task, work string) (*closeResult, error) {
	if _, busy := closing.LoadOrStore(t.ID, true); busy {
		return nil, errors.New("that card is being closed already")
	}
	defer closing.Delete(t.ID)
	pv, err := s.closePreview(context.Background(), t)
	if err != nil {
		return nil, err
	}
	answers := map[string]string{}
	for _, a := range pv.Asks {
		ans := work
		if ans == "" {
			ans = CloseKeep
			if pv.CanStash {
				ans = CloseStash
			}
		}
		if ans == CloseStash && (a.Hub || !pv.CanStash) {
			ans = CloseKeep
		}
		answers[strconv.Itoa(a.Seq)] = ans
	}
	return s.closeCard(t, pv, answers, false, s.newCloseSteps("", t.ID)), nil
}

// POST /v1/cards/archive
func (s *Server) postCardsArchive(w http.ResponseWriter, r *http.Request) {
	var in archiveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	in.Cwd = strings.TrimSpace(in.Cwd)
	if in.Cwd == "" {
		prError(w, http.StatusBadRequest, "bad_request", "say which folder with cwd", nil)
		return
	}
	in.Work = strings.ToLower(strings.TrimSpace(in.Work))
	switch in.Work {
	case "", CloseKeep, CloseStash, CloseDelete:
	default:
		prError(w, http.StatusBadRequest, "bad_request", "work is keep, stash or delete, not "+in.Work, nil)
		return
	}
	cards, err := s.cardsAt(in.Cwd)
	if err != nil {
		s.fail(w, err)
		return
	}
	res := archiveResult{Cwd: in.Cwd, Wait: in.Wait, Cards: []string{}}
	for _, t := range cards {
		res.Cards = append(res.Cards, t.ID)
	}
	if len(cards) == 0 {
		writeJSON(w, http.StatusOK, res)
		return
	}
	run := func() {
		for _, t := range cards {
			cr, err := s.closeUnasked(t, in.Work)
			if err != nil {
				log.Printf("[atrium api] archive %s: %s: %v", in.Cwd, t.ID, err)
				if res.Failed == nil {
					res.Failed = map[string]string{}
				}
				res.Failed[t.ID] = err.Error()
				continue
			}
			res.Closed = append(res.Closed, cr)
		}
	}
	if !in.Wait {
		writeJSON(w, http.StatusAccepted, res)
		go run()
		return
	}
	run()
	writeJSON(w, http.StatusOK, res)
}
