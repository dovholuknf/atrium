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
	external_id, resume_id, branch, window_name, gated, auto_approve, tags, pinned, theme, sound,
	archived_at, source, url, prompt, intake_key, auto_until, recap, recap_at, note, waiting_reason,
	icon`

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
		archived     string
		autoUntil    string
		recapAt      string
	)
	if err := sc.Scan(&t.ID, &t.Title, &t.Why, &t.Repo, &t.Worktree, &t.Runner, &t.Hostname,
		&t.PID, &t.Status, &created, &act, &waiting, &wire, &overrides, &t.Rank,
		&t.ExternalID, &t.ResumeID, &t.Branch, &t.WindowName, &gated, &auto,
		&tags, &pinned, &t.Theme, &t.Sound, &archived, &t.Source, &t.URL,
		&t.Prompt, &t.IntakeKey, &autoUntil, &t.Recap, &recapAt, &t.Note,
		&t.WaitingReason, &t.Icon); err != nil {
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
	if archived != "" {
		a, err := parseTS(archived)
		if err != nil {
			return nil, fmt.Errorf("task %s archived_at: %w", t.ID, err)
		}
		t.ArchivedAt = &a
	}
	if autoUntil != "" {
		u, err := parseTS(autoUntil)
		if err != nil {
			return nil, fmt.Errorf("task %s auto_until: %w", t.ID, err)
		}
		t.AutoUntil = &u
	}
	if recapAt != "" {
		r, err := parseTS(recapAt)
		if err != nil {
			return nil, fmt.Errorf("task %s recap_at: %w", t.ID, err)
		}
		t.RecapAt = &r
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
	// Qualified here rather than at the eight call sites, because this is the
	// one boundary where a name off the wire becomes a name in the database.
	// A session that says it is called `atrium` on a machine called `sg4` is
	// stored as `sg4/atrium`, and cannot claim another machine's card.
	obs.WireName = s.Qualify(obs.WireName)

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
	// What repository this is, asked of the directory rather than guessed from
	// its name. See gitinfo.go: the wire name is the last path segment, which
	// is the subdirectory for a session started inside one and the branch for
	// a worktree.
	git := ReadGitInfo(obs.Worktree)
	repo := obs.Repo
	if repo == "" {
		repo = git.Repo
	}
	title := TitleFor(git, obs.Worktree, LocalName(obs.WireName))

	rank, err := s.topRank(StatusRunning)
	if err != nil {
		return nil, err
	}
	t := &Task{
		ID: newID(), Title: title, Repo: repo, Branch: git.Branch, Worktree: obs.Worktree,
		Runner: obs.Runner, Hostname: obs.Hostname, PID: obs.PID,
		Status: StatusRunning, CreatedAt: n, LastActivityAt: n,
		WireName: obs.WireName, Overrides: map[string]string{}, Rank: rank,
		Tags: []string{},
	}
	if err := s.insertTask(t); err != nil {
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
// insertTask writes one new row. Shared by the two ways a card comes into
// existence: a runner registering itself, and an item offered by a source.
//
// One place, because the column list and its placeholders have to agree and
// counting question marks twice is how a migration goes in and one of the two
// insert sites silently keeps writing the old shape.
func (s *Store) insertTask(t *Task) error {
	overrides := "{}"
	if len(t.Overrides) > 0 {
		raw, err := json.Marshal(t.Overrides)
		if err != nil {
			return err
		}
		overrides = string(raw)
	}
	tags := "[]"
	if len(t.Tags) > 0 {
		raw, err := json.Marshal(t.Tags)
		if err != nil {
			return err
		}
		tags = string(raw)
	}
	_, err := s.db.Exec(`INSERT INTO task (`+taskColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Title, t.Why, t.Repo, t.Worktree, t.Runner, t.Hostname, t.PID, t.Status,
		ts(t.CreatedAt), ts(t.LastActivityAt), nil, nullable(t.WireName), overrides, t.Rank,
		t.ExternalID, t.ResumeID, t.Branch, t.WindowName, 0, 0, tags, 0, "", "", "",
		t.Source, t.URL, t.Prompt, t.IntakeKey, "", "", "", "", "", "")
	return err
}

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
//
// Qualified on the way in, matching Register, so a hook that says `atrium`
// finds the card stored as `sg4/atrium`. A caller that already has the
// qualified name is unaffected, since Qualify is idempotent.
func (s *Store) GetByWireName(name string) (*Task, error) {
	name = s.Qualify(name)
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
		// Archived cards are off the board. They are still rows, and
		// ListArchived is how they are read: this is the board's question,
		// which is "what is here now".
		q := `SELECT ` + taskColumns + ` FROM task WHERE archived_at = ''`
		var args []any
		if len(statuses) > 0 {
			q += ` AND status IN (` + placeholders(len(statuses)) + `)`
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
			  AND archived_at = ''
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
	return s.SetStatusBecause(id, status, "")
}

// SetStatusBecause is SetStatus with a note on WHY, for the one case where
// the destination column does not say it.
//
// Only entry into a waiting column carries a reason, and only one caller has
// one to give: the session hook, which knows the difference between a session
// that has just come up and one that has handed a turn back. Everything else
// goes through SetStatus and reads as the default, a turn that ended.
//
// The reason is cleared on the way out rather than left behind, because a card
// that has gone back to running is not waiting for anything and a stale reason
// would be waiting to be believed the next time it stopped.
func (s *Store) SetStatusBecause(id, status, reason string) error {
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
		why := ""
		if status == StatusNeedsInput || status == StatusNeedsPermission {
			if prev.WaitingSince != nil {
				waiting = ts(*prev.WaitingSince)
			} else {
				waiting = ts(n)
			}
			why = reason
		}
		// A card that is alive again comes back onto the board.
		//
		// Archiving is for work that is over, and the sweep applies it to dead
		// cards on a timer. A dead card revives all the time: the session says
		// something, or a fixture starts it again. Leaving the stamp on made
		// the card invisible to every board query while it was plainly running,
		// which showed up as a terminal that had started and was nowhere.
		//
		// Cleared here rather than at each caller because this is the one place
		// a status changes, and the rule is about the status: anything that is
		// not over is not archived.
		archived := ""
		if status == StatusDone || status == StatusDead {
			archived = tsOrEmpty(prev.ArchivedAt)
		}
		if _, err := s.db.Exec(
			`UPDATE task SET status = ?, waiting_since = ?, last_activity_at = ?, archived_at = ?,
			 waiting_reason = ? WHERE id = ?`,
			status, waiting, ts(n), archived, why, id); err != nil {
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

// SetOrigin records where a card's work came from: which system, that system's
// own identifier, and the way back to it.
//
// All three or none. A url with no source is a link with nothing to say what
// it points at, and an identifier with no source cannot be deduplicated
// against, since the same issue number means different work in two trackers.
//
// Empty strings are ignored rather than written, so a caller that knows only
// the url does not blank out an identifier something else supplied.
func (s *Store) SetOrigin(id, source, externalID, url string) error {
	source = strings.TrimSpace(source)
	externalID = strings.TrimSpace(externalID)
	url = SafeURL(url)
	if source == "" && externalID == "" && url == "" {
		return nil
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET
			source      = CASE WHEN ? = '' THEN source      ELSE ? END,
			external_id = CASE WHEN ? = '' THEN external_id ELSE ? END,
			url         = CASE WHEN ? = '' THEN url         ELSE ? END
			WHERE id = ?`,
			source, source, externalID, externalID, url, url, id)
		return err
	})
}

