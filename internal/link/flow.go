package link

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The merge flow's HTTP half. See internal/hubstore/flow.go for the rows and docs/design/merge-flow.md for why.
//
//	GET  /_hub/flow/roles?role=      who holds each role, or one
//	POST /_hub/flow/roles            {do:"set"|"clear", role, room, card, by?}
//	GET  /_hub/flow/repo             the repository stages are read from, POST {repo} sets it ("" unsets)
//	GET  /_hub/flow/epoch            the open epoch, the latest ones and the plan for a reset of claude/main onto main
//	POST /_hub/flow/epoch            {do:"open", sha?} | {do:"close", n, branch?, expect?}: see flowEpoch
//	GET  /_hub/flow/items?stage=     the items in the flow
//	GET  /_hub/flow/items/<id>       one
//	POST /_hub/flow/items/<id>       {stage, branch?, sha?, note?, by?}: report a stage
//	GET  /_hub/flow/checks?sha=      the checks recorded, newest first
//	POST /_hub/flow/checks           {sha, name, result, by?}
//	GET  /_hub/flow/deploys          the sha each room runs
//	POST /_hub/flow/deploys          {room, sha, by?}
//
// WHO MAY WRITE is what the backlog allows: the cross-origin check and the machine the hub runs on. A room's control
// server asks from there and says who it speaks for in `by`. The hub store is the backlog's.

func (p *Proxy) serveFlow(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.backlogStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if write {
		switch {
		case r.Method != http.MethodPost:
			crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
			return
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "the merge flow is written only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "flow"), "/")
	switch {
	case rest == "roles" && !write:
		hs, err := st.Roles(r.URL.Query().Get("role"))
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"roles": hs})
	case rest == "roles":
		p.flowRole(w, r, st)
	case rest == "repo" && !write:
		name, err := st.FlowRepo()
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"repo": name})
	case rest == "repo":
		var in struct{ Repo, By string }
		if err := crDecode(r, &in); err != nil {
			crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		if err := st.SetFlowRepo(in.Repo); err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit("", "flow-repo", in.Repo+" by "+in.By)
		crJSON(w, http.StatusOK, map[string]any{"repo": strings.TrimSpace(in.Repo)})
	case rest == "epoch" && !write:
		cur, err := st.EpochOpenNow()
		if err != nil && !errors.Is(err, hubstore.ErrNoEpoch) {
			ffail(w, err)
			return
		}
		recent, err := st.Epochs(0)
		if err != nil {
			ffail(w, err)
			return
		}
		out := map[string]any{"epochs": recent}
		if cur.N != 0 {
			out["open"] = cur
		}
		if g := p.flowGitOf(); g != nil {
			if plan, perr := g.EpochPlan(r.Context()); perr == nil {
				out["plan"] = plan
			}
		}
		crJSON(w, http.StatusOK, out)
	case rest == "epoch":
		p.flowEpoch(w, r, st)
	case rest == "items" && !write:
		items, err := st.FlowList(r.URL.Query().Get("stage"))
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"items": items})
	case strings.HasPrefix(rest, "items/") && backlogIDRe.MatchString(strings.TrimPrefix(rest, "items/")):
		p.flowItem(w, r, st, strings.TrimPrefix(rest, "items/"), write)
	case rest == "checks" && !write:
		cs, err := st.Checks(r.URL.Query().Get("sha"), 0)
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"checks": cs})
	case rest == "checks":
		var in struct{ SHA, Name, Result, By string }
		if err := crDecode(r, &in); err != nil {
			crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		c, err := st.CheckAdd(in.SHA, in.Name, in.Result, in.By)
		if err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit("", "flow-check", c.Name+" "+c.Result+" "+c.SHA)
		crJSON(w, http.StatusOK, c)
	case rest == "deploys" && !write:
		ds, err := st.Deploys()
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"deploys": ds})
	case rest == "deploys":
		var in struct{ Room, SHA, By string }
		if err := crDecode(r, &in); err != nil {
			crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		d, err := st.DeploySet(in.Room, in.SHA, in.By)
		if err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit(d.Room, "flow-deploy", d.SHA)
		crJSON(w, http.StatusOK, d)
	default:
		http.NotFound(w, r)
	}
}

