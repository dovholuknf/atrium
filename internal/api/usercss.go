package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/safepath"
)

// userCSSLink is the line index.html and m/index.html carry, and the one `?safe` leaves out.
const userCSSLink = `<link rel="stylesheet" href="/user.css">`

// userCSSPath is <state>/board/user.css, or empty when the room has no state folder.
func (s *Server) userCSSPath() string {
	if strings.TrimSpace(s.StateDir) == "" {
		return ""
	}
	return filepath.Join(s.StateDir, "board", "user.css")
}

// userCSS serves the operator's own stylesheet. Missing is an empty 200, so the board never shows a broken link.
func (s *Server) userCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	p := s.userCSSPath()
	if p == "" {
		return
	}
	real, err := safepath.Contained(s.StateDir, p)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	raw, err := os.ReadFile(real)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	_, _ = w.Write(raw)
}

// servedSafe answers a board page asked for with `?safe` with the user.css link taken out, reporting whether it did.
func servedSafe(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	if !r.URL.Query().Has("safe") || !strings.HasSuffix(name, ".html") {
		return false
	}
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	_, _ = w.Write([]byte(strings.Replace(string(raw), userCSSLink, "", 1)))
	return true
}
