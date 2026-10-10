//go:build integration

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestAnotherRoomsPRMakesNoRowHere(t *testing.T) {
	s, st, _ := prServer(t)
	run := &fakePRRunner{st: st}
	s.PRRunner = run
	var asked []PRClaimAsk
	s.ClaimPR = func(_ context.Context, a PRClaimAsk) (PRClaimReply, error) {
		asked = append(asked, a)
		return PRClaimReply{Owner: "beta"}, nil
	}
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	if w.Code != 200 || !contains(w.Body.String(), `"held_by":"beta"`) {
		t.Fatalf("answer = %d %s", w.Code, w.Body.String())
	}
	if rows, _ := st.PRs(store.PRFilter{}); len(rows) != 0 || len(run.started) != 0 {
		t.Fatalf("rows %d started %v, want none", len(rows), run.started)
	}
	if len(asked) != 1 || asked[0].Key != "github.com/openziti/tlsuv/378" {
		t.Fatalf("asked = %+v", asked)
	}
}

func TestAnUnreachableHubMakesAPendingRowAndTheReturnReconciles(t *testing.T) {
	s, st, _ := prServer(t)
	run := &fakePRRunner{st: st}
	s.PRRunner = run
	hub := errors.New("no hub")
	var owner string
	s.ClaimPR = func(context.Context, PRClaimAsk) (PRClaimReply, error) {
		if hub != nil {
			return PRClaimReply{}, hub
		}
		return PRClaimReply{Owner: owner, Mine: owner == "here"}, nil
	}
	w := prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"`+prURL+`"}`, "")
	if w.Code != 201 {
		t.Fatalf("answer = %d %s", w.Code, w.Body.String())
	}
	rows, _ := st.PendingClaims()
	if len(rows) != 1 || rows[0].Claim != "pending" {
		t.Fatalf("pending = %+v", rows)
	}
	id := rows[0].ID

	// Back, and the hub says it is ours.
	hub, owner = nil, "here"
	s.ReconcilePRClaims()
	if p, _ := st.PRByID(id); p.Claim != "claimed" {
		t.Fatalf("claim = %q", p.Claim)
	}

	// Another pending row, and by then another room has claimed that key.
	hub = errors.New("no hub")
	prDo(s, s.postPR, "POST", "/v1/prs", `{"url":"https://github.com/openziti/tlsuv/pull/400"}`, "")
	rows, _ = st.PendingClaims()
	if len(rows) != 1 {
		t.Fatalf("pending = %+v", rows)
	}
	st.SetPRState(rows[0].ID, store.PRQueued, "", "")
	hub, owner = nil, "beta"
	s.ReconcilePRClaims()
	p, _ := st.PRByID(rows[0].ID)
	if p.Claim != "folded into beta" || p.State != store.PRAborted {
		t.Fatalf("row = claim %q state %q", p.Claim, p.State)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
