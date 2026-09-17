// Package link carries a room's board to a hub over a connection the ROOM
// dialled.
//
// ── why this exists ─────────────────────────────────────
//
// Atrium today is one process. It serves the board, it holds the database, and
// it owns a pseudo terminal per running agent. Closing it takes every agent
// with it, so changing one line of CSS costs you every session on the machine.
//
// That is the whole problem this solves. Split the process in two along the
// line where the cost is:
//
//	HUB   serves index.html, the CSS and the JS. Holds nothing. Owns no
//	      process. Restart it as often as you like.
//	ROOM  holds the store, the supervisor and the agents. Serves JSON and
//	      byte streams. Stays up for days.
//
// The board's JavaScript does not change and does not know. It asks for
// `/v1/tasks` the way it always did. The hub serves the files it has and hands
// everything else to the room.
//
// ── why the room dials out ──────────────────────────────
//
// Two reasons, and the second is the one that matters.
//
// The obvious one is reachability: a room behind a firewall, on a laptop, or on
// another account cannot be dialled and does not have to be.
//
// The real one is that a room must not care whether the hub is up. If the hub
// dialled the room, a hub restart would be a connection refused on the room's
// side and a reconnect storm on the hub's. Inverted, a hub restart is the room
// noticing its socket closed and redialling on a backoff it already has. The
// room is the stable thing. The hub is the disposable thing. The connection
// points the same way.
//
// ── the trick, which is smaller than it sounds ───────────
//
// A room does not learn a new protocol. It serves the http.Handler it already
// has, on a net.Listener whose Accept returns connections it DIALLED rather
// than accepted:
//
//	http.Serve(reverseListener{hub}, daemon.Handler())
//
// The hub keeps those connections in a pool and speaks ordinary HTTP/1.1 down
// them. So websockets upgrade, server-sent events stream, and a 400MB file
// download is a file download, because none of it is being translated. There is
// no second representation of anything.
//
// ── what a transport has to provide ─────────────────────
//
// Exactly two things, which is the point:
//
//	hub:  a net.Listener
//	room: something that returns a net.Conn
//
// `direct.go` is mutual TLS over TCP and is what ships first. OpenZiti and zrok
// both hand back precisely those two types, so each is a file in this package
// rather than a change to anything above it. See `docs/hub-room-plan.md`.
package link

import (
	"context"
	"net"
	"time"
)

// Version is the wire version. A hub and a room that disagree say so on the
// first frame and hang up, rather than failing later in a way that reads as a
// network fault.
const Version = 1

// Dialer is the room's half of a transport. One method, because a transport
// that needs more than this is doing something the design does not want.
type Dialer interface {
	// Dial opens one connection to the hub. It is called for the control
	// connection and again for every data connection.
	Dial(ctx context.Context) (net.Conn, error)
	// Describe is what to log and show on the board. No secrets.
	Describe() string
}

// Timings collects everything the link waits on, in one place, because tuning
// these while they are scattered is how a heartbeat ends up faster than the
// timeout that consumes it.
type Timings struct {
	// Beat is how often a room says it is still there on the control
	// connection.
	Beat time.Duration
	// Silence is how long the hub waits for a beat before deciding a room is
	// gone. Comfortably more than one beat, or a slow moment reads as death.
	Silence time.Duration
	// Warm is how many data connections a room keeps ready. The first request
	// after a hub restart should not pay for a dial.
	Warm int
	// DialWait bounds how long the hub waits for the room to produce a
	// connection before answering 504. A room that is gone must not turn the
	// board into a spinner.
	DialWait time.Duration
	// Backoff is how long a room waits before redialling a hub that is not
	// answering, and the ceiling it grows to.
	Backoff    time.Duration
	BackoffMax time.Duration
}

// DefaultTimings are what both sides use unless something says otherwise.
//
// The relationship that matters: Silence is three beats. One missed beat on a
// busy machine is normal and must not evict a room holding live terminals.
func DefaultTimings() Timings {
	return Timings{
		Beat:    5 * time.Second,
		Silence: 15 * time.Second,
		// Four is enough for the board's own traffic with room for one
		// terminal, and a fifth is dialled the moment the pool is drawn down.
		Warm: 4,
		// Ten seconds is a long time to wait for a local socket and a short
		// time to wait for one over an overlay on a bad network.
		DialWait:   10 * time.Second,
		Backoff: time.Second,
		// FIVE SECONDS, NOT THIRTY, and the reason is the whole point of the
		// split. A hub is the disposable half: it is restarted to change a
		// stylesheet, and it is back within a second. A ceiling of thirty
		// meant a room that had been retrying for a minute would sit out most
		// of a minute AFTER the hub returned, and what somebody watching sees
		// is a board that takes half a minute to come back from a restart that
		// took one second. That reads as the link being broken.
		//
		// What a ceiling protects against is a room hammering a hub that is
		// down for hours. At five seconds that is 720 connect attempts an
		// hour, each one a TCP handshake that fails immediately. Nothing.
		BackoffMax: 5 * time.Second,
	}
}

// fill replaces zero values with the defaults, so a caller can set one field.
func (t Timings) fill() Timings {
	d := DefaultTimings()
	if t.Beat <= 0 {
		t.Beat = d.Beat
	}
	if t.Silence <= 0 {
		t.Silence = d.Silence
	}
	if t.Warm <= 0 {
		t.Warm = d.Warm
	}
	if t.DialWait <= 0 {
		t.DialWait = d.DialWait
	}
	if t.Backoff <= 0 {
		t.Backoff = d.Backoff
	}
	if t.BackoffMax <= 0 {
		t.BackoffMax = d.BackoffMax
	}
	return t
}
