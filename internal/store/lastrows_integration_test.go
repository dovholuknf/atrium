//go:build integration

package store

import "testing"

func TestALastSizeRoundTripsBothSides(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "sized")
	if err := s.SetLastSize(c.ID, 177, 48); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastCols != 177 || got.LastRows != 48 {
		t.Fatalf("read back %dx%d, want 177x48", got.LastCols, got.LastRows)
	}

	// Half a size is no size, so it never replaces a real answer.
	if err := s.SetLastSize(c.ID, 90, 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(c.ID); got.LastCols != 177 || got.LastRows != 48 {
		t.Fatalf("half a size replaced the saved one: %dx%d", got.LastCols, got.LastRows)
	}
}

func TestANewCardHasNoLastRows(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "fresh")
	if got, _ := s.Get(c.ID); got.LastRows != 0 {
		t.Fatalf("a new card reads %d rows, want none", got.LastRows)
	}
}

func TestTheLastRowsMigrationToleratesItsColumn(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "kept")
	if err := s.SetLastSize(c.ID, 120, 33); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0070_task_last_rows'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("the migration did not tolerate its column already being there: %v", err)
	}
	if got, _ := s.Get(c.ID); got.LastRows != 33 {
		t.Fatalf("the height did not survive the migration running again: %d", got.LastRows)
	}
}
