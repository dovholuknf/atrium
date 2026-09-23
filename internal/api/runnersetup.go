package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/dovholuknf/atrium/internal/runnersetup"
	"github.com/dovholuknf/atrium/internal/store"
)

// A runner's setup, as something the board can see and fix. The report rides
// on `GET /v1/harnesses`, and this is the fix half. See
// docs/runner-setup-design.md.

// setupEnv is what a check may look at for one row on this room.
func (s *Server) setupEnv(h *store.Harness, found string) runnersetup.Env {
	env := runnersetup.Env{
		RowEnv:  h.Env,
		Prepare: strings.TrimSpace(h.Prepare) != "",
		Exe:     found,
	}
	if home, err := os.UserHomeDir(); err == nil {
		env.Home = home
	}
	if ps, err := s.st.Providers(); err == nil {
		env.Roots = runnersetup.WorkspaceRoots(ps)
	}
	if exe, err := atriumExe(); err == nil {
		env.AtriumExe = exe
	}
	return env
}

// fixRunnerSetup applies one check's fix and answers the new report.
//
// The board confirms first and names the file, so this is the second half of
// a decision already made. A check atrium only explains, or a target the check
// did not offer, is refused with 409 rather than applied.
func (s *Server) fixRunnerSetup(w http.ResponseWriter, r *http.Request) {
	h, err := s.st.Harness(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such runner"))
		return
	}
	a := runnersetup.For(h)
	if a == nil {
		writeErr(w, http.StatusConflict, errors.New("atrium has no setup checks for this runner"))
		return
	}
	var body struct {
		Check  string `json:"check"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	env := s.setupEnv(h, RunnerFound(h))
	res, err := runnersetup.Fix(a, env, body.Check, body.Target)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	rep := runnersetup.Inspect(a, env)
	if res.Changed {
		s.Broadcast("harnesses", map[string]any{"id": h.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"report": rep, "changed": res.Changed, "path": res.Path, "backup": res.Backup,
	})
}
