//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
)

// A clear mark put in the stream of a terminal under a pty host is still in its replay after the room restarts and
// re-adopts it, at the offset it was written at and not at the end.
func TestAClearMarkSurvivesAReadoptOfAPtyHostTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "mark-readopt", t.TempDir())
	sh, args := someShell(t)
	gate := filepath.Join(t.TempDir(), "gate")
	if _, err := d1.spawnPTY(task.ID, sh, append(args,
		"echo AAA-first; "+gatedOn(gate)+"; echo BBB-second; sleep 60"), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	r1 := d1.sup.get(task.ID)
	if hostOf(r1) == nil {
		t.Fatal("the runner is not on the host")
	}
	hostWait(t, 20*time.Second, "AAA in the first daemon's ring", func() bool { return ringHas(r1, "AAA-first") })
	r1.markClear()
	hostWait(t, 5*time.Second, "the mark in the first daemon's ring", func() bool { return ringHas(r1, screen.ClearMarkOSC) })
	// The mark reaches the host on its own round trip, and the gate holds BBB back until the test opens it.
	time.Sleep(500 * time.Millisecond) // wait: the mark's round trip to the host raises no event on this side

	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	r2 := d2.sup.get(task.ID)
	if r2 == nil || r2.runID != r1.runID {
		t.Fatalf("the run was not reattached: %+v", r2)
	}
	openGate(t, gate)
	hostWait(t, 30*time.Second, "BBB in the reattached ring", func() bool { return ringHas(r2, "BBB-second") })

	out := replayOf(r2)
	a, m, b := strings.Index(out, "AAA-first"), strings.Index(out, screen.ClearMarkOSC), strings.Index(out, "BBB-second")
	if !(a >= 0 && m > a && b > m) {
		t.Fatalf("the mark is not between the two lines of the replay: A=%d mark=%d B=%d in %q", a, m, b, out)
	}
	if n := strings.Count(out, screen.ClearMarkOSC); n != 1 {
		t.Fatalf("the replay holds %d marks, want 1", n)
	}
}
