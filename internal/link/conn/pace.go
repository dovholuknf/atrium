package conn

import (
	"sync"
	"time"
)

// A push queues its bytes past the room, in the overlay and toward the hub, and every round trip to the hub waits
// behind that queue. The hub echoes each beat with the room's own timestamp, so the room reads the queue's delay
// from the echo and paces git writes to keep it near paceTarget. The rate follows what drains, never a fixed cap. While
// paced, git writes do not yield to control calls: the queue is short, and a yield would leave the link idle.

const (
	// paceTarget is the queue delay a push may build past the lowest round trip seen.
	paceTarget = 400 * time.Millisecond
	// paceBeat is how often the room beats while git moves, so the pace hears the queue in time.
	paceBeat = 500 * time.Millisecond
	// paceFloor keeps a push moving whatever the samples say.
	paceFloor = 64 << 10
	// paceGrow is the step up per sample while the queue is short.
	paceGrow = 1.5
	// paceCut bounds one cut, so a deep queue does not leave the link idle while the rate climbs back.
	paceCut = 0.5
	// paceProbe is the step up per sample past the rate that drained at the last cut.
	paceProbe = 1.1
)

// pacer is the room's git write pace. A zero rate is unpaced.
type pacer struct {
	mu   sync.Mutex
	base time.Duration
	rate float64
	// sent is what git wrote since the last sample, at lastAt.
	sent   int64
	lastAt time.Time
	// wroteAt is when git last finished a write, and writing says one is under way. A write blocked on a full overlay
	// buffer is a push, not an idle room.
	wroteAt time.Time
	writing bool
	// cutAt skips samples whose beat left before the last cut, or within a beat of it.
	cutAt time.Time
	// lastQ is the queue delay at the last sample, so a draining queue is not cut again.
	lastQ time.Duration
	// ceil is what drained at the last cut. The pace climbs back to it fast and past it slowly.
	ceil float64
	// next is when the next write may go.
	next time.Time
}

// sample takes one beat's round trip.
func (p *pacer) sample(rtt time.Duration, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.base == 0 || rtt < p.base {
		p.base = rtt
	}
	sent, since := p.sent, now.Sub(p.lastAt)
	first := p.lastAt.IsZero()
	if !first && sent == 0 {
		if !p.writing && now.Sub(p.wroteAt) >= 2*paceBeat {
			p.rate = 0
		}
		return
	}
	if !first && since < paceBeat/2 {
		return
	}
	p.sent, p.lastAt = 0, now
	q := rtt - p.base
	rise := q - p.lastQ
	p.lastQ = q
	if first || since <= 0 || now.Add(-rtt).Before(p.cutAt.Add(paceBeat)) {
		return
	}
	switch {
	case q > paceTarget && rise < 0 && p.rate > 0:
	case q > paceTarget:
		r := float64(sent) / since.Seconds()
		if p.rate > 0 && p.rate < r {
			r = p.rate
		}
		p.ceil = max(r, p.ceil*paceCut)
		r *= max(float64(paceTarget)/float64(q), paceCut)
		if r < paceFloor {
			r = paceFloor
		}
		p.rate = r
		p.cutAt = now
	case p.rate > 0 && q < paceTarget/2:
		p.rate = min(p.rate*paceGrow, max(p.ceil, p.rate*paceProbe))
	}
}

// delay counts n bytes written and says how long to wait before writing them.
func (p *pacer) delay(n int, now time.Time) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent += int64(n)
	p.writing = true
	if p.rate <= 0 {
		return 0
	}
	if p.next.Before(now) {
		p.next = now
	}
	d := p.next.Sub(now)
	p.next = p.next.Add(time.Duration(float64(n) / p.rate * float64(time.Second)))
	return d
}

// paceWrite waits out the pace for n bytes of git.
func (r *Room) paceWrite(n int) {
	if d := r.pace.delay(n, time.Now()); d > 0 {
		sleep := r.yieldSleep
		if sleep == nil {
			sleep = time.Sleep
		}
		sleep(d)
	}
}

// paced says the pace holds the queue, so a control call has no push queue to wait out.
func (p *pacer) paced() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rate > 0
}

// starts marks a git write under way before it yields, so a write parked in the yield is not an idle room.
func (p *pacer) starts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writing = true
}

// wrote marks the end of one git write.
func (p *pacer) wrote(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writing, p.wroteAt = false, now
}
