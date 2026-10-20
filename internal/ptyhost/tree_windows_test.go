//go:build windows

package ptyhost

import "testing"

func TestDescendsFromRefusesAChildOlderThanItsParent(t *testing.T) {
	parents := map[uint32]uint32{10: 1, 11: 10, 12: 10, 13: 11}
	created := map[uint32]uint64{1: 100, 10: 200, 11: 300, 12: 50, 13: 400}
	for pid, want := range map[uint32]bool{10: true, 11: true, 13: true, 12: false} {
		if got := descendsFrom(parents, created, pid, 1); got != want {
			t.Errorf("pid %d: got %v, want %v", pid, got, want)
		}
	}
}

func TestDescendsFromRefusesAnUnreadableCreationTime(t *testing.T) {
	if descendsFrom(map[uint32]uint32{10: 1}, map[uint32]uint64{1: 100}, 10, 1) {
		t.Fatal("a process with no creation time was adopted")
	}
}
