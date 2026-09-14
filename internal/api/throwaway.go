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

// Keeping the work a throwaway turned out to be worth keeping.
//
// A throwaway session runs in a directory atrium made and deletes when the
// session ends, and the whole feature depends on that being safe to reach for
// without deciding anything first. It is only safe if there is a way out:
// somebody clones a repository into a throwaway, works in it for an hour, and
// then needs the directory to become an ordinary one rather than to be
// destroyed with no undo.
//
// THE MOVE HAPPENS WHEN THE SESSION ENDS, not when the button is pressed.
// Windows will not let a live process have its working directory renamed, and
// the runner's working directory IS this directory. So a promote on a running
// session writes down where it is going and the end of the session does it,
// which is the same place the delete it replaces would have happened. With
// nothing running there is no such obstacle and the move is immediate, because
// making somebody start a session to move a directory would be absurd.

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

	// A runner is holding the directory open, so the move waits for it. The
	// card stops saying it is temporary as far as the operator is concerned
	// only when the move has happened, which is why the flag stays set and the
	// destination is what changes.
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

// promoteTarget checks where the directory is being sent, before anything is
// moved.
//
// Every refusal here is a way of losing the work this call exists to save: a
// relative path resolved against whatever the daemon's own directory happens
// to be, or a destination that already holds something, which a move would
// either fail on or merge into.
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

// MoveWorktree moves a directory, and the conversations that were held against
// where it used to be.
//
// Exported because the daemon does this too: a promote asked for while a
// session was running happens once that session has exited, which is inside
// the supervisor. This side owns it because the transcripts are Claude Code's
// files and `sessions.go` is already the one place that knows where they live.
//
// A RENAME FIRST AND A COPY WHEN THAT FAILS. A temporary directory is on
// whichever volume the operating system keeps temporary files on, and the
// place work is being promoted to is usually not that volume. Rename across
// volumes fails, and a promote that only worked when the two happened to match
// would fail exactly when somebody had an hour of work in it.
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

// ForgetTranscripts deletes every conversation held against a directory.
//
// The third of the three deletions a throwaway owes, and the one that is
// invisible when it is forgotten: Claude Code keys its transcripts on the
// working directory, so deleting the directory leaves them orphaned under an
// encoded name for a path that no longer exists, one set per throwaway,
// forever.
//
// The whole project directory rather than one file, which is the difference
// from `forgetSession`. That deletes a conversation somebody picked out of a
// list; this is a directory that is about to stop existing, and every
// transcript in it belongs to it.
func ForgetTranscripts(cwd string) error {
	dir, err := projectDirFor(cwd)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// moveTranscripts makes a directory's conversations follow it.
//
// Nothing is merged. A destination that already has transcripts belongs to
// work that was done there before, and folding one session's history into
// another's is worse than leaving the old ones where they are.
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

// copyTree copies a directory recursively, for the cross-volume case.
//
// Symlinks are copied as what they point at, which is what `fs.WalkDir`
// reports without being asked to follow them, and is the right answer for the
// only tree this is ever given: one somebody has been working in for an hour.
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
