package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/store"
)

// The inbox atrium owns and does not fill.
//
// One endpoint, one shape, and no knowledge of any external system. A hand
// written script, a poller, a CI job and a future forum peer all post the same
// item, and atrium renders `source` as a badge without ever learning what it
// means. See docs/intake-design.md.

// maxIntakeBody bounds one post.
//
// Generous for a batch of items and nowhere near enough for somebody piping a
// log file in. A source that has more than this to say on one tick is
// reporting a repository, not a work queue.
const maxIntakeBody = 4 << 20

// intake records one item, or a batch of them.
//
// Both shapes are accepted because a shell script producing one item should
// not have to wrap it in brackets, and `gh issue list --json` produces an
// array. Deciding which is which by the first non-space byte rather than by a
// flag, since the caller already knows what it sent and should not have to say
// so twice.
func (s *Server) intake(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxIntakeBody))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	items, err := decodeIntake(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(items) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no items were posted"))
		return
	}

	// Each item answers for itself. One malformed entry in a batch of forty
	// must not discard the thirty nine that were fine, because the source that
	// produced it will send the same forty again next tick and the operator
	// would never see any of them.
	type result struct {
		ExternalID string `json:"external_id"`
		TaskID     string `json:"task_id,omitempty"`
		Created    bool   `json:"created"`
		Error      string `json:"error,omitempty"`
	}
	out := make([]result, 0, len(items))
	created := 0
	for _, item := range items {
		task, isNew, err := s.st.Offer(item)
		if err != nil {
			out = append(out, result{ExternalID: item.ExternalID, Error: err.Error()})
			continue
		}
		if isNew {
			created++
			s.Broadcast("task", toView(task))
		}
		out = append(out, result{
			ExternalID: item.ExternalID, TaskID: task.ID, Created: isNew,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"offered": len(items), "created": created, "items": out,
	})
}

// decodeIntake reads either one item or an array of them.
func decodeIntake(raw []byte) ([]store.IntakeItem, error) {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '[':
			var items []store.IntakeItem
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
			return items, nil
		default:
			var item store.IntakeItem
			if err := json.Unmarshal(raw, &item); err != nil {
				return nil, err
			}
			return []store.IntakeItem{item}, nil
		}
	}
	return nil, fmt.Errorf("the body was empty")
}

// listOffered returns the inbox.
func (s *Server) listOffered(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.st.Offered()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": toViews(tasks)})
}

// The sources: commands atrium runs on a timer to find work.
//
// Configuration and reporting only. Atrium holds an argv and an interval, and
// the settings screen has to say what that means: a source is a command run as
// the daemon's user with the daemon's environment, which is exactly as trusted
// as a harness and no more.

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.st.Sources()
	if err != nil {
		s.fail(w, err)
		return
	}
	if sources == nil {
		sources = []*store.Source{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Server) saveSource(w http.ResponseWriter, r *http.Request) {
	var src store.Source
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&src); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	src.ID = r.PathValue("id")
	saved, err := s.st.SaveSource(src)
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("sources", saved)
	writeJSON(w, http.StatusOK, saved)
}

// deleteSource removes a source. The cards it raised stay, because they are
// work, and deleting the thing that found it does not make it not work.
func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteSource(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("sources", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// runSource runs one now, without waiting for its interval.
//
// The reason this exists is that a source is a script somebody just wrote, and
// the question they have is whether it works. Waiting fifteen minutes to find
// out that a path was wrong is how a feature goes unused.
//
// It answers with what happened rather than just accepting, because "it ran
// and found nothing" and "it ran and could not start" are the two answers and
// they need telling apart.
func (s *Server) runSourceNow(w http.ResponseWriter, r *http.Request) {
	if s.RunSource == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no scheduler wired"))
		return
	}
	created, runErr := s.RunSource(r.PathValue("id"))
	src, err := s.st.SourceByID(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("sources", src)
	out := map[string]any{"created": created, "source": src}
	if runErr != nil {
		out["error"] = runErr.Error()
	}
	writeJSON(w, http.StatusOK, out)
}
