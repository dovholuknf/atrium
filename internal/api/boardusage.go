package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// The board counts clicks on its own controls and sends the counts here in batches. See js/usage-board.js.
const (
	usageBodyMax    = 64 << 10
	usageControlMax = 200
	usageCountMax   = 100000
)

// postBoardUsage adds a batch of counts to today. Best effort: a body that is too big, not JSON or not counts is dropped
// with a 204, and so is any single entry that is malformed, so the board never has an error to show.
func (s *Server) postBoardUsage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Counts map[string]int `json:"counts"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, usageBodyMax+1))
	if err != nil || len(raw) > usageBodyMax || json.Unmarshal(raw, &body) != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	day := time.Now().Format("2006-01-02")
	for name, n := range body.Counts {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > usageControlMax || !utf8.ValidString(name) || n < 1 || n > usageCountMax {
			continue
		}
		if err := s.st.AddBoardUsage(name, day, n); err != nil {
			s.fail(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// getBoardUsage is every control ever used, most used first, for the "controls used" view.
func (s *Server) getBoardUsage(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.BoardUsageTotals()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"controls": rows})
}
