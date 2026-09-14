package store

import "strings"

// Record temporary status at launch, a promotion request while running,
// and the final state after exit. Filesystem moves and deletion are handled
// by the daemon once the process has stopped.

// SetThrowaway marks a directory created by launch for later cleanup.
// Never mark a directory supplied by the operator.
func (s *Store) SetThrowaway(id string, on bool) error {
	return s.guard(func() error {
		v := 0
		if on {
			v = 1
		}
		_, err := s.db.Exec(`UPDATE task SET throwaway = ? WHERE id = ?`, v, id)
		return err
	})
}

// SetPromoteTo records where to move the directory after the session exits.
// It does not move files while the working directory is in use. An empty
// destination cancels promotion.
func (s *Store) SetPromoteTo(id, dir string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET promote_to = ? WHERE id = ?`,
			strings.TrimSpace(dir), id)
		return err
	})
}

// Promoted updates the path and clears temporary state atomically so cleanup
// cannot delete a directory that has just been moved.
func (s *Store) Promoted(id, worktree string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`UPDATE task SET worktree = ?, throwaway = 0, promote_to = '' WHERE id = ?`,
			strings.TrimSpace(worktree), id)
		return err
	})
}
