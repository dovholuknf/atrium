package conn

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
	Direct         = transport.Direct
	Join           = transport.Join
	Saved          = transport.Saved
	RoomTransports = transport.RoomTransports
	hello          = wire.Hello
	welcome        = wire.Welcome
	note           = wire.Note
)

const DefaultZitiService = transport.DefaultZitiService

const handshakeWait = wire.HandshakeWait

var (
	DefaultTimings = wire.DefaultTimings
	writeJSON      = wire.WriteJSON
	writeJSONLimit = wire.WriteJSONLimit
	readJSON       = wire.ReadJSON
	readJSONLimit  = wire.ReadJSONLimit
	sayHello       = wire.SayHello
	RejoinSentence = transport.RejoinSentence
	Arrange        = transport.Arrange
	Candidates     = transport.Candidates
	ParseOrder     = transport.ParseOrder
	hearHello      = wire.HearHello
)
