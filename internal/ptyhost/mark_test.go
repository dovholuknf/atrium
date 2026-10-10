package ptyhost

import (
	"reflect"
	"testing"
)

func TestRingKeepsMarksAsOffsetsAndLeavesTheBytesAlone(t *testing.T) {
	r := newRing(8, 80, 24)
	r.write([]byte("abcd"))
	r.mark(4)
	r.write([]byte("efgh"))
	if got := r.marksFrom(0); !reflect.DeepEqual(got, []int64{4}) {
		t.Fatalf("marks %v, want [4]", got)
	}
	if data, _, _, _ := r.snapshot(0); string(data) != "abcdefgh" {
		t.Fatalf("the mark changed the bytes: %q", data)
	}
	// The window moves past the mark: it is forgotten with the bytes it sat after.
	r.write([]byte("ijklmn"))
	if got := r.marksFrom(0); len(got) != 0 {
		t.Fatalf("a mark behind the window survived: %v", got)
	}
}
