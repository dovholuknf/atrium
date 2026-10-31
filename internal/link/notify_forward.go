package link

import (
	"context"
	"net/http"
	"sync"

	"github.com/dovholuknf/atrium/internal/link/notify"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// What the notify package declares and the rest of this package reaches by its old name.
type (
	Notifier     = notify.Notifier
	Push         = notify.Push
	Growler      = notify.Growler
	GrowlRow     = notify.GrowlRow
	GrowlStore   = notify.GrowlStore
	Notice       = notify.Notice
	Result       = notify.Result
	Sink         = notify.Sink
	NotifyStore  = notify.NotifyStore
	NotifyStatus = notify.NotifyStatus
	PushStore    = notify.PushStore
	CommandSink  = notify.CommandSink
)

const (
	ReasonPermission = notify.ReasonPermission
	ReasonQuestion   = notify.ReasonQuestion
	ReasonInput      = notify.ReasonInput
	ReasonFinished   = notify.ReasonFinished
	ReasonLogin      = notify.ReasonLogin
	ReasonCI         = notify.ReasonCI
)

var (
	ErrGrowlNotFound = notify.ErrGrowlNotFound
	ErrGrowlStale    = notify.ErrGrowlStale
	ErrNoSub         = notify.ErrNoSub
)

var (
	NewNotifier    = notify.NewNotifier
	NewPush        = notify.NewPush
	NewGrowler     = notify.NewGrowler
	NotifyIdentity = notify.NotifyIdentity
)

type notifyHolder struct {
	once sync.Once
	svc  *notify.Service
}

// notifyService is the notifier, the push and the growler, made on first use so a Proxy built as a literal has them too.
func (p *Proxy) notifyService() *notify.Service {
	p.notifyH.once.Do(func() {
		p.notifyH.svc = notify.New(p.notifyCalls())
	})
	return p.notifyH.svc
}

func (p *Proxy) notifyCalls() notify.Calls {
	return notify.Calls{
		RecordAudit: p.RecordAudit,
		Rooms: func() []wire.Attached {
			if p.hub == nil {
				return nil
			}
			return p.hub.Rooms()
		},
		Has:        func(room string) bool { return p.hub != nil && p.hub.Has(room) },
		RoomClient: p.roomClient,
		HostFor:    hostFor,
		Broadcast:  func(ev wire.Event) { p.feeds.broadcast(ev) },
		Subscribe: func(room string) (<-chan wire.Event, func()) {
			s := p.feeds.add(room)
			return s.ch, func() { p.feeds.drop(s) }
		},
		Inventory: func() notify.BoardAuto {
			if st := p.inventory(); st != nil {
				return st
			}
			return nil
		},
		PRWarnSweep: p.prWarnSweep,
		GoWork:      p.goWork,
	}
}

func (p *Proxy) SetNotify(n *Notifier) { p.notifyService().SetNotify(n) }

func (p *Proxy) notifier() *Notifier { return p.notifyService().Notifier() }

func (p *Proxy) SetPush(x *Push) { p.notifyService().SetPush(x) }

func (p *Proxy) pusher() *Push { return p.notifyService().Pusher() }

func (p *Proxy) SetGrowler(g *Growler) { p.notifyService().SetGrowler(g) }

func (p *Proxy) growler() *Growler { return p.notifyService().Growler() }

func (p *Proxy) servePresence(w http.ResponseWriter, r *http.Request) {
	p.notifyService().ServePresence(w, r)
}

func (p *Proxy) roomGet(ctx context.Context, room, path string, out any) bool {
	return p.notifyService().RoomGet(ctx, room, path, out)
}
