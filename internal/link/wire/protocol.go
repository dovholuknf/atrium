package wire

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
// A data connection says Hello and from that byte on it is an ordinary HTTP/1.1
// socket in both directions. That is what lets a websocket upgrade and a large
// download work without this package knowing they exist.
//
// The control connection keeps speaking lines for its whole life, because it is
// the only thing that needs to: a hub asking for more connections, and a room
// saying it is still there.

// Hello is the first line on every connection, in both kinds.
type Hello struct {
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
	// Commit is the git commit this room's binary was built from, so the hub can say which landed commits the room is
	// still missing. Observed, never trusted: it only decides what the deploy queue lists. Empty from an older room.
	Commit string `json:"commit,omitempty"`
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
	// Git says this room takes git syncs: it serves /v1/git/sync and its own claude/*
	// branches. The hub never asks a room that did not say so. See git.go.
	Git bool `json:"git,omitempty"`
	// Fallback says this room is on a transport below the first it would try,
	// because the ones above did not answer. The room's word: the hub knows which
	// listener took the connection and not what the room would rather be on.
	Fallback bool `json:"fallback,omitempty"`
	// Tools is where each toolchain (go, node, ...) is on this room, for atrium_resources. Observed, never trusted.
	Tools map[string]string `json:"tools,omitempty"`
}

// Welcome is the hub's answer to a hello.
type Welcome struct {
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
	// Git says this hub serves repositories on the `git` connection kind. A room never
	// dials that kind at a hub that did not say so. See git.go.
	Git bool `json:"git,omitempty"`
	// Paths are the ways this hub can be reached, in the hub's order, so a room
	// cut off on one knows the next on its next start. Never a credential.
	Paths []Path `json:"paths,omitempty"`
}

// note is a line on the control connection, after the handshake. One struct
// rather than a type tag per message, because there are two kinds of thing to
// say and a tagged union of two is ceremony.
type Note struct {
	// Need asks the room for this many more data connections.
	Need int `json:"need,omitempty"`
	// Beat is a room saying it is still there, and a hub saying the same back.
	// The echo is not decoration: it is how a room finds out its socket is a
	// half-open one that will never error on write.
	Beat int64 `json:"beat,omitempty"`
	// IdleCPU is a room's idle CPU percent, riding on its beat. A pointer so a room at 0 is not mistaken for one
	// that did not say, and an older room that never sends it reads as unknown. See `placePRRoom`.
	IdleCPU *float64 `json:"idle_cpu,omitempty"`
	// Login is why this room's claude login needs doing, "" when it does not, riding on the beat. A pointer so a room
	// that never says (an older build) is not read as a room that is fine. Observed, and never acted on by the hub
	// beyond showing it and ringing once. See internal/daemon/loginneeded.go.
	Login *string `json:"login,omitempty"`
	// PathStates is a room saying how each of its rungs looks from where it stands, riding on the beat (not `paths`, which
	// is the hub's own list going the other way). Observed, bounded by the hub, never acted on. An older hub ignores it and
	// an older room never sends it. See pathstate.go.
	PathStates []PathState `json:"path_states,omitempty"`
	// Bye is a side closing deliberately, so the other end logs a shutdown
	// rather than a failure.
	Bye string `json:"bye,omitempty"`
	// Offer is a hub saying what it is running, in case the room wants it.
	// Saying, not doing. See `upgrade.go`.
	Offer *Offer `json:"offer,omitempty"`
	// Restart is a hub asking a room to restart ITSELF. The hub spawns nothing on
	// the room's machine: it forwards the instruction and the room parks its
	// other agents, spawns a detached restarter and winds down. See RestartAsk
	// and the room's OnRestart.
	Restart *RestartAsk `json:"restart,omitempty"`
	// Access is a room saying what its remote access switch says, at attach and on every change. Observed, and never
	// trusted for granting: the room checks its own file again at every stream. See stream.go.
	Access *RemoteAccess `json:"access,omitempty"`
	// Paths is a hub saying its list of ways in changed, in the shape the Welcome carries it. A room hears it
	// exactly as it hears the welcome's list. Never a credential.
	Paths []Path `json:"paths,omitempty"`
}

