package daemon

import (
	"strings"
	"testing"
)

// WHY A SOCKET CLOSED, which the board cannot work out on its own.
//
// Three things end an attach and they want three different answers: a session
// that ended should stop and stay stopped, a shell that closed should drop back
// to the agent, and a restart should be waited out. From the browser all three
// are a socket closing.
//
// The failure mode when these blur together is silent and arrives minutes
// later: either a retry storm against a runner that is deliberately gone, or a
// window that gave up on a session one second before it came back.

func TestASessionEndingSaysSo(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	say, reason := d.whyClosed(false)
	if reason != "runner exited" {
		t.Fatalf("a runner ending closes with %q, and the board keys off that string", reason)
	}
	if !strings.Contains(say, "exited") {
		t.Errorf("the line on screen does not say what happened: %q", say)
	}
}

func TestAShellClosingIsNotASessionEnding(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	say, reason := d.whyClosed(true)
	if reason != "shell closed" {
		t.Fatalf("a shell ending closes with %q, so the board cannot fall back to the agent", reason)
	}
	// Wording matters here as much as the reason. "this runner has exited" over
	// a shell somebody typed `exit` into reads as the agent having died.
	if strings.Contains(say, "runner") {
		t.Errorf("a closing shell announces itself as the runner: %q", say)
	}
}

// Once a wind-down has started every close is the restart, whichever kind of
// terminal it was.
func TestDuringAWindDownEveryCloseSaysRestarting(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	d.stop.request("a test")

	if _, reason := d.whyClosed(false); reason != "restarting" {
		t.Fatalf("a runner stopped by a wind-down closes with %q. the board reads that "+
			"as the session ending and tears down a pane that is about to come back", reason)
	}
	if _, reason := d.whyClosed(true); reason != "restarting" {
		t.Fatalf("a shell closed by a wind-down closes with %q", reason)
	}
}

// The three reasons have to stay distinct. Two of them collapsing into one is
// how the board starts guessing again, and it guesses wrong in the case that
// costs the most.
func TestTheThreeReasonsAreDistinct(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	_, runner := d.whyClosed(false)
	_, shell := d.whyClosed(true)
	d.stop.request("a test")
	_, restarting := d.whyClosed(false)

	seen := map[string]bool{}
	for _, r := range []string{runner, shell, restarting} {
		if r == "" {
			t.Fatal("a close reason is empty, so the board sees no reason at all")
		}
		if seen[r] {
			t.Fatalf("two closes share the reason %q", r)
		}
		seen[r] = true
	}
}
