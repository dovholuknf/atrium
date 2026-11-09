package api

import (
	"context"
	"testing"
)

func TestWorktreeNamesThatClimbOrReadAsOptionsAreRefused(t *testing.T) {
	for _, n := range []string{"..", "../x", "a/../b", `a\..\b`, "."} {
		if !climbs(n) {
			t.Errorf("%q should climb", n)
		}
	}
	for _, n := range []string{"org", "group/sub", "a..b", "feature-thing"} {
		if climbs(n) {
			t.Errorf("%q should not climb", n)
		}
	}
	for _, b := range []string{"--detach", "-b", "..", "a/../b"} {
		if checkBranchName(context.Background(), b) == nil {
			t.Errorf("%q is not a branch to hand git", b)
		}
	}
}
