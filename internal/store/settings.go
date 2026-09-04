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
