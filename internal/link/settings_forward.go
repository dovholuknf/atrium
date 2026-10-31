package link

import (
	"context"
	"net/http"
	"sync"

	"github.com/dovholuknf/atrium/internal/link/settings"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// What the settings package declares and the tests reach by its old name.
const (
	hubContextLimits = settings.HubContextLimits
	hubWorkerPolicy  = settings.HubWorkerPolicy
	workerPolicyOff  = settings.WorkerPolicyOff
)

type settingsHolder struct {
	once sync.Once
	svc  *settings.Service
}

// settingsService is the fan-out settings, made on first use so a Proxy built as a literal has them too.
func (p *Proxy) settingsService() *settings.Service {
	p.settingsH.once.Do(func() {
		p.settingsH.svc = settings.New(settings.Calls{
			Store: func() settings.Store {
				s, _ := p.inventory().(settings.Store)
				return s
			},
			Rooms: func() []wire.Attached {
				if p.hub == nil {
					return nil
				}
				return p.hub.Rooms()
			},
			RoomClient: p.roomClient,
			HostFor:    hostFor,
		})
	})
	return p.settingsH.svc
}

func (p *Proxy) ApplyInputLag() { p.settingsService().ApplyInputLag() }

func (p *Proxy) PushInputLag(room string) { p.settingsService().PushInputLag(room) }

func (p *Proxy) PushContextLimits(room string) { p.settingsService().PushContextLimits(room) }

func (p *Proxy) PushWorkerPolicy(room string) { p.settingsService().PushWorkerPolicy(room) }

func (p *Proxy) fanContextLimits(ctx context.Context, stored, skip string) {
	p.settingsService().FanContextLimits(ctx, stored, skip)
}

func (p *Proxy) fanWorkerPolicy(ctx context.Context, stored, skip string) {
	p.settingsService().FanWorkerPolicy(ctx, stored, skip)
}

func (p *Proxy) fanMinCols(w http.ResponseWriter, r *http.Request, payload []byte) {
	p.settingsService().FanMinCols(w, r, payload)
}
