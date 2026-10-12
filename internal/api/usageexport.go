package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// usageExport answers the usage rows as they are, one JSON object per line, for the hub's atrium_export. The charts
// read buckets and this reads rows. Read-only, and the same rows the room's other usage routes read.
//
//	GET /v1/usage/export?since=<RFC 3339>   default seven days ago
func (s *Server) usageExport(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Add(-7 * 24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, `{"error":"since is not an RFC 3339 time"}`, http.StatusBadRequest)
			return
		}
		since = t
	}
	enc := json.NewEncoder(w)
	started := false
	err := s.st.SessionUsageSince(since, func(u *store.SessionUsage) error {
		if !started {
			w.Header().Set("Content-Type", "application/x-ndjson")
			started = true
		}
		return enc.Encode(u)
	})
	if err != nil {
		if !started {
			s.fail(w, err)
			return
		}
		log.Printf("[api] usage export ended early: %v", err)
		return
	}
	if !started {
		w.Header().Set("Content-Type", "application/x-ndjson")
	}
}
