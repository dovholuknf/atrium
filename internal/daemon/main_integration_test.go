//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// seedMigratedDB puts an already-migrated, empty database at path, so a test
// that does not care how its database came to exist skips the schema
// migration, which costs far more than the test. A test of a NEW database
// starts its own daemon and does not call this.
func seedMigratedDB(t *testing.T, path string) {
	t.Helper()
	migratedOnce.Do(func() {
		dir, err := os.MkdirTemp("", "atrium-template-")
		if err != nil {
			migratedErr = err
			return
		}
		migratedPath = filepath.Join(dir, "atrium.db")
		st, err := store.Open(filepath.ToSlash(migratedPath))
		if err != nil {
			migratedErr = err
			return
		}
		migratedErr = st.Close()
	})
	if migratedErr != nil {
		t.Fatalf("building the migrated template: %v", migratedErr)
	}
	b, err := os.ReadFile(migratedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
