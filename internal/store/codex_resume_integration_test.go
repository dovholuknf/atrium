//go:build integration

package store

import (
	"strings"
	"testing"
)

func TestCodexHasAWayToResume(t *testing.T) {
	s := open(t)
	if err := s.SeedHarnesses(); err != nil {
		t.Fatal(err)
	}
	h, err := s.Harness("codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ResumeArgs) == 0 {
		t.Fatal("codex still has no way to resume, so its cards cannot be picked back up")
	}
	if strings.Join(h.ResumeArgs, " ") != "resume {resume}" {
		t.Fatalf("codex resumes with %v, and codex takes `codex resume <SESSION_ID>`", h.ResumeArgs)
	}
}

// A database that already exists never runs the seed, so the value has to
// arrive as a migration or every machine atrium is already installed on keeps
// the empty list forever.
func TestAnExistingCodexRowGetsItsResumeArgs(t *testing.T) {
	s := open(t)
	if err := s.SeedHarnesses(); err != nil {
		t.Fatal(err)
	}
	// Back to what shipped before, the way an old database looks.
	if _, err := s.db.Exec(`UPDATE harness SET resume_args = '[]' WHERE id = 'codex'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0040_codex_resume'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}

	h, err := s.Harness("codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ResumeArgs) == 0 {
		t.Fatal("an existing codex row was left with no way to resume")
	}
}

// And an operator who already wrote their own is not overwritten by it. A
// migration that corrects a default has no business correcting a decision.
func TestTheMigrationLeavesAnOperatorsOwnResumeAlone(t *testing.T) {
	s := open(t)
	if err := s.SeedHarnesses(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`UPDATE harness SET resume_args = '["resume","--last"]' WHERE id = 'codex'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0040_codex_resume'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}

	h, err := s.Harness("codex")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.ResumeArgs, " ") != "resume --last" {
		t.Fatalf("the migration overwrote what the operator wrote: %v", h.ResumeArgs)
	}
}
