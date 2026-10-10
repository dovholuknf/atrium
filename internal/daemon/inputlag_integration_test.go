//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// THE ECHO LINE SAYS WHOSE TIME IT WAS. A 2.9s keystroke logged the same gap on
// the hub and the room and nothing else, which left runner and atrium to be
// told apart by the absence of other lines. The split names it.
func TestEchoSplitNamesTheRunnerAndAtrium(t *testing.T) {
	in := time.Unix(100, 0).UnixNano()
	ms := int64(time.Millisecond)

	if got, want := echoSplit(in, in+2920*ms, in+2926*ms), "runner 2920.0ms, atrium 6.0ms"; got != want {
		t.Fatalf("split = %q, want %q", got, want)
	}
	// A read from before the keystroke is the last echo's, and one after the
	// write is a later chunk's. Neither is this echo's split.
	for _, read := range []int64{0, in - ms, in + 3000*ms} {
		if got := echoSplit(in, read, in+2926*ms); got != "runner/atrium split unknown" {
			t.Fatalf("read %d: split = %q, want unknown", read, got)
		}
	}
}

// The pty reader stamps the read the split is taken at, and only while the
// logging is on, so the default path stays one atomic load.
func TestDeliverOutputStampsThePtyReadOnlyWhenLogging(t *testing.T) {
	if !inputlag.SetLive(false) {
		t.Skip(inputlag.Env + " is set, so the logging cannot be switched")
	}
	t.Cleanup(func() { inputlag.SetLive(false) })

	r := &runner{taskID: "lag", buf: newRing(1<<16, 80), watchers: map[chan []byte]struct{}{}}
	r.deliverOutput([]byte("off"))
	if got := r.lagRead.Load(); got != 0 {
		t.Fatalf("stamped %d with the logging off", got)
	}

	inputlag.SetLive(true)
	before := time.Now().UnixNano()
	r.deliverOutput([]byte("on"))
	if got := r.lagRead.Load(); got < before || got > time.Now().UnixNano() {
		t.Fatalf("stamp %d is not the read just made (after %d)", got, before)
	}
}
