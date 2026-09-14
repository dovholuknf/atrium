package store

import (
	"path/filepath"
	"testing"
)

// A launch that needs a click is not a launch. A global MCP server that will
// not connect makes claude ask, at startup, whether to go on without it, and a
// worker atrium started has nobody watching to answer. The flag that ends that
// has to be on the command line atrium builds, on the first start and on every
// resume after it.

func carries(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// The seeded row, which is what a machine setting atrium up today gets.
func TestTheClaudeRowStartsWithNoMCPServers(t *testing.T) {
	s := openTestStore(t)

	h, err := s.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !carries(h.Args, "--strict-mcp-config") {
		t.Fatalf("a fresh launch still inherits the global MCP servers: %q", h.Args)
	}
	// Resume arguments REPLACE the base ones rather than adding to them, so a
	// flag only in `args` is a flag a resumed session does not have, and the
	// second start pops the dialog the first one avoided.
	if !carries(h.ResumeArgs, "--strict-mcp-config") {
		t.Fatalf("a resumed session still inherits the global MCP servers: %q", h.ResumeArgs)
	}
	if !carries(h.ResumeArgs, "{resume}") {
		t.Fatalf("the resume arguments lost the id they resume by: %q", h.ResumeArgs)
	}
}

// Nothing names an MCP config file. `--mcp-config` pointed at a path that does
// not exist is a hard startup failure, so a default naming `.mcp.json` would
// refuse to start in every worktree without one: a dead launch instead of a
// modal, which is not an improvement.
func TestNoMCPConfigFileIsNamed(t *testing.T) {
	s := openTestStore(t)

	h, err := s.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{h.Args, h.ResumeArgs} {
		if carries(args, "--mcp-config") {
			t.Fatalf("the launch names an MCP config file, so it fails to start "+
				"wherever that file is missing: %q", args)
		}
	}
}

// The flag is claude's spelling. A shell handed it would try to execute it.
func TestAShellIsNotGivenTheFlag(t *testing.T) {
	s := openTestStore(t)

	h, err := s.Harness("shell")
	if err != nil {
		t.Fatal(err)
	}
	if carries(h.Args, "--strict-mcp-config") {
		t.Fatalf("a shell was given claude's flag: %q", h.Args)
	}
}

// THE BACKFILL IS WHY THIS WORKS ON A DATABASE THAT ALREADY EXISTS.
// `DefaultHarnesses` is seeded once on first run, so without the migration the
// operator's own `claude` row keeps the command line it was seeded with and
// keeps raising the dialog on every launch.
func TestAnExistingClaudeRowIsBackfilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Put the row back the way it was before this change, and forget the
	// migration, so reopening has to do the work again.
	if _, err := s.db.Exec(
		`UPDATE harness SET args = '[]', resume_args = '["--resume","{resume}"]'
		   WHERE id = 'claude'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`DELETE FROM schema_migration WHERE name = '0048_strict_mcp_config'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	h, err := reopened.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !carries(h.Args, "--strict-mcp-config") {
		t.Fatalf("an existing claude row still launches with the global MCP servers: %q", h.Args)
	}
	if !carries(h.ResumeArgs, "--strict-mcp-config") {
		t.Fatalf("an existing claude row still resumes with the global MCP servers: %q", h.ResumeArgs)
	}
}

// An operator who has written their own command line keeps it. The migration
// is a fix for a default nobody chose, not permission to rewrite an edit.
func TestAnEditedClaudeRowIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edited.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`UPDATE harness SET args = '["--verbose"]', resume_args = '["--continue"]'
		   WHERE id = 'claude'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`DELETE FROM schema_migration WHERE name = '0048_strict_mcp_config'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	h, err := reopened.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Args) != 1 || h.Args[0] != "--verbose" {
		t.Fatalf("an operator's own arguments were overwritten: %q", h.Args)
	}
	if len(h.ResumeArgs) != 1 || h.ResumeArgs[0] != "--continue" {
		t.Fatalf("an operator's own resume arguments were overwritten: %q", h.ResumeArgs)
	}
}
