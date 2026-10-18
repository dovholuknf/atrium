//go:build integration

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var templateDir = filepath.Join(os.TempDir(), fmt.Sprintf("atrium-store-template-%d", os.Getpid()))

var migratedTemplate = sync.OnceValue(func() error {
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		return err
	}
	s, err := Open(filepath.Join(templateDir, "atrium.db"))
	if err != nil {
		return err
	}
	return s.Close()
})

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll(templateDir)
	os.Exit(code)
}

// Migrating a database is about 110 transactions, which a hosted Windows runner pays hundreds of times over.
func openMigrated(t *testing.T) *Store {
	t.Helper()
	if err := migratedTemplate(); err != nil {
		t.Fatalf("template: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(templateDir, "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "atrium.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
