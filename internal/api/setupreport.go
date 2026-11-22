package api

import (
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/setupcheck"
	"github.com/dovholuknf/atrium/internal/runnersetup"
)

// setupReport answers GET /v1/setup: the setup checks as the account this room runs as, with each runner row's own
// checks folded in. Read only. `atrium doctor` prints it and the bring-up reads the same rows.
func (s *Server) setupReport(w http.ResponseWriter, r *http.Request) {
	rep := setupcheck.Inspect(setupcheck.Env{})
	hs, err := s.st.Harnesses()
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, h := range hs {
		if a := runnersetup.For(h); a != nil && h.Enabled {
			rep.Add(runnersetup.Rows(h.ID, runnersetup.Inspect(a, s.setupEnv(h, RunnerFound(h))))...)
		}
	}
	writeJSON(w, http.StatusOK, rep)
}
