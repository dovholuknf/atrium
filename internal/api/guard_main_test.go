package api

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/testguard"
	"github.com/dovholuknf/atrium/internal/store"
)

// TestMain keeps every test here off a live atrium: no ATRIUM_* pointer from the
// shell this ran in reaches a test. See internal/testguard.
func TestMain(m *testing.M) {
	dir := testguard.Scrub()
	code := m.Run()
	if upstreamRepo.dir != "" {
		_ = os.RemoveAll(upstreamRepo.dir)
	}
	if migrated.dir != "" {
		_ = os.RemoveAll(migrated.dir)
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

// upstreamRepo is the one upstream repo the package builds, on the first ask. Its folder is removed by TestMain.
var upstreamRepo upstreamFixture

type upstreamFixture struct {
	once sync.Once
	dir  string
	err  error
}

// migrated is one empty, already-migrated database, built on the first ask and copied per test. TestMain removes its folder.
var migrated migratedFixture

type migratedFixture struct {
	once sync.Once
	dir  string
	err  error
}

func (m *migratedFixture) path() (string, error) {
	m.once.Do(func() {
		m.dir, m.err = os.MkdirTemp("", "atrium-api-template-")
		if m.err != nil {
			return
		}
		st, err := store.Open(filepath.ToSlash(filepath.Join(m.dir, "atrium.db")))
		if err != nil {
			m.err = err
			return
		}
		m.err = st.Close()
	})
	return filepath.Join(m.dir, "atrium.db"), m.err
}

// openMigrated opens this test's own copy of the migrated template, so no test pays the migration chain.
func openMigrated(t *testing.T) *store.Store {
	t.Helper()
	template, err := migrated.path()
	if err != nil {
		t.Fatalf("building the migrated template: %v", err)
	}
	b, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "atrium.db")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
