package daemon

import "testing"

// One pseudo terminal, several windows looking at it.
//
// The bug this closes: a session shared over zrok and also open locally. Each
// browser sent its own size, the daemon passed each one straight to the pty,
// and the last window dragged set the width for everybody. The other viewer
// then rendered lines the runner had already wrapped for a different width,
// which is not a cosmetic mismatch but torn text, duplicated status lines and
// rows that never clear.

func TestOneViewerGetsWhatItAsksFor(t *testing.T) {
	got := agreedViewport(map[any]viewport{"a": {120, 40}})
	if got != (viewport{120, 40}) {
		t.Fatalf("a single viewer was not given its own size: %+v", got)
	}
}

// THE ONE THAT MATTERS. A narrow viewer does not drag the session down to its
// width, because Claude reprints its whole conversation at every width change
// and every other window keeps that copy in its scrollback. The narrow viewer
// scrolls sideways instead.
func TestTheWidestViewerSetsTheWidth(t *testing.T) {
	got := agreedViewport(map[any]viewport{
		"wide":   {200, 50},
		"narrow": {80, 24},
	})
	if got.cols != 200 {
		t.Fatalf("the narrow window shrank the session for everybody: %+v", got)
	}
}

// Each axis on its own. The width is the widest and the height is the
// shortest, because a pty taller than a pane puts the prompt off its bottom.
func TestEachAxisIsDecidedSeparately(t *testing.T) {
	got := agreedViewport(map[any]viewport{
		"tall-narrow": {80, 60},
		"short-wide":  {200, 20},
	})
	if got != (viewport{200, 20}) {
		t.Fatalf("wanted the wide width and the short height, got %+v", got)
	}
}

// A viewer that left stops counting, or one wide monitor that attached once
// holds every phone at its width for the rest of the day.
func TestADepartedViewerStopsCounting(t *testing.T) {
	all := map[any]viewport{"desk": {200, 50}, "phone": {40, 20}}
	delete(all, "desk")
	got := agreedViewport(all)
	if got != (viewport{40, 20}) {
		t.Fatalf("a viewer that detached was still deciding the size: %+v", got)
	}
}

// Nobody attached is not a size. Resizing to zero is what would happen if the
// size of an empty set were applied, and `dropViewport` refuses to.
func TestNoViewersIsNotZero(t *testing.T) {
	if got := agreedViewport(map[any]viewport{}); got != (viewport{}) {
		t.Fatalf("an empty set answered something: %+v", got)
	}
}
