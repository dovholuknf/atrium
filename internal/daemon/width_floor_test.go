package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
)

// A RUNNER'S PTY NEVER GOES UNDER THE FLOOR, whatever a viewer reports. A
// narrow viewer is sized up to the floor and scrolls sideways on the board.
func TestANarrowViewerIsHeldAtTheFloor(t *testing.T) {
	d := testDaemon(t)
	f := narrowSession(t, d, "floored", "drawn at eighty columns\n")

	attachAs(t, d, "floored", 60, 30)

	sizes := f.resized()
	if len(sizes) == 0 || sizes[len(sizes)-1] != (viewport{120, 30}) {
		t.Fatalf("a 60 column viewer sized the pty to %+v, wanted the 120 floor", sizes)
	}
}

// The floor is the setting in force, not a constant.
func TestTheFloorFollowsTheSetting(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(api.SettingTerminalMinCols, "200"); err != nil {
		t.Fatal(err)
	}
	f := narrowSession(t, d, "floored-200", "drawn at eighty columns\n")

	attachAs(t, d, "floored-200", 150, 30)

	sizes := f.resized()
	if len(sizes) == 0 || sizes[len(sizes)-1].cols != 200 {
		t.Fatalf("a 150 column viewer under a 200 floor sized the pty to %+v", sizes)
	}
}

// A SHELL IS EXEMPT. It does not reprint, so narrow does no harm there.
func TestAShellIgnoresTheFloor(t *testing.T) {
	d := testDaemon(t)
	f := newFakePTY()
	r := &runner{
		taskID:   "shell-floor",
		pty:      f,
		started:  time.Now(),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	d.sup.addShell(r)
	t.Cleanup(func() { f.Close() })

	attachPath(t, d, "/v1/tasks/shell-floor/attach?kind=shell", 60, 30)

	sizes := f.resized()
	if len(sizes) == 0 || sizes[len(sizes)-1] != (viewport{60, 30}) {
		t.Fatalf("a shell was sized to %+v, wanted the viewer's 60 columns", sizes)
	}
}
