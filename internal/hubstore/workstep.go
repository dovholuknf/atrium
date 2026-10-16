package hubstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// THE DERIVED STEPS OF AN ITEM, as the hub last read them (item lifecycle L3: landed, deployed, pushed). A cache, written
// by the derivation and never by a card: a bug in the rule is fixed by running it again. See
// docs/rnd/item-lifecycle-design.md sections 2.4 and 4.1.

// The states of a step.
const (
	StepPending = "pending"
	StepActive  = "active"
	StepBlocked = "blocked"
	StepDone    = "done"
	StepStale   = "stale"
	StepSkipped = "skipped"
)

// WorkStepRow is one step of one item.
type WorkStepRow struct {
	BacklogID string `json:"backlog_id"`
	Step      string `json:"step"`
	State     string `json:"state"`
	// Party is who the step waits on while it is active: the human, or the cards on it. Only the step engine (L2) says.
	Party     string `json:"party,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	SHA       string `json:"sha,omitempty"`
	// Cards is a JSON list of the cards that did the step, `[]` for none.
	Cards string `json:"cards"`
	// Reason is the words for a step that is skipped, or active for a stated cause (the rooms still on an older build).
	Reason string `json:"reason,omitempty"`
	// Tag is how strong the evidence is: `observed` when atrium saw it, `reported` when a card said so, empty before any.
	Tag string `json:"tag,omitempty"`
}

// Evidence tags.
const (
	TagObserved = "observed"
	TagReported = "reported"
)

// PutWorkStep writes a step, keeping the time it started and replacing the rest. The same row again changes nothing
// that a reader can see, so a derivation that runs on every collect is safe to repeat.
func (s *Store) PutWorkStep(r WorkStepRow) error {
	if strings.TrimSpace(r.Cards) == "" {
		r.Cards = "[]"
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO work_step (backlog_id, step, state, party, started_at, ended_at, sha, cards, reason, tag)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (backlog_id, step) DO UPDATE SET state = excluded.state, party = excluded.party,
				started_at = CASE WHEN work_step.started_at = '' THEN excluded.started_at ELSE work_step.started_at END,
				ended_at = CASE WHEN excluded.state IN ('done','skipped') AND work_step.state = excluded.state
					AND work_step.ended_at <> '' THEN work_step.ended_at ELSE excluded.ended_at END, sha = excluded.sha, cards = excluded.cards, reason = excluded.reason, tag = excluded.tag`,
			r.BacklogID, r.Step, r.State, r.Party, r.StartedAt, r.EndedAt, r.SHA, r.Cards, r.Reason, r.Tag)
		return err
	})
}

// WorkSteps is the stored steps of an item, in the order they were first written.
func (s *Store) WorkSteps(id string) ([]WorkStepRow, error) {
	var out []WorkStepRow
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT backlog_id, step, state, party, started_at, ended_at, sha, cards, reason, tag FROM work_step
			WHERE backlog_id = ? ORDER BY rowid`, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r WorkStepRow
			if err := rows.Scan(&r.BacklogID, &r.Step, &r.State, &r.Party, &r.StartedAt, &r.EndedAt, &r.SHA, &r.Cards, &r.Reason, &r.Tag); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// SetWorkBranch names the build branch of an item, once it has one. An item the hub has no row for is ErrNoWork.
func (s *Store) SetWorkBranch(id, branch string) error {
	return s.updateWork(`UPDATE work SET branch = ?, updated_at = ? WHERE backlog_id = ?`, strings.TrimSpace(branch), id)
}

// updateWork runs an UPDATE of one work row whose last two arguments are the time and the id, and says ErrNoWork for an id
// with no row. THAT ANSWER IS RETURNED OUTSIDE guard: inside it an error nobody marked as deliberate halts the store.
func (s *Store) updateWork(query, value, id string) error {
	var n int64
	err := s.guard(func() error {
		res, err := s.db.Exec(query, value, time.Now().UTC().Format(time.RFC3339), strings.TrimSpace(id))
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	if err == nil && n == 0 {
		return ErrNoWork
	}
	return err
}

// WorkOfBranch is the item that already holds a build branch, other than `except`: a branch belongs to at most one item. False
// when none does.
func (s *Store) WorkOfBranch(branch, except string) (string, bool, error) {
	var id string
	var found bool
	err := s.guard(func() error {
		id, found = "", false
		err := s.db.QueryRow(`SELECT backlog_id FROM work WHERE branch = ? AND backlog_id <> ? LIMIT 1`,
			strings.TrimSpace(branch), strings.TrimSpace(except)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	return id, found, err
}

// SetWorkKind says whether an item is `code`, `doc` or `mixed`. A `doc` item has no deploy.
func (s *Store) SetWorkKind(id, kind string) error {
	return s.updateWork(`UPDATE work SET kind = ?, updated_at = ? WHERE backlog_id = ?`, kind, id)
}

// WorkIdsOpen is the items with a branch, or a finished build when no branch was ever named, whose `deployed` and
// `pushed` steps are not both settled (done or skipped): the ones a collect has anything left to learn about.
func (s *Store) WorkIdsOpen() ([]string, error) {
	var out []string
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT w.backlog_id FROM work w
			WHERE (w.branch <> '' OR EXISTS (SELECT 1 FROM work_step b WHERE b.backlog_id = w.backlog_id
					AND b.step = 'building' AND b.state = 'done')) AND (SELECT COUNT(*) FROM work_step s WHERE s.backlog_id = w.backlog_id
				AND s.step IN ('deployed','pushed') AND s.state IN ('done','skipped')) < 2
			ORDER BY w.updated_at DESC LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// PutWorkPaths remembers the files an item changed. An empty list is not remembered over a known one.
func (s *Store) PutWorkPaths(id string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	b, _ := json.Marshal(paths)
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO work_paths (backlog_id, paths) VALUES (?, ?)
			ON CONFLICT (backlog_id) DO UPDATE SET paths = excluded.paths`, strings.TrimSpace(id), string(b))
		return err
	})
}

// WorkPaths is the files remembered for an item, nil when none are.
func (s *Store) WorkPaths(id string) ([]string, error) {
	var raw string
	err := s.guard(func() error {
		raw = ""
		err := s.db.QueryRow(`SELECT paths FROM work_paths WHERE backlog_id = ?`, strings.TrimSpace(id)).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	var out []string
	if err != nil || raw == "" {
		return nil, err
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

// CardsJSON is a list of cards as the `cards` column holds it.
func CardsJSON(cards []string) string {
	if len(cards) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(cards)
	return string(b)
}

// AllWorkSteps is the stored steps of every item, by backlog id, so a list is one query.
func (s *Store) AllWorkSteps() (map[string][]WorkStepRow, error) {
	out := map[string][]WorkStepRow{}
	err := s.guard(func() error {
		out = map[string][]WorkStepRow{}
		rows, err := s.db.Query(`SELECT backlog_id, step, state, party, started_at, ended_at, sha, cards, reason, tag FROM work_step ORDER BY rowid`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r WorkStepRow
			if err := rows.Scan(&r.BacklogID, &r.Step, &r.State, &r.Party, &r.StartedAt, &r.EndedAt, &r.SHA, &r.Cards, &r.Reason, &r.Tag); err != nil {
				return err
			}
			out[r.BacklogID] = append(out[r.BacklogID], r)
		}
		return rows.Err()
	})
	return out, err
}
