package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A room asks the hub to claim a PR's key before it makes a row, so two rooms that are given one PR make one row and
// one run. See docs/rnd/scm-forge-design.md section 6 and internal/link/prclaim.go for the hub's side.

// PRClaimAsk is what a room says to the hub. Held says this room already has a live row for the key, so the hub
// records it as the owner and does not place the key elsewhere.
type PRClaimAsk struct {
	Key    string `json:"key"`
	URL    string `json:"url,omitempty"`
	Why    string `json:"why,omitempty"`
	Head   string `json:"head,omitempty"`
	Source string `json:"source,omitempty"`
	Held   bool   `json:"held,omitempty"`
}

// PRClaimReply is the hub's answer. Mine is whether this room owns the key. Forward is the owner's own answer to the
// paste when the hub handed it over.
type PRClaimReply struct {
	Owner         string
	Mine          bool
	Forwarded     bool
	ForwardStatus int
	Forward       json.RawMessage
}

// prClaimWait bounds the ask. A hub that does not answer is the same as no hub.
const prClaimWait = 45 * time.Second

// claimPR asks the hub. It returns the claim to record on a row and, when the key belongs to another room, true
// with the answer already written.
func (s *Server) claimPR(w http.ResponseWriter, r *http.Request, ask PRClaimAsk) (claim string, answered bool) {
	if s.ClaimPR == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(r.Context(), prClaimWait)
	defer cancel()
	rep, err := s.ClaimPR(ctx, ask)
	if err != nil {
		return "pending", false
	}
	if rep.Mine {
		return "claimed", false
	}
	body := map[string]any{"created": false, "key": ask.Key, "held_by": rep.Owner,
		"claim": "folded into " + rep.Owner}
	if rep.Forwarded {
		body["owner_status"] = rep.ForwardStatus
		if len(rep.Forward) > 0 {
			body["owner_answer"] = rep.Forward
		}
	}
	writeJSON(w, http.StatusOK, body)
	return "", true
}

// ReconcilePRClaims asks the hub about every row made while it could not be reached. It runs when the room attaches
// again, and nothing else calls it: no timer. A key another room claimed first folds this row into that room's: a
// row that has not started is aborted, and one that has is left to finish and labelled, because deleting a finished
// review is worse than showing two.
func (s *Server) ReconcilePRClaims() {
	if s.ClaimPR == nil {
		return
	}
	rows, err := s.st.PendingClaims()
	if err != nil {
		return
	}
	for _, p := range rows {
		ctx, cancel := context.WithTimeout(context.Background(), prClaimWait)
		rep, err := s.ClaimPR(ctx, PRClaimAsk{Key: store.PRKey(p.Host, p.Org, p.Repo, p.Number), Held: true})
		cancel()
		if err != nil {
			return
		}
		if rep.Mine {
			_ = s.st.SetPRClaim(p.ID, "claimed")
			continue
		}
		_ = s.st.SetPRClaim(p.ID, "folded into "+rep.Owner)
		if p.State == store.PRQueued {
			prOpMu.Lock()
			s.prRunner().Abort(p.ID)
			_, _ = s.st.MovePR(p.ID, []string{store.PRQueued}, store.PRAborted, "", "folded into "+strings.TrimSpace(rep.Owner))
			prOpMu.Unlock()
		}
		s.PublishPR(p.ID)
	}
}
