package store

import (
	"database/sql"
	"strings"
	"time"
)

// Settings the daemon holds, as opposed to anything about one card.
//
// Kept in the database rather than in memory, because the whole reason the
// daemon has a database is that "what was I doing" must survive a restart.
// Global auto mode surviving one is the point: a restart is not consent to
// start asking again, and it is not consent to keep approving either. It is
// whatever it was.

// SettingGlobalAuto is on when every request from every session is approved
// without asking.
const SettingGlobalAuto = "global_auto"

// SettingSweepDead is how long a dead card stays on the board before it is
// ARCHIVED, in seconds. `off` means never, and unset means the default.
//
// SettingPruneAfter is how old a finished card has to be before it is DELETED,
// in seconds. `off`, which is also what unset means, is never.
//
// The two are different operations and the difference is the point. Sweeping
// takes a card off a screen and keeps every word of its history. Pruning
// destroys that history. One is on by default and the other is off by default
// for exactly that reason.
//
// The keys live here rather than next to the code that reads them, because the
// HTTP layer has to name them too and it cannot import the daemon.
const (
	SettingSweepDead  = "sweep_dead_after"
	SettingPruneAfter = "prune_after"
)

// SettingReplayMode picks how a card's history is turned back into a terminal.
//
// Three answers, and they differ in WHO EMULATES THE TERMINAL:
//
//   - `raw`: nobody here. The ring's bytes go down the socket untouched and
//     xterm.js renders them, which is the same emulator already rendering the
//     live stream. One emulator for both halves is the only arrangement where
//     history and live cannot disagree, and it is the only one that is
//     identical for claude, codex, ollama and a bare shell, because it
//     interprets nothing.
//   - `screen` (the default): a grid in `screen.go` applies the bytes and
//     reports what the terminal would have held plus what scrolled off it.
//     A second emulator, written here, and worse than the one in the browser.
//   - `flat`: `flatten.go` deletes every sequence that could overwrite
//     anything and pads with spaces where a cursor move was. Not an emulator
//     at all, which is why it cannot get a repaint right.
//
// `raw` is what the design says it should be. It is not the default yet for
// one reason, recorded in `flatten.go`: replaying a megabyte of history into a
// terminal was measured collapsing into a couple of screens, because a
// terminal user interface draws by moving the cursor and erasing, and on
// replay those moves land on the history instead of on the frame they were
// meant for. That measurement predates the screen model, which is an emulator
// and does not collapse, so it is worth re-testing rather than inheriting.
//
// A SWITCH RATHER THAN A REBUILD. This rendering has been declared fixed twice
// on the strength of tests written beside it, and reverted twice. Read on
// every attach, so changing it takes effect on the next attach.
const SettingReplayMode = "replay_mode"

// SettingEventSink names the hot sink and any cold sinks a card's history goes
// to, as a comma-separated list. The FIRST name is the hot sink that serves
// Recent; the rest are write-only cold sinks fanned out best-effort.
//
// Unset, or `db` alone, is the default: the event table is the hot sink and
// there are no cold sinks, which is byte-for-byte how every install behaved
// before this existed. `db,file` keeps the db hot and also appends every event
// to rolling JSONL files. Per the observed-versus-overrides rule this is an
// override a human types; nothing infers it.
//
// A name this build does not know is logged and skipped rather than fatal. A
// misconfigured cold trail must never keep the daemon from starting.
const SettingEventSink = "event_sink"

// SettingEventWindowBytes bounds the db hot sink to a recent window per card,
// measured in bytes of event payload. When a card's retained events exceed the
// window the OLDEST roll off the db, so the primary database stops growing
// without limit.
//
// The measure is a byte cap rather than a last-N-events count, for the same
// reason scrollback is bounded by size: `output` events carry chunks of
// terminal text and dominate the table, and their size varies wildly, so a
// count cannot bound what the db actually holds while a byte cap can.
//
// OFF BY DEFAULT. Unset, empty, or a value at or below zero means no bound: the
// db keeps every event, which is byte-for-byte how every install behaved before
// this existed. The bound only applies when an operator sets a positive value.
// Per the observed-versus-overrides rule this is an override a human types;
// nothing infers it and nothing auto-enables it.
//
// Safety with the cold trail: cold sinks receive every event at append time, so
// an event that later rolls off the db is already durable in any configured cold
// sink (a `file` sink, say). With no cold sink the roll-off is the operator's
// explicit choice to keep only the window; the board reports history rolled off
// rather than pretend the window is the whole story. See HistoryRolledOff.
const SettingEventWindowBytes = "event_window_bytes"

// Setting reads one value. A key that has never been written reads as empty
// rather than as an error, so a caller does not have to seed anything.
func (s *Store) Setting(key string) (string, error) {
	var v string
	err := s.guard(func() error {
		err := s.db.QueryRow(`SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
		if err == sql.ErrNoRows {
			v = ""
			return nil
		}
		return err
	})
	return v, err
}

// SetSetting writes one value.
func (s *Store) SetSetting(key, value string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value,
			                                updated_at = excluded.updated_at`,
			key, value, ts(now()))
		return err
	})
}

// untilPrefix marks a global auto value that has a deadline on it.
//
// Encoded into the value rather than given a second setting key, so there is
// one thing to read and one thing to write and the two cannot disagree. A
// deadline with the switch off, or a switch on with somebody else's stale
// deadline, are both states that simply cannot be represented.
const untilPrefix = "until:"

// GlobalAuto reports whether every session is being approved without asking.
//
// A read failure answers false. This sits on the permission path, and the safe
// answer to "should I stop asking" is no.
//
// A deadline that has passed also answers false, checked against the clock
// here rather than enforced by a timer somewhere. A timer that has to fire is
// a timer that does not fire across a restart, and auto mode surviving a
// restart it should not have survived is the failure worth designing against.
func (s *Store) GlobalAuto() bool {
	on, _ := s.GlobalAutoUntil()
	return on
}

// GlobalAutoUntil reports whether it is on, and when it stops.
//
// The second value is nil when it is on with no deadline, which is what the
// switch did before deadlines existed and still what turning it on by hand
// means.
func (s *Store) GlobalAutoUntil() (bool, *time.Time) {
	v, err := s.Setting(SettingGlobalAuto)
	if err != nil {
		return false, nil
	}
	if v == "on" {
		return true, nil
	}
	if !strings.HasPrefix(v, untilPrefix) {
		return false, nil
	}
	deadline, err := parseTS(strings.TrimPrefix(v, untilPrefix))
	if err != nil {
		// A value that will not parse is not a licence to approve everything.
		return false, nil
	}
	if !now().Before(deadline) {
		return false, &deadline
	}
	return true, &deadline
}

// SetGlobalAuto turns it on or off with no deadline.
func (s *Store) SetGlobalAuto(on bool) error {
	return s.SetGlobalAutoUntil(on, nil)
}

// SetGlobalAutoUntil turns it on for a while.
//
// Turning it off always clears the deadline, for the same reason a card's does:
// "off until Tuesday" is not a thing anybody means.
func (s *Store) SetGlobalAutoUntil(on bool, until *time.Time) error {
	if !on {
		return s.SetSetting(SettingGlobalAuto, "off")
	}
	if until == nil {
		return s.SetSetting(SettingGlobalAuto, "on")
	}
	return s.SetSetting(SettingGlobalAuto, untilPrefix+ts(*until))
}
