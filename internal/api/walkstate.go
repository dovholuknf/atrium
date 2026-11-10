package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// maxWalkStateBytes bounds one saved walk, which is comments and threads and no diff.
const maxWalkStateBytes = 1 << 20

func walkStateKey(id string) string { return "walk." + id }

// GET /v1/tasks/{id}/walk is the walk saved for a card, `{"state": null}` when there is none.
func (s *Server) walkStateGet(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	raw, err := s.st.Setting(walkStateKey(task.ID))
	if err != nil {
		s.fail(w, err)
		return
	}
	if raw == "" {
		raw = "null"
	}
	out := map[string]any{"state": json.RawMessage(raw)}
	if prs, _ := s.st.PRsWalkedBy(task.ID); len(prs) > 0 && prs[0].URL != "" {
		out["pr_url"] = prs[0].URL
	}
	writeJSON(w, http.StatusOK, out)
}

// PUT /v1/tasks/{id}/walk replaces the walk saved for a card with the JSON object in the body.
func (s *Server) walkStatePut(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWalkStateBytes))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, errors.New("a saved walk is at most 1 MiB"))
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		writeErr(w, http.StatusBadRequest, errors.New("the walk must be a JSON object"))
		return
	}
	walkMu.Lock()
	defer walkMu.Unlock()
	if err := s.st.SetSetting(walkStateKey(task.ID), string(body)); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
