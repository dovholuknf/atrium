package conn

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// A room's transports, best first, and which one it is on.
//
// THE CONTROL CONNECTION CLIMBS, EVERYTHING ELSE FOLLOWS. `Climb` walks the
// rungs from the top and stays on the first one that answers. `Dial`, which is
// what data, announce, relay, git and upgrade connections use, dials the rung
// the control connection is on, so nothing wanders onto another transport.
//
// A failed DIAL moves to the next rung. A hub that answers and says no is not a
// transport problem and is not this file's to see: the refusal comes after the
// dial, in the hello, and would be the same on every transport.

// Rung is one transport a room can climb onto.
type Rung struct {
	Path Path
	Dial Dialer
}

// Ladder is a Dialer over several transports.
//
// IT STANDS ON A RUNG, NOT ON A KIND. Two direct rungs at two addresses are two
// rungs, and every later dial uses the one the control connection reached. A Set
// that leaves that rung out keeps standing on it, because the control connection
// is still on it and a data connection that went anywhere else is a pool that
// never fills.
type Ladder struct {
	mu    sync.Mutex
	rungs []Rung
	on    Rung
	has   bool

	// off is the kinds turned off. See transportoff.go.
	off map[string]bool

	pmu     sync.Mutex
	probing map[string]bool
	rtts    map[string]int64
}

// NewLadder makes a ladder over rungs, best first, standing on the top one.
func NewLadder(rungs []Rung) *Ladder {
	l := &Ladder{rungs: rungs}
	if len(rungs) > 0 {
		l.on, l.has = rungs[0], true
	}
	return l
}

// samePath is whether two paths are the one rung.
func samePath(a, b Path) bool {
	return a.Transport == b.Transport && equalFold(a.Addr, b.Addr) && a.Service == b.Service &&
		a.Share == b.Share && a.Identity == b.Identity
}

// index is where p is in the rungs, or -1. Called with mu held.
func (l *Ladder) index(p Path) int {
	for i, r := range l.rungs {
		if samePath(r.Path, p) {
			return i
		}
	}
	return -1
}

// Set replaces the rungs, staying on the same rung. Where the new list still has
// it, its new dialer is taken. Where it does not, the old one is kept until the
// next climb.
//
// A dialer that is no longer in use is closed: a ziti dialer holds a context whose goroutines live until it is.
func (l *Ladder) Set(rungs []Rung) {
	l.mu.Lock()
	old := l.rungs
	l.rungs = rungs
	if i := l.index(l.on.Path); i >= 0 {
		l.on = rungs[i]
	} else if !l.has && len(rungs) > 0 {
		l.on, l.has = rungs[0], true
	}
	unused := l.unusedLocked(old)
	l.mu.Unlock()
	closeDialers(unused)
}

// unusedLocked is the dialers in old that neither the rungs nor the rung stood on still hold.
func (l *Ladder) unusedLocked(old []Rung) []Dialer {
	var out []Dialer
	for _, o := range old {
		if sameDialer(o.Dial, l.on.Dial) {
			continue
		}
		kept := false
		for _, r := range l.rungs {
			if sameDialer(o.Dial, r.Dial) {
				kept = true
				break
			}
		}
		if !kept {
			out = append(out, o.Dial)
		}
	}
	return out
}

// sameDialer is whether a and b are one dialer. Dialers are pointers or small values, and a value that cannot be
// compared is never the same.
func sameDialer(a, b Dialer) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a != nil && b != nil && a == b
}

type closer interface{ Close() }

// closeDialers closes each dialer that can be closed.
func closeDialers(ds []Dialer) {
	for _, d := range ds {
		if c, ok := d.(closer); ok {
			c.Close()
		}
	}
}

// Reset tells every rung's dialer to throw away what it holds, so the next dial starts afresh. The room calls it
// through the resetter interface after repeated data dial failures.
func (l *Ladder) Reset() {
	for _, r := range l.Rungs() {
		if rs, ok := r.Dial.(resetter); ok {
			rs.Reset()
		}
	}
}

