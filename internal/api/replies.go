package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
)

// cardReplies is a card's last few replies as text, for the phone's
// conversation page at /m (r-024, M2 in docs/backlog/ui/mobile-design.md).
// `?n=` asks for how many, 3 by default and at most 10. The daemon reads them
// from the transcript, or from the screen for a card with no readable one.
func (s *Server) cardReplies(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, NotOnRoom(id, s.Room))
			return
		}
		s.fail(w, err)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	v, err := s.Replies(id, n)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, NotOnRoom(id, s.Room))
			return
		}
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}
