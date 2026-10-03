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
// See docs/runtime/a2a-reliability-design.md.

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
	// docs/fabric/cross-room-say-design.md.
	if handle != "" && handle != HumanLauncher && !strings.Contains(handle, "@") {
		handle = s.Qualify(handle)
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET spawned_by = ?, spawned_by_id = ?
			WHERE id = ? AND spawned_by = '' AND spawned_by_id = ''`, handle, parentID, id)
		return err
	})
}

// SetReportTo keeps the name a launch was told to report to, as given.
func (s *Store) SetReportTo(id, name string) error {
	name = strings.TrimSpace(name)
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET report_to = ? WHERE id = ?`, name, id)
		return err
	})
}

// ReportTo is the name the card was launched to report to, or empty.
func (s *Store) ReportTo(id string) string {
	var name string
	_ = s.guard(func() error {
		name = ""
		err := s.db.QueryRow(`SELECT report_to FROM task WHERE id = ?`, id).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return name
}

// SetLauncher points a card at a new launcher, unlike SetLineage, which writes
// once. Only for a card that reports to a named card, whose launcher is
// resolved again at each delivery. The handle is qualified as SetLineage does.
func (s *Store) SetLauncher(id, handle, parentID string) error {
	handle = strings.TrimSpace(handle)
	if handle != "" && handle != HumanLauncher && !strings.Contains(handle, "@") {
		handle = s.Qualify(handle)
	}
	return s.guard(func() error {
		if _, err := s.db.Exec(`UPDATE task SET spawned_by = ?, spawned_by_id = ? WHERE id = ?`,
			handle, strings.TrimSpace(parentID), id); err != nil {
			return err
		}
		// The work item carries its own copy of the launcher, and its arbiter is
		// who a report goes to. An arbiter that is still the launcher follows it,
		// and one somebody handed the work to does not.
		if _, err := s.db.Exec(`UPDATE work_item SET arbiter_id = ?, arbiter_handle = ?
			WHERE task_id = ? AND (arbiter_id = launcher_id OR arbiter_id = '')`,
			strings.TrimSpace(parentID), handle, id); err != nil {
			return err
		}
		_, err := s.db.Exec(`UPDATE work_item SET launcher_id = ?, launcher_handle = ? WHERE task_id = ?`,
			strings.TrimSpace(parentID), handle, id)
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
// not one per sighting. See docs/runtime/a2a-reliability-design.md.
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
// written later cannot forget it. See docs/rnd/owed-report-design.md.
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

// LauncherID is the BARE id of the card on THIS room that launched `t`, or "". COMPUTED, never
// stored: the stored `spawned_by_id` is only the fallback it ends on. Resolved the way the daemon's
// launcherOf finds a delivery's launcher: the card's `report_to` (a handle, alias or id), then
// `spawned_by_id`, then `spawned_by` as a wire name. Empty for an operator launch (`@human`), for a
// session nobody launched, and for a launcher on another room (`name@room`, `room~id`), which stay
// in spawned_by and spawned_by_id.
func (s *Store) LauncherID(t *Task) string {
	if t == nil || t.SpawnedBy == "" || t.SpawnedBy == HumanLauncher {
		return ""
	}
	if name := s.ReportTo(t.ID); name != "" {
		if l := s.LocalCard(name); l != nil && l.ID != t.ID {
			return l.ID
		}
	}
	if t.SpawnedByID != "" {
		if l, err := s.Get(t.SpawnedByID); err == nil && l.ID != t.ID {
			return l.ID
		}
	}
	if !strings.Contains(t.SpawnedBy, "@") {
		if l, err := s.GetByWireName(t.SpawnedBy); err == nil && l.ID != t.ID {
			return l.ID
		}
	}
	return ""
}

// LocalCard finds a card on this room by handle, alias or id, all exact.
func (s *Store) LocalCard(name string) *Task {
	if t, err := s.GetByWireName(name); err == nil {
		return t
	}
	if t, err := s.GetByAlias(name); err == nil {
		return t
	}
	if t, err := s.Get(name); err == nil {
		return t
	}
	return nil
}

// LauncherIDs is LauncherID for every card at once, as one SELECT resolved in memory, for the list
// that decorates every row (a per-row LauncherID is up to five queries a card on the store's one
// connection). Keyed by card id; a card with no launcher is absent. NIL when the read fails, which
// a caller takes as "ask LauncherID per card", never as "nobody has a launcher". Same order and same checks as
// LauncherID, and a test holds the two equal over one table.
func (s *Store) LauncherIDs() map[string]string {
	type row struct{ id, wire, alias, status, archived, by, byID, reportTo string }
	var rows []row
	if err := s.guard(func() error {
		rows = nil
		// newest first, so the first match of an alias is the one GetByAlias picks
		q, err := s.db.Query(`SELECT id, COALESCE(wire_name, ''), alias, status, archived_at, spawned_by, spawned_by_id, report_to
			FROM task ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer q.Close()
		for q.Next() {
			var r row
			if err := q.Scan(&r.id, &r.wire, &r.alias, &r.status, &r.archived, &r.by, &r.byID, &r.reportTo); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return q.Err()
	}); err != nil {
		// nil, so the caller falls back to the per-card resolve rather than serving a list with no launchers
		return nil
	}
	ids := make(map[string]bool, len(rows))
	wire := make(map[string]string, len(rows))
	liveAlias := map[string]string{}
	doneAlias := map[string]string{}
	for _, r := range rows {
		ids[r.id] = true
		if _, ok := wire[r.wire]; !ok {
			wire[r.wire] = r.id
		}
		if a := NormalizeAlias(r.alias); a != "" && r.status != StatusDead && r.archived == "" {
			m := liveAlias
			if r.status == StatusDone {
				m = doneAlias
			}
			if _, ok := m[a]; !ok {
				m[a] = r.id
			}
		}
	}
	byWire := func(name string) string { return wire[s.Qualify(name)] }
	local := func(name string) string {
		if id := byWire(name); id != "" {
			return id
		}
		if a := NormalizeAlias(name); a != "" {
			if id := liveAlias[a]; id != "" {
				return id
			}
			if id := doneAlias[a]; id != "" {
				return id
			}
		}
		if ids[name] {
			return name
		}
		return ""
	}
	out := map[string]string{}
	for _, r := range rows {
		if r.by == "" || r.by == HumanLauncher {
			continue
		}
		if id := local(r.reportTo); r.reportTo != "" && id != "" && id != r.id {
			out[r.id] = id
		} else if r.byID != "" && ids[r.byID] && r.byID != r.id {
			out[r.id] = r.byID
		} else if !strings.Contains(r.by, "@") {
			if id := byWire(r.by); id != "" && id != r.id {
				out[r.id] = id
			}
		}
	}
	return out
}
