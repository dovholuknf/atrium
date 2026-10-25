//go:build integration

package store

import (
	"errors"

	"github.com/dovholuknf/atrium/internal/base/dblock"
	"os"
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

// TWO ROOMS STARTED AT THE SAME MOMENT. One was seen to halt with "table schema_migration already exists". Whether on
// one file or on two, no Open may fail mid-migration: the winner is unaffected and a loser either succeeds or refuses.
func TestOpeningTheSameStoreTogetherNeverFailsMidMigration(t *testing.T) {
	for round := 0; round < 30; round++ {
		path := filepath.Join(t.TempDir(), "x.db")
		won := 0
		for i, err := range openTogether(t, []string{path, path, path, path}) {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, dblock.ErrHeld):
				t.Fatalf("round %d, opener %d: %v", round, i, err)
			}
		}
		if won != 1 {
			t.Fatalf("round %d: %d openers got the file, want exactly one", round, won)
		}
	}
}

func TestOpeningSeparateStoresTogetherDoNotFailMidMigration(t *testing.T) {
	for round := 0; round < 30; round++ {
		dir := t.TempDir()
		paths := []string{filepath.Join(dir, "a", "room.db"), filepath.Join(dir, "b", "room.db")}
		for _, p := range paths {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for i, err := range openTogether(t, paths) {
			if err != nil {
				t.Fatalf("round %d, opener %d: %v", round, i, err)
			}
		}
	}
}

func TestASecondOpenerOfAHeldStoreIsRefusedAndTheFirstIsUnharmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(path)
	var held *dblock.HeldError
	if !errors.As(err, &held) || held.Pid != os.Getpid() {
		t.Fatalf("want held by pid %d, got %v", os.Getpid(), err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatalf("after the holder closed: %v", err)
	}
	again.Close()
}

// A refused or shared open must not disturb the holder, and a crashed holder's leftover lock file must not block.
func TestASharedOpenBesideAHolderWorksAndAStaleLockFileDoesNotBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := OpenShared(path)
	if err != nil {
		t.Fatalf("shared open beside the holder: %v", err)
	}
	shared.Close()
	first.Close()
	if err := os.WriteFile(dblock.Path(path), []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("a lock file left by a crash blocked a restart: %v", err)
	}
	s.Close()
}
