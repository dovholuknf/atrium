package hublink

import (
	"context"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"github.com/dovholuknf/atrium/internal/store"
)

// Service is the link to the hub and the other rooms: relays, ssh streams, the hub dial and the rooms reporting in.
type Service struct {
	St *store.Store
	Calls

	// rooms holds the other machines reporting into this hub. In memory and
	// nowhere else: a room's cards are that room's state, and a second durable
	// copy here would be a source of truth that is wrong whenever the room is
	// unreachable. See rooms.go.
	RoomTable Rooms
	// relays is the way to other rooms, through the hub, and the drain of what
	// is owed to them. Empty on a daemon that is not a room. See relay.go.
	Relays RelayState
	// streams is the room's link for ssh streams to another room's sshd. See stream.go.
	Streams StreamHolder
	// hubDial is the room's ladder for commands on this machine. See hubdial.go.
	HubDial HubDialHolder
}

// Calls is every call out of the hub link into another feature.
type Calls struct {
	RecordSay           func(v store.Say, text string) string
	OtherRoom           func(room string) string
	Liveness            func(t *store.Task) store.Liveness
	WriteMissNote       func(w http.ResponseWriter, from, name, door, text, when string, reply bool, note string)
	SenderTask          func(from string) string
	KickItems           func()
	ItemSayRefused      func(from string) bool
	WriteItemSayRefused func(w http.ResponseWriter)
	HandoutFor          func(rep RoomReport) []shared.DispatchHandout
	HandleMessage       func(w http.ResponseWriter, r *http.Request)
	DoneBySay           func(sender *store.Task, text string)
	WithSayTrace        func(ctx context.Context, to, via string) context.Context
	PromptKind          func(kind string, reply bool) string
	Nat                 func(kind publish.OverlayKind) *publish.Native
	ZitiConfig          func() publish.ZitiConfig
	DefaultBackend      func() string
	Publish             func(id string)
	RelayKeep           func() time.Duration
	PeerAllow           func(from string) bool
	HumanAddr           func() string
}

// Config is what the root hands the service.
type Config struct {
	St *store.Store
	Calls
}

func New(c Config) *Service {
	return &Service{St: c.St, Calls: c.Calls}
}
