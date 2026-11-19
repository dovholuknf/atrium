package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// The folders `atrium adopt` put on this room's repo list, a JSON array of paths. No card is made and nothing starts.
const adoptedKey = "adopted_dirs"

type adoptResult struct {
	Dir    string `json:"dir"`
	Status string `json:"status"`
	Branch string `json:"branch,omitempty"`
	Why    string `json:"why,omitempty"`
}

func (s *Server) adoptedDirs() []string {
	if s.st == nil {
		return nil
	}
	raw, err := s.st.Setting(adoptedKey)
	if err != nil || raw == "" {
		return nil
	}
	var dirs []string
	if json.Unmarshal([]byte(raw), &dirs) != nil {
		return nil
	}
	return dirs
}

func (s *Server) adopt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Dirs []string `json:"dirs"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if s.st == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("no store"))
		return
	}
	have := s.adoptedDirs()
	known := map[string]bool{}
	for _, d := range have {
		known[strings.ToLower(d)] = true
	}
	for _, c := range s.checkoutList() {
		known[strings.ToLower(c.Path)] = true
	}
	out := make([]adoptResult, 0, len(in.Dirs))
	added := false
	for _, d := range in.Dirs {
		f, ok := readRepoFacts(d)
		top := filepath.ToSlash(f.top)
		switch {
		case !ok:
			out = append(out, adoptResult{Dir: d, Status: "skipped", Why: "not a git repository or worktree"})
		case known[strings.ToLower(top)]:
			out = append(out, adoptResult{Dir: top, Status: "already", Branch: f.branch})
		default:
			known[strings.ToLower(top)] = true
			have = append(have, top)
			added = true
			out = append(out, adoptResult{Dir: top, Status: "adopted", Branch: f.branch})
		}
	}
	if added {
		raw, _ := json.Marshal(have)
		if err := s.st.SetSetting(adoptedKey, string(raw)); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.refreshCheckouts()
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

func adoptedUses(dirs []string) []dirUse {
	uses := make([]dirUse, 0, len(dirs))
	for _, d := range dirs {
		uses = append(uses, dirUse{d, time.Time{}})
	}
	return uses
}
