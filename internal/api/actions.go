package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/store"
)

// Actions on a card, written by the operator.
//
// Configuration only. Running one belongs to the daemon, which owns the
// terminals and the message queue.

func (s *Server) listActions(w http.ResponseWriter, r *http.Request) {
	actions, err := s.st.CardActions()
	if err != nil {
		s.fail(w, err)
		return
	}
	if actions == nil {
		actions = []*store.CardAction{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) saveAction(w http.ResponseWriter, r *http.Request) {
	var a store.CardAction
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&a); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// `new` in the path means "give this one an id". A caller that had to mint
	// one would be inventing a key format atrium owns.
	if id := r.PathValue("id"); id != "new" {
		a.ID = id
	} else {
		a.ID = ""
	}
	saved, err := s.st.SaveCardAction(a)
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("actions", saved)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteAction(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteCardAction(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("actions", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}
