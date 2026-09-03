package api

import (
	"encoding/json"
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
	if body.GlobalAuto != nil {
		if err := s.st.SetGlobalAuto(*body.GlobalAuto); err != nil {
			s.fail(w, err)
			return
		}
		// Every open board, so a switch this broad is never on in one tab and
		// off in another.
		s.Broadcast("settings", map[string]any{"global_auto": *body.GlobalAuto})
	}
	writeJSON(w, http.StatusOK, map[string]any{"global_auto": s.st.GlobalAuto()})
}
