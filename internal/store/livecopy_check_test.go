package store

import (
	"os"
	"testing"
)

// Run the migrations against a COPY of a real database, which is the rule in
// internal/store/CLAUDE.md and the only way to find out what a guarded UPDATE
// does to rows that already exist.
//
// Skipped unless ATRIUM_LIVE_COPY names one, so the suite stays hermetic. Copy
// the database out, point this at the copy, delete the copy.
func TestMigrationsAgainstACopyOfALiveDatabase(t *testing.T) {
	path := os.Getenv("ATRIUM_LIVE_COPY")
	if path == "" {
		t.Skip("set ATRIUM_LIVE_COPY to a COPY of a real atrium.db")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("opening a copy of a real database failed, so the migration would halt a daemon: %v", err)
	}
	defer s.Close()

	hs, err := s.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hs {
		t.Logf("%-10s enabled=%v resume=%v prompt=%v", h.ID, h.Enabled, h.ResumeArgs, h.PromptArgs)
	}
}
