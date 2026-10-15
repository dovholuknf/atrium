//go:build integration

package hubstore

import (
	"path/filepath"
	"sync"
	"testing"
)

// openTogether opens every path at the same moment and returns what each Open said.
func openTogether(t *testing.T, paths []string) []error {
	t.Helper()
	errs := make([]error, len(paths))
	stores := make([]*Store, len(paths))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			stores[i], errs[i] = Open(p)
		}()
	}
	close(start)
	wg.Wait()
	for _, s := range stores {
		if s != nil {
			s.Close()
		}
	}
	return errs
}

// TWO HUBS STARTED AT THE SAME MOMENT. One was seen to halt with "table schema_migration already exists". Whether on
// one file or on two, no Open may fail mid-migration: the winner is unaffected and a loser either succeeds or refuses.
func TestHubsOpeningTheSameStoreTogetherDoNotFailMidMigration(t *testing.T) {
	for round := 0; round < 30; round++ {
		path := filepath.Join(t.TempDir(), "hub.db")
		for i, err := range openTogether(t, []string{path, path, path, path}) {
			if err != nil {
				t.Fatalf("round %d, opener %d: %v", round, i, err)
			}
		}
	}
}

func TestHubsOpeningSeparateStoresTogetherDoNotFailMidMigration(t *testing.T) {
	for round := 0; round < 30; round++ {
		dir := t.TempDir()
		paths := []string{filepath.Join(dir, "a", "hub.db"), filepath.Join(dir, "b", "hub.db")}
		for i, err := range openTogether(t, paths) {
			if err != nil {
				t.Fatalf("round %d, opener %d: %v", round, i, err)
			}
		}
	}
}
