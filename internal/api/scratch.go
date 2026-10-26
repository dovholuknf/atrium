package api

import (
	"net/http"
	"path/filepath"

	"github.com/dovholuknf/atrium/internal/base/scratch"
	"github.com/dovholuknf/atrium/internal/store"
)

// A card's scratch folder, as the board reads it. See internal/scratch and internal/daemon/scratch.go.

// scratchOf is the folder the room tells a card to put throwaway files in. Empty when the room has no scratch root.
func (s *Server) scratchOf(t *store.Task) string {
	root := s.st.ScratchRoot()
	if root == "" {
		return ""
	}
	if row, ok, err := s.st.CardScratchOf(t.ID); err == nil && ok {
		return row.Path
	}
	return scratch.Dir(root, t.DisplayTitle(), t.WireName, t.ID)
}

// fileRoot is the folder a file request resolves against: the card's own directory, or with `?root=scratch` the scratch
// folder atrium made for that card. Only a ledger row names it, never the request, so it stays one folder and every path
// is still checked against it by safepath.
func (s *Server) fileRoot(r *http.Request, t *store.Task) (root string, scratchRoot, ok bool) {
	if r.URL.Query().Get("root") == "scratch" {
		row, found, err := s.st.CardScratchOf(t.ID)
		if err != nil || !found {
			return "", true, false
		}
		return filepath.FromSlash(row.Path), true, true
	}
	return filepath.FromSlash(t.Worktree), false, true
}
