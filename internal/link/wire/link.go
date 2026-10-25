// Package wire is the vocabulary of the room link: the frames, the timings and the types both halves speak.
// Package link carries it.
package wire

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
	// DataDeadline is how long a data dial may be pending before it counts as a failure. The dial cannot be cancelled
	// and keeps its slot until it returns. See dials.go.
	DataDeadline time.Duration
	// Backoff is how long a room waits before redialling a hub that is not
	// answering, and the ceiling it grows to.
	Backoff    time.Duration
	BackoffMax time.Duration
	// ProbeEvery is how often a room on a fallback transport tries the better
	// ones. Two good probes in a row move it back up. See ladder.go.
	ProbeEvery time.Duration
	// Starve is how long an attached room's pool may sit empty with no data
	// connection arriving before the hub says so. The control connection being
	// up is not the room serving anything.
	Starve time.Duration
	// Access is how often a room looks at its remote access switch, and ends the ssh streams it no longer allows. See
	// stream.go.
	Access time.Duration
	// PathsEvery is how often a room probes every rung for the board's per path badges. See pathstate.go.
	PathsEvery time.Duration
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
		DialWait: 10 * time.Second,
		Backoff:  time.Second,
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
		// TWO MINUTES, and two in a row before moving. A move is a reattach and
		// costs what a hub restart costs, so a path that flaps must not drag a
		// room back and forth. See docs/rnd/room-transport-order.md.
		ProbeEvery: 2 * time.Minute,
		// Three of the hub's own DialWait, so a room on a slow overlay that is
		// still bringing its warm pool up is not called starved.
		Starve: 30 * time.Second,
		// Ten seconds: turning remote access off ends every open stream within this.
		Access: 10 * time.Second,
		// Thirty seconds: a rung that came back should show soon, and a probe is one dial.
		PathsEvery: 30 * time.Second,
	}
}

// fill replaces zero values with the defaults, so a caller can set one field.
func (t Timings) Fill() Timings {
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
	if t.DataDeadline <= 0 {
		t.DataDeadline = 30 * time.Second
	}
	if t.Backoff <= 0 {
		t.Backoff = d.Backoff
	}
	if t.BackoffMax <= 0 {
		t.BackoffMax = d.BackoffMax
	}
	if t.ProbeEvery <= 0 {
		t.ProbeEvery = d.ProbeEvery
	}
	if t.Starve <= 0 {
		t.Starve = d.Starve
	}
	if t.Access <= 0 {
		t.Access = d.Access
	}
	if t.PathsEvery <= 0 {
		t.PathsEvery = d.PathsEvery
	}
	return t
}
