package store

import (
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
	if handle != "" && handle != HumanLauncher {
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

// OwesReport reports whether a card has been given something to do since it
// last told its launcher anything. A card never prompted owes nothing.
func (t *Task) OwesReport() bool {
	if t.PromptedAt == nil {
		return false
	}
	return t.ReportedAt == nil || t.ReportedAt.Before(*t.PromptedAt)
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
	if t.PromptedAt == nil {
		return ""
	}
	return t.PromptedAt.UTC().Format(time.RFC3339Nano)
}
