package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// WORK, ON THE HUB: the item as the unit of work, one per backlog id, with every card attached. The rows are written when a
// room pushes an assignment that names its backlog id (POST /_hub/items/sync), so there is nothing to post to make one.
// See internal/hubstore/work.go and docs/changes/item-lifecycle-l1.md. Since L2 an item also has its steps, derived by the
// engine (lifecycle.go), and the human's three actions on them (lifecycle_engine.go). L3 reads the git ones (lifecycle_git.go).
//
//	GET  /_hub/work           the items, newest change first, each with its `steps`, `state` and `current`
//	GET  /_hub/work/<id>      one: `work`, its `cards`, its `assignments`, its `facts`, its `steps` (all eight) and its
//	                          `lifecycle`. The git steps (landed, deployed, pushed) are read fresh. Once it has landed it may
//	                          carry `docs`: {not_said} or {offer: {id}}, see docdebt_landed.go
//	GET  /_hub/work/<id>/cost what it cost, estimated at list price: per step, the headline (the steps alone, "+
//	                          coordination"), the coordination, the sum, and the tokens behind every figure
//	GET  /_hub/work/cost      the same for every item, newest change first, a line each
//	GET  /_hub/prices         the price table the dollars are worked out on, and its version
//	POST /_hub/work/<id>      {do: approve|send_back|build_directly, by, sha?, note?}: the human acts on the design, or
//	                          {"branch","kind"}: name the build branch, and say whether it is code, doc or mixed. Only from
//	                          the machine the hub runs on

// workView is one row of the list: the item, and what the engine says of it.
type workView struct {
	hubstore.Work
	Lifecycle
}

func (p *Proxy) serveWork(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "work"), "/")
	if r.Method == http.MethodPost && rest == "record" {
		// A CARD'S TEST. The one write here a card makes, through the hub's own tool, so it is not the operator's route: it has
		// its own checks (work_record.go), and the same loopback and origin guards.
		switch {
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "records are written only from the machine the hub runs on"+edge.ProxyNote(r))
		default:
			p.serveWorkRecord(w, r, st)
		}
		return
	}
	if id, ok := strings.CutSuffix(rest, "/review"); ok && r.Method == http.MethodPost && id != "" && !strings.Contains(id, "/") {
		p.serveWorkReview(w, r, st, id)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(rest, "/event") && !strings.Contains(strings.TrimSuffix(rest, "/event"), "/") {
		switch {
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "items are written only from the machine the hub runs on"+edge.ProxyNote(r))
		default:
			p.serveWorkEvent(w, r, st, strings.TrimSuffix(rest, "/event"))
		}
		return
	}
	if r.Method == http.MethodPost && rest != "" && !strings.Contains(rest, "/") {
		switch {
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "items are written only from the machine the hub runs on"+edge.ProxyNote(r))
		default:
			// ONE ROUTE, TWO BODIES: a human's act on the design names `do`, and L3's names the branch or the kind.
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 8<<10))
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var peek struct {
				Do string `json:"do"`
			}
			_ = json.Unmarshal(raw, &peek)
			if peek.Do != "" {
				p.workAction(w, r, st, rest)
			} else {
				p.serveWorkSet(w, r, st, rest)
			}
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		crFail(w, http.StatusMethodNotAllowed, "items are made by pushing an assignment, so this is a GET, or a POST of an action on one")
		return
	}
	switch {
	case rest == "":
		list, err := st.WorkList(0)
		if err != nil {
			wfail(w, err)
			return
		}
		out := make([]workView, 0, len(list))
		cached, err := st.AllWorkSteps()
		if err != nil {
			wfail(w, err)
			return
		}
		for _, wk := range list {
			out = append(out, p.listedWork(r, st, wk, cached[wk.BacklogID]))
		}
		crJSON(w, http.StatusOK, map[string]any{"work": out})
	case rest == "efficacy":
		p.serveWorkEfficacy(w, r, st)
	case rest == "cost":
		p.writeWorkCosts(w, st)
	case strings.HasSuffix(rest, "/cost") && !strings.Contains(strings.TrimSuffix(rest, "/cost"), "/"):
		p.writeWorkCost(w, st, strings.TrimSuffix(rest, "/cost"))
	case !strings.Contains(rest, "/"):
		p.writeWorkDetail(w, r, st, rest)
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

// listedWork is one item of the list. The steps come from the cache, and the engine runs when it has not written its own yet
// (an item from before this stage), or when the item holds an approval, whose staleness is a fact about git and so changes
// without a push.
func (p *Proxy) listedWork(r *http.Request, st *hubstore.Store, wk hubstore.Work, cached []hubstore.WorkStepRow) workView {
	if !engineStepsWritten(cached) || approvalHeld(cached) {
		if got, err := p.refreshWork(r.Context(), st, wk.BacklogID, approvalHeld(cached)); err == nil {
			return workView{Work: wk, Lifecycle: got.Lifecycle}
		}
	}
	as, _ := st.WorkAssignments(wk.BacklogID)
	l := completeSteps(cached)
	l.State, l.Waiting = itemState(l, as)
	return workView{Work: wk, Lifecycle: l}
}

// engineStepsWritten is whether the engine's own steps (designed to reviewed) are all stored. L3 writes the other three.
func engineStepsWritten(steps []hubstore.WorkStepRow) bool {
	n := 0
	for _, s := range steps {
		if engineStep(s.Step) {
			n++
		}
	}
	return n == len(engineSteps)
}

// approvalHeld is whether an approval stands, which git can take away.
func approvalHeld(steps []hubstore.WorkStepRow) bool {
	for _, s := range steps {
		if s.Step == hubstore.StepApproved && (s.State == hubstore.StepDone || s.State == hubstore.StepStale) {
			return true
		}
	}
	return false
}

func (p *Proxy) writeWorkDetail(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	// THE GIT STEPS ARE READ FRESH, bounded, and what is stored answers when the read cannot be made.
	rctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	_, _ = p.refreshOne(rctx, id)
	cancel()
	got, err := p.refreshWork(r.Context(), st, id, true)
	if err != nil {
		wfail(w, err)
		return
	}
	if got.Cards == nil {
		got.Cards = []hubstore.WorkCard{}
	}
	if got.Assignments == nil {
		got.Assignments = []hubstore.Item{}
	}
	if got.Facts == nil {
		got.Facts = []hubstore.WorkFact{}
	}
	tasks, _ := st.TaskList(id)
	if tasks == nil {
		tasks = []hubstore.Task{}
	}
	crJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "work": got.Work, "cards": got.Cards, "assignments": got.Assignments,
		"facts": got.Facts, "steps": got.Lifecycle.Steps, "lifecycle": got.Lifecycle, "cost": p.costOf(st, got.Work.BacklogID, got.Cards, got.Lifecycle.Steps),
		"efficacy": p.efficacyOf(st, got.Work.BacklogID), "docs": p.docsOf(r.Context(), st, id)})
}

func wfail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, hubstore.ErrNoWork) {
		code = http.StatusNotFound
	}
	crFail(w, code, err.Error())
}
