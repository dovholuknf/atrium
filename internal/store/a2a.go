package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// What agent-to-agent work needs written down: who launched a card, what the
// card last told its launcher, and which of its hooks can carry a message.
// See docs/a2a-reliability-design.md.

// HumanLauncher is the `spawned_by` of a card started from the board's own
// launch dialog. A card carrying it has nobody to report back to but the board.
const HumanLauncher = "@human"

// SetLineage records who launched a card. WRITTEN ONCE: it writes only while
// both columns are still empty, so a reopen or an unshelve, which runs the
// launch again, cannot overwrite the parent with whoever pressed the button
// the second time.
//
// The handle is qualified at this boundary, exactly as `Register` qualifies a
// wire name, so a parent is named the way `GetByWireName` will look it up.
// `@human` is not a wire name and is stored as it is.
func (s *Store) SetLineage(id, handle, parentID string) error {
	handle = strings.TrimSpace(handle)
	parentID = strings.TrimSpace(parentID)
	if handle == "" && parentID == "" {
		return nil
	}
	// A launcher on another room, `name@room`, is named the way THAT room
	// names it, so it is not qualified with this one's tenant. See
	// docs/cross-room-say-design.md.
	if handle != "" && handle != HumanLauncher && !strings.Contains(handle, "@") {
		handle = s.Qualify(handle)
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET spawned_by = ?, spawned_by_id = ?
			WHERE id = ? AND spawned_by = '' AND spawned_by_id = ''`, handle, parentID, id)
		return err
	})
}

// MarkReported stamps the moment a card said something to its launcher.
func (s *Store) MarkReported(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET reported_at = ? WHERE id = ?`, ts(now()), id)
		return err
	})
}

// SetReportSHA records the commit a `done` report named, and whether it was
// found in the card's worktree. An empty sha clears both, which is what a
// report that is not `done` says about the last one.
func (s *Store) SetReportSHA(id, sha string, unverified bool) error {
	sha = strings.TrimSpace(sha)
	flag := 0
	if sha != "" && unverified {
		flag = 1
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET report_sha = ?, report_unverified = ? WHERE id = ?`,
			sha, flag, id)
		return err
	})
}

// Hooks that can carry a queued message into the model.
const (
	HookTool = "tool"
	HookStop = "stop"
)

// SawHook stamps the first time a card was heard from on one of the two hooks
// that deliver messages. FIRST ONLY, so a hook that fires on every tool call
// writes once per card rather than once per call.
func (s *Store) SawHook(id, which string) error {
	col := ""
	switch which {
	case HookTool:
		col = "tool_hook_seen_at"
	case HookStop:
		col = "stop_hook_seen_at"
	default:
		return nil
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET `+col+` = ? WHERE id = ? AND `+col+` = ''`, ts(now()), id)
		return err
	})
}

// RecordNotice claims one automatic notice and reports whether it is new.
//
// The key names the one event that created the notice (the prompt that opened
// an unreported turn, a tool call that ran too long), so a trigger seen on
// every tick of the watchdog and again at the next turn end sends one notice,
// not one per sighting. See docs/a2a-reliability-design.md.
func (s *Store) RecordNotice(workerID, source, key string) (bool, error) {
	fresh := false
	err := s.guard(func() error {
		res, err := s.db.Exec(`INSERT OR IGNORE INTO a2a_notice (worker_id, source, key, created_at)
			VALUES (?, ?, ?, ?)`, workerID, source, key, ts(now()))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		fresh = n > 0
		return nil
	})
	return fresh, err
}

// ForgetNotices drops a worker's claimed notices from one source, so the next
// sighting of that event is a new notice. For an event that can end and come
// back, like a card falling back under the context threshold.
func (s *Store) ForgetNotices(workerID, source string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM a2a_notice WHERE worker_id = ? AND source = ?`, workerID, source)
		return err
	})
}

// OwesReport reports whether the card's launcher has given it something to do
// since it last told the launcher anything. A card its launcher never prompted
// owes nothing, whoever else has.
func (t *Task) OwesReport() bool {
	if t.OwedAt == nil {
		return false
	}
	return t.ReportedAt == nil || t.ReportedAt.Before(*t.OwedAt)
}

// promptOwes decides whether one `prompted` event makes the card owe its
// launcher a report: a prompt or message whose sender is the launcher. The
// opening prompt names the session that asked for the launch as its sender, so
// a reopen by the operator is not counted. The operator, a note, an action, atrium's own wake and a message
// from any other session do not. Decided here, where the stamp is, so a door
// written later cannot forget it. See docs/owed-report-design.md.
func promptOwes(q querier, s *Store, taskID string, payload []byte) (bool, error) {
	var p struct {
		FromPeer string `json:"from_peer"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, nil
	}
	from := strings.TrimSpace(p.FromPeer)
	if from == "" {
		return false, nil
	}
	var by, byID string
	if err := q.QueryRow(`SELECT spawned_by, spawned_by_id FROM task WHERE id = ?`, taskID).Scan(&by, &byID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if by == "" || by == HumanLauncher {
		return false, nil
	}
	if strings.EqualFold(from, by) {
		return true, nil
	}
	// Read through q, never s.Tenant: q may be a transaction holding the pool's
	// only connection, and that deadlocked the room at startup.
	var tenant string
	if err := q.QueryRow(`SELECT value FROM setting WHERE key = ?`, SettingTenant).Scan(&tenant); err != nil &&
		!errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	qualify := func(n string) string { return qualifyAs(tenant, n) }
	if strings.EqualFold(qualify(from), qualify(by)) {
		return true, nil
	}
	if byID != "" {
		var wire sql.NullString
		err := q.QueryRow(`SELECT wire_name FROM task WHERE id = ?`, byID).Scan(&wire)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		if wire.Valid && wire.String != "" && strings.EqualFold(qualify(from), wire.String) {
			return true, nil
		}
	}
	return false, nil
}

// TurnEndedAt is when a card last went from working to waiting, or nil when no
// turn has ended on it. See 0062_turn_end.
func (s *Store) TurnEndedAt(id string) (*time.Time, error) {
	var out *time.Time
	err := s.guard(func() error {
		out = nil
		var raw string
		err := s.db.QueryRow(`SELECT ended_at FROM turn_end WHERE task_id = ?`, id).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		at, err := parseTS(raw)
		if err != nil {
			return fmt.Errorf("turn_end %s: %w", id, err)
		}
		out = &at
		return nil
	})
	return out, err
}

// Launched reports whether this card was started by another session, as
// opposed to a human at the board or a session nobody launched.
func (t *Task) Launched() bool {
	return t.SpawnedBy != "" && t.SpawnedBy != HumanLauncher
}

// WaitingSinceOr is when the card started waiting, or `def` when it is not.
func (t *Task) WaitingSinceOr(def time.Time) time.Time {
	if t.WaitingSince == nil {
		return def
	}
	return *t.WaitingSince
}

// PromptKey names the prompt that opened a card's current turn, for the
// silent-stop notice's dedupe key.
func (t *Task) PromptKey() string {
	if t.OwedAt == nil {
		return ""
	}
	return t.OwedAt.UTC().Format(time.RFC3339Nano)
}
