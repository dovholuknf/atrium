package hubstore

import (
	"testing"
)

// f-026. A NAME THAT SPENT A JOIN SECRET IS PROVEN, and stays proven. The hub
// refuses the old overlay path for it on this answer.

func enrolledAt(t *testing.T, s *Store, id string) string {
	t.Helper()
	var at string
	if err := s.db.QueryRow(`SELECT enrolled_at FROM room WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestSpendingASecretEnrolsTheName(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	added(t, s, "athens")
	if on, err := s.Enrolled("sparta"); err != nil || on {
		t.Fatalf("a room that never joined reads as enrolled: %v %v", on, err)
	}
	secret, err := s.Mint(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Spend(secret); err != nil {
		t.Fatal(err)
	}
	// Folded, as every other name lookup is.
	if on, err := s.Enrolled("Sparta"); err != nil || !on {
		t.Fatalf("a room that spent its secret does not read as enrolled: %v %v", on, err)
	}
	if on, _ := s.Enrolled("athens"); on {
		t.Fatal("another room's enrolment leaked onto athens")
	}
	if on, err := s.Enrolled("nobody"); err != nil || on {
		t.Fatalf("a name with no row read as %v %v", on, err)
	}
}

// Only the first time is kept, so the column says when the name became proven.
func TestMarkingEnrolledTwiceKeepsTheFirst(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	if err := s.MarkEnrolled(r.ID); err != nil {
		t.Fatal(err)
	}
	first := enrolledAt(t, s, r.ID)
	if first == "" {
		t.Fatal("marking did not record anything")
	}
	if _, err := s.db.Exec(`UPDATE room SET enrolled_at = '2001-01-01T00:00:00Z' WHERE id = ?`, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkEnrolled(r.ID); err != nil {
		t.Fatal(err)
	}
	if got := enrolledAt(t, s, r.ID); got != "2001-01-01T00:00:00Z" {
		t.Fatalf("a second mark moved the time to %q", got)
	}
}

// A room forced out and added again is a new row, and a different room that
// happens to share the name. It starts unproven.
func TestARoomAddedAgainStartsUnproven(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	if err := s.MarkEnrolled(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Force(r.ID, "test"); err != nil {
		t.Fatal(err)
	}
	added(t, s, "sparta")
	if on, err := s.Enrolled("sparta"); err != nil || on {
		t.Fatalf("a re-added room inherited the old one's enrolment: %v %v", on, err)
	}
}

// THE BACKFILL, on a store that had rooms join before the column existed, and
// run twice. Only the `joined` line counts, and the second run changes nothing.
func TestTheEnrolledBackfillReadsJoinedLinesAndIsSafeTwice(t *testing.T) {
	s := open(t)
	joined := added(t, s, "sparta")
	other := added(t, s, "athens")
	s.Log(joined, "joined", "a join string was spent and this room has a credential")
	s.Log(other, "secret-minted", "a join string was issued")
	var at string
	if err := s.db.QueryRow(`SELECT at FROM room_audit WHERE room_id = ? AND kind = 'joined'`,
		joined.ID).Scan(&at); err != nil {
		t.Fatal(err)
	}

	rerun := func() {
		t.Helper()
		if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0006_room_enrolled'`); err != nil {
			t.Fatal(err)
		}
		if err := s.migrate(); err != nil {
			t.Fatalf("the migration did not run again cleanly: %v", err)
		}
	}
	if _, err := s.db.Exec(`UPDATE room SET enrolled_at = ''`); err != nil {
		t.Fatal(err)
	}
	rerun()
	if got := enrolledAt(t, s, joined.ID); got != at {
		t.Fatalf("the backfill set %q, wanted the joined line's %q", got, at)
	}
	if got := enrolledAt(t, s, other.ID); got != "" {
		t.Fatalf("a room that never joined was backfilled with %q", got)
	}
	rerun()
	if got := enrolledAt(t, s, joined.ID); got != at {
		t.Fatalf("a second run moved the time to %q", got)
	}
}
