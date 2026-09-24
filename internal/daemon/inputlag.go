package daemon

import (
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// Input-lag timing for the room's two hops, off unless the gear or
// ATRIUM_DEBUG_INPUTLAG turns it on. See internal/inputlag. Every helper here
// asks on each call, so a switch from the gear takes effect on the next
// keystroke, and is a no-op when it is off, so the default path is unchanged.
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
// read is the runner's `lagRead`, which splits the gap in two. See `echoSplit`.
func noteLagOut(at *atomic.Int64, label string, read int64, sent, wrote time.Time, queued, n int) {
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
		inputlag.Logf("room %s echo: ws frame in -> first output out %s (%s, ws write %s, %d bytes, %d chunks queued)",
			label, inputlag.Ms(gap), echoSplit(in, read, wrote.UnixNano()), inputlag.Ms(write), n, queued)
	}
}

// echoSplit divides an echo gap at the moment the pty handed output over:
//
//	runner  ws frame in -> pty read, which is the pty write, the runner's own
//	        think and redraw, and ConPTY. Atrium's share of it, the pty write,
//	        has its own "in" line when slow.
//	atrium  pty read -> ws write out, which is the ring, the fan-out, the
//	        attach queue and the websocket write.
//
// A 2.9s echo with the time on the runner side is the runner or the machine,
// not atrium. The read is the runner's latest, so a second chunk landing
// before this one goes out moves the split later. A read outside the gap
// means the stamp is not this echo's, and the split is left unsaid.
func echoSplit(in, read, wrote int64) string {
	if read < in || read > wrote {
		return "runner/atrium split unknown"
	}
	return "runner " + inputlag.Ms(time.Duration(read-in)) + ", atrium " + inputlag.Ms(time.Duration(wrote-read))
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
