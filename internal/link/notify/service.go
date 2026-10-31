package notify

import (
	"net/http"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// BoardAuto is the hub-wide auto-approve flag, read from the hub's inventory.
type BoardAuto interface {
	BoardAuto() (on bool, until *time.Time, err error)
}

// Calls is every call out of the notifier, the push, the growler and the approver into the rest of the hub.
type Calls struct {
	RecordAudit func(room, kind, detail string)
	// Rooms lists the attached rooms, and Has says whether one is. Both answer for no rooms on a hub with no link.
	Rooms func() []wire.Attached
	Has   func(room string) bool
	// RoomClient is the client that reaches one room, and HostFor the host to address it by.
	RoomClient func(room string) *http.Client
	HostFor    func(room string) string
	// Broadcast sends one event to every board.
	Broadcast func(ev wire.Event)
	// Subscribe joins the event feed for a room, empty meaning all, and answers the stream and how to leave it.
	Subscribe func(room string) (events <-chan wire.Event, leave func())
	// Inventory is the hub's inventory, nil when the hub has none.
	Inventory func() BoardAuto
	// PRWarnSweep is the PR claim warning the growler waits on.
	PRWarnSweep func(g *Growler) bool
	// GoWork runs fn as hub work that a shutdown waits for.
	GoWork func(fn func())
}

// Service is the notifier, the phone push and the growler of a hub, and the HTTP half of each.
type Service struct {
	// mu guards notify, push and growl, which are wired after the Service is built.
	mu     sync.Mutex
	notify *Notifier
	push   *Push
	growl  *Growler
	Calls
}

// New is a Service with nothing wired yet.
func New(c Calls) *Service { return &Service{Calls: c} }
