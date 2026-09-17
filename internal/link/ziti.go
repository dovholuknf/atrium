package link

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/openziti/sdk-golang/ziti"
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
// So there is no enrolment here, no certificate, no join secret, and no CA to
// keep. `Hub.Enrol` is left nil and the hub says so plainly when somebody tries.
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

	// The context is expensive to build and holds the authenticated session,
	// so it is made once and reused for every dial. Without this a room with
	// four data connections would authenticate to the controller four times on
	// every reconnect.
	once sync.Once
	ctx  ziti.Context
	err  error
}

// Describe is what to log. The service, which is the whole address.
func (z *Ziti) Describe() string {
	if z.Service == "" {
		return "a ziti service"
	}
	return "ziti:" + z.Service
}

// context authenticates once.
func (z *Ziti) context() (ziti.Context, error) {
	z.once.Do(func() {
		id := strings.TrimSpace(z.Identity)
		if id == "" {
			z.err = errors.New(
				"no ziti identity. enroll one and pass --identity, or use --transport direct")
			return
		}
		if strings.TrimSpace(z.Service) == "" {
			z.err = errors.New("no ziti service named. --service is which service to use")
			return
		}
		cfg, err := ziti.NewConfigFromFile(id)
		if err != nil {
			z.err = fmt.Errorf("could not load that identity: %w", err)
			return
		}
		c, err := ziti.NewContext(cfg)
		if err != nil {
			z.err = fmt.Errorf("could not use that identity: %w", err)
			return
		}
		z.ctx = c
	})
	return z.ctx, z.err
}

// Listen binds the service, which is the hub's side.
//
// NOTHING IS HOSTED AND NO PORT IS OPENED. The hub answers on the service
// itself, so there is no address on this machine for anything to reach and no
// tunneler in the middle.
func (z *Ziti) Listen() (net.Listener, error) {
	ctx, err := z.context()
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
	zc, err := z.context()
	if err != nil {
		return nil, err
	}
	// The SDK's Dial takes no context. The caller's deadline still applies to
	// everything above, and a dial that hangs is bounded by the controller's
	// own timeouts rather than by this package.
	conn, err := zc.Dial(z.Service)
	if err != nil {
		return nil, fmt.Errorf("could not dial the ziti service %q. "+
			"does this identity have a dial policy for it: %w", z.Service, err)
	}
	return conn, nil
}

// Close releases the authenticated session.
func (z *Ziti) Close() {
	if z.ctx != nil {
		z.ctx.Close()
	}
}

// ZitiAuthenticated is `Hub.Authenticated` for this transport.
//
// ALWAYS TRUE, AND THAT IS NOT A SHORTCUT. A connection only exists because a
// policy on the network allowed an enrolled identity to dial a service. There
// is no unauthenticated path to this handler, which is exactly the property the
// certificate machinery in `direct.go` is trying to reconstruct by hand.
func ZitiAuthenticated(net.Conn) bool { return true }
