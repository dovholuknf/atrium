package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const taskColumns = `id, title, why, repo, worktree, runner, hostname, pid, status,
	created_at, last_activity_at, waiting_since, wire_name, overrides, rank,
	external_id, resume_id, branch, window_name, gated, auto_approve, tags, pinned, theme`

func scanTask(sc interface{ Scan(...any) error }) (*Task, error) {
	var (
		t            Task
		waiting      sql.NullString
		wire         sql.NullString
		created, act string
		overrides    string
		gated, auto  int
		tags         string
		pinned       int
	)
	if err := sc.Scan(&t.ID, &t.Title, &t.Why, &t.Repo, &t.Worktree, &t.Runner, &t.Hostname,
		&t.PID, &t.Status, &created, &act, &waiting, &wire, &overrides, &t.Rank,
		&t.ExternalID, &t.ResumeID, &t.Branch, &t.WindowName, &gated, &auto,
		&tags, &pinned, &t.Theme); err != nil {
		return nil, err
	}
	t.Gated = gated != 0
	t.AutoApprove = auto != 0
	t.Pinned = pinned != 0
	// Always a list, never nil, so the board can filter without a guard and
	// the JSON carries `[]` rather than `null`.
	t.Tags = []string{}
	if tags != "" {
		if err := json.Unmarshal([]byte(tags), &t.Tags); err != nil {
			return nil, fmt.Errorf("task %s tags: %w", t.ID, err)
		}
	}
	var err error
	if t.CreatedAt, err = parseTS(created); err != nil {
		return nil, fmt.Errorf("task %s created_at: %w", t.ID, err)
	}
	if t.LastActivityAt, err = parseTS(act); err != nil {
		return nil, fmt.Errorf("task %s last_activity_at: %w", t.ID, err)
	}
	if waiting.Valid && waiting.String != "" {
		w, err := parseTS(waiting.String)
		if err != nil {
			return nil, fmt.Errorf("task %s waiting_since: %w", t.ID, err)
		}
		t.WaitingSince = &w
	}
	t.WireName = wire.String
	t.Overrides = map[string]string{}
	if overrides != "" {
		if err := json.Unmarshal([]byte(overrides), &t.Overrides); err != nil {
			return nil, fmt.Errorf("task %s overrides: %w", t.ID, err)
		}
	}
	return &t, nil
}

