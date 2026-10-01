package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ChangesError is a refusal the daemon wants answered with a status and a sentence.
type ChangesError struct {
	Status int
	Msg    string
}

func (e *ChangesError) Error() string { return e.Msg }

// cardChanges is what a card's worktree has changed (docs/rnd/changes-view-design.md,
// section 1). `?against=head|base` picks the comparison and `?turn=<at>` asks for one
// reply's edits instead, and the two together are refused.
//
// IT TAKES NO PATH. Any query parameter but those two is a 400, so the endpoint cannot be
// pointed at a directory or a file the card does not own.
func (s *Server) cardChanges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, NotOnRoom(id, s.Room))
			return
		}
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	for k := range q {
		if k != "against" && k != "turn" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown parameter %q: this takes against and turn, and no path", k))
			return
		}
	}
	against, rawTurn := q.Get("against"), q.Get("turn")
	if against != "" && rawTurn != "" {
		writeErr(w, http.StatusBadRequest, errors.New("against and turn cannot be used together"))
		return
	}
	switch against {
	case "", "head", "base":
	default:
		writeErr(w, http.StatusBadRequest, errors.New("against must be head or base"))
		return
	}
	var turn time.Time
	if rawTurn != "" {
		var perr error
		if turn, perr = time.Parse(time.RFC3339Nano, rawTurn); perr != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("turn must be the at of a reply, an RFC3339 time: %w", perr))
			return
		}
	}
	v, err := s.Changes(r.Context(), id, against, turn)
	if err != nil {
		var ce *ChangesError
		switch {
		case errors.As(err, &ce):
			writeErr(w, ce.Status, ce)
		case errors.Is(err, sql.ErrNoRows):
			writeErr(w, http.StatusNotFound, NotOnRoom(id, s.Room))
		default:
			s.fail(w, err)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}
