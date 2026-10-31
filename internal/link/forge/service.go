package forge

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/notify"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// Calls is every call out of the forge, the PR claims, the merge queue, the git side and the CI hook into the rest of
// the hub.
type Calls struct {
	RecordAudit func(room, kind, detail string)
	// Rooms lists the attached rooms, and Has says whether one is. Both answer for no rooms on a hub with no link.
	Rooms func() []wire.Attached
	Has   func(room string) bool
	// RoomClient is the client that reaches one room, RoomFor the room a request is for, and RoomGet one bounded GET
	// against a room decoded into out.
	RoomClient func(room string) *http.Client
	RoomFor    func(r *http.Request) (name string, named bool)
	RoomGet    func(ctx context.Context, room, path string, out any) bool
	// MarkedRooms is the rooms the inventory marks for deletion, keyed by wire.KeyOf.
	MarkedRooms func() map[string]bool
	// CachedCardRoom and RememberCardRoom are the hub's cache of which room a bare card id resolved to.
	CachedCardRoom   func(bare string) (string, bool)
	RememberCardRoom func(bare, room string)
	// HubUp says whether the hub has a link, Transport is the round tripper that reaches one room, and the Mount calls
	// put the forge's and the PR claim's handlers on the link.
	HubUp        func() bool
	Transport    func(room string) http.RoundTripper
	MountForge   func(h http.Handler)
	MountPRClaim func(h http.Handler)
	// Deliverer is the hub's own say path for the CI hook and the event hook.
	Deliverer func() Deliverer
	// Growler is the hub's growlers, nil when it has none.
	Growler func() *notify.Growler
	// Ctl is the hub's control server, nil until it is up.
	Ctl func() ctl.Caller
	// BacklogStore is the hub's backlog store, and BacklogEvent says a backlog event on every board.
	BacklogStore func() *hubstore.Store
	BacklogEvent func(kind string, v any)
	// Broadcast sends one event to every board.
	Broadcast func(ev wire.Event)
	// WorkOfTags is the backlog id a launch's tags attach it to, or "".
	WorkOfTags func(tags []string) string
}

// Service is the hub's forge, PR claims, merge queue, git side and CI hook.
type Service struct {
	Calls

	mu          sync.Mutex
	ci          *CIHook
	ev          *EventHook
	gitHub      *gitsync.Hub
	gitSettings GitSettings
	gitDir      string
	ciDir       string
	prc         *hubstore.Store
	prGone      map[string]time.Time
	fg          *HubForge
	deaf        map[string]string
	Merges      mergeQueue

	// WarnGrace is how long a claim's room is offline before the hub warns.
	WarnGrace time.Duration
}

// New is a Service with nothing wired yet.
func New(c Calls) *Service { return &Service{Calls: c} }

func (p *Service) ctlCaller() ctl.Caller {
	if p.Ctl == nil {
		return nil
	}
	return p.Ctl()
}
