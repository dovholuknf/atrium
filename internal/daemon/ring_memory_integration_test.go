//go:build integration

package daemon

import (
	"bytes"
	"math/rand"
	"runtime"
	"testing"
)

// THE BUG, as the live room had it: the scrollback setting at 512MB and a
// board of cards that had printed very little. Each ring used to be that
// setting in full from the moment it was made. Now it is what it holds.
func TestARingCostsWhatItHoldsNotItsCeiling(t *testing.T) {
	r := newRing(512<<20, testCols)
	if cap(r.data) != 0 {
		t.Fatalf("an empty ring allocated %d bytes before anything was written", cap(r.data))
	}
	r.Write([]byte("PS C:\\> "))
	if cap(r.data) > ringFloor {
		t.Fatalf("a prompt's worth of output took a %d byte buffer, more than the %d floor",
			cap(r.data), ringFloor)
	}
	// And a busy one grows to what it holds, within one doubling.
	chunk := bytes.Repeat([]byte("a line of output\n"), 1<<10)
	for r.retained() < 5<<20 {
		r.Write(chunk)
	}
	if got, held := cap(r.data), r.retained(); got > 2*held+len(chunk) {
		t.Fatalf("holding %d bytes cost %d", held, got)
	}
}

// Measured in the heap rather than in one slice, and for a board's worth of
// cards: twenty six, the size the live room reopened at. Before the fix this
// is 26 times the ceiling, which is why the ceiling here is small: at the live
// 512MB it would be 13GB and the test would take the machine with it.
func TestABoardOfQuietRingsStaysSmall(t *testing.T) {
	const cards, ceiling = 26, 32 << 20
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	rings := make([]*ringBuffer, cards)
	for i := range rings {
		rings[i] = newRing(ceiling, testCols)
		// What a reopened card prints before anybody looks: a banner and a
		// prompt.
		rings[i].Write(bytes.Repeat([]byte("resumed session banner line\r\n"), 40))
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(rings)
	grew := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%d quiet rings with a %dMB ceiling hold %d bytes of heap", cards, ceiling>>20, grew)
	if limit := int64(cards * 2 * ringFloor); grew > limit {
		t.Fatalf("%d quiet rings hold %d bytes of heap, over %d: the ceiling is being allocated",
			cards, grew, limit)
	}
}

// Growing must not lose or reorder a byte. A ring that grows through several
// doublings, wraps, and is then raised, checked after every write against a
// plain model of "the last max bytes".
func TestAGrowingRingHoldsExactlyTheLastBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(57))
	ceiling := 3*ringFloor + 1234
	r := newRing(ceiling, testCols)
	var model []byte
	check := func(when string) {
		t.Helper()
		want := model
		if len(want) > ceiling {
			want = want[len(want)-ceiling:]
		}
		if got := r.raw(); !bytes.Equal(got, want) {
			t.Fatalf("%s: ring holds %d bytes, model %d, and they differ", when, len(got), len(want))
		}
		if r.retained() != len(want) {
			t.Fatalf("%s: retained says %d, holds %d", when, r.retained(), len(want))
		}
	}
	write := func(n int) {
		p := make([]byte, n)
		for i := range p {
			p[i] = byte('a' + rng.Intn(26))
		}
		r.Write(p)
		model = append(model, p...)
	}
	// Sizes that land exactly on the slice's end, one short, one over, and a
	// write bigger than the whole ring.
	for _, n := range []int{ringFloor - 1, 1, ringFloor, 7, 3 * ringFloor, 1, ceiling + 5, 1} {
		write(n)
		check("fixed")
	}
	for i := 0; i < 400; i++ {
		write(rng.Intn(ringFloor / 2))
		check("random")
	}
	if !r.full || len(r.data) != ceiling {
		t.Fatalf("a ring written past its ceiling is %d long, full=%v, want %d and full",
			len(r.data), r.full, ceiling)
	}

	// Raised after wrapping: everything held stays, and it grows again. What
	// was overwritten before the raise stays gone.
	model = model[len(model)-ceiling:]
	ceiling *= 2
	r.Grow(ceiling)
	check("raised")
	if r.full {
		t.Fatal("a raised ring still claims to be full")
	}
	for i := 0; i < 400; i++ {
		write(rng.Intn(ringFloor / 2))
		check("after raising")
	}
}

// Raising the ceiling on a ring that has not wrapped allocates nothing, which
// is every attach on a quiet card: attach calls Grow with the setting.
func TestRaisingAQuietRingAllocatesNothing(t *testing.T) {
	r := newRing(16<<20, testCols)
	r.Write([]byte("hello\n"))
	before := cap(r.data)
	r.Grow(512 << 20)
	if cap(r.data) != before {
		t.Fatalf("raising the ceiling on a quiet ring moved it from %d to %d bytes", before, cap(r.data))
	}
	if got := string(r.raw()); got != "hello\n" {
		t.Fatalf("raising lost the output: %q", got)
	}
}

// A zero ceiling holds nothing and must not panic.
func TestAZeroRingHoldsNothing(t *testing.T) {
	r := newRing(0, testCols)
	r.Write([]byte("anything"))
	if r.retained() != 0 || len(r.raw()) != 0 {
		t.Fatalf("a zero ring retained %d bytes", r.retained())
	}
}
