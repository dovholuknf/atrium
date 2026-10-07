package api

import (
	"net/http"
	"strconv"
	"time"
)

// roomTurnCost is what the turns atrium caused cost, by the kind of text that caused them: by day, card and kind,
// the costliest few, and the ones that came to nothing. Read off the delivery rows the daemon costs at each Stop.
// The dollar figure is an estimate. See internal/store/delivery.go.
func (s *Server) roomTurnCost(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	until := time.Now().UTC()
	since := until.Add(-7 * 24 * time.Hour)
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `{"error":"since is not an RFC 3339 time"}`, http.StatusBadRequest)
			return
		}
		since = t
	}
	top := 10
	if v := q.Get("top"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			top = n
		}
	}
	out, err := s.st.DeliveryCosts(since, until, top)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
