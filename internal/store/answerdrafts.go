package store

import (
	"encoding/json"
	"fmt"
)

// MaxAnswerDrafts bounds a card's drafted answers, for the same reason MaxNote exists: operator text with no
// other limit on it.
const MaxAnswerDrafts = 32000

// SetAnswerDrafts replaces a card's drafted answers. Empty clears them. Anything else must be a JSON array, which
// is all the store knows of its shape. Like a note, it is the operator thinking and does not touch last_activity_at.
func (s *Store) SetAnswerDrafts(id, drafts string) error {
	if len(drafts) > MaxAnswerDrafts {
		return fmt.Errorf("drafted answers are over %d characters", MaxAnswerDrafts)
	}
	if drafts != "" {
		var v []json.RawMessage
		if err := json.Unmarshal([]byte(drafts), &v); err != nil {
			return fmt.Errorf("drafted answers must be a JSON array: %w", err)
		}
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET answer_drafts = ? WHERE id = ?`, drafts, id)
		return err
	})
}
