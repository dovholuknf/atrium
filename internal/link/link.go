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
// rather than a change to anything above it. See `docs/archive/hub-room-plan.md`.
package link

import (
	"github.com/dovholuknf/atrium/internal/link/transport"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

const Version = wire.Version

type (
	Dialer         = wire.Dialer
	Timings        = wire.Timings
	RestartAsk     = wire.RestartAsk
	Offer          = wire.Offer
	Path           = wire.Path
	PathState      = wire.PathState
	RemoteAccess   = wire.RemoteAccess
	Proven         = transport.Proven
	Ziti           = transport.Ziti
	Zrok           = transport.Zrok
	ZrokAPI        = transport.ZrokAPI
	Direct         = transport.Direct
	Keys           = transport.Keys
	Join           = transport.Join
	Saved          = transport.Saved
	RoomTransports = transport.RoomTransports
	hello          = wire.Hello
	welcome        = wire.Welcome
	note           = wire.Note
)

const DefaultZitiService = transport.DefaultZitiService

const (
	handshakeWait = wire.HandshakeWait
	maxLine       = wire.MaxLine
)

var (
	DefaultTimings = wire.DefaultTimings
	otherRoom      = wire.OtherRoom
	writeJSON      = wire.WriteJSON
	writeJSONLimit = wire.WriteJSONLimit
	readJSON       = wire.ReadJSON
	readJSONLimit  = wire.ReadJSONLimit
	sayHello       = wire.SayHello
	RejoinSentence = transport.RejoinSentence
	DefaultOrder   = transport.DefaultOrder
	Arrange        = transport.Arrange
	Candidates     = transport.Candidates
	Hosts          = transport.Hosts
	ParseOrder     = transport.ParseOrder
	MixedListener  = transport.MixedListener
	NewCSR         = transport.NewCSR
	ParseToken     = transport.ParseToken
	EnrolOver      = transport.EnrolOver

	MintOverlayToken     = transport.MintOverlayToken
	DirectAuthenticated  = transport.DirectAuthenticated
	OverlayAuthenticated = transport.OverlayAuthenticated
	ZitiAuthenticated    = transport.ZitiAuthenticated
	ZrokAuthenticated    = transport.ZrokAuthenticated
	hearHello            = wire.HearHello
)
