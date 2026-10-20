package ptyhost

import (
	"slices"
	"testing"
	"time"
)

func TestStartArgsCarryTheBuildTheHostReports(t *testing.T) {
	got := startArgs("/s", StartOptions{Build: "atrium dev (abc123)", IdleExit: time.Minute})
	want := []string{"ptyhost", "--state-dir", "/s", "--idle-exit", "1m0s", "--build", "atrium dev (abc123)"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := startArgs("/s", StartOptions{}); slices.Contains(got, "--build") {
		t.Fatalf("an empty build is passed on: %q", got)
	}
	if got := startArgs("/s", StartOptions{Args: []string{"x"}}); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("Args does not replace the defaults: %q", got)
	}
}
