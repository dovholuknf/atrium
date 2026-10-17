//go:build integration

package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	templateOnce sync.Once
	templateDir  string
	templateErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if templateDir != "" {
		os.RemoveAll(templateDir)
	}
	os.Exit(code)
}

// Migrating a database is about 110 transactions, which a hosted Windows runner pays hundreds of times over.
func openMigrated(t *testing.T) *Store {
	t.Helper()
	templateOnce.Do(func() {
		templateDir, templateErr = os.MkdirTemp("", "atrium-store-template")
		if templateErr != nil {
			return
		}
		s, err := Open(filepath.Join(templateDir, "atrium.db"))
		if err != nil {
			templateErr = err
			return
		}
		templateErr = s.Close()
	})
	if templateErr != nil {
		t.Fatalf("template: %v", templateErr)
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
