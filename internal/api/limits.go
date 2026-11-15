package api

import (
	"net/http"
	"time"
)

// roomLimits is the limit readings the room kept between two times, for the usage
// tab's first paint and its past pages. New readings arrive on the telemetry the
// board already gets.
func (s *Server) roomLimits(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Add(-8 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `{"error":"since is not an RFC 3339 time"}`, http.StatusBadRequest)
			return
		}
		since = t
	}
	var until time.Time
	if v := r.URL.Query().Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `{"error":"until is not an RFC 3339 time"}`, http.StatusBadRequest)
			return
		}
		until = t
	}
	readings, err := s.st.LimitReadingsIn(since, until)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"readings": readings})
}
