package store

import (
	"database/sql"
	"strings"
)

// SettingScratchRoot is the room setting that names where its cards put throwaway files. Empty means the card's own home.
const SettingScratchRoot = "scratch_root"

// CardScratch is a scratch folder atrium made for a card. A folder with no row here is never removed by atrium.
type CardScratch struct {
	Path      string `json:"path"`
	Card      string `json:"card"`
	CreatedAt string `json:"created_at"`
	State     string `json:"state"`
}

// ScratchRoot is the room's scratch root as stored, trimmed. Empty when unset.
func (s *Store) ScratchRoot() string {
	v, _ := s.Setting(SettingScratchRoot)
	return strings.TrimSpace(v)
}

// CardScratchAdd records a folder atrium is about to make for a card, live. A path already recorded is left as it is.
func (s *Store) CardScratchAdd(path, card string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO card_scratch (path, card, created_at, state) VALUES (?, ?, ?, 'live')`,
			path, card, ts(s.now()))
		return err
	})
}

// CardScratchOf is the live scratch folder atrium made for a card. False when there is none.
func (s *Store) CardScratchOf(card string) (CardScratch, bool, error) {
	var c CardScratch
	found := false
	err := s.guard(func() error {
		err := s.db.QueryRow(`SELECT path, card, created_at, state FROM card_scratch WHERE card = ? AND state = 'live' ORDER BY created_at DESC LIMIT 1`,
			card).Scan(&c.Path, &c.Card, &c.CreatedAt, &c.State)
		if err == sql.ErrNoRows {
			return nil
		}
		found = err == nil
		return err
	})
	return c, found, err
}

// CardScratchRemoved marks a folder's row removed once the folder is gone.
func (s *Store) CardScratchRemoved(path string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE card_scratch SET state = 'removed' WHERE path = ?`, path)
		return err
	})
}