// BySourceExternal finds the card already raised for an external item, or
// sql.ErrNoRows.
//
// The pair is the key rather than the identifier alone, because `4211` is a
// GitHub issue and a Zendesk ticket and they are not the same work.
//
// Archived cards count. An item raised, worked and swept should not come back
// the next time a source runs, which is the whole failure mode a poller has.
func (s *Store) BySourceExternal(source, externalID string) (*Task, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(externalID) == "" {
		return nil, sql.ErrNoRows
	}
	var t *Task
	err := s.guard(func() error {
		got, err := s.getBy(`source = ? AND external_id = ?`, source, externalID)
		if err != nil {
			return err
		}
		t = got
		return nil
	})
	return t, err
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
	return s.SetAutoApproveUntil(id, on, nil)
}

// SetAutoApproveUntil turns auto mode on for a while, or on with no deadline
// when until is nil.
//
// Turning it OFF always clears the deadline. "Off until Tuesday" is not a
// thing anybody means, and a deadline left behind by an off switch would turn
// itself back on the next time somebody flipped it.
func (s *Store) SetAutoApproveUntil(id string, on bool, until *time.Time) error {
	return s.guard(func() error {
		v := 0
		if on {
			v = 1
		}
		deadline := ""
		if on && until != nil {
			deadline = ts(*until)
		}
		_, err := s.db.Exec(
			`UPDATE task SET auto_approve = ?, auto_until = ?, last_activity_at = ? WHERE id = ?`,
			v, deadline, ts(now()), id)
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
	name = s.Qualify(name)
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

// Archive takes cards off the board without forgetting them.
//
// The counterpart to Prune. Prune deletes, which is right when a human presses
// `clear` and says so, and wrong as something that happens on a timer: a card
// and its audit log are the only account of what a session ran and what it was
// allowed to do, and a board that discards that a minute after a session ends
// cannot answer what has been running this week.
//
// Same rules as Prune about what may be taken, for the same reason: shelving
// says the work is coming back.
func (s *Store) Archive(olderThan time.Duration, statuses ...string) (int, error) {
	want := statuses
	if len(want) == 0 {
		want = PrunableStatuses
	}
	var keep []any
	for _, st := range want {
		for _, ok := range PrunableStatuses {
			if st == ok {
				keep = append(keep, st)
			}
		}
	}
	if len(keep) == 0 {
		return 0, nil
	}
	var n int
	err := s.guard(func() error {
		args := []any{ts(now())}
		args = append(args, keep...)
		args = append(args, ts(now().Add(-olderThan)))
		res, err := s.db.Exec(
			`UPDATE task SET archived_at = ?
			 WHERE archived_at = ''
			   AND status IN (`+placeholders(len(keep))+`)
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

// ListArchived is every card that has left the board, newest first.
//
// The whole history of what has ever run here, which is a different question
// from what the board answers and deserves its own way in rather than a filter
// on the board's.
func (s *Store) ListArchived(limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 500
	}
	var out []*Task
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+taskColumns+` FROM task
			WHERE archived_at != '' ORDER BY archived_at DESC LIMIT ?`, limit)
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
