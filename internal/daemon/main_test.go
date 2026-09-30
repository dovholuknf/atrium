package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/internal/testguard"
)

// TestMain shortens the waits that production keeps long. Each is a seam, and
// a test that needs the real value sets it back itself.
func TestMain(m *testing.M) {
	// NO TEST REACHES A LIVE ROOM, whatever the shell this ran from. Every
	// ATRIUM_* pointer goes, the process gets an empty home so a runner finds no
	// hooks, and an agent is never started for real: it becomes a shell that
	// waits. See internal/testguard.
	guard := testguard.Scrub()
	testguard.Home(guard)
	agentSpawn = func(exe string, args []string) (string, []string) {
		if !testguard.IsAgent(exe) {
			return exe, args
		}
		if runtime.GOOS == "windows" {
			return "cmd.exe", []string{"/d", "/k"}
		}
		return "sh", []string{"-c", "read x"}
	}
	agentFork = func(exe string, args []string) (string, []string) {
		if !testguard.IsAgent(exe) {
			return exe, args
		}
		if runtime.GOOS == "windows" {
			return "cmd.exe", []string{"/d", "/c", "exit", "1"}
		}
		return "sh", []string{"-c", "exit 1"}
	}
	// A launched runner has 2 s to fall over; a test runner that will survive
	// shows it in a fraction of that, and one that falls over does at once.
	settleWindow = 300 * time.Millisecond
	shelveGrace = time.Second
	// A test runner that ignores an exit key would otherwise cost 600 ms per key
	// on cleanup.
	windDownKeyGap = 100 * time.Millisecond
	// No PATH walk on every daemon a test starts.
	runnerFound = func(*store.Harness) string { return "" }
	code := m.Run()
	if migratedPath != "" {
		_ = os.RemoveAll(filepath.Dir(migratedPath))
	}
	if guard != "" {
		_ = os.RemoveAll(guard)
	}
	os.Exit(code)
}

var (
	migratedOnce sync.Once
	migratedPath string
	migratedErr  error
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
