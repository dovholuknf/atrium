//go:build integration && windows

package cli

import (
	"testing"

	"golang.org/x/sys/windows"
)

// The pty host is the third process a keystroke crosses, and at Normal it lagged keys on room-d under a go test load.
func TestThePtyHostRunsAboveNormal(t *testing.T) {
	self := windows.CurrentProcess()
	was, err := windows.GetPriorityClass(self)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.SetPriorityClass(self, was) }()
	if err := windows.SetPriorityClass(self, windows.NORMAL_PRIORITY_CLASS); err != nil {
		t.Fatal(err)
	}
	t.Setenv(priorityEnv, "")

	cmd := newPtyHost()
	cmd.SetArgs([]string{"--state-dir="})
	if err := cmd.Execute(); err == nil {
		t.Fatal("an empty state dir started a host")
	}
	got, err := windows.GetPriorityClass(self)
	if err != nil {
		t.Fatal(err)
	}
	if got != windows.ABOVE_NORMAL_PRIORITY_CLASS {
		t.Fatalf("priority class is %#x after atrium ptyhost, want above normal %#x", got,
			windows.ABOVE_NORMAL_PRIORITY_CLASS)
	}
}
