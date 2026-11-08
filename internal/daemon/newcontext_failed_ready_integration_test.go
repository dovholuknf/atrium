//go:build integration

package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestAReadyAfterTheCycleFailedBeforeTheAckStillClears(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	file := cycleFile(d, task.ID)
	if !d.Nctx.Fail(task.ID, d.Nctx.Get(task.ID).Gen, "the room restarted") {
		t.Fatal("could not fail the chip")
	}
	if err := os.WriteFile(file, handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code, err := d.Ready(task); err != nil {
		t.Fatalf("atrium ready refused with %d: %v", code, err)
	}
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}
