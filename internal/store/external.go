package store

import (
	"database/sql"
	"errors"
	"time"
)

// External describes a session atrium did not start, as reported by whatever
// already tracks it. The gwt session ledger is the first source of these, which
// is what makes an existing claude appear on the board without wiring anything
// into that session.
type External struct {
	// ID is the source's own identifier, stable across restarts of atrium.
	ID string
	// ResumeID is what the runner needs to pick the conversation back up.
	ResumeID   string
	Title      string
	Repo       string
	Worktree   string
	Branch     string
	WindowName string
	Runner     string
	PID        int
	Status     string
	// LastActivity is when the source last saw this session change state.
	LastActivity time.Time
}

// UpsertExternal creates or refreshes the card for a session atrium does not
// own. Matching order is the source's id, then the worktree, so a session that
// later connects through the hook lands on the same card rather than a second
// one.
//
// Everything written here is observed data. Overrides are never touched, and a
// status the operator set by hand on a shelved or done card is left alone: the
// ledger reporting "idle" should not drag something back off the shelf.
func (s *Store) UpsertExternal(e External) (*Task, bool, error) {
	var (
		task    *Task
		created bool
	)
	err := s.guard(func() error {
		task, created = nil, false

		if e.ID != "" {
			t, err := s.getBy(`external_id = ?`, e.ID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			task = t
		}
		if task == nil && e.Worktree != "" {
			t, err := s.getBy(`worktree = ? AND external_id = ''`, e.Worktree)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			task = t
		}

		activity := e.LastActivity
		if activity.IsZero() {
			activity = now()
		}
		activity = activity.UTC().Truncate(time.Millisecond)

		if task == nil {
			rank, err := s.topRank(e.Status)
			if err != nil {
				return err
			}
			title := e.Title
			if title == "" {
				title = e.Branch
			}
			if title == "" {
				title = e.Worktree
			}
			t := &Task{
				ID: newID(), Title: title, Repo: e.Repo, Worktree: e.Worktree,
				Runner: e.Runner, PID: e.PID, Status: e.Status,
				CreatedAt: activity, LastActivityAt: activity, Rank: rank,
				Overrides: map[string]string{},
				ExternalID: e.ID, ResumeID: e.ResumeID, Branch: e.Branch, WindowName: e.WindowName,
			}
			if e.Status == StatusNeedsInput || e.Status == StatusNeedsPermission {
				t.WaitingSince = &activity
			}
			var waiting any
			if t.WaitingSince != nil {
				waiting = ts(*t.WaitingSince)
			}
			if _, err := s.db.Exec(`INSERT INTO task (`+taskColumns+`)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				t.ID, t.Title, t.Why, t.Repo, t.Worktree, t.Runner, t.Hostname, t.PID, t.Status,
				ts(t.CreatedAt), ts(t.LastActivityAt), waiting, nil, "{}", t.Rank,
				t.ExternalID, t.ResumeID, t.Branch, t.WindowName); err != nil {
				return err
			}
			if err := s.appendEvent(t.ID, EventCreated, map[string]any{
				"external": e.ID, "source": "ledger", "status": e.Status,
			}); err != nil {
				return err
			}
			task, created = t, true
			return nil
		}

		// A card the operator put down stays down. Only its observed fields
		// refresh, so shelving something does not fight the next poll.
		status := task.Status
		var waiting any
		if task.WaitingSince != nil {
			waiting = ts(*task.WaitingSince)
		}
		if task.Status != StatusShelved && task.Status != StatusDone && e.Status != "" {
			status = e.Status
			if status == StatusNeedsInput || status == StatusNeedsPermission {
				if task.WaitingSince == nil {
					waiting = ts(activity)
				}
			} else {
				waiting = nil
			}
			if status != task.Status {
				if err := s.appendEvent(task.ID, EventStatusChanged, map[string]any{
					"from": task.Status, "to": status, "source": "ledger",
				}); err != nil {
					return err
				}
			}
		}

		if _, err := s.db.Exec(`UPDATE task SET repo = ?, worktree = ?, runner = ?, pid = ?,
			status = ?, waiting_since = ?, last_activity_at = ?, external_id = ?, resume_id = ?,
			branch = ?, window_name = ? WHERE id = ?`,
			e.Repo, e.Worktree, e.Runner, e.PID, status, waiting, ts(activity),
			e.ID, e.ResumeID, e.Branch, e.WindowName, task.ID); err != nil {
			return err
		}

		refreshed, err := s.getBy(`id = ?`, task.ID)
		if err != nil {
			return err
		}
		task = refreshed
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return task, created, nil
}

// ExternalIDs returns the source ids atrium currently knows about, so a poller
// can tell which sessions have disappeared.
func (s *Store) ExternalIDs() (map[string]string, error) {
	out := map[string]string{}
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT external_id, id FROM task WHERE external_id != ''`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ext, id string
			if err := rows.Scan(&ext, &id); err != nil {
				return err
			}
			out[ext] = id
		}
		return rows.Err()
	})
	return out, err
}
