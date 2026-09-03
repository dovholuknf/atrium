package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dovholuknf/atrium/internal/claudeconf"
)

// The hook wiring, as something the board can see and fix. Activity, subagent
// counts and the message channel stay inert until five lines land in
// settings.json.

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
// hookStatus answers for one runner, defaulting to Claude Code.
//
// `?runner=codex` asks about that one instead. The default keeps every caller
// that predates a second runner working, and the board asks by name.
func (s *Server) hookStatus(w http.ResponseWriter, r *http.Request) {
	exe, err := atriumExe()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rep, err := claudeconf.InspectTarget(claudeconf.TargetFor(r.URL.Query().Get("runner")), exe)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// installHooks writes hooks and reports where the old file went.
//
// A body naming events writes only those; an empty body writes all of them.
// Which to report on is the operator's call: wanting the tool events without
// the subagent count is reasonable.
//
// The board confirms first and shows what it would write, so this is the
// second half of a decision already made.
func (s *Server) installHooks(w http.ResponseWriter, r *http.Request) {
	exe, err := atriumExe()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var body struct {
		Events []string `json:"events"`
		// Which runner's configuration. Empty is Claude Code, so a caller that
		// predates a second runner is unchanged.
		Runner string `json:"runner"`
	}
	// A missing or unreadable body means all of them.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)

	rep, res, err := claudeconf.InstallOnlyTarget(
		claudeconf.TargetFor(body.Runner), exe, body.Events)
	if err != nil {
		// 409 because the usual cause is a settings file atrium will not
		// rewrite, which is the operator's to fix, not a server fault.
		writeErr(w, http.StatusConflict, err)
		return
	}
	// So a second tab, and the chip on the row behind this dialog, do not wait
	// for their next poll.
	if res.Changed {
		s.Broadcast("hooks", map[string]any{"missing": rep.Missing})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"report": rep, "backup": res.Backup, "changed": res.Changed,
	})
}
