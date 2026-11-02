package ptyhost

import (
	"syscall"
	"testing"
)

func TestScopeNameKeepsOnlyWhatSystemdTakes(t *testing.T) {
	got := scopeName("atrium fix the bug@sg3 shell", "01ABC")
	if want := "atrium-fix-the-bug-sg3-shell-01ABC.scope"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGroupMemCountsThisProcessGroup(t *testing.T) {
	if n := treeMem(syscall.Getpgrp(), ""); n < 1<<20 {
		t.Fatalf("this process group holds %d bytes", n)
	}
}
