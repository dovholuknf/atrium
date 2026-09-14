package store

import "strings"

// A card whose directory atrium made and will delete, and the way back out of
// one.
//
// Three writes, and they are three because the decision arrives at three
// different moments: the launch says "this is temporary", the operator says
// "keep it" while the session is still running, and the end of the session is
// where either is acted on. Nothing here deletes or moves anything: the store
// records which of the two a card is in, and `internal/daemon` does the work
// once the process is gone.

// SetThrowaway marks a card as living in a directory atrium will delete.
//
// Written by the launch that made the directory, and by nothing else. A card
// whose directory somebody chose is never temporary, however it was started.
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

// SetPromoteTo records where a throwaway's directory should go instead of
// being deleted.
//
// A PROMISE, NOT A MOVE. The session is usually still running when this is
// called, and its working directory cannot be moved out from under it on
// Windows. So the answer is written down and read at the end, which is the
// same place the delete happens and therefore the only place the two can be
// told apart.
//
// Empty cancels it, which is how somebody changes their mind before the
// session ends.
func (s *Store) SetPromoteTo(id, dir string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET promote_to = ? WHERE id = ?`,
			strings.TrimSpace(dir), id)
		return err
	})
}

// Promoted records that a throwaway now lives somewhere real.
//
// One statement for three fields on purpose: a card with a new directory that
// is still marked temporary would be deleted by the next thing that looked at
// it, and that is not a window worth leaving open.
func (s *Store) Promoted(id, worktree string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(
			`UPDATE task SET worktree = ?, throwaway = 0, promote_to = '' WHERE id = ?`,
			strings.TrimSpace(worktree), id)
		return err
	})
}