// Register resolves a runner's observed identity to a task, creating one if
// nothing matches. This is the self-started path: the runner knows only what
// its own process can see, and gets back a stable id to use from then on.
//
// Matching order is wire name first, then the pid-plus-worktree hint. A pid is
// never an identity, because processes restart and the operating system
// recycles pids. It is only ever a hint that this is the same live process.
func (s *Store) Register(obs Observed) (*Task, bool, error) {
	var (
		task    *Task
		created bool
	)
	err := s.guard(func() error {
		task, created = nil, false
		if obs.WireName != "" {
			t, err := s.getBy(`wire_name = ?`, obs.WireName)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			task = t
		}
		if task == nil && obs.PID != 0 && obs.Worktree != "" {
			t, err := s.getBy(`pid = ? AND worktree = ? AND status NOT IN ('done','shelved','dead')`,
				obs.PID, obs.Worktree)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			task = t
		}
		if task != nil {
			return s.refreshObserved(task, obs)
		}
		t, err := s.create(obs)
		if err != nil {
			return err
		}
		task, created = t, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return task, created, nil
}

func (s *Store) getBy(where string, args ...any) (*Task, error) {
	row := s.db.QueryRow(`SELECT `+taskColumns+` FROM task WHERE `+where+` LIMIT 1`, args...)
	return scanTask(row)
}

// refreshObserved updates only the observed bucket. It never touches
// overrides, which is what lets a hand-picked title survive a reconnect.
func (s *Store) refreshObserved(t *Task, obs Observed) error {
	n := now()
	_, err := s.db.Exec(`UPDATE task SET worktree = ?, repo = ?, runner = ?, hostname = ?,
		pid = ?, wire_name = ?, last_activity_at = ? WHERE id = ?`,
		obs.Worktree, obs.Repo, obs.Runner, obs.Hostname, obs.PID,
		nullable(obs.WireName), ts(n), t.ID)
	if err != nil {
		return err
	}
	t.Worktree, t.Repo, t.Runner = obs.Worktree, obs.Repo, obs.Runner
	t.Hostname, t.PID, t.WireName = obs.Hostname, obs.PID, obs.WireName
	t.LastActivityAt = n
	return nil
}

func (s *Store) create(obs Observed) (*Task, error) {
	n := now()
	title := obs.WireName
	if title == "" {
		title = obs.Worktree
	}
	if title == "" {
		title = "untitled"
	}
	rank, err := s.topRank(StatusRunning)
	if err != nil {
		return nil, err
	}
	t := &Task{
		ID: newID(), Title: title, Repo: obs.Repo, Worktree: obs.Worktree,
		Runner: obs.Runner, Hostname: obs.Hostname, PID: obs.PID,
		Status: StatusRunning, CreatedAt: n, LastActivityAt: n,
		WireName: obs.WireName, Overrides: map[string]string{}, Rank: rank,
		Tags: []string{},
	}
	if _, err := s.db.Exec(`INSERT INTO task (`+taskColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Title, t.Why, t.Repo, t.Worktree, t.Runner, t.Hostname, t.PID, t.Status,
		ts(t.CreatedAt), ts(t.LastActivityAt), nil, nullable(t.WireName), "{}", t.Rank,
		t.ExternalID, t.ResumeID, t.Branch, t.WindowName, 0, 0, "[]", 0, ""); err != nil {
		return nil, err
	}
	if err := s.appendEvent(t.ID, EventCreated, map[string]any{"observed": obs}); err != nil {
		return nil, err
	}
	return t, nil
}

// topRank returns a rank that sorts above everything currently in a column.
// New cards land at the top. Midpoint insertion handles everything after that,
// so reordering never renumbers a column.
func (s *Store) topRank(status string) (float64, error) {
	var min sql.NullFloat64
	if err := s.db.QueryRow(`SELECT MIN(rank) FROM task WHERE status = ?`, status).Scan(&min); err != nil {
		return 0, err
	}
	if !min.Valid {
		return 0, nil
	}
	return min.Float64 - 1, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Get returns one task by id.
func (s *Store) Get(id string) (*Task, error) {
	var t *Task
	err := s.guard(func() error {
		got, err := s.getBy(`id = ?`, id)
		if err != nil {
			return err
		}
		t = got
		return nil
	})
	return t, err
}

// GetByWireName returns the task a session calling itself name belongs to.
// Hooks know their session's name, not its card id.
func (s *Store) GetByWireName(name string) (*Task, error) {
	var t *Task
	err := s.guard(func() error {
		got, err := s.getBy(`wire_name = ?`, name)
		if err != nil {
			return err
		}
		t = got
		return nil
	})
	return t, err
}

// List returns tasks, newest activity first within each status, ordered by
// rank. Pass an empty status set for everything.
func (s *Store) List(statuses ...string) ([]*Task, error) {
	var out []*Task
	err := s.guard(func() error {
		out = nil
		q := `SELECT ` + taskColumns + ` FROM task`
		var args []any
		if len(statuses) > 0 {
			q += ` WHERE status IN (` + placeholders(len(statuses)) + `)`
			for _, st := range statuses {
				args = append(args, st)
			}
		}
		q += ` ORDER BY rank ASC, created_at ASC`
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Waiting returns everything blocked on a human, longest wait first. This is
// the Stack view, and it ignores rank: "who has waited longest"
// is a fact, not a judgement.
func (s *Store) Waiting() ([]*Task, error) {
	var out []*Task
	err := s.guard(func() error {
		out = nil
		// Only work you can actually act on from here. A session adopted from
		// the ledger has no wire name, so atrium can watch it but cannot send
		// it anything. Listing those as "waiting on you" would be a lie about
		// what clicking them does.
		rows, err := s.db.Query(`SELECT ` + taskColumns + ` FROM task
			WHERE status IN ('needs-input','needs-permission')
			  AND wire_name IS NOT NULL AND wire_name != ''
			ORDER BY waiting_since ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// SetStatus moves a task between columns and records the transition.
// waiting_since is set on entry to a waiting state and cleared on exit, which
// is what makes the Stack view's ordering meaningful.
func (s *Store) SetStatus(id, status string) error {
	return s.guard(func() error {
		prev, err := s.getBy(`id = ?`, id)
		if err != nil {
			return err
		}
		if prev.Status == status {
			return nil
		}
		n := now()
		var waiting any
		if status == StatusNeedsInput || status == StatusNeedsPermission {
			if prev.WaitingSince != nil {
				waiting = ts(*prev.WaitingSince)
			} else {
				waiting = ts(n)
			}
		}
		if _, err := s.db.Exec(
			`UPDATE task SET status = ?, waiting_since = ?, last_activity_at = ? WHERE id = ?`,
			status, waiting, ts(n), id); err != nil {
			return err
		}
		return s.appendEvent(id, EventStatusChanged, map[string]any{
			"from": prev.Status, "to": status,
		})
	})
}

// SetOverrides merges operator-set values. An empty value removes an override,
// which falls the field back to whatever the runner observes.
func (s *Store) SetOverrides(id string, patch map[string]string) error {
	return s.guard(func() error {
		t, err := s.getBy(`id = ?`, id)
		if err != nil {
			return err
		}
		for k, v := range patch {
			if v == "" {
				delete(t.Overrides, k)
				continue
			}
			t.Overrides[k] = v
		}
		blob, err := json.Marshal(t.Overrides)
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`UPDATE task SET overrides = ?, last_activity_at = ? WHERE id = ?`,
			string(blob), ts(now()), id)
		return err
	})
}

// SetWhy records intent. This is the answer to "what was I even doing."
func (s *Store) SetWhy(id, why string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET why = ?, last_activity_at = ? WHERE id = ?`,
			why, ts(now()), id)
		return err
	})
}

// BackdateActivity moves a task's last activity into the past. Test support:
// the alternative is a test that sleeps for hours.
func (s *Store) BackdateActivity(id string, by time.Duration) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET last_activity_at = ? WHERE id = ?`,
			ts(now().Add(-by)), id)
		return err
	})
}

// SetResumeID records the runner's own id for the conversation, so the card can
// start it again later.
//
// A supervised runner dies with the daemon: the daemon owns its pseudo
// terminal, and closing one takes the attached process with it. An id to resume
// from turns that from losing the conversation into restarting it, and does the
// same for shelving and terminating.
//
// An empty id is ignored. Not every harness reports one, and overwriting a good
// id with a blank breaks the resume button silently.
func (s *Store) SetResumeID(id, resumeID string) error {
	if strings.TrimSpace(resumeID) == "" {
		return nil
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET resume_id = ? WHERE id = ?`, resumeID, id)
		return err
	})
}

// PrunableStatuses are the only statuses a sweep will ever delete.
//
// Shelved is absent: shelving says the work is coming back, so a sweep that
// took shelved cards would discard the ones kept on purpose.
var PrunableStatuses = []string{StatusDone, StatusDead}

// Prune deletes finished cards that have been sitting untouched. Returns how
// many went. An empty statuses list sweeps everything prunable.
func (s *Store) Prune(olderThan time.Duration, statuses ...string) (int, error) {
	want := statuses
	if len(want) == 0 {
		want = PrunableStatuses
	}
	// Anything outside the prunable set is dropped rather than refused, so a
	// caller can pass a column name without knowing the rule.
	var keep []any
	for _, s := range want {
		for _, ok := range PrunableStatuses {
			if s == ok {
				keep = append(keep, s)
			}
		}
	}
	if len(keep) == 0 {
		return 0, nil
	}
	var n int
	err := s.guard(func() error {
		args := append([]any{}, keep...)
		args = append(args, ts(now().Add(-olderThan)))
		res, err := s.db.Exec(
			// Inclusive, because timestamps are stored to the second. With a
			// zero age the cutoff is now, and a card that finished this second
			// would otherwise survive a sweep meant to take everything.
			`DELETE FROM task WHERE status IN (`+placeholders(len(keep))+`)
			 AND last_activity_at <= ?`, args...)
		if err != nil {
			return err
		}
		got, err := res.RowsAffected()
		if err != nil {
			return err
		}
		n = int(got)
		return nil
	})
	return n, err
}

// SetRank places a task at an explicit position within its column. Callers
// compute the midpoint of the two neighbours a card was dropped between, so an
// insert never renumbers the cards around it.
func (s *Store) SetRank(id string, rank float64) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET rank = ? WHERE id = ?`, rank, id)
		return err
	})
}

// SetGated records whether a session has joined atrium.
//
// Joining is what turns permission gating on for a session that was not
// started with it. The hook asks the daemon rather than reading its own
// environment, so this takes effect on the very next tool call without the
// session being restarted.
func (s *Store) SetGated(id string, on bool) error {
	return s.guard(func() error {
		v := 0
		if on {
			v = 1
		}
		_, err := s.db.Exec(`UPDATE task SET gated = ?, last_activity_at = ? WHERE id = ?`,
			v, ts(now()), id)
		return err
	})
}

// SetAutoApprove turns auto mode on or off for one session.
//
// Per task, so one session can be trusted for a stretch while the others keep
// asking. Stored rather than held in memory: losing it on a restart would start
// interrupting again with no sign of why.
func (s *Store) SetAutoApprove(id string, on bool) error {
	return s.guard(func() error {
		v := 0
		if on {
			v = 1
		}
		_, err := s.db.Exec(`UPDATE task SET auto_approve = ?, last_activity_at = ? WHERE id = ?`,
			v, ts(now()), id)
		return err
	})
}

// SetTags replaces a card's tags.
//
// Replaced rather than merged, because the board sends the whole set and a
// merge would make removing one impossible.
//
// last_activity_at is deliberately NOT touched. Tagging is the operator
// filing a card, not the session doing anything, and bumping it would move a
// silent card to the top of an activity-sorted list.
func (s *Store) SetTags(id string, tags []string) error {
	clean := NormalizeTags(tags)
	body, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET tags = ? WHERE id = ?`, string(body), id)
		return err
	})
}

