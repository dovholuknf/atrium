package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Finding a file so it can be taken back out.
//
// Bytes going IN already worked: paste, drop and upload all land under a
// card's own directory. `GET /v1/tasks/{id}/files?path=` serves one back and
// nothing called it, because nothing could say what was in there.
//
// This is that missing half, and it is deliberately NOT `browse.go`. That one
// lists the daemon's whole filesystem so a launch can pick a directory, takes
// no card, and has no containment. This takes a card and resolves everything
// through safepath against that card's worktree.
//
// **It works over an overlay for free**, which is the reason it is worth
// having at all. The download is the board's own HTTP, so whatever already
// carries the board carries this. Nothing new is published and there is no
// second transport to configure or secure.

// maxListEntries bounds one directory listing.
//
// A working directory with a `node_modules` in it has more entries than any
// person is going to read, and the answer to that is a shorter list and a note
// saying so, not a slow board.
const maxListEntries = 500

// fileEntry is one row in the panel.
type fileEntry struct {
	Name string `json:"name"`
	// Path is the full path, in the daemon's forward-slash form, ready to hand
	// straight back as `?path=` without the caller reassembling anything.
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size,omitempty"`
	Mtime string `json:"mtime,omitempty"`
}

// listFiles answers what is in one directory under a card.
func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	root := filepath.FromSlash(task.Worktree)

	// No path means the card's own directory, except that the interesting
	// place is usually the one something just wrote to.
	want := strings.TrimSpace(r.URL.Query().Get("path"))
	if want == "" {
		want = root
		incoming := filepath.Join(root, filepath.FromSlash(incomingDir))
		if fi, err := os.Stat(incoming); err == nil && fi.IsDir() {
			want = incoming
		}
	}

	dir, err := safepath.Contained(root, want)
	if err != nil {
		// One answer for outside and for missing, so this cannot be used to
		// find out what is on the machine outside the card. Same rule the
		// download endpoint follows.
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	fi, err := os.Stat(dir)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such directory"))
		return
	}
	if !fi.IsDir() {
		// A file was asked for. Answer with its directory rather than an
		// error, since clicking a file in a picker meaning "show me where that
		// is" is a reasonable thing to have meant.
		dir = filepath.Dir(dir)
	}

	raw, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, http.StatusForbidden, errors.New("cannot read that directory"))
		return
	}

	entries := make([]fileEntry, 0, len(raw))
	truncated := false
	for _, e := range raw {
		if len(entries) >= maxListEntries {
			truncated = true
			break
		}
		full := filepath.Join(dir, e.Name())
		row := fileEntry{
			Name: e.Name(),
			Path: filepath.ToSlash(full),
			Dir:  e.IsDir(),
		}
		if info, err := e.Info(); err == nil {
			if !e.IsDir() {
				row.Size = info.Size()
			}
			row.Mtime = info.ModTime().UTC().Format(time.RFC3339)
		}
		entries = append(entries, row)
	}

	// Directories first, then by name. The same order browse.go uses, because
	// two pickers in one application sorting differently is its own small
	// annoyance.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	// Where up goes, or empty at the card's own directory. Computed here so the
	// board never has to do path arithmetic, which is where a traversal would
	// come from if one were going to.
	parent := ""
	if !sameDir(dir, root) {
		if up, err := safepath.Contained(root, filepath.Dir(dir)); err == nil {
			parent = filepath.ToSlash(up)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":      filepath.ToSlash(dir),
		"root":      filepath.ToSlash(root),
		"parent":    parent,
		"entries":   entries,
		"truncated": truncated,
	})
}

// sameDir reports whether a listing has reached the card's own directory,
// which is where walking up stops.
//
// Both sides are already resolved by the time this is called: `dir` came back
// from safepath.Contained, and `root` is the card's worktree. So this is a
// comparison and not a second containment check, and writing it as one would
// read as a bug the next time somebody looks.
func sameDir(dir, root string) bool {
	clean := func(s string) string {
		return strings.ToLower(filepath.Clean(filepath.FromSlash(s)))
	}
	return clean(dir) == clean(root)
}
