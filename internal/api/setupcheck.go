package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

// The room's setup checklist, which the hub reads beside `/v1/hooks` and puts in the rooms pill's tooltip. Only what
// failed is listed, one line each. See internal/link/setupwatch.go.

// UsageFacts is how many runners are live and when a status line last posted a 5h or weekly figure. Supplied by the
// daemon, which holds telemetry in memory.
var UsageFacts func() (live int, at time.Time)

var apiStart = time.Now()

// usagePostWait is how long a live runner may go without a usage post before the status line is called silent.
const usagePostWait = 15 * time.Minute

// checkIssue is one failed item. Fix says atrium can fix it itself, with `POST /v1/setup-check/fix`.
type checkIssue struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Fix  bool   `json:"fix,omitempty"`
}

func (s *Server) setupCheck(w http.ResponseWriter, r *http.Request) {
	issues := []checkIssue{}
	if home, err := os.UserHomeDir(); err == nil {
		if exe, err := atriumExe(); err == nil {
			for _, text := range claudeconf.StatusLineIssues(home, exe) {
				issues = append(issues, checkIssue{ID: "statusline", Text: text, Fix: true})
			}
		}
	}
	if UsageFacts != nil {
		live, at := UsageFacts()
		if at.Before(apiStart) {
			at = apiStart
		}
		if live > 0 && time.Since(at) > usagePostWait {
			issues = append(issues, checkIssue{ID: "usage", Text: "status line is posting no usage: no 5h or weekly figure reached the room in 15 minutes"})
		}
	}
	if _, err := exec.LookPath("git"); err != nil {
		issues = append(issues, checkIssue{ID: "git", Text: "git is not on the room's PATH"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

// setupFix is `POST /v1/setup-check/fix` with {"id": "..."}: fixes the one item it names, and only an item that says it
// can be fixed.
func (s *Server) setupFix(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	if body.ID != "statusline" {
		writeErr(w, http.StatusBadRequest, errors.New("atrium cannot fix "+body.ID+" itself"))
		return
	}
	home, err := os.UserHomeDir()
	if err == nil {
		var exe string
		if exe, err = atriumExe(); err == nil {
			var backup string
			if backup, err = claudeconf.InstallStatusLine(home, exe); err == nil {
				writeJSON(w, http.StatusOK, map[string]any{"backup": backup})
				return
			}
		}
	}
	writeErr(w, http.StatusConflict, err)
}
