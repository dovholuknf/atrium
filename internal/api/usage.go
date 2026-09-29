package api

import (
	"net/http"
	"strconv"
	"time"
)

// roomUsage is the whole room's spend in buckets, for the usage tab. Summed in
// SQL and bounded there, so the answer is a few hundred buckets at most. The
// card titles are not here: the board has the card list already.
func (s *Server) roomUsage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	until := time.Now().UTC()
	since := until.Add(-time.Hour)
	if v := q.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `{"error":"since is not an RFC 3339 time"}`, http.StatusBadRequest)
			return
		}
		since = t
	}
	bucket := 60
	if v := q.Get("bucket"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, `{"error":"bucket is a number of seconds, at least 1"}`, http.StatusBadRequest)
			return
		}
		bucket = n
	}
	out, err := s.st.UsageBuckets(since, until, bucket, q.Get("card"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

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
