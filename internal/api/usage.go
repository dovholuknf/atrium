package api

import (
	"net/http"
	"strconv"
)

// A card's token use on record, for its details and nowhere else. The daemon
// owns the reading and the rows. See internal/daemon/usage.go.
func (s *Server) cardUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.Get(id); err != nil {
		s.fail(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := s.UsageOf(id, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