// ffail answers a store error: a refusal is the caller's to fix, anything else is the hub's.
func ffail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, hubstore.ErrItemNotFound), errors.Is(err, hubstore.ErrFlowItemNotFound), errors.Is(err, hubstore.ErrNoEpoch):
		code = http.StatusNotFound
	case hubstore.IsRefusal(err):
		code = http.StatusBadRequest
	}
	crFail(w, code, err.Error())
}

func (p *Proxy) flowRole(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct{ Do, Role, Room, Card, By string }
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if in.Room != "" && !validRoomName(in.Room) {
		crFail(w, http.StatusBadRequest, "room is not a room name")
		return
	}
	switch in.Do {
	case "set":
		h, replaced, err := st.RoleSet(in.Role, in.Room, in.Card, in.By)
		if err != nil {
			ffail(w, err)
			return
		}
		detail := h.Role + " is " + h.Room + "/" + h.Card + " by " + in.By
		for _, o := range replaced {
			detail += ", replacing " + o.Room + "/" + o.Card
		}
		p.RecordAudit(h.Room, "flow-role", detail)
		crJSON(w, http.StatusOK, map[string]any{"holder": h, "replaced": replaced})
	case "clear":
		if err := st.RoleClear(in.Role, in.Room, in.Card); err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit(in.Room, "flow-role", in.Role+" cleared for "+in.Room+"/"+in.Card+" by "+in.By)
		crJSON(w, http.StatusOK, map[string]any{"cleared": true})
	default:
		crFail(w, http.StatusBadRequest, "do is set or clear")
	}
}

func (p *Proxy) flowItem(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string, write bool) {
	if !write {
		v, err := p.flowRefresh(r.Context(), st, id)
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, v)
		return
	}
	var in struct{ Stage, Branch, SHA, Note, By string }
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	f, err := st.FlowSetStage(hubstore.FlowSet{ItemID: id, Stage: in.Stage, Branch: in.Branch, SHA: in.SHA, Note: in.Note, By: in.By})
	if err != nil {
		ffail(w, err)
		return
	}
	p.RecordAudit("", "flow-stage", id+" "+f.Stage)
	p.backlogEvent("flow", map[string]string{"id": id, "stage": f.Stage})
	crJSON(w, http.StatusOK, f)
}

// flowEpoch opens or closes an epoch. Opening says clint fetched claude/main at sha, which is claude/main's tip when
// none is given. Closing is the reset: the hub moves claude/main onto the branch the orchestrator replayed on (or onto main
// when nothing is to be replayed), after the checks in gitsync.EpochMove, and records it.
func (p *Proxy) flowEpoch(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct {
		Do, SHA, Branch, Expect, By string
		N                           int64
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	g := p.flowGitOf()
	switch in.Do {
	case "open":
		sha := in.SHA
		if sha == "" {
			if g == nil {
				crFail(w, http.StatusBadRequest, "say which sha clint fetched")
				return
			}
			plan, err := g.EpochPlan(r.Context())
			if err != nil {
				ffail(w, err)
				return
			}
			sha = plan.OldTip
		}
		e, err := st.EpochOpen(sha, in.By)
		if err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit("", "flow-epoch", fmt.Sprintf("%d opened at %s by %s", e.N, e.FetchedSHA, in.By))
		crJSON(w, http.StatusOK, e)
	case "close":
		if g == nil {
			crFail(w, http.StatusServiceUnavailable, "the hub's repository is not wired to the merge flow")
			return
		}
		open, err := st.EpochOpenNow()
		if err != nil {
			ffail(w, err)
			return
		}
		if in.N != 0 && in.N != open.N {
			crFail(w, http.StatusBadRequest, fmt.Sprintf("epoch %d is the open one, not %d", open.N, in.N))
			return
		}
		oldTip, newTip, err := g.EpochMove(r.Context(), open.N, in.Branch, in.Expect)
		if err != nil {
			ffail(w, err)
			return
		}
		e, err := st.EpochClose(open.N, oldTip, newTip, in.By)
		if err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit("", "flow-epoch", fmt.Sprintf("%d closed: claude/main %s to %s by %s", e.N, oldTip, newTip, in.By))
		crJSON(w, http.StatusOK, e)
	default:
		crFail(w, http.StatusBadRequest, "do is open or close")
	}
}
