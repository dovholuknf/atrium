package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// Settings that belong to the board rather than to any one card.

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"global_auto": s.st.GlobalAuto(),
	})
}

// setSettings takes whichever settings are present in the body and leaves the
// rest alone, so a caller that knows about one does not have to send them all.
func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GlobalAuto *bool `json:"global_auto"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	drained := 0
	if body.GlobalAuto != nil {
		if err := s.st.SetGlobalAuto(*body.GlobalAuto); err != nil {
			s.fail(w, err)
			return
		}
		// Turning it on empties the queue it was turned on because of. The
		// chain only runs when a request arrives, so anything already waiting
		// had asked before the switch existed and would sit there under a
		// header saying nothing will stop to ask.
		if *body.GlobalAuto && s.DrainAuto != nil {
			n, err := s.DrainAuto()
			if err != nil {
				// Not fatal. The setting is written and every later request is
				// approved, so reporting a failure here would undersell what
				// did happen.
				log.Printf("[atrium] could not drain the queue: %v", err)
			}
			drained = n
		}
		// Every open board, so a switch this broad is never on in one tab and
		// off in another.
		s.Broadcast("settings", map[string]any{"global_auto": *body.GlobalAuto})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"global_auto": s.st.GlobalAuto(), "drained": drained,
	})
}
