package publish

import (
	"sync"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// Options is what the published board reads of the room's options.
type Options struct{ HumanAddr, Room, DBPath string }

// Service is the published board: overlays, card and doc shares, sign-in.
type Service struct {
	st *store.Store
	ap *api.Server
	// opts is read at call time, so a test that changes the daemon's options is seen.
	opts func() Options
	sup  interface{ Has(id string) bool }
	// publish forwards to Daemon.Publish at call time, so a test that replaces it is seen.
	publish func(id string)

	// Guard is the sign-in in front of every published board, the room's copy of the one the hub uses.
	*Guard

	Nats   map[OverlayKind]*Native
	NatsMu sync.Mutex
	Guests GuestShares
	Docs   DocShares
}

// Config is what the root hands the service.
type Config struct {
	St      *store.Store
	Ap      *api.Server
	Opts    func() Options
	Sup     interface{ Has(id string) bool }
	Publish func(id string)
}

func New(c Config) *Service {
	d := &Service{st: c.St, ap: c.Ap, opts: c.Opts, sup: c.Sup, publish: c.Publish, Nats: map[OverlayKind]*Native{}}
	d.Guard = NewGuard(c.St, d.PublicShareRunning)
	return d
}
