//go:build integration

package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

// A launch that arrives while a stop is in flight is refused with a retry, never answered running: the restart
// takes the runner and its first prompt, and nothing keeps the prompt.
func TestLaunchWhileStopIsInFlightIsRefusedRetryably(t *testing.T) {
	dir := t.TempDir()
	d, err := testNew(Options{
		DBPath:       filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatalf("daemon did not start: %v", err)
	}
	closeAtCleanup(t, d)

	if err := d.launchWhileStopping(); err != nil {
		t.Fatalf("a launch with no stop in flight was refused: %v", err)
	}
	d.stop.begin()
	_, err = d.launchFromJSON([]byte(`{"harness":"claude","cwd":"` + filepath.ToSlash(dir) + `","prompt":"hi"}`))
	if err == nil || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("a launch during a stop = %v, want a refusal that says to retry", err)
	}
}
