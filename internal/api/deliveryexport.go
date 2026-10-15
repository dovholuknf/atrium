package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// deliveryExport answers the costed deliveries as they are, one JSON object per line, for the hub's item cost. The usage
// export has a card's own turns, and this has what the orchestrator spent answering them. Read only.
//
//	GET /v1/deliveries/export?since=<RFC 3339>   default seven days ago
func (s *Server) deliveryExport(w http.ResponseWriter, r *http.Request) {
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
	err := s.st.DeliveriesSince(since, func(d *store.DeliveryExport) error {
		if !started {
			w.Header().Set("Content-Type", "application/x-ndjson")
			started = true
		}
		return enc.Encode(d)
	})
	if err != nil {
		if !started {
			s.fail(w, err)
			return
		}
		log.Printf("[api] delivery export ended early: %v", err)
		return
	}
	if !started {
		w.Header().Set("Content-Type", "application/x-ndjson")
	}
}
