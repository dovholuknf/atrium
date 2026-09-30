package daemon

import (
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	settleWindow = 300 * time.Millisecond
	shelveGrace = time.Second
	// A test runner that ignores the first exit key would otherwise cost every
	// test that stops one 600 ms per key on cleanup.
	// The settle window is 2 s in production; a test runner that will survive
	// launch shows it in a fraction of that, and one that falls over does at once.
	windDownKeyGap = 100 * time.Millisecond
	os.Exit(m.Run())
}
