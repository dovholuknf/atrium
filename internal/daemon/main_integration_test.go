//go:build integration

package daemon

import (
	"os"
	"testing"
)

// seedMigratedDB puts an already-migrated, empty database at path, so a test
// that does not care how its database came to exist skips the schema
// migration, which costs far more than the test. A test of a NEW database
// starts its own daemon and does not call this.
func seedMigratedDB(t testing.TB, path string) {
	t.Helper()
	template, err := migratedTemplate()
	if err != nil {
		t.Fatalf("building the migrated template: %v", err)
	}
	b, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
