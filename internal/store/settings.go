package store

import "database/sql"

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

// GlobalAuto reports whether every session is being approved without asking.
//
// A read failure answers false. This sits on the permission path, and the safe
// answer to "should I stop asking" is no.
func (s *Store) GlobalAuto() bool {
	v, err := s.Setting(SettingGlobalAuto)
	return err == nil && v == "on"
}

func (s *Store) SetGlobalAuto(on bool) error {
	if on {
		return s.SetSetting(SettingGlobalAuto, "on")
	}
	return s.SetSetting(SettingGlobalAuto, "off")
}
