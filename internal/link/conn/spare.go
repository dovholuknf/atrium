package conn

import (
	"bufio"
	"context"
	"net"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// While a push runs, every round trip to the hub waits behind push bytes already past the room, so a relay that dials
// pays the ziti dial and the TLS handshake behind that queue. The room keeps one connection dialled and through TLS
// ahead of time, and a relay says its hello on it. The hub waits wire.SpareWait for that hello.

// spareLife is how long a spare is offered, well inside the hub's wire.SpareWait.
const spareLife = 40 * time.Second

// spareFrom is how much one git connection writes before it counts as a push. A fetch request is far smaller.
const spareFrom = 1 << 20

// keepSpare starts the spare loop unless it runs. Called by a push's git writes.
func (r *Room) keepSpare() {
	if r.Dial == nil || !r.spareRun.CompareAndSwap(false, true) {
		return
	}
	go r.spareLoop()
}

func (r *Room) spareLoop() {
	defer func() {
		r.mu.Lock()
		r.spareReady = nil
		r.mu.Unlock()
		r.spareRun.Store(false)
	}()
	for r.gitBusy() {
		ready := make(chan struct{})
		r.mu.Lock()
		r.spareReady = ready
		r.mu.Unlock()
		dialCtx, cancel := context.WithTimeout(context.Background(), r.T.DialWait)
		c, err := r.Dial.Dial(dialCtx)
		cancel()
		if err != nil {
			close(ready)
			return
		}
		taken := make(chan struct{}, 1)
		r.mu.Lock()
		r.spare, r.spareAt, r.spareTaken = c, time.Now(), taken
		r.mu.Unlock()
		close(ready)
		t := time.NewTimer(spareLife)
		select {
		case <-taken:
		case <-t.C:
		}
		t.Stop()
		r.dropSpare(c)
	}
}

// dropSpare closes c if it is still the spare on offer.
func (r *Room) dropSpare(c net.Conn) {
	r.mu.Lock()
	mine := r.spare == c
	if mine {
		r.spare = nil
	}
	r.mu.Unlock()
	if mine {
		c.Close()
	}
}

// waitSpare waits up to the dial bound for a spare being dialled, which started before this relay and is held up by
// the same push queue, so it is ready sooner than a fresh dial would be.
func (r *Room) waitSpare(ctx context.Context) {
	r.mu.Lock()
	ready := r.spareReady
	pending := r.spare == nil && ready != nil
	r.mu.Unlock()
	if !pending {
		return
	}
	t := time.NewTimer(r.T.DialWait)
	defer t.Stop()
	select {
	case <-ready:
	case <-t.C:
	case <-ctx.Done():
	}
}

// takeSpare hands the spare on offer to one caller, or nil.
func (r *Room) takeSpare() net.Conn {
	r.mu.Lock()
	c, at, taken := r.spare, r.spareAt, r.spareTaken
	r.spare = nil
	r.mu.Unlock()
	if c == nil {
		return nil
	}
	taken <- struct{}{}
	if time.Since(at) >= spareLife {
		c.Close()
		return nil
	}
	return c
}

// relayConn is a relay connection past its welcome: the spare when one is on offer, else a fresh dial.
func (r *Room) relayConn(ctx context.Context, session string) (net.Conn, *bufio.Reader, wire.Welcome, error) {
	return r.kindConn(ctx, session, relayKind)
}

// kindConn is a connection of the named kind past its welcome, the same way.
func (r *Room) kindConn(ctx context.Context, session, kind string) (net.Conn, *bufio.Reader, wire.Welcome, error) {
	hi := hello{Kind: kind, Room: r.Name, Session: session}
	r.waitSpare(ctx)
	if c := r.takeSpare(); c != nil {
		br := bufio.NewReader(c)
		w, err := sayHello(c, br, hi)
		if err == nil {
			return c, br, w, nil
		}
		c.Close()
		if !w.OK && w.Error != "" {
			return nil, nil, w, err
		}
		// No welcome, so the hub took nothing: the request is written only after one.
	}
	dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
	c, err := r.Dial.Dial(dialCtx)
	cancel()
	if err != nil {
		return nil, nil, wire.Welcome{}, err
	}
	br := bufio.NewReader(c)
	w, err := sayHello(c, br, hi)
	if err != nil {
		c.Close()
	}
	return c, br, w, err
}

// helloWait is how long the hub waits for a hello on c: wire.SpareWait once a TLS handshake proved a room certificate.
func helloWait(c net.Conn) time.Duration {
	tc, ok := c.(interface{ Handshake() error })
	if !ok {
		return handshakeWait
	}
	if c.SetDeadline(time.Now().Add(handshakeWait)) != nil || tc.Handshake() != nil || identify(c) == "" {
		return handshakeWait
	}
	return wire.SpareWait
}
