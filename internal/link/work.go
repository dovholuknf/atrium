package link

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// WORK, ON THE HUB: the item as the unit of work, one per backlog id, with every card attached. READ ONLY. The rows are
// written when a room pushes an assignment that names its backlog id (POST /_hub/items/sync), so there is nothing to
// post here. See internal/hubstore/work.go and docs/changes/item-lifecycle-l1.md.
//
//	GET /_hub/work           the items, newest change first
//	GET /_hub/work/<id>      one: `work`, its `cards`, its `assignments` (the rows of /_hub/items that belong to it) and
//	                         its `steps`, the git ones (landed, deployed, pushed) read fresh
//	POST /_hub/work/<id>     {"branch","kind"}: name the build branch, and say whether it is code, doc or mixed

func (p *Proxy) serveWork(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "work"), "/")
	if r.Method == http.MethodPost && rest != "" && !strings.Contains(rest, "/") {
		p.serveWorkSet(w, r, st, rest)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		crFail(w, http.StatusMethodNotAllowed, "items are written by pushing an assignment, so this is a GET")
		return
	}
	switch {
	case rest == "":
		list, err := st.WorkList(0)
		if err != nil {
			wfail(w, err)
			return
		}
		if list == nil {
			list = []hubstore.Work{}
		}
		crJSON(w, http.StatusOK, map[string]any{"work": list})
	case !strings.Contains(rest, "/"):
		wk, err := st.WorkByID(rest)
		if err != nil {
			wfail(w, err)
			return
		}
		cards, err := st.WorkCards(rest)
		if err != nil {
			wfail(w, err)
			return
		}
		as, err := st.WorkAssignments(rest)
		if err != nil {
			wfail(w, err)
			return
		}
		if cards == nil {
			cards = []hubstore.WorkCard{}
		}
		if as == nil {
			as = []hubstore.Item{}
		}
		// THE GIT STEPS ARE READ FRESH, bounded, and the stored ones answer when the read cannot be made.
		rctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		steps, err := p.refreshOne(rctx, rest)
		cancel()
		if steps == nil || err != nil {
			if steps, err = st.WorkSteps(rest); err != nil {
				wfail(w, err)
				return
			}
		}
		if steps == nil {
			steps = []hubstore.WorkStepRow{}
		}
		crJSON(w, http.StatusOK, map[string]any{"work": wk, "cards": cards, "assignments": as, "steps": steps})
	default:
		http.NotFound(w, r)
	}
}

// serveWorkSet answers POST /_hub/work/<id> {"branch": ..., "kind": ...}: the build branch of an item and whether it is
// `code`, `doc` or `mixed`, which are what `landed` and `deployed` read. Either may be left out.
func (p *Proxy) serveWorkSet(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	switch {
	case docsCrossOrigin.Check(r) != nil:
		crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
		return
	case !edge.LocalOperator(r):
		crFail(w, http.StatusForbidden, "items are written only from the machine the hub runs on"+edge.ProxyNote(r))
		return
	}
	var in struct {
		Branch *string `json:"branch"`
		Kind   *string `json:"kind"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil || (in.Branch == nil && in.Kind == nil) {
		crFail(w, http.StatusBadRequest, "send {\"branch\": ..., \"kind\": ...}, either of them")
		return
	}
	if in.Kind != nil && *in.Kind != "code" && *in.Kind != "doc" && *in.Kind != "mixed" {
		crFail(w, http.StatusBadRequest, "kind is code, doc or mixed")
		return
	}
	if in.Branch != nil {
		if why := gitsync.CheckPushBranch(*in.Branch); *in.Branch != "" && why != "" {
			crFail(w, http.StatusBadRequest, why)
			return
		}
		if err := st.SetWorkBranch(id, *in.Branch); err != nil {
			wfail(w, err)
			return
		}
	}
	if in.Kind != nil {
		if err := st.SetWorkKind(id, *in.Kind); err != nil {
			wfail(w, err)
			return
		}
	}
	rctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	_, _ = p.refreshOne(rctx, id)
	crJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func wfail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, hubstore.ErrNoWork) {
		code = http.StatusNotFound
	}
	crFail(w, code, err.Error())
}
