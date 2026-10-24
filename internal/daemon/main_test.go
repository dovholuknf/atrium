package daemon

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/testguard"
	"github.com/dovholuknf/atrium/internal/store"
)

// TestMain scrubs the process of the operator's room. The waits production keeps long are shortened per daemon,
// in testTune, and a test that needs the real value sets it back on its own daemon.
func TestMain(m *testing.M) {
	// NO TEST REACHES A LIVE ROOM, whatever the shell this ran from. Every
	// ATRIUM_* pointer goes, the process gets an empty home so a runner finds no
	// hooks, and an agent is never started for real: it becomes a shell that
	// waits. See internal/testguard.
	guard := testguard.Scrub()
	testguard.Home(guard)
	// A fake claude runner is this test binary, and every claude launch now carries atrium's own
	// `--settings <json>`, which the test flag parser would refuse. Real claude takes it.
	for i := 1; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--settings" {
			os.Args = append(os.Args[:i], os.Args[i+2:]...)
			break
		}
	}
	code := m.Run()
	if migratedBuilt.Load() {
		if path, _ := migratedTemplate(); path != "" {
			_ = os.RemoveAll(filepath.Dir(path))
		}
	}
	if guard != "" {
		_ = os.RemoveAll(guard)
	}
	os.Exit(code)
}

// migratedBuilt says migratedTemplate ran, so TestMain removes what it made and builds nothing to do it.
var migratedBuilt atomic.Bool

// migratedTemplate is the path of an empty, already-migrated database, made once for the whole run.
var migratedTemplate = sync.OnceValues(func() (string, error) {
	migratedBuilt.Store(true)
	dir, err := os.MkdirTemp("", "atrium-template-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "atrium.db")
	st, err := store.Open(filepath.ToSlash(path))
	if err != nil {
		return path, err
	}
	return path, st.Close()
})