// SetPinned marks a card as a fixture, or stops.
func (s *Store) SetPinned(id string, on bool) error {
	return s.guard(func() error {
		v := 0
		if on {
			v = 1
		}
		_, err := s.db.Exec(`UPDATE task SET pinned = ? WHERE id = ?`, v, id)
		return err
	})
}

// NormalizeTags puts a set of tags into the one form everything else can rely
// on: lower case, trimmed, no blanks, no duplicates, in order.
//
// Case folding matters more than it looks. "Lab" and "lab" as two groups is
// the failure this feature would be judged on, and free text means both will
// be typed.
func NormalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range in {
		t := strings.ToLower(strings.TrimSpace(raw))
		// Commas separate tags everywhere else, so one inside a tag would
		// come back as two after a round trip through any text box.
		t = strings.ReplaceAll(t, ",", " ")
		t = strings.Join(strings.Fields(t), " ")
		if t == "" || seen[t] {
			continue
		}
		if len(t) > 40 {
			t = t[:40]
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// GatedByWireName reports whether the session calling itself name has joined.
// Unknown names are not gated: a session atrium has never heard of has not
// opted in.
func (s *Store) GatedByWireName(name string) (bool, error) {
	var gated bool
	err := s.guard(func() error {
		gated = false
		t, err := s.getBy(`wire_name = ?`, name)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		// A card put down is not a session to gate. Its requests are refused
		// by the shelved rule instead.
		gated = t.Gated && t.Status != StatusDone && t.Status != StatusDead
		return nil
	})
	return gated, err
}

// Touch marks activity without changing status.
func (s *Store) Touch(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET last_activity_at = ? WHERE id = ?`, ts(now()), id)
		return err
	})
}

// Forget removes a task and everything hanging off it.
func (s *Store) Forget(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM task WHERE id = ?`, id)
		return err
	})
}

