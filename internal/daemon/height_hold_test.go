package daemon

import (
	"testing"
	"time"
)

// The height hold, rule by rule. See `holdHeight` and docs/backlog-2.md item
// 74. Most of these never wait on the clock: the hold is an hour and the timer
// firing is called by hand, so a slow machine cannot make one flaky.

func heldRunner(t *testing.T) (*runner, *fakePTY) {
	t.Helper()
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{
		taskID:   "hold",
		pty:      f,
		buf:      newRingSized(1<<16, 120, 50),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
		hold:     time.Hour,
	}
	t.Cleanup(func() {
		r.resizeMu.Lock()
		r.cancelHeld()
		r.resizeMu.Unlock()
	})
	// The first viewer matches the launch size, so it is a no-op.
	if err := r.setViewport("pane", 120, 50); err != nil {
		t.Fatal(err)
	}
	return r, f
}

// fireHeld is the hold running out for whatever height is waiting now, and
// returns the generation it fired for.
func fireHeld(r *runner) uint64 {
	r.resizeMu.Lock()
	gen := r.pendingGen
	r.resizeMu.Unlock()
	r.applyHeld(gen)
	return gen
}

func heldRows(r *runner) int {
	r.resizeMu.Lock()
	defer r.resizeMu.Unlock()
	return r.pendingRows
}

func woke(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// 50, 40, 50 INSIDE THE HOLD NEVER TOUCHES THE PTY, which is the flip that
// lost ten lines. The shorter viewer is a reattach or a window passing through.
func TestAHeightFlipInsideTheHoldNeverResizes(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("passing", 120, 40)
	if got := heldRows(r); got != 40 {
		t.Fatalf("the shorter height is not waiting: pending %d", got)
	}
	r.dropViewport("passing")
	if got := heldRows(r); got != 0 {
		t.Fatalf("the height came back and is still waiting: pending %d", got)
	}
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("a flip inside the hold reached the pty: %+v", sizes)
	}
}

// THE REATTACH SHAPE: the shortest viewer's old socket goes before its new one
// says how big it is, so the height GROWS and comes back. Growing waits too.
func TestAReattachOfTheShortestViewerNeverResizes(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short-old", 120, 40)
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 1 || sizes[0] != (viewport{120, 40}) {
		t.Fatalf("setup: the short viewer did not bind: %+v", sizes)
	}
	r.dropViewport("short-old")
	if got := heldRows(r); got != 50 {
		t.Fatalf("a grow did not wait: pending %d", got)
	}
	_ = r.setViewport("short-new", 120, 40)
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 1 {
		t.Fatalf("a reattach flipped the rows: %+v", sizes)
	}
}

// A HEIGHT THAT HOLDS IS APPLIED ONCE, and until then everything reports the
// applied size.
func TestAHeldHeightIsAppliedOnceItHolds(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short", 120, 40)
	if cols, rows := r.buf.CurrentSize(); cols != 120 || rows != 50 {
		t.Fatalf("during the hold CurrentSize said %dx%d, want the applied 120x50", cols, rows)
	}
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("resized before the hold ran out: %+v", sizes)
	}
	wake := r.sizeChanged()
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 1 || sizes[0] != (viewport{120, 40}) {
		t.Fatalf("want one resize to 120x40, got %+v", sizes)
	}
	if cols, rows := r.buf.CurrentSize(); cols != 120 || rows != 40 {
		t.Fatalf("the ring says %dx%d after the apply", cols, rows)
	}
	if !woke(wake) {
		t.Fatal("the apply did not tell the viewers")
	}
}

// THE WIDTH IS IMMEDIATE, AT THE APPLIED ROWS. A width change during a waiting
// shrink must not carry the new height past the hold.
func TestAWidthChangeDuringAHeldShrinkKeepsTheAppliedRows(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short", 120, 40)
	_ = r.setViewport("pane", 200, 50)
	if sizes := f.resized(); len(sizes) != 1 || sizes[0] != (viewport{200, 50}) {
		t.Fatalf("want the width at once with the applied rows, 200x50, got %+v", sizes)
	}
	if got := heldRows(r); got != 40 {
		t.Fatalf("the width change disturbed the waiting height: pending %d", got)
	}
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 2 || sizes[1] != (viewport{200, 40}) {
		t.Fatalf("want the shrink at the new width when the hold ran out, got %+v", sizes)
	}
}

