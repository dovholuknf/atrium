package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A database created before standing rules existed has 0001 already recorded,
// so adding perm_rule to 0001 would never reach it and the first rule lookup
// would fail at runtime. Reopening must repair it.
func TestMigrationRepairsDatabaseMissingPermRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the old shape: drop the table, and leave every migration
	// recorded so a naive runner would consider the schema current.
	if _, err := s.db.Exec(`DROP TABLE perm_rule`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name != '0001_initial'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening a pre-rules database failed: %v", err)
	}
	defer reopened.Close()

	if _, err := reopened.AddRule("Bash", "go build", "approve", "", ""); err != nil {
		t.Fatalf("perm_rule was not restored: %v", err)
	}
	rule, err := reopened.MatchRule("Bash", "go build ./...", "")
	if err != nil {
		t.Fatalf("rule lookup still fails, which is what wedged the daemon: %v", err)
	}
	if rule == nil {
		t.Fatal("restored table did not match")
	}
	if wedged, cause := reopened.Wedged(); wedged {
		t.Fatalf("store wedged during repair: %v", cause)
	}
}

// Reopening an already-current database must be a no-op, not a duplicate
// column error from the ADD COLUMN migration.
func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repeat.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migration`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(migrations) {
		t.Fatalf("recorded %d migrations, expected %d", n, len(migrations))
	}
}

// Every table the code touches has to exist after a fresh open. This is the
// check that would have caught the missing perm_rule before it wedged.
func TestAllTablesExistAfterMigrate(t *testing.T) {
	s := open(t)
	for _, table := range []string{"task", "event", "permission", "perm_rule", "launch_spec", "schema_migration"} {
		var name string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %s missing after migrate", table)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// decided_by has to survive a decision, or the log cannot say who answered.
func TestDecisionRecordsWhoAnswered(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "hist", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	byHand, _, err := s.RecordPermission(task.ID, "Bash", "ls", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermission(byHand.ID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	byRule, _, err := s.RecordPermission(task.ID, "Bash", "go build ./...", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermissionBy(byRule.ID, "approve", "", "go build"); err != nil {
		t.Fatal(err)
	}
	hist, err := s.History(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("want 2 decisions, got %d", len(hist))
	}
	seen := map[string]string{}
	for _, p := range hist {
		seen[p.Command] = p.DecidedBy
	}
	if seen["ls"] != DecidedBySelf {
		t.Errorf("hand decision recorded as %q, want %q", seen["ls"], DecidedBySelf)
	}
	if seen["go build ./..."] != "go build" {
		t.Errorf("rule decision recorded as %q, want the rule pattern", seen["go build ./..."])
	}
	// Pending must not include decided rows.
	pending, err := s.PendingPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("decided requests still pending: %d", len(pending))
	}
}
