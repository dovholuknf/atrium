package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/persona"
	"github.com/dovholuknf/atrium/internal/store"
)

// The persona pack, as the board sees it: the catalog on the runners page, and
// the lessons view. See docs/personas-design.md. The launch lives in the
// daemon, which owns process spawning, and is reached through PersonaReview.

// PersonaRunsDir is where persona run directories go, beside the database.
// Set by the daemon. Read here only for each persona's last run.
var PersonaRunsDir string

// personaPack is the configured pack path, empty when the feature is off.
func (s *Server) personaPack() (string, error) {
	v, err := s.st.Setting(store.SettingPersonaPackPath)
	return strings.TrimSpace(v), err
}

func (s *Server) listPersonas(w http.ResponseWriter, r *http.Request) {
	pack, err := s.personaPack()
	if err != nil {
		s.fail(w, err)
		return
	}
	// OFF IS AN ANSWER, not an error. The pane says how to turn it on.
	if pack == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"pack": "", "personas": []any{}, "setting": store.SettingPersonaPackPath,
		})
		return
	}
	list, err := persona.Catalog(pack, PersonaRunsDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"pack": pack, "personas": []any{}, "setting": store.SettingPersonaPackPath, "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pack": pack, "personas": list, "setting": store.SettingPersonaPackPath,
	})
}

func (s *Server) personaLessons(w http.ResponseWriter, r *http.Request) {
	pack, err := s.personaPack()
	if err != nil {
		s.fail(w, err)
		return
	}
	if pack == "" {
		writeErr(w, http.StatusConflict, errors.New("no persona pack is configured. set "+store.SettingPersonaPackPath))
		return
	}
	rev, err := s.lessons.Read(pack, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

// decideLesson is one promote, keep or delete. It edits the dotagents working
// tree and nothing else, and never commits.
func (s *Server) decideLesson(w http.ResponseWriter, r *http.Request) {
	pack, err := s.personaPack()
	if err != nil {
		s.fail(w, err)
		return
	}
	if pack == "" {
		writeErr(w, http.StatusConflict, errors.New("no persona pack is configured. set "+store.SettingPersonaPackPath))
		return
	}
	var d persona.Decision
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&d); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rev, err := s.lessons.Apply(pack, r.PathValue("id"), d)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

// reviewWithPersona launches a persona at a card's diff.
func (s *Server) reviewWithPersona(w http.ResponseWriter, r *http.Request) {
	if s.PersonaReview == nil {
		writeErr(w, http.StatusNotImplemented, errors.New("this build cannot launch a persona"))
		return
	}
	var in struct {
		Persona string `json:"persona"`
		Runner  string `json:"runner"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Runner) == "" {
		in.Runner = "claude"
	}
	t, err := s.PersonaReview(r.PathValue("id"), in.Persona, in.Runner)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// PersonaReviewFunc starts a persona reviewing a card. Supplied by the daemon.
type PersonaReviewFunc func(taskID, personaID, runner string) (*store.Task, error)
