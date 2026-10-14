package link

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
type Ladder struct {
	mu    sync.Mutex
	rungs []Rung
	at    int
}

// NewLadder makes a ladder over rungs, best first, standing on the top one.
func NewLadder(rungs []Rung) *Ladder { return &Ladder{rungs: rungs} }

// Set replaces the rungs, staying on the same transport when it is still there.
func (l *Ladder) Set(rungs []Rung) {
	l.mu.Lock()
	defer l.mu.Unlock()
	on := ""
	if l.at < len(l.rungs) {
		on = l.rungs[l.at].Path.Transport
	}
	l.rungs, l.at = rungs, 0
	for i, r := range rungs {
		if r.Path.Transport == on {
			l.at = i
		}
	}
}

func (l *Ladder) current() (Rung, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.at >= len(l.rungs) {
		return Rung{}, 0, false
	}
	return l.rungs[l.at], l.at, true
}

// Dial opens one connection on the rung the ladder is on.
func (l *Ladder) Dial(ctx context.Context) (net.Conn, error) {
	r, _, ok := l.current()
	if !ok {
		return nil, errors.New("this room has no transport to reach its hub")
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

// Fallback says it is on a rung below the first.
func (l *Ladder) Fallback() bool {
	_, at, ok := l.current()
	return ok && at > 0
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
	rungs := l.Rungs()
	if len(rungs) == 0 {
		return nil, errors.New("this room has no transport to reach its hub")
	}
	var why []string
	for i, r := range rungs {
		c, err := dialWithin(ctx, r.Dial, wait)
		if err == nil {
			l.mu.Lock()
			// The rungs may have been Set while this dialled. Stand on the same
			// transport in whatever the list is now.
			l.at = i
			for j, now := range l.rungs {
				if now.Path.Transport == r.Path.Transport {
					l.at = j
					break
				}
			}
			l.mu.Unlock()
			return c, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		why = append(why, r.Path.Transport+" "+r.Dial.Describe()+": "+err.Error())
	}
	return nil, errors.New(strings.Join(why, "; "))
}

// Probe dials every rung above the current one and says whether any answered.
// It closes what it opened and never moves the ladder: only a climb does that.
func (l *Ladder) Probe(ctx context.Context, wait time.Duration) bool {
	l.mu.Lock()
	above := append([]Rung(nil), l.rungs[:min(l.at, len(l.rungs))]...)
	l.mu.Unlock()
	for _, r := range above {
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
			if r := <-ch; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, errors.New("no answer within " + wait.String())
	}
}
