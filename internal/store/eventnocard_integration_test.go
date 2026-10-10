//go:build integration

package store

import (
	"database/sql"
	"errors"
	"testing"
)

// AN EVENT ON A CARD THAT IS NOT THERE IS REFUSED, NEVER A HALT
// (r-new-review-c184ae8c). The foreign key failure used to halt the room.
func TestAnEventOnNoCardDoesNotHalt(t *testing.T) {
	s := open(t)
	for _, id := range []string{"", "01a0dead-0000-7000-8000-000000000000"} {
		err := s.AppendEvent(id, EventNotified, map[string]any{"by": "test"})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("an event on card %q answered %v, want no rows", id, err)
		}
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("the store halted: %v", cause)
	}
}
