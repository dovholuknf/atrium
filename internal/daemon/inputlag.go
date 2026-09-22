package daemon

import (
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// Input-lag timing for the room's two hops, off unless ATRIUM_DEBUG_INPUTLAG is
// set. See internal/inputlag. Every helper here is a no-op when it is off, so
// the call sites stay one line and the default path is unchanged.
//
// The room sees two hops of a keystroke:
//
//	in    websocket frame read -> bytes written to the pty
//	echo  that frame -> the first output this attach sends back
//
// "echo" includes the runner's own think time, so a slow echo with a fast "in"
// is the runner (or the machine) being slow, not atrium.

// writeOperatorInputTimed is `writeOperatorInput` with the wait split out.
//
// THE SAME LOCK, taken the same way. It exists so the timing can tell a
// keystroke stuck behind a peer paste (input lock) from a pty that is slow to
// take bytes (pty write).
func (r *runner) writeOperatorInputTimed(p []byte, got time.Time, label string) error {
	t0 := time.Now()
	r.pasteMu.Lock()
	defer r.pasteMu.Unlock()
	t1 := time.Now()
	err := r.Write(p)
	t2 := time.Now()
	if inputlag.Over(t2.Sub(got)) {
		inputlag.Logf("room %s in: ws frame -> pty write %s (before lock %s, input lock %s, pty write %s, %d bytes)",
			label, inputlag.Ms(t2.Sub(got)), inputlag.Ms(t0.Sub(got)), inputlag.Ms(t1.Sub(t0)),
			inputlag.Ms(t2.Sub(t1)), len(p))
	}
	return err
}

// noteLagIn remembers when the oldest unanswered keystroke on an attach
// arrived. Only the first one after an echo is kept, so the gap reads as "how
// long the operator waited for anything".
func noteLagIn(at *atomic.Int64, got time.Time) {
	if !inputlag.On() {
		return
	}
	at.CompareAndSwap(0, got.UnixNano())
}

// noteLagOut closes the gap `noteLagIn` opened, once a chunk has gone out.
func noteLagOut(at *atomic.Int64, label string, sent, wrote time.Time, queued, n int) {
	if !inputlag.On() {
		return
	}
	write := wrote.Sub(sent)
	in := at.Swap(0)
	if in == 0 {
		if inputlag.Over(write) {
			inputlag.Logf("room %s out: ws write %s (%d bytes, %d chunks queued behind it)",
				label, inputlag.Ms(write), n, queued)
		}
		return
	}
	gap := wrote.Sub(time.Unix(0, in))
	if inputlag.Over(gap) {
		inputlag.Logf("room %s echo: ws frame in -> first output out %s (ws write %s, %d bytes, %d chunks queued)",
			label, inputlag.Ms(gap), inputlag.Ms(write), n, queued)
	}
}

// lagStart is the clock for a pty read, zero when the logging is off.
func lagStart() time.Time {
	if !inputlag.On() {
		return time.Time{}
	}
	return time.Now()
}

// lagFanout logs a pty read that took too long to reach the attachers. The
// ring write and the fan-out run under r.mu, which subscribe and the peer echo
// also take, so a slow one here is contention inside the room. See
// `deliverOutput`.
func (r *runner) lagFanout(t0 time.Time, n int) {
	if t0.IsZero() {
		return
	}
	if d := time.Since(t0); inputlag.Over(d) {
		inputlag.Logf("room %s out: pty read -> fanout %s (%d bytes)", r.taskID, inputlag.Ms(d), n)
	}
}
