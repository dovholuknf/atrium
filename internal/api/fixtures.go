package api

import (
	"encoding/json"
	"net/http"

	"github.com/dovholuknf/atrium/internal/store"
)

// Terminals that come up with the daemon.
//
// Rows in a table, edited one at a time, like runners. The daemon owns
// starting one, so that is injected the same way launching is.

// StartFixture brings one up now rather than at boot. Supplied by the daemon,
// which owns process spawning.
var StartFixture func(id string) error

func (s *Server) getFixtures(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Fixtures()
	if err != nil {
		s.fail(w, err)
		return
	}
	if list == nil {
		list = []*store.Fixture{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"fixtures": list})
}

// putFixture creates or replaces one.
//
// The same handler for POST and PUT: the board holds the id either way, and
// two paths that do the same thing is two places for them to disagree.
func (s *Server) putFixture(w http.ResponseWriter, r *http.Request) {
	var f store.Fixture
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&f); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if id := r.PathValue("id"); id != "" {
		f.ID = id
	}
	saved, err := s.st.SaveFixture(&f)
	if err != nil {
		// A fixture with no runner is the operator's to fix, not a fault.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("fixtures", nil)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteFixture(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteFixture(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("fixtures", nil)
	w.WriteHeader(http.StatusNoContent)
}

// startFixture brings one up now, for testing a definition without restarting
// the daemon to find out whether it works.
func (s *Server) startFixture(w http.ResponseWriter, r *http.Request) {
	if StartFixture == nil {
		writeErr(w, http.StatusNotImplemented, errNoOverlays)
		return
	}
	if err := StartFixture(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
