package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// LaunchKeyWindow is how long after a card is filed its launch key still answers a repeat. A retry comes within
// minutes of the first attempt, so an older card under the same key is a different launch, and the key is let go of
// rather than kept for ever. The key stays on the row, and the row goes when the card does.
const LaunchKeyWindow = time.Hour

// SetLaunchKey records the key a card was launched under. An empty key writes nothing.
func (s *Store) SetLaunchKey(id, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET launch_key = ? WHERE id = ?`, key, id)
		return err
	})
}

// GetByLaunchKey is the newest card filed under the key inside LaunchKeyWindow, or sql.ErrNoRows. The store is one
// room's, so the key is scoped to the room by construction.
func (s *Store) GetByLaunchKey(key string) (*Task, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, sql.ErrNoRows
	}
	var t *Task
	err := s.guard(func() error {
		var id, created string
		if err := s.db.QueryRow(
			`SELECT id, created_at FROM task WHERE launch_key = ? ORDER BY created_at DESC LIMIT 1`, key).Scan(&id, &created); err != nil {
			return err
		}
		at, err := parseTS(created)
		if err != nil {
			return err
		}
		if s.now().Sub(at) > LaunchKeyWindow {
			return sql.ErrNoRows
		}
		got, err := s.getBy(`id = ?`, id)
		t = got
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	return t, err
}
