//go:build integration

package store

import (
	"testing"
)

// A CONSTRAINT NEVER HALTS THE ROOM (r-new-review-f4466ea0): a foreign key on any
// table, not only the event insert, is the caller's error.
func TestAConstraintNeverHalts(t *testing.T) {
	s := open(t)
	err := s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO event (id, task_id, at, kind, payload) VALUES ('x', 'no-such-card', '2026-09-30T00:00:00.000Z', 'notified', '{}')`)
		return err
	})
	if err == nil || !constraint(err) {
		t.Fatalf("a foreign key failure answered %v, want the constraint error", err)
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("a constraint halted the store: %v", cause)
	}
}
