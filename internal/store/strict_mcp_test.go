package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// Default Claude launches and resumes must disable MCP connection prompts
// so they can start unattended.

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

// Do not name an MCP config file: a missing file would fail startup in
// worktrees that have no .mcp.json.
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

// THE FLAG IS CLAUDE'S SPELLING and belongs to nothing else. Anything handed
// it would try to execute it, and the backfill that adds it is a `LIKE
// '%claude%'` over the command, which is the kind of match that catches a
// neighbour.
//
// This used to name the shell row, which no longer exists: a shell is the
// machine's, not a runner.
func TestOnlyClaudeIsGivenClaudesFlag(t *testing.T) {
	s := openTestStore(t)

	rows, err := s.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rows {
		if strings.Contains(strings.ToLower(h.Cmd), "claude") {
			continue
		}
		for _, args := range [][]string{h.Args, h.ResumeArgs} {
			if carries(args, "--strict-mcp-config") {
				t.Errorf("%s (%s) was given claude's flag: %q", h.ID, h.Cmd, args)
			}
		}
	}
}

// Verify the migration updates existing defaults, since DefaultHarnesses
// only seeds new databases.
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
