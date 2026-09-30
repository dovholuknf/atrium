package ptyhost

import (
	"bytes"
	"testing"
)

func TestRingOffsetsAndWindow(t *testing.T) {
	r := newRing(10, 80, 24)
	r.write([]byte("0123456789ab"))
	if r.total != 12 || r.start() != 2 || r.retained() != 10 {
		t.Fatalf("total %d start %d retained %d", r.total, r.start(), r.retained())
	}
	d, from, trunc, _ := r.snapshot(0)
	if !trunc || from != 2 || string(d) != "23456789ab" {
		t.Fatalf("%q %d %v", d, from, trunc)
	}
	d, from, trunc, _ = r.snapshot(9)
	if trunc || from != 9 || string(d) != "9ab" {
		t.Fatalf("%q %d %v", d, from, trunc)
	}
	d, from, _, _ = r.snapshot(99)
	if from != 12 || len(d) != 0 {
		t.Fatalf("past the end: %q %d", d, from)
	}
	// a long run keeps the window exact however far the backing slice ran over
	big := bytes.Repeat([]byte("x"), 100000)
	r2 := newRing(4096, 80, 24)
	for i := 0; i < 10; i++ {
		r2.write(big)
	}
	d, from, _, _ = r2.snapshot(0)
	if len(d) != 4096 || from != r2.total-4096 {
		t.Fatalf("window %d from %d total %d", len(d), from, r2.total)
	}
}

func TestRingCutsRebaseAndPrune(t *testing.T) {
	r := newRing(100, 80, 24)
	r.write(bytes.Repeat([]byte("a"), 50))
	r.cut(120, 40)
	r.write(bytes.Repeat([]byte("b"), 50))
	r.cut(100, 30)
	r.write(bytes.Repeat([]byte("c"), 60)) // total 160, window [60, 160)
	_, from, trunc, cuts := r.snapshot(0)
	if !trunc || from != 60 {
		t.Fatalf("from %d trunc %v", from, trunc)
	}
	want := []Cut{{60, 120, 40}, {100, 100, 30}}
	if len(cuts) != 2 || cuts[0] != want[0] || cuts[1] != want[1] {
		t.Fatalf("cuts %+v, want %+v", cuts, want)
	}
	// a cut at the same offset replaces the last
	r.cut(90, 20)
	r.cut(91, 21)
	if c := r.cuts[len(r.cuts)-1]; c.Cols != 91 || c.Off != 160 {
		t.Fatalf("%+v", r.cuts)
	}
	// from the middle: the cut in force there comes first, at that offset
	_, _, _, cuts = r.snapshot(110)
	if cuts[0] != (Cut{110, 100, 30}) {
		t.Fatalf("%+v", cuts)
	}
}
