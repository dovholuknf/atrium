//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

// THE REAL TIMER, once, so the path by hand above is the one that runs.
func TestTheHoldTimerAppliesTheHeight(t *testing.T) {
	t.Parallel()
	r, f := heldRunner(t)
	r.resizeMu.Lock()
	r.hold = 20 * time.Millisecond
	r.resizeMu.Unlock()
	_ = r.setViewport("short", 120, 40)
	testwait.Poll(5*time.Second, func() bool { return len(f.resized()) != 0 })
	if sizes := f.resized(); len(sizes) != 1 || sizes[0] != (viewport{120, 40}) {
		t.Fatalf("the timer did not apply the height once: %+v", sizes)
	}
}
