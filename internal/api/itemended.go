package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// A BACKLOG ITEM ENDING ENDS ITS CARD. The hub says an item went done, dropped or held, and this is the room's answer
// for each card attached to it. Done and dropped close the card the way atrium_close does, with nothing deleted: a dirty
// worktree is stashed (kept when the room cannot stash) and a hub branch is kept. Held exits the runner and leaves the card,
// which wakes with atrium_say wake.
//
// ONLY A CARD AN AGENT LAUNCHED FOR THAT ITEM. A card the human launched or owns, a resident, a director and the
// orchestrator are never touched, whatever the hub says, because the hub's list of attached cards is a claim.

const (
	itemEndedOriginTag = "origin:agent"
	itemEndedWorkTag   = "atrium:item:"
)

var itemEndedNever = []string{"atrium:resident", "atrium:director", "atrium:orchestrator", "atrium:park-idle", "atrium:hold-notices"}

// itemEndedSkip is why a card is left alone, or "".
func itemEndedSkip(t *store.Task, item string) string {
	switch {
	case !small.HasTag(t.Tags, itemEndedWorkTag+item):
		return "it is not attached to " + item
	case !small.HasTag(t.Tags, itemEndedOriginTag):
		return "a person launched it"
	case t.Status == store.StatusDone:
		return "it is closed already"
	}
	for _, tag := range itemEndedNever {
		if small.HasTag(t.Tags, tag) {
			return "it is " + strings.TrimPrefix(tag, "atrium:")
		}
	}
	return ""
}

// POST /v1/tasks/{id}/item-ended
func (s *Server) postItemEnded(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Item   string `json:"item"`
		Status string `json:"status"`
		By     string `json:"by"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	in.Item, in.Status = strings.TrimSpace(in.Item), strings.TrimSpace(in.Status)
	if why := itemEndedSkip(t, in.Item); why != "" {
		log.Printf("[atrium api] item %s %s: left %s open: %s", in.Item, in.Status, logLine(t.ID), why)
		writeJSON(w, http.StatusOK, map[string]any{"card": t.ID, "action": "kept", "why": why})
		return
	}
	switch in.Status {
	case "held":
		if s.StopRunnerBy == nil || !s.sessionLive(t) {
			writeJSON(w, http.StatusOK, map[string]any{"card": t.ID, "action": "kept", "why": "no runner to exit"})
			return
		}
		if err := s.StopRunnerBy(t.ID, "atrium (item held)"); err != nil {
			log.Printf("[atrium api] item %s held: could not exit %s: %v", in.Item, logLine(t.ID), err)
			writeJSON(w, http.StatusOK, map[string]any{"card": t.ID, "action": "kept", "why": err.Error()})
			return
		}
		log.Printf("[atrium api] item %s held by %s: exited the runner of %s, the card stays", in.Item, in.By, logLine(t.ID))
		writeJSON(w, http.StatusOK, map[string]any{"card": t.ID, "action": "exited"})
	case "done", "dropped":
		if _, busy := closing.LoadOrStore(t.ID, true); busy {
			writeJSON(w, http.StatusOK, map[string]any{"card": t.ID, "action": "kept", "why": "it is being closed already"})
			return
		}
		defer closing.Delete(t.ID)
		res, err := s.autoClose(context.WithoutCancel(r.Context()), t)
		if err != nil {
			s.fail(w, err)
			return
		}
		log.Printf("[atrium api] item %s %s by %s: closed %s (%d stashed, %d left)", in.Item, in.Status, in.By,
			logLine(t.ID), len(res.Stashes), res.Left)
		writeJSON(w, http.StatusOK, res)
	default:
		prError(w, http.StatusBadRequest, "bad_request", "status is done, dropped or held", nil)
	}
}

// autoClose is a confirmed close with every question answered the way that loses nothing.
func (s *Server) autoClose(ctx context.Context, t *store.Task) (*closeResult, error) {
	pv, err := s.closePreview(ctx, t)
	if err != nil {
		return nil, err
	}
	answers := map[string]string{}
	for _, a := range pv.Asks {
		answers[strconv.Itoa(a.Seq)] = CloseKeep
		if !a.Hub && pv.CanStash {
			answers[strconv.Itoa(a.Seq)] = CloseStash
		}
	}
	return s.closeCard(t, pv, answers, false, s.newCloseSteps("", t.ID)), nil
}
