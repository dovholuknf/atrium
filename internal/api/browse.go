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
	raw := r.URL.Query().Get("path")
	if strings.TrimSpace(raw) == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path": "", "parent": "", "entries": browseRoots(), "roots": true,
		})
		return
	}

	dir := filepath.Clean(raw)
	info, err := os.Stat(dir)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
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

	parent := filepath.ToSlash(filepath.Dir(dir))
	if parent == filepath.ToSlash(dir) {
		// At the top of a drive or filesystem, so up means the root list.
		parent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    filepath.ToSlash(dir),
		"parent":  parent,
		"entries": entries,
	})
}
