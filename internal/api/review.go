package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/dovholuknf/atrium/internal/cardreview"
	"github.com/dovholuknf/atrium/internal/store"
)

// ReviewTag is the tag of a card launched with kind review (link.ReviewTag, daemon.ReviewTag).
const ReviewTag = "atrium:review"

const maxReviewBody = 4 << 20

// GET /v1/tasks/{id}/atrium-review
func (s *Server) getReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		s.fail(w, err)
		return
	}
	v, err := (&cardreview.Service{St: s.st}).View(id)
	if errors.Is(err, store.ErrNoReview) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type reviewIn struct {
	Action string                 `json:"action"`
	Base   string                 `json:"base"`
	Num    int                    `json:"id"`
	State  string                 `json:"state"`
	Reason string                 `json:"reason"`
	Items  []cardreview.FindingIn `json:"items"`
	cardreview.FindingIn
}

// POST /v1/tasks/{id}/atrium-review
func (s *Server) postReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.st.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !slices.Contains(task.Tags, ReviewTag) {
		writeErr(w, http.StatusForbidden, errors.New("only a card launched as a review holds one"))
		return
	}
	var in reviewIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReviewBody)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}
	svc := &cardreview.Service{St: s.st}
	dir := strings.TrimSpace(task.Worktree)
	var out any
	switch in.Action {
	case "start":
		out, err = svc.Start(r.Context(), id, dir, in.Base)
	case "finding":
		out, err = svc.Finding(r.Context(), id, dir, in.FindingIn)
	case "batch":
		out, err = svc.Batch(r.Context(), id, dir, in.Items)
	case "apply":
		err = svc.Apply(id, dir, in.Num)
		out = map[string]any{"ok": err == nil}
	case "close":
		err = svc.Close(id, dir, in.Num, in.State, in.Reason)
		out = map[string]any{"ok": err == nil}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("action is start, finding, batch, apply or close"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.PublishTask(task)
	writeJSON(w, http.StatusOK, out)
}
