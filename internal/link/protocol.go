package link

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// The frames, and there are five of them.
//
// ONE JSON OBJECT PER LINE, and then the connection stops being framed at all.
// A data connection says hello and from that byte on it is an ordinary HTTP/1.1
// socket in both directions. That is what lets a websocket upgrade and a large
// download work without this package knowing they exist.
//
// The control connection keeps speaking lines for its whole life, because it is
// the only thing that needs to: a hub asking for more connections, and a room
// saying it is still there.

// hello is the first line on every connection, in both kinds.
type hello struct {
	V int `json:"v"`
	// Kind is "control" or "data".
	Kind string `json:"kind"`
	// Room is what the operator named this room. It is a label, not an
	// identity: the client CERTIFICATE decides who this is. A room that lies
	// here gets its certificate's name used instead, and the mismatch is
	// logged, because a name that disagrees with a certificate is either a
	// mistake or the beginning of an attempt.
	Room string `json:"room"`
	// Session ties a data connection to the control connection that was asked
	// for it, so two rooms enrolling at once cannot cross their pools.
	Session string `json:"session,omitempty"`
	// What this room is, for the board to show. Observed, never trusted.
	Version string `json:"version,omitempty"`
	Host    string `json:"host,omitempty"`
	// WHAT IT WOULD RUN, which is the difference between an upgrade offer that
	// is useful and one that is a broken binary. A Windows hub has nothing a
	// Linux room can execute, and finding that out after the swap is the worst
	// possible moment.
	OS   string `json:"os,omitempty"`
	Arch string `json:"arch,omitempty"`
	// Upgrades says this room is willing to be OFFERED a new binary. It never
	// means the hub may install one: the room still decides, fetches and
	// verifies. See `upgrade.go`.
	Upgrades bool `json:"upgrades,omitempty"`
}

// welcome is the hub's answer to a hello.
type welcome struct {
	OK bool `json:"ok"`
	// Error is why not, in the operator's words. A version mismatch says which
	// versions, because "handshake failed" sends somebody to a packet capture
	// for a thing the sentence could have told them.
	Error string `json:"error,omitempty"`
	// Session is minted by the hub on the control connection and echoed by
	// every data connection that follows.
	Session string `json:"session,omitempty"`
	// Warm is how many data connections to bring up straight away.
	Warm int `json:"warm,omitempty"`
	// Caches says this hub keeps a record of what its rooms are holding, and
	// so is worth telling.
	//
	// ASKED ONCE, AT THE HANDSHAKE, rather than found out by being refused.
	// A hub with no store is a normal thing: it is what every test builds and
	// what a hub whose store has not been wired up is. Without this the room
	// dials, is told no, and logs a failure on every change for the life of
	// the attachment, which reads as something being broken.
	Caches bool `json:"caches,omitempty"`
}

// note is a line on the control connection, after the handshake. One struct
// rather than a type tag per message, because there are two kinds of thing to
// say and a tagged union of two is ceremony.
type note struct {
	// Need asks the room for this many more data connections.
	Need int `json:"need,omitempty"`
	// Beat is a room saying it is still there, and a hub saying the same back.
	// The echo is not decoration: it is how a room finds out its socket is a
	// half-open one that will never error on write.
	Beat int64 `json:"beat,omitempty"`
	// Bye is a side closing deliberately, so the other end logs a shutdown
	// rather than a failure.
	Bye string `json:"bye,omitempty"`
	// Offer is a hub saying what it is running, in case the room wants it.
	// Saying, not doing. See `upgrade.go`.
	Offer *Offer `json:"offer,omitempty"`
}

// Offer is a binary a hub has, described well enough for a room to decide
// about it without fetching anything.
//
// THE HASH IS THE POINT. A room fetches over the link it already trusts, and
// then checks that what arrived is what was offered, so a truncated transfer
// or a hub that changed underneath is caught before anything is swapped rather
// than after it fails to start.
type Offer struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// handshakeWait bounds the hello exchange. A connection that opens and then
// says nothing is either a port scanner or a broken room, and either way it
// must not hold a slot.
const handshakeWait = 10 * time.Second

// maxLine bounds a frame. Nothing here is large, and an unbounded ReadString on
// a socket somebody else controls is a memory exhaustion waiting to be found.
const maxLine = 64 << 10

// writeJSON writes one frame and a newline.
func writeJSON(w io.Writer, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > maxLine {
		return errors.New("frame too large")
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}

// readJSON reads one frame, bounded.
//
// TAKES A *bufio.Reader AND KEEPS IT. A data connection stops being framed
// after the hello, and whatever the bufio.Reader has already buffered is the
// first bytes of HTTP. Reading the hello with a throwaway reader eats them, and
// the symptom is a first request that hangs while every later one works, which
// is a bad afternoon.
func readJSON(br *bufio.Reader, v any) error {
	// BOUNDED WHILE READING, NOT AFTER, and the difference is the whole point
	// of this loop existing instead of a one-line `ReadString`.
	//
	// `bufio.Reader.ReadString` has no size limit. It appends until it finds
	// the delimiter or the connection fails, so checking the length of what it
	// returned is checking an allocation that has already happened. An
	// unauthenticated peer, which enrolment deliberately allows, could open a
	// connection and stream gigabytes with no newline in it.
	//
	// `ReadSlice` fills the buffered reader and returns `ErrBufferFull` rather
	// than growing, so this reads in bounded pieces and gives up the moment the
	// total passes the limit. A check on the result is not a bound. The bound
	// has to be on the read.
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return errors.New("frame too large")
		}
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
	return json.Unmarshal(bytes.TrimSpace(line), v)
}

// sayHello is the room's side of the handshake.
func sayHello(conn net.Conn, br *bufio.Reader, h hello) (welcome, error) {
	var w welcome
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return w, err
	}
	h.V = Version
	if err := writeJSON(conn, h); err != nil {
		return w, err
	}
	if err := readJSON(br, &w); err != nil {
		return w, err
	}
	if !w.OK {
		return w, fmt.Errorf("the hub refused this room: %s", w.Error)
	}
	// CLEARED, and this is load bearing. The deadline above is for the
	// handshake. Leaving it set would kill a terminal fifteen seconds into
	// somebody reading their scrollback.
	return w, conn.SetDeadline(time.Time{})
}

// hearHello is the hub's side.
func hearHello(conn net.Conn, br *bufio.Reader) (hello, error) {
	var h hello
	if err := conn.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		return h, err
	}
	if err := readJSON(br, &h); err != nil {
		return h, err
	}
	if h.V != Version {
		// SAID OUT LOUD BEFORE HANGING UP. A room on an old binary against a
		// new hub is the ordinary consequence of upgrading one and not the
		// other, and it deserves a sentence rather than a closed socket.
		_ = writeJSON(conn, welcome{OK: false, Error: fmt.Sprintf(
			"this hub speaks link version %d and that room speaks %d. "+
				"update whichever is older", Version, h.V)})
		return h, fmt.Errorf("link version %d, wanted %d", h.V, Version)
	}
	switch h.Kind {
	case "control", "data", "enrol", upgradeKind, announceKind:
	default:
		_ = writeJSON(conn, welcome{OK: false,
			Error: "a connection is control, data, enrol, upgrade or announce"})
		return h, fmt.Errorf("unknown connection kind %q", h.Kind)
	}
	return h, conn.SetDeadline(time.Time{})
}
