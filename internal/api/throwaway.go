package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Promote a throwaway by moving its directory to a permanent path.
// Wait for a running session to exit because Windows cannot rename its working
// directory while it is in use. Otherwise, move it immediately.

// promoteRequest is where the operator wants the directory to end up.
type promoteRequest struct {
	To string `json:"to"`
}

// promoteCard turns a throwaway into an ordinary card.
func (s *Server) promoteCard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.st.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !task.Throwaway {
		writeErr(w, http.StatusBadRequest, errors.New("this card is not a throwaway"))
		return
	}
	var body promoteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	to, err := promoteTarget(body.To)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// Keep the throwaway flag until the move succeeds. While the runner holds
	// the directory open, record only the destination.
	if IsSupervised != nil && IsSupervised(id) {
		if err := s.st.SetPromoteTo(id, to); err != nil {
			s.fail(w, err)
			return
		}
		t, err := s.st.Get(id)
		if err != nil {
			s.fail(w, err)
			return
		}
		s.Broadcast("task", toView(t))
		writeJSON(w, http.StatusOK, map[string]any{
			"task": toView(t), "to": to, "when": "the session ends",
		})
		return
	}

	if err := MoveWorktree(task.Worktree, to); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.Promoted(id, filepath.ToSlash(to)); err != nil {
		s.fail(w, err)
		return
	}
	t, err := s.st.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("task", toView(t))
	writeJSON(w, http.StatusOK, map[string]any{"task": toView(t), "to": to, "when": "now"})
}

// promoteTarget requires an absolute, unused destination before moving files.
func promoteTarget(raw string) (string, error) {
	to := filepath.FromSlash(strings.TrimSpace(raw))
	if to == "" {
		return "", errors.New("say where to put it")
	}
	if !filepath.IsAbs(to) {
		return "", errors.New("give the whole path, starting from the drive or the root")
	}
	to = filepath.Clean(to)
	if _, err := os.Stat(to); err == nil {
		return "", fmt.Errorf("%s already exists", to)
	}
	parent := filepath.Dir(to)
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", parent)
	}
	return to, nil
}

// MoveWorktree moves a directory and its Claude Code transcripts.
// The daemon also calls this after a running session exits. Try renaming first,
// then fall back to copying for moves across volumes.
func MoveWorktree(from, to string) error {
	from = filepath.FromSlash(strings.TrimSpace(from))
	if from == "" {
		return errors.New("that card has no directory to move")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		if err := copyTree(from, to); err != nil {
			return err
		}
		// Best effort. The copy is what mattered and a temporary directory
		// left behind is tidied by the operating system, so failing the
		// promote over it would throw away the work to report the litter.
		_ = os.RemoveAll(from)
	}
	// Best effort for the same reason: the work has moved, and transcripts
	// that did not follow cost a resume, not the directory.
	moveTranscripts(from, to)
	return nil
}

// ForgetTranscripts deletes the Claude Code project directory for this path.
// Deleting only the working directory would leave transcripts behind, since
// Claude Code stores them separately under an encoded path.
func ForgetTranscripts(cwd string) error {
	dir, err := projectDirFor(cwd)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// moveTranscripts relocates conversations without merging into an existing
// transcript directory, which may belong to earlier work at that path.
func moveTranscripts(from, to string) {
	src, err := projectDirFor(from)
	if err != nil {
		return
	}
	dst, err := projectDirFor(to)
	if err != nil {
		return
	}
	if _, err := os.Stat(src); err != nil {
		return
	}
	if _, err := os.Stat(dst); err == nil {
		return
	}
	if err := os.Rename(src, dst); err != nil {
		if err := copyTree(src, dst); err == nil {
			_ = os.RemoveAll(src)
		}
	}
}

// copyTree recursively copies the working directory for cross-volume moves.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if e.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !e.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := e.Info()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
