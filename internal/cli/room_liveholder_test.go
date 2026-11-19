package cli

import (
	"os"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
)

func TestLiveRoomHoldsLocation(t *testing.T) {
	yes := func(int) bool { return true }
	no := func(int) bool { return false }
	at := func(pid int, ok bool) func() (daemon.Location, bool) {
		return func() (daemon.Location, bool) { return daemon.Location{PID: pid}, ok }
	}
	cases := []struct {
		name string
		read func() (daemon.Location, bool)
		live func(int) bool
		want bool
	}{
		{"another live room", at(os.Getpid()+1, true), yes, true},
		{"a stale file", at(os.Getpid()+1, true), no, false},
		{"this process", at(os.Getpid(), true), yes, false},
		{"no file", at(0, false), yes, false},
		{"no pid", at(0, true), yes, false},
	}
	for _, tc := range cases {
		if got := liveRoomHoldsLocation(tc.read, tc.live); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
