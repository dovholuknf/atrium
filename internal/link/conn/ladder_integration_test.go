//go:build integration

package conn

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClimbBoundsARungThatHangsAndSaysEveryFailure(t *testing.T) {
	hang, dead := &fakeDial{name: "hangs", hang: true}, &fakeDial{name: "dead"}
	dead.fail.Store(true)
	l := NewLadder([]Rung{{Path: Path{Transport: "ziti"}, Dial: hang}, {Path: Path{Transport: "direct"}, Dial: dead}})
	start := time.Now()
	_, err := l.Climb(context.Background(), 100*time.Millisecond)
	if err == nil {
		t.Fatal("every rung failed and the climb said nothing")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a hanging rung held the climb past its wait")
	}
	if !strings.Contains(err.Error(), "ziti") || !strings.Contains(err.Error(), "direct") {
		t.Fatalf("the error should name each rung: %v", err)
	}
}