// A NEW HEIGHT RESTARTS THE HOLD, and the timer it replaced does nothing.
func TestASupersededHeightIsNotAppliedByTheOldTimer(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short", 120, 40)
	r.resizeMu.Lock()
	old := r.pendingGen
	r.resizeMu.Unlock()
	_ = r.setViewport("short", 120, 45)
	r.applyHeld(old)
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("a superseded timer resized the pty: %+v", sizes)
	}
	if got := heldRows(r); got != 45 {
		t.Fatalf("the stale timer cleared the new height: pending %d", got)
	}
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 1 || sizes[0] != (viewport{120, 45}) {
		t.Fatalf("want one resize to the newer height, got %+v", sizes)
	}
}

// A CANCELLED OR SUPERSEDED HOLD RE-TELLS THE VIEWERS, with no resize.
func TestACancelledOrSupersededHoldReTellsWithoutResizing(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short", 120, 40)
	wake := r.sizeChanged()
	_ = r.setViewport("short", 120, 45)
	if !woke(wake) {
		t.Fatal("a superseded height did not re-tell")
	}
	wake = r.sizeChanged()
	r.dropViewport("short")
	if !woke(wake) {
		t.Fatal("a height that came back did not re-tell")
	}
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("a re-tell resized the pty: %+v", sizes)
	}
	// And a frame at the size already in force still wakes nobody.
	wake = r.sizeChanged()
	_ = r.setViewport("pane", 120, 50)
	if woke(wake) {
		t.Fatal("a no-op frame woke the watchers")
	}
}

// THE TIMER RE-READS THE VIEWERS. A height the viewers no longer agree on is
// dropped and re-told, not applied.
func TestTheTimerRevalidatesTheAgreedHeight(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short", 120, 40)
	// Behind the hold's back, which no path does: the defensive branch.
	r.mu.Lock()
	r.views["short"] = viewport{120, 45}
	r.mu.Unlock()
	wake := r.sizeChanged()
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("applied a height the viewers no longer agree on: %+v", sizes)
	}
	if !woke(wake) {
		t.Fatal("a cancel at the timer did not re-tell")
	}
	if got := heldRows(r); got != 0 {
		t.Fatalf("the timer left a height waiting: %d", got)
	}
}

// EVERY VIEWER GONE BEFORE THE HOLD RUNS OUT keeps the size the pty had.
func TestAHeldHeightIsCancelledWhenEveryViewerLeaves(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("pane", 120, 40)
	r.dropViewport("pane")
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("resized with nobody attached: %+v", sizes)
	}
	if got := heldRows(r); got != 0 {
		t.Fatalf("a height still waiting with nobody attached: %d", got)
	}
}

// NEVER A DEAD TERMINAL.
func TestAHeldHeightIsNotAppliedAfterExit(t *testing.T) {
	r, f := heldRunner(t)
	_ = r.setViewport("short", 120, 40)
	close(r.done)
	fireHeld(r)
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("resized an exited runner: %+v", sizes)
	}
}

// THE REAL TIMER, once, so the path by hand above is the one that runs.
func TestTheHoldTimerAppliesTheHeight(t *testing.T) {
	r, f := heldRunner(t)
	r.resizeMu.Lock()
	r.hold = 20 * time.Millisecond
	r.resizeMu.Unlock()
	_ = r.setViewport("short", 120, 40)
	deadline := time.Now().Add(5 * time.Second)
	for len(f.resized()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sizes := f.resized(); len(sizes) != 1 || sizes[0] != (viewport{120, 40}) {
		t.Fatalf("the timer did not apply the height once: %+v", sizes)
	}
}
