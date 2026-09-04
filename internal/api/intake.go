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