// Close closes every rung's dialer.
func (l *Ladder) Close() {
	var ds []Dialer
	for _, r := range l.Rungs() {
		ds = append(ds, r.Dial)
	}
	closeDialers(ds)
}

func (l *Ladder) current() (Rung, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.has {
		return Rung{}, 0, false
	}
	return l.on, l.index(l.on.Path), true
}

// Dial opens one connection on the rung the ladder is on.
func (l *Ladder) Dial(ctx context.Context) (net.Conn, error) {
	r, _, ok := l.current()
	if !ok {
		return nil, errors.New("this room has no transport to reach its hub")
	}
	if l.IsOff(r.Path.Transport) {
		return nil, errTransportOff
	}
	return r.Dial.Dial(ctx)
}

// Describe is the rung it is on, which is what the log and the board want.
func (l *Ladder) Describe() string {
	r, _, ok := l.current()
	if !ok {
		return "no transport"
	}
	return r.Dial.Describe()
}

// Transport names the kind of the rung it is on.
func (l *Ladder) Transport() string {
	r, _, _ := l.current()
	return r.Path.Transport
}

// Fallback says it is on a rung below the first, or on one the list no longer has.
func (l *Ladder) Fallback() bool {
	_, at, ok := l.current()
	return ok && at != 0
}

// Rungs is a copy of the rungs, best first.
func (l *Ladder) Rungs() []Rung {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Rung(nil), l.rungs...)
}

// Climb dials from the top and stands on the first rung that answers, each
// bounded by `wait`. When none does, the error names every rung and why.
func (l *Ladder) Climb(ctx context.Context, wait time.Duration) (net.Conn, error) {
	rungs := l.live()
	if len(rungs) == 0 {
		return nil, errors.New("this room has no transport to reach its hub, or every one is turned off")
	}
	var why, tried []string
	for _, r := range rungs {
		c, err := dialWithin(ctx, r.Dial, wait)
		if err == nil {
			l.mu.Lock()
			prev := l.on
			// The rungs may have been Set while this dialled. Stand on the same
			// rung in whatever the list is now, or on the one that answered.
			l.on, l.has = r, true
			if j := l.index(r.Path); j >= 0 {
				l.on = l.rungs[j]
			}
			// Moving off a rung the list no longer has frees its dialer.
			unused := l.unusedLocked([]Rung{prev})
			l.mu.Unlock()
			closeDialers(unused)
			return c, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		why = append(why, r.Path.Transport+" "+r.Dial.Describe()+": "+err.Error())
		tried = append(tried, r.Path.Transport+" "+r.Dial.Describe())
	}
	return nil, &unreachable{why: strings.Join(why, "; "), tried: tried}
}

// unreachable is a climb on which every rung failed to dial.
type unreachable struct {
	why   string
	tried []string
}

func (u *unreachable) Error() string { return u.why }

// Probe dials every rung above the current one and says whether any answered.
// It closes what it opened and never moves the ladder: only a climb does that.
// Standing on a rung the list no longer has, every rung is above it.
func (l *Ladder) Probe(ctx context.Context, wait time.Duration) bool {
	l.mu.Lock()
	at := l.index(l.on.Path)
	if at < 0 {
		at = len(l.rungs)
	}
	above := append([]Rung(nil), l.rungs[:at]...)
	l.mu.Unlock()
	for _, r := range above {
		if l.IsOff(r.Path.Transport) {
			continue
		}
		if c, err := dialWithin(ctx, r.Dial, wait); err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// dialWithin bounds one dial by `wait` and the caller's context, even for a
// transport whose own dial takes no context, which is the ziti SDK. A connection
// that turns up after the wait is closed rather than leaked.
func dialWithin(ctx context.Context, d Dialer, wait time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := d.Dial(ctx)
		ch <- res{c, err}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.err == nil && r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, errors.New("no answer within " + wait.String())
	}
}
