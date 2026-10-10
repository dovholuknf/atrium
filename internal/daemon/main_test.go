package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// Every daemon a test runs probes its runners' --help at start. A bare agent name is the machine's own install,
	// so it is not asked, which reads as a runner that takes the flag. A test's fake runner is a path and still runs.
	helpRunner = func(exe string) (string, error) {
		if testguard.IsAgent(exe) && !strings.ContainsAny(exe, `/\`) {
			return "", os.ErrNotExist
		}
		return runHelp(exe)
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
