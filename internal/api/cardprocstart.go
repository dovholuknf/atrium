package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A PROCESS THE ROOM STARTS FOR A CARD. Design: docs/rnd/card-lifecycle-design.md section 9. Item r-card-procs-ports.
//
//	POST /v1/tasks/{id}/proc {argv, cwd, name}   run argv in the background, held on the card as a proc
//
// It is atrium_own's proc made in one step: the room starts the process in a group of its own, reads its start time
// and records it, so the pid is never unowned. Its life is the card's, not the runner's. Closing the card stops it.
// Output goes to a log named for the card's short id, so two cards never share one.

// cardShortID is the first eight characters of a card's id, without the room's tag.
func cardShortID(id string) string {
	if i := strings.LastIndex(id, "~"); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

// procLogDir is where a card's started processes write, one folder under the system temp for every card.
func procLogDir() string { return filepath.Join(os.TempDir(), "atrium-card-procs") }

// procLogName is a log file name made only of what is safe in one: the card's short id and the name given.
func procLogName(card, name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	n := b.String()
	if len(n) > 32 {
		n = n[:32]
	}
	if n == "" {
		n = "proc"
	}
	return cardShortID(card) + "-" + n + ".log"
}

// POST /v1/tasks/{id}/proc
func (s *Server) postCardProc(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var in struct {
		Argv []string `json:"argv"`
		Cwd  string   `json:"cwd"`
		Name string   `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(in.Argv) == 0 || strings.TrimSpace(in.Argv[0]) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("argv is the program and its arguments"))
		return
	}
	dir, err := s.procDir(t, in.Cwd)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bin, err := exec.LookPath(in.Argv[0])
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("%s: %v", in.Argv[0], err))
		return
	}
	if err := os.MkdirAll(procLogDir(), 0o700); err != nil {
		s.fail(w, err)
		return
	}
	logFile := filepath.Join(procLogDir(), procLogName(t.ID, in.Name))
	pid, seq, err := s.startOwned(t.ID, dir, logFile, bin, in.Argv[1:]...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.publishTaskID(t.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"pid": pid, "seq": seq, "log": filepath.ToSlash(logFile),
		"cwd": filepath.ToSlash(dir)})
}

// procDir is where a card's process runs: its worktree by default, else a folder inside it or the scratch folder.
func (s *Server) procDir(t *store.Task, want string) (string, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		want = t.Worktree
	}
	if want == "" || !filepath.IsAbs(filepath.FromSlash(want)) {
		return "", errors.New("the card has no worktree, so say an absolute cwd inside the scratch folder")
	}
	for _, root := range []string{t.Worktree, s.scratchRoot()} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		real, err := safepath.Contained(filepath.FromSlash(root), filepath.FromSlash(want))
		if err != nil {
			continue
		}
		if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
			return "", errors.New(want + " is not a directory")
		}
		return real, nil
	}
	return "", errors.New("a card runs a process only inside its own worktree or the scratch folder")
}
