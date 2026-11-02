package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// A TANGENT is a named scratch folder and a card in it, no repo. `<git.scm_root>/scratch/tangents/<name>`. The folder
// is not on the card's inventory, so a close leaves it, and the next tangent of that name works in it again.

// tangentHost is the scratch folder's host segment for a tangent.
const tangentHost = "tangents"

type tangentRequest struct {
	Name    string `json:"name"`
	Harness string `json:"harness"`
	Prompt  string `json:"prompt"`
	Why     string `json:"why"`
}

var tangentUnsafe = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// tangentName is name as a folder name: every run of other characters is one dash, and no leading dot or dash.
func tangentName(name string) string {
	n := tangentUnsafe.ReplaceAllString(strings.TrimSpace(name), "-")
	n = strings.Trim(n, "-.")
	if len(n) > 64 {
		n = strings.TrimRight(n[:64], "-.")
	}
	return n
}

// tangent makes or reuses the folder and starts a card in it, or answers the card already there.
func (s *Server) tangent(w http.ResponseWriter, r *http.Request) {
	var in tangentRequest
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := tangentName(in.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("a tangent needs a name with a letter or a digit in it"))
		return
	}
	dir, err := s.scratchFolder(tangentHost, name, true)
	if err != nil {
		worktreeFail(w, http.StatusBadRequest, err)
		return
	}
	if s.Launch == nil {
		writeErr(w, http.StatusNotImplemented, errors.New("no launcher wired"))
		return
	}
	body, _ := json.Marshal(map[string]any{
		"harness":    strings.TrimSpace(in.Harness),
		"cwd":        dir,
		"title":      name,
		"why":        in.Why,
		"prompt":     in.Prompt,
		"window":     "tangent",
		"tags":       []string{"tangent"},
		"if_running": "skip",
	})
	task, err := s.Launch(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.PublishTask(task)
	writeJSON(w, http.StatusOK, s.toView(task))
}
