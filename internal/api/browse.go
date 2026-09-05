package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Picking a directory to launch a runner in.
//
// A server side browse rather than the browser's own picker, because they
// browse different machines. `showDirectoryPicker` reads the filesystem of
// whatever runs the browser, so a board open on a phone would offer the phone's
// folders. The daemon is what has to open the directory, so it lists them.

// browseEntry is one directory.
type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Repo marks a directory holding a .git, so a list of thirty folders shows
	// which few are checkouts.
	Repo bool `json:"repo"`
}

// browse lists the directories inside a path. Directories only: the launch form
// asks where to run, and a file is not an answer to that.
func (s *Server) browse(w http.ResponseWriter, r *http.Request) {
	roots := s.browseRootsFor()

	raw := r.URL.Query().Get("path")
	if strings.TrimSpace(raw) == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path": "", "parent": "", "entries": browseRootEntries(roots), "roots": true,
		})
		return
	}

	// CONTAINED, not cleaned. `filepath.Clean` collapses `..` as text and knows
	// nothing about symlinks, so it was never a containment check and
	// `internal/safepath` exists because nothing here answered the question.
	// See browseroots.go for why the picker is bounded at all.
	dir, ok := insideARoot(roots, filepath.Clean(raw))
	if !ok {
		// The same answer for outside, missing and unreadable. Distinguishing
		// them makes this an oracle for what is on the machine, which is most
		// of what an unbounded lister was giving away in the first place.
		writeErr(w, http.StatusForbidden, errOutsideRoots)
		return
	}

	info, err := os.Stat(dir)
	if err != nil {
		writeErr(w, http.StatusForbidden, errOutsideRoots)
		return
	}
	if !info.IsDir() {
		// The parent of a file is checked again rather than assumed: a root
		// can BE a file's directory, and stepping up from a root's own child
		// must not step outside it.
		up, ok := insideARoot(roots, filepath.Dir(dir))
		if !ok {
			writeErr(w, http.StatusForbidden, errOutsideRoots)
			return
		}
		dir = up
	}

	listing, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}

	entries := make([]browseEntry, 0, len(listing))
	for _, e := range listing {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// A checkout is found by its .git, not by browsing into it.
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.ToSlash(filepath.Join(dir, name))
		repo := false
		if _, err := os.Stat(filepath.Join(dir, name, ".git")); err == nil {
			repo = true
		}
		entries = append(entries, browseEntry{Name: name, Path: full, Repo: repo})
	}
	// Checkouts first, then alphabetical.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Repo != entries[j].Repo {
			return entries[i].Repo
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	// Up, as far as the roots allow. At the top of a root, `up` is the root
	// list rather than the filesystem's parent, which is the directory the
	// picker is not allowed to see.
	parent := ""
	if p := filepath.Dir(dir); !eqPath(p, dir) {
		if real, ok := insideARoot(roots, p); ok {
			parent = filepath.ToSlash(real)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    filepath.ToSlash(dir),
		"parent":  parent,
		"entries": entries,
	})
}
