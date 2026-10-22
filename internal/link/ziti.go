package link

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/zitictx"
	"github.com/openziti/sdk-golang/ziti/edge"
)

// The OpenZiti transport, which is the one that should have been first.
//
// ── why this is nine tenths shorter than the direct one ──
//
// `direct.go` spends two hundred lines minting a certificate authority,
// signing requests and pinning fingerprints, and every one of those lines
// exists to answer one question: who is this. OpenZiti answered that question
// before the first byte arrived. An identity is enrolled once, a policy says
// which identities may dial and which may bind, and a connection that should
// not exist is never made.
//
// That proves the connection may reach the hub and not WHICH ROOM it is, so the
// old form of a join string has no enrolment here, no certificate and no join
// secret, and the hub says so plainly when somebody tries. The new form runs
// direct.go's mutual TLS inside this transport (see overlaytls.go), and the old
// form keeps attaching beside it until `atrium rooms legacy refuse`.
//
// This is what the design meant by "a transport supplies a net.Listener and
// something that returns a net.Conn". Everything above this file is unchanged.
//
// ── what is given up, said plainly ───────────────────────
//
// A ziti service has to exist and have policies, which is a controller and an
// administrator. `atrium2 hub --transport direct` needs neither and is why it
// ships first. Where there is already a network, this is strictly better: the
// board is not on a port on a machine, there is nothing to firewall, and
// revoking a room is a policy change rather than a certificate to chase.

// Ziti reaches a hub over an OpenZiti service.
//
// The hub BINDS the service and a room DIALS it, which is the same direction
// the rest of this package assumes and for the same reason: the room is the
// thing that must not care whether the hub is up.
type Ziti struct {
	// Identity is the path to an enrolled identity file, the `.json` that
	// `ziti edge enroll` produced.
	Identity string
	// Service is the name of the service the hub binds and rooms dial.
	Service string

	// The context is expensive to build and holds the authenticated session, so the process has ONE per identity
	// (internal/zitictx), opened by the first dial and never closed while the process runs. Reset logs it in again
	// rather than building another, since every context left behind keeps its own refresh loop.
	//
	// The control connection has a context of its own (ctl), the identity's control lane, so its beats never queue
	// behind bulk circuits on the same router connections. See lane.go.
	//
	// mu guards the pointers and nothing else. It is never held across a dial.
	mu  sync.Mutex
	ctx zitiConn
	ctl zitiConn
	// newCtx builds one. Nil means the SDK, through internal/zitictx.
	newCtx func() (zitiConn, error)
	// gate backs dials off after a failure. See dialgate.go.
	gate dialGate
}

// zitiConn is the part of a ziti context this package uses.
type zitiConn interface {
	Dial(service string) (edge.Conn, error)
	Listen(service string) (edge.Listener, error)
	Close()
}

// Describe is what to log. The service, which is the whole address.
func (z *Ziti) Describe() string {
	if z.Service == "" {
		return "a ziti service"
	}
	return "ziti:" + z.Service
}

func (z *Ziti) build(control bool) (zitiConn, error) {
	if z.newCtx != nil {
		return z.newCtx()
	}
	id := strings.TrimSpace(z.Identity)
	if id == "" {
		return nil, errors.New(
			"no ziti identity. enroll one and pass --identity, or use --transport direct")
	}
	if strings.TrimSpace(z.Service) == "" {
		return nil, errors.New("no ziti service named. --service is which service to use")
	}
	lane := ""
	if control {
		lane = "control"
	}
	c, err := zitictx.SharedFileLane("link", id, lane)
	if err != nil {
		return nil, fmt.Errorf("could not use that identity: %w", err)
	}
	return c, nil
}

// context returns the live context for a lane, building one when there is none.
func (z *Ziti) context(control bool) (zitiConn, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	p := &z.ctx
	if control {
		p = &z.ctl
	}
	if *p != nil {
		return *p, nil
	}
	c, err := z.build(control)
	if err != nil {
		return nil, err
	}
	*p = c
	return c, nil
}

// Reset logs the live context in again, beside any dial in flight. The link calls it after repeated data dial
// failures, when the context's session is the likelier fault. Nothing is closed and nothing is built.
func (z *Ziti) Reset() {
	z.mu.Lock()
	cs := []zitiConn{z.ctx, z.ctl}
	z.mu.Unlock()
	for _, c := range cs {
		if r, ok := c.(interface{ Relogin() error }); ok {
			go func() { _ = r.Relogin() }()
		}
	}
}

// Listen binds the service, which is the hub's side.
//
// NOTHING IS HOSTED AND NO PORT IS OPENED. The hub answers on the service
// itself, so there is no address on this machine for anything to reach and no
// tunneler in the middle.
func (z *Ziti) Listen() (net.Listener, error) {
	ctx, err := z.context(false)
	if err != nil {
		return nil, err
	}
	ln, err := ctx.Listen(z.Service)
	if err != nil {
		return nil, fmt.Errorf("could not bind the ziti service %q. "+
			"does this identity have a bind policy for it: %w", z.Service, err)
	}
	return ln, nil
}

// Dial opens one connection, which is the room's side.
func (z *Ziti) Dial(ctx context.Context) (net.Conn, error) {
	if wait, ok := z.gate.allow(); !ok {
		return nil, fmt.Errorf("the ziti controller is not answering. not dialling again for %s",
			wait.Round(time.Second))
	}
	zc, err := z.context(onControlLane(ctx))
	if err != nil {
		z.gate.failed()
		return nil, err
	}
	// The SDK's Dial takes no context, so it runs beside one.
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := zc.Dial(z.Service)
		if err != nil {
			ch <- res{nil, err}
			return
		}
		ch <- res{c, nil}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			z.dialFailed(zc)
			return nil, fmt.Errorf("could not dial the ziti service %q. "+
				"does this identity have a dial policy for it: %w", z.Service, r.err)
		}
		z.gate.worked()
		return r.c, nil
	case <-ctx.Done():
		z.dialFailed(zc)
		go func() {
			if r := <-ch; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, fmt.Errorf("the ziti dial for %q did not finish: %w", z.Service, ctx.Err())
	}
}

// dialFailed backs the next dial off. The context stays: closing it would only make the next dial build another.
func (z *Ziti) dialFailed(zitiConn) { z.gate.failed() }

// Hammering says dials to the controller are failing fast. Shown on the room's row.
func (z *Ziti) Hammering() bool { return z.gate.hammering() }

// Close does nothing. The context belongs to the process.
func (z *Ziti) Close() {}

// ZitiAuthenticated is `Hub.Authenticated` for this transport.
//
// This is the legacy path's word: `OverlayAuthenticated` answers for a room
// that arrives without a certificate, and asks the certificate for one that
// does.
//
// TRUE, AND THAT IS NOT A SHORTCUT. A connection only exists because a
// policy on the network allowed an enrolled identity to dial a service. There
// is no unauthenticated path to this handler, which is exactly the property the
// certificate machinery in `direct.go` is trying to reconstruct by hand.
func ZitiAuthenticated(net.Conn) bool { return true }