// RestartAsk is a hub asking a room to turn itself off and on again.
//
// FORWARDED, NOT PERFORMED. The hub never reaches into a room, so this is the
// whole of the hub's part: it names why, whether to interrupt busy agents, and
// how long to wait for them. The room decides what to do with it, exactly as it
// decides about an Offer. See Room.OnRestart.
type RestartAsk struct {
	// Why is recorded and told to the parked agents, so a board that came back
	// has an account of who asked and what for.
	Why string `json:"why,omitempty"`
	// Force skips waiting for other agents to reach a stopping point. Off by
	// default: a restart closes every terminal the room owns, and closing one
	// kills the process in it.
	Force bool `json:"force,omitempty"`
	// WaitSeconds bounds how long to wait for busy agents. Zero takes the room's
	// default.
	WaitSeconds int `json:"wait_seconds,omitempty"`
	// Immediate keeps the old restart: no wrap-up of the working sessions, only the park-and-refuse
	// before. Off by default, so a restart asks its working cards to wrap up, wakes them and then goes.
	Immediate bool `json:"immediate,omitempty"`
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

// HandshakeWait bounds the Hello exchange. A connection that opens and then
// says nothing is either a port scanner or a broken room, and either way it
// must not hold a slot.
const HandshakeWait = 10 * time.Second

// MaxLine bounds a frame. Nothing here is large, and an unbounded ReadString on
// a socket somebody else controls is a memory exhaustion waiting to be found.
const MaxLine = 64 << 10

// WriteJSON writes one frame and a newline.
func WriteJSON(w io.Writer, v any) error { return WriteJSONLimit(w, v, MaxLine) }

// WriteJSONLimit is WriteJSON with the frame bound the caller names.
func WriteJSONLimit(w io.Writer, v any, limit int) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > limit {
		return errors.New("frame too large")
	}
	_, err = w.Write(append(raw, '\n'))
	return err
}

// ReadJSON reads one frame, bounded.
//
// TAKES A *bufio.Reader AND KEEPS IT. A data connection stops being framed
// after the Hello, and whatever the bufio.Reader has already buffered is the
// first bytes of HTTP. Reading the Hello with a throwaway reader eats them, and
// the symptom is a first request that hangs while every later one works, which
// is a bad afternoon.
func ReadJSON(br *bufio.Reader, v any) error { return ReadJSONLimit(br, v, MaxLine) }

// ReadJSONLimit is ReadJSON with the frame bound the caller names.
func ReadJSONLimit(br *bufio.Reader, v any, limit int) error {
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
		if len(line) > limit {
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

// SayHello is the room's side of the handshake.
func SayHello(conn net.Conn, br *bufio.Reader, h Hello) (Welcome, error) {
	var w Welcome
	if err := conn.SetDeadline(time.Now().Add(HandshakeWait)); err != nil {
		return w, err
	}
	h.V = Version
	if err := WriteJSON(conn, h); err != nil {
		return w, err
	}
	if err := ReadJSON(br, &w); err != nil {
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

// HearHello is the hub's side.
func HearHello(conn net.Conn, br *bufio.Reader) (Hello, error) {
	var h Hello
	if err := conn.SetDeadline(time.Now().Add(HandshakeWait)); err != nil {
		return h, err
	}
	if err := ReadJSON(br, &h); err != nil {
		return h, err
	}
	if h.V != Version {
		// SAID OUT LOUD BEFORE HANGING UP. A room on an old binary against a
		// new hub is the ordinary consequence of upgrading one and not the
		// other, and it deserves a sentence rather than a closed socket.
		_ = WriteJSON(conn, Welcome{OK: false, Error: fmt.Sprintf(
			"this hub speaks link version %d and that room speaks %d. "+
				"update whichever is older", Version, h.V)})
		return h, fmt.Errorf("link version %d, wanted %d", h.V, Version)
	}
	switch h.Kind {
	case "control", "data", "enrol", UpgradeKind, AnnounceKind, RelayKind, GitKind, RoomSpecKind, StreamKind:
	default:
		_ = WriteJSON(conn, Welcome{OK: false,
			Error: "a connection is control, data, enrol, upgrade, announce, relay, git, roomspec or stream"})
		return h, fmt.Errorf("unknown connection kind %q", h.Kind)
	}
	return h, conn.SetDeadline(time.Time{})
}

// The connection kinds after control and data, named here so HearHello can check them.
const (
	UpgradeKind  = "upgrade"
	AnnounceKind = "announce"
	RelayKind    = "relay"
	GitKind      = "git"
	RoomSpecKind = "roomspec"
	StreamKind   = "stream"
)
