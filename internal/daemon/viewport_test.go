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
	got := smallestViewport(map[any]viewport{"a": {120, 40}})
	if got != (viewport{120, 40}) {
		t.Fatalf("a single viewer was not given its own size: %+v", got)
	}
}

// THE ONE THAT MATTERS. Everybody can draw the smallest, and nobody can draw
// more than they have.
func TestTheSmallestViewerDecides(t *testing.T) {
	got := smallestViewport(map[any]viewport{
		"wide":   {200, 50},
		"narrow": {80, 24},
	})
	if got != (viewport{80, 24}) {
		t.Fatalf("the wide window won, which is what makes the narrow one unreadable: %+v", got)
	}
}

// Each axis on its own. A tall narrow window beside a short wide one leaves a
// terminal that is both narrow and short, because those are the two limits
// that actually exist.
func TestEachAxisIsDecidedSeparately(t *testing.T) {
	got := smallestViewport(map[any]viewport{
		"tall-narrow": {80, 60},
		"short-wide":  {200, 20},
	})
	if got != (viewport{80, 20}) {
		t.Fatalf("wanted the narrow width and the short height, got %+v", got)
	}
}

// A viewer that left stops constraining the rest, or one phone that attached
// once holds the session at its width for the rest of the day.
func TestADepartedViewerStopsCounting(t *testing.T) {
	all := map[any]viewport{"desk": {200, 50}, "phone": {40, 20}}
	delete(all, "phone")
	got := smallestViewport(all)
	if got != (viewport{200, 50}) {
		t.Fatalf("a viewer that detached was still deciding the size: %+v", got)
	}
}

// Nobody attached is not a size. Resizing to zero is what would happen if the
// smallest of an empty set were applied, and `dropViewport` refuses to.
func TestNoViewersIsNotZero(t *testing.T) {
	if got := smallestViewport(map[any]viewport{}); got != (viewport{}) {
		t.Fatalf("an empty set answered something: %+v", got)
	}
}
