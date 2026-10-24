package daemon

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testguard"
	"github.com/dovholuknf/atrium/internal/store"
)

// testTune is what every daemon a test starts runs with instead of production's values: no agent is started for
// real, no PATH walk, and the waits are short. Each daemon has its own copy, so what one test changes after that
// reaches no other.
func testTune(hk *hooks) {
	hk.agentSpawn = func(exe string, args []string) (string, []string) {
		if !testguard.IsAgent(exe) {
			return exe, args
		}
		if runtime.GOOS == "windows" {
			return "cmd.exe", []string{"/d", "/k"}
		}
		return "sh", []string{"-c", "read x"}
	}
	hk.agentFork = func(exe string, args []string) (string, []string) {
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
	hk.helpRunner = func(exe string) (string, error) {
		if testguard.IsAgent(exe) && !strings.ContainsAny(exe, `/\`) {
			return "", os.ErrNotExist
		}
		return runHelp(exe)
	}
	// A launched runner has 2 s to fall over; a test runner that will survive
	// shows it in a fraction of that, and one that falls over does at once.
	hk.settleWindow = 300 * time.Millisecond
	hk.shelveGrace = time.Second
	// A test runner that ignores an exit key would otherwise cost 600 ms per key
	// on cleanup.
	hk.windDownKeyGap = 100 * time.Millisecond
	// No PATH walk on every daemon a test starts.
	hk.runnerFound = func(*store.Harness) string { return "" }
}

// testNew is New with testTune applied, for a test that builds its own daemon.
func testNew(opts Options) (*Daemon, error) {
	d, err := testNewAutoUnwritten(opts)
	if err != nil {
		return nil, err
	}
	if err := d.st.SetGlobalAuto(false); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func testNewAutoUnwritten(opts Options) (*Daemon, error) {
	d, err := newWith(opts, testTune)
	if err != nil {
		return nil, err
	}
	if err := d.st.NoSync(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// testHooks is the hooks of a test that has no daemon. Each call is its own copy.
func testHooks() *hooks {
	hk := defaultHooks()
	testTune(&hk)
	return &hk
}
