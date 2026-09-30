package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// TestMain shortens the waits that production keeps long. Each is a seam, and
// a test that needs the real value sets it back itself.
func TestMain(m *testing.M) {
	// A launched runner has 2 s to fall over; a test runner that will survive
	// shows it in a fraction of that, and one that falls over does at once.
	settleWindow = 300 * time.Millisecond
	shelveGrace = time.Second
	// A test runner that ignores an exit key would otherwise cost 600 ms per key
	// on cleanup.
	windDownKeyGap = 100 * time.Millisecond
	// No PATH walk on every daemon a test starts.
	runnerFound = func(*store.Harness) string { return "" }
	os.Exit(m.Run())
}
