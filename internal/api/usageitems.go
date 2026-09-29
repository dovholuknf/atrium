package api

import (
	"net/http"
	"time"
)

// roomUsageItems is the tokens each accepted work item cost, for the usage tab.
func (s *Server) roomUsageItems(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `{"error":"since is not an RFC 3339 time"}`, http.StatusBadRequest)
			return
		}
		since = t
	}
	out, err := s.st.AcceptedItemUsage(since)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
