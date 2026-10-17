package link

import (
	"sync"
	"time"
)

// A backoff in front of an overlay's controller.
//
// THE SDK'S OWN REFRESH LOOP IS NOT OURS TO SLOW, so the guard is to stop making new ones. Two rooms once sent 10,000
// POSTs a minute each to a controller that was refusing, because every ziti context built for a retry kept refreshing
// its api session with no pause. The process now holds one context per identity and never builds another, and after a
// failed dial the next one is refused here, without touching the SDK, until the backoff has run. Each failure doubles
// it, to a cap.

// dialFloor and dialCeiling bound the wait after a failed overlay dial. A minute, not longer: a room whose only path is
// the overlay should find its controller again within the minute, and 60 s is 1/60th the rate that was seen.
const (
	dialFloor   = time.Second
	dialCeiling = time.Minute
)

// hammerFails is how many failed dials in the last minute flag a transport as hammering its controller.
const hammerFails = 4

// dialGate is safe to use at its zero value.
type dialGate struct {
	mu     sync.Mutex
	now    func() time.Time // nil is time.Now
	min    time.Duration    // zero is dialFloor
	max    time.Duration    // zero is dialCeiling
	fails  int
	next   time.Time
	recent []time.Time
}

func (g *dialGate) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// allow says whether a dial may go now, and when not, how long until one may.
func (g *dialGate) allow() (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if wait := g.next.Sub(g.clock()); wait > 0 {
		return wait, false
	}
	return 0, true
}

// failed records a dial that failed and sets the wait before the next.
func (g *dialGate) failed() {
	g.mu.Lock()
	defer g.mu.Unlock()
	lo, hi := g.min, g.max
	if lo <= 0 {
		lo = dialFloor
	}
	if hi <= 0 {
		hi = dialCeiling
	}
	wait := lo
	for i := 0; i < g.fails && wait < hi; i++ {
		wait *= 2
	}
	if wait > hi {
		wait = hi
	}
	g.fails++
	now := g.clock()
	g.next = now.Add(wait)
	g.recent = append(g.pruneLocked(now), now)
}

// worked clears the backoff.
func (g *dialGate) worked() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fails, g.next = 0, time.Time{}
}

func (g *dialGate) pruneLocked(now time.Time) []time.Time {
	keep := g.recent[:0]
	for _, t := range g.recent {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	return keep
}

// perMinute is how many dials failed in the last minute.
func (g *dialGate) perMinute() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recent = g.pruneLocked(g.clock())
	return len(g.recent)
}

// hammering says the transport is failing fast enough that somebody should look.
func (g *dialGate) hammering() bool { return g.perMinute() >= hammerFails }
