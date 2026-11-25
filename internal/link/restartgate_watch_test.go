package link

import (
	"testing"
	"time"
)

func TestTheGateSaysWatchingOnlyWhileABoardWearsTheHeadsUp(t *testing.T) {
	now := time.Unix(1000, 0)
	g := newRestartGate(func(map[string]any) {}, func() int { return 1 }, func(string, string) {})
	g.now = func() time.Time { return now }
	if g.state()["watching"] != false {
		t.Fatal("watching before any board said so")
	}
	g.watch()
	if g.state()["watching"] != true {
		t.Fatal("not watching right after a board said so")
	}
	now = now.Add(watchFor + time.Second)
	if g.state()["watching"] != false {
		t.Fatal("still watching after the board went quiet")
	}
}
