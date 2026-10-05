package store

import (
	"database/sql"
	"fmt"
	"os"
)

// claimFile proves nobody else has the database at path open, the way Compact does,
// and lets go again. A running room holds its store, so this fails with ErrDatabaseInUse.
func claimFile(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// EXCLUSIVE locking is what a WAL database refuses while another process has it open,
	// the same probe Compact makes.
	for _, stmt := range []string{"PRAGMA busy_timeout = 0", "PRAGMA locking_mode = EXCLUSIVE"} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("%w (%s: %v)", ErrDatabaseInUse, path, err)
	}
	_, err = db.Exec("COMMIT")
	return err
}

// SettingOfFile reads one setting from the database at path, which must already exist and
// must not be open elsewhere. A key never written reads as empty.
func SettingOfFile(path, key string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	if err := claimFile(path); err != nil {
		return "", err
	}
	s, err := Open(path)
	if err != nil {
		return "", err
	}
	defer s.Close()
	return s.Setting(key)
}

// SetSettingOfFile writes one setting into the database at path, which must not be open
// elsewhere. A database that does not exist yet is made, so a machine can be set up before its
// room first runs: the room opens it later and migrates it as it would any other.
func SetSettingOfFile(path, key, value string) error {
	if _, err := os.Stat(path); err == nil {
		if err := claimFile(path); err != nil {
			return err
		}
	}
	s, err := Open(path)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.SetSetting(key, value)
}
