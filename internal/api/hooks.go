package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dovholuknf/atrium/internal/claudeconf"
)

// The hook wiring, as something the board can see and fix.
//
// Activity, subagent counts and the message channel are all built and all
// inert until five lines land in settings.json. That was a documentation page,
// which meant the features existed for whoever read it. Reporting what is
// missing, and offering to write it, is the difference between a feature and a
// feature somebody might one day turn on.

// atriumExe is the absolute path of the running binary, which is what gets
// written into settings.json.
//
// Not the word "atrium": settings.json is read by a session whose PATH atrium
// has no say over, and a hook that cannot be found fails silently by design.
func atriumExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolved, so a symlinked or shimmed atrium writes the real target and
	// keeps working when the link moves.
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.ToSlash(exe), nil
}

// hookStatus reports which hooks are registered. Read only: it never touches
// the file.
func (s *Server) hookStatus(w http.ResponseWriter, r *http.Request) {
	exe, err := atriumExe()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rep, err := claudeconf.Inspect(exe)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// installHooks writes hooks and reports where the old file went.
//
// A body naming events writes only those, so a single row's own button wires
// one hook. An empty body writes all of them. Deciding which of these to
// report on is the operator's, not atrium's: turning on the tool ones and
// leaving the subagent count alone is a reasonable thing to want.
//
// The board asks for confirmation first and shows what it would write, so this
// is the second half of a decision the operator already made.
func (s *Server) installHooks(w http.ResponseWriter, r *http.Request) {
	exe, err := atriumExe()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var body struct {
		Events []string `json:"events"`
	}
	// A missing or unreadable body means all of them, which is what the
	// original single button sent.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)

	rep, res, err := claudeconf.InstallOnly(exe, body.Events)
	if err != nil {
		// A settings file atrium could not parse is the main way this fails,
		// and it is worth saying plainly that nothing was changed.
		writeErr(w, http.StatusConflict, err)
		return
	}
	// So a second tab, and the chip on the runner row behind this dialog, do
	// not wait for their next poll.
	if res.Changed {
		s.Broadcast("hooks", map[string]any{"missing": rep.Missing})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"report": rep, "backup": res.Backup, "changed": res.Changed,
	})
}