// AppendEvent records one entry in a task's history.
func (s *Store) AppendEvent(taskID, kind string, payload any) error {
	return s.guard(func() error { return s.appendEvent(taskID, kind, payload) })
}

// appendEvent is the unguarded form, for use inside an existing guard.
func (s *Store) appendEvent(taskID, kind string, payload any) error {
	blob := []byte("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		blob = b
	}
	_, err := s.db.Exec(`INSERT INTO event (id, task_id, at, kind, payload) VALUES (?,?,?,?,?)`,
		newID(), taskID, ts(now()), kind, string(blob))
	return err
}

// Events returns a task's history, oldest first.
func (s *Store) Events(taskID string, limit int) ([]*Event, error) {
	if limit <= 0 {
		limit = 200
	}
	var out []*Event
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(
			`SELECT id, task_id, at, kind, payload FROM event
			 WHERE task_id = ? ORDER BY at ASC, id ASC LIMIT ?`, taskID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				e       Event
				at, pay string
			)
			if err := rows.Scan(&e.ID, &e.TaskID, &at, &e.Kind, &pay); err != nil {
				return err
			}
			if e.At, err = parseTS(at); err != nil {
				return err
			}
			e.Payload = json.RawMessage(pay)
			out = append(out, &e)
		}
		return rows.Err()
	})
	return out, err
}

var _ = time.Time{}
