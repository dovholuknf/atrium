package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/store"
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
	changed := false
	for name, n := range body.Counts {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > usageControlMax || !utf8.ValidString(name) || n < 1 || n > usageCountMax {
			continue
		}
		if err := s.st.AddBoardUsage(name, day, n); err != nil {
			s.fail(w, err)
			return
		}
		changed = true
	}
	if changed {
		s.usageSnap.drop()
	}
	w.WriteHeader(http.StatusNoContent)
}

// boardUsageSnap is the totals as of the last write. Reading them is a GROUP BY over every day of every control, which
// is what made "controls used" slow to open, so it is done once per batch that changed something and not once per
// request. A write drops it and the next read rebuilds it.
type boardUsageSnap struct {
	mu   sync.Mutex
	rows []store.BoardUsage
	ok   bool
}

func (b *boardUsageSnap) drop() {
	b.mu.Lock()
	b.ok = false
	b.mu.Unlock()
}

func (b *boardUsageSnap) get(read func() ([]store.BoardUsage, error)) ([]store.BoardUsage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.ok {
		rows, err := read()
		if err != nil {
			return nil, err
		}
		b.rows, b.ok = rows, true
	}
	return b.rows, nil
}

// getBoardUsage is every control ever used, most used first, for the "controls used" view. Served from the snapshot.
func (s *Server) getBoardUsage(w http.ResponseWriter, r *http.Request) {
	rows, err := s.usageSnap.get(s.st.BoardUsageTotals)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"controls": rows})
}
