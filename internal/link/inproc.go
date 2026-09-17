package link

import (
	"context"
	"errors"
	"net"
	"sync"
)

// The hub being a room too, without a network in between.
//
// ── why anybody wants this ───────────────────────────────
//
// A hub serves the board and holds nothing, which is the whole point: restart
// it whenever you like. But the machine the hub is on is usually a machine you
// also work on, and telling somebody to run a second process in a second
// terminal to use the computer in front of them is a silly answer.
//
// So a hub can also be a room. OFF BY DEFAULT, deliberately: a hub that holds a
// database is a hub whose restart is no longer free, which is the property the
// whole split exists to buy. Turning it on is saying you want that machine's
// agents here and you accept the trade.
//
// ── why it is a transport rather than a special case ─────
//
// The room half could have been wired straight into the proxy, and everything
// about it would then be a second code path: a second way a room is registered,
// a second way requests reach a handler, a second thing to keep in step with
// every change to the first.
//
// Instead this is one more transport, the same shape as `direct.go`, `ziti.go`
// and `zrok.go`. It hands back a listener and a dialer, the pair is
// `net.Pipe`, and the hub's own room enrols, heartbeats, pools connections and
// appears in `Rooms()` exactly like a machine across the world does. Nothing
// above knows the difference, which means nothing above can drift.
//
// The one thing it does NOT do is cross a network, so there is no certificate,
// no token and nothing to enrol: both ends are this process, and a process that
// cannot trust itself has bigger problems.

// InProc is a hub and a room in one process, joined by a pipe.
type InProc struct {
	once sync.Once
	// conns carries dialled connections to whoever is accepting.
	conns chan net.Conn
	done  chan struct{}
}

func (p *InProc) start() {
	p.once.Do(func() {
		p.conns = make(chan net.Conn)
		p.done = make(chan struct{})
	})
}

// Listen is the hub's half. The returned listener yields one end of every pipe
// the room dials.
func (p *InProc) Listen() net.Listener {
	p.start()
	return p
}

// Dialer is the room's half.
func (p *InProc) Dialer() Dialer {
	p.start()
	return inprocDialer{p}
}

// ── net.Listener ─────────────────────────────────────────

func (p *InProc) Accept() (net.Conn, error) {
	select {
	case c := <-p.conns:
		return c, nil
	case <-p.done:
		return nil, net.ErrClosed
	}
}

func (p *InProc) Close() error {
	p.start()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	return nil
}

func (p *InProc) Addr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "inproc" }
func (pipeAddr) String() string  { return "this process" }

// ── Dialer ───────────────────────────────────────────────

type inprocDialer struct{ p *InProc }

func (d inprocDialer) Dial(ctx context.Context) (net.Conn, error) {
	// A SYNCHRONOUS, UNBUFFERED HANDOFF, which is what makes this behave like a
	// real dial. The room is handed its end only once the hub has taken the
	// other, so a room dialling a hub that has stopped accepting blocks and
	// then fails, rather than piling up connections nobody will ever read.
	mine, theirs := net.Pipe()
	select {
	case d.p.conns <- inprocConn{theirs}:
		return mine, nil
	case <-d.p.done:
		mine.Close()
		theirs.Close()
		return nil, errors.New("the hub in this process has stopped listening")
	case <-ctx.Done():
		mine.Close()
		theirs.Close()
		return nil, ctx.Err()
	}
}

func (inprocDialer) Describe() string { return "this process" }

// inprocConn marks the hub's end of a pipe, so the hub can tell its own room
// apart from a machine across the world.
type inprocConn struct{ net.Conn }

// IsInProc reports whether a connection is the hub talking to itself.
//
// THE ONE THING THIS EXEMPTS IS AUTHENTICATION. Every other transport proves
// who a room is with a certificate the hub signed, because the connection
// crossed a network somebody else could also reach. This one did not leave the
// process. There is no key to check and nothing an attacker could be between.
func IsInProc(c net.Conn) bool {
	_, yes := c.(inprocConn)
	return yes
}
