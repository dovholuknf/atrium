package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

// The hub and the room run above normal, and ATRIUM_PRIORITY=normal keeps them
// where they started. Losing the raise brings the keystroke lag back on a busy
// machine, and nothing else would notice. See priority_windows.go.
func TestTheHubAndTheRoomRunAboveNormal(t *testing.T) {
	self := windows.CurrentProcess()
	was, err := windows.GetPriorityClass(self)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.SetPriorityClass(self, was) }()

	set := func(class uint32) {
		t.Helper()
		if err := windows.SetPriorityClass(self, class); err != nil {
			t.Fatal(err)
		}
	}
	class := func() uint32 {
		t.Helper()
		c, err := windows.GetPriorityClass(self)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	set(windows.NORMAL_PRIORITY_CLASS)
	t.Setenv(priorityEnv, "")
	raisePriority()
	if got := class(); got != windows.ABOVE_NORMAL_PRIORITY_CLASS {
		t.Fatalf("priority class is %#x after the raise, want above normal %#x",
			got, windows.ABOVE_NORMAL_PRIORITY_CLASS)
	}

	set(windows.NORMAL_PRIORITY_CLASS)
	t.Setenv(priorityEnv, "normal")
	raisePriority()
	if got := class(); got != windows.NORMAL_PRIORITY_CLASS {
		t.Fatalf("priority class is %#x with %s=normal, want it left at normal", got, priorityEnv)
	}
}
