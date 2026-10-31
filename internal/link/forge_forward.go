package link

import (
	"context"
	"net/http"
	"sync"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	forgepkg "github.com/dovholuknf/atrium/internal/link/forge"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// What the forge package declares and the rest of this package reaches by its old name.
type (
	CIHook                 = forgepkg.CIHook
	EventHook              = forgepkg.EventHook
	GitSettings            = forgepkg.GitSettings
	StashRetentionSettings = forgepkg.StashRetentionSettings
	ForgeSettings          = forgepkg.ForgeSettings
	CIBinder               = forgepkg.CIBinder
	CIRoute                = forgepkg.CIRoute
	HubRecognisers         = forgepkg.HubRecognisers
	PRClaimMove            = forgepkg.PRClaimMove
)

var (
	NewCIHook     = forgepkg.NewCIHook
	ParseCIRoutes = forgepkg.ParseCIRoutes
	MergerBranch  = forgepkg.MergerBranch
)

type forgeHolder struct {
	once sync.Once
	svc  *forgepkg.Service
}

// forgeService is the forge, the PR claims, the merge queue, the git side and the CI hook, made on first use so a Proxy
// built as a literal has them too.
func (p *Proxy) forgeService() *forgepkg.Service {
	p.forgeH.once.Do(func() {
		s := forgepkg.New(p.forgeCalls())
		s.WarnGrace = forgepkg.PRWarnGrace
		p.forgeH.svc = s
	})
	return p.forgeH.svc
}

func (p *Proxy) forgeCalls() forgepkg.Calls {
	return forgepkg.Calls{
		RecordAudit: p.RecordAudit,
		Rooms: func() []wire.Attached {
			if p.hub == nil {
				return nil
			}
			return p.hub.Rooms()
		},
		Has:              func(room string) bool { return p.hub != nil && p.hub.Has(room) },
		RoomClient:       p.roomClient,
		RoomFor:          p.roomFor,
		RoomGet:          p.roomGet,
		MarkedRooms:      p.markedRooms,
		CachedCardRoom:   p.cachedCardRoom,
		RememberCardRoom: p.rememberCardRoom,
		HubUp:            func() bool { return p.hub != nil },
		Transport:        func(room string) http.RoundTripper { return p.hub.Transport(room) },
		MountForge:       func(h http.Handler) { p.hub.Forge = h },
		MountPRClaim:     func(h http.Handler) { p.hub.PRClaim = h },
		Deliverer:        p.Deliverer,
		Growler:          p.growler,
		Ctl: func() ctl.Caller {
			p.mu.Lock()
			c := p.ctl
			p.mu.Unlock()
			if c == nil {
				return nil
			}
			return c
		},
		BacklogStore: p.backlogStore,
		BacklogEvent: p.backlogEvent,
		Broadcast:    func(ev wire.Event) { p.feeds.broadcast(ev) },
		WorkOfTags:   WorkOfTags,
	}
}

// markedRooms is the rooms that start nothing new.
func (p *Proxy) markedRooms() map[string]bool {
	out := map[string]bool{}
	stock := p.inventory()
	if stock == nil {
		return out
	}
	known, err := stock.Known()
	if err != nil {
		return out
	}
	for _, k := range known {
		if k.State == "marked-for-deletion" {
			out[wire.KeyOf(k.Name)] = true
		}
	}
	return out
}

const (
	hubSubsPath      = forgepkg.HubSubsPath
	PlacedRoomHeader = forgepkg.PlacedRoomHeader
)

type (
	placedPaste = forgepkg.PlacedPaste
	placedKey   = forgepkg.PlacedKey
)

var (
	prIDIn       = forgepkg.PRIDIn
	walkerLaunch = forgepkg.WalkerLaunch
	mergeN       = forgepkg.MergeN
	itoaN        = forgepkg.ItoaN
)

func (p *Proxy) git() *gitsync.Hub { return p.forgeService().Git() }

func (p *Proxy) forgeSide() *forgepkg.HubForge { return p.forgeService().ForgeSide() }

func (p *Proxy) forgeCI(ctx context.Context, f *forgepkg.HubForge, ask forge.HubCIAsk) (forge.HubCI, error) {
	return p.forgeService().ForgeCI(ctx, f, ask)
}

func (p *Proxy) serveCIHook(w http.ResponseWriter, r *http.Request, sub string) {
	p.forgeService().ServeCIHook(w, r, sub)
}

func (p *Proxy) serveForgeAdmin(w http.ResponseWriter, r *http.Request, rest string) {
	p.forgeService().ServeForgeAdmin(w, r, rest)
}

func (p *Proxy) servePRClaims(w http.ResponseWriter, r *http.Request, sub string) {
	p.forgeService().ServePRClaims(w, r, sub)
}

func (p *Proxy) serveMerge(w http.ResponseWriter, r *http.Request, sub string) {
	p.forgeService().ServeMerge(w, r, sub)
}

func (p *Proxy) serveGit(w http.ResponseWriter, r *http.Request, sub string) {
	p.forgeService().ServeGit(w, r, sub)
}

func (p *Proxy) serveGitStore(w http.ResponseWriter, r *http.Request) {
	p.forgeService().ServeGitStore(w, r)
}

func (p *Proxy) serveHubEvents(w http.ResponseWriter, r *http.Request, sub string) {
	p.forgeService().ServeHubEvents(w, r, sub)
}

func (p *Proxy) prWarnSweep(g *Growler) bool { return p.forgeService().PRWarnSweep(g) }

func (p *Proxy) prClaims() *hubstore.Store { return p.forgeService().PRClaims() }

func (p *Proxy) placedOn(w http.ResponseWriter, r *http.Request, room string) *http.Request {
	return p.forgeService().PlacedOn(w, r, room)
}

func (p *Proxy) placeRoomHolding(ctx context.Context, fallback string, skip map[string]string, repo string) string {
	return p.forgeService().PlaceRoomHolding(ctx, fallback, skip, repo)
}

func (p *Proxy) placePaste(r *http.Request, room string) *http.Request {
	return p.forgeService().PlacePaste(r, room)
}

func (p *Proxy) hubRecognisersRoute(w http.ResponseWriter, r *http.Request) bool {
	return p.forgeService().HubRecognisersRoute(w, r)
}

func (p *Proxy) hubRecognise(w http.ResponseWriter, r *http.Request) bool {
	return p.forgeService().HubRecognise(w, r)
}

func (p *Proxy) placePR(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	return p.forgeService().PlacePR(w, r)
}

func (p *Proxy) placeNewPR(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	return p.forgeService().PlaceNewPR(w, r)
}

func (p *Proxy) placePRWorktree(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	return p.forgeService().PlacePRWorktree(w, r)
}

func (p *Proxy) releaseOnRefusal(w http.ResponseWriter, r *http.Request) http.ResponseWriter {
	return p.forgeService().ReleaseOnRefusal(w, r)
}

func (p *Proxy) pullsList(w http.ResponseWriter, r *http.Request) bool {
	return p.forgeService().PullsList(w, r)
}

func (p *Proxy) retagPR(res *http.Response) error { return p.forgeService().RetagPR(res) }

func (p *Proxy) retryDeaf(res *http.Response) (string, error) { return p.forgeService().RetryDeaf(res) }

func (p *Proxy) SetCIDir(dir string)                         { p.forgeService().SetCIDir(dir) }
func (p *Proxy) SetEvents(h *EventHook)                      { p.forgeService().SetEvents(h) }
func (p *Proxy) SetForge(st ForgeSettings, run forge.Runner) { p.forgeService().SetForge(st, run) }
func (p *Proxy) SetPRClaims(st *hubstore.Store)              { p.forgeService().SetPRClaims(st) }
func (p *Proxy) SetCIHook(h *CIHook)                         { p.forgeService().SetCIHook(h) }
func (p *Proxy) SetGit(g *gitsync.Hub)                       { p.forgeService().SetGit(g) }
func (p *Proxy) SetGitSettings(s GitSettings, hubDir string) {
	p.forgeService().SetGitSettings(s, hubDir)
}
func (p *Proxy) NewEventHook(st *hubstore.Store, from string, audit func(kind, detail string)) *EventHook {
	return p.forgeService().NewEventHook(st, from, audit)
}
func (p *Proxy) RunMerges(ctx context.Context)  { p.forgeService().RunMerges(ctx) }
func (p *Proxy) MergePruneRoom(room string)     { p.forgeService().MergePruneRoom(room) }
func (p *Proxy) ForgeHelper(host string) string { return p.forgeService().ForgeHelper(host) }
func (p *Proxy) GitCards(ctx context.Context, room, card string) (gitsync.CardState, error) {
	return p.forgeService().GitCards(ctx, room, card)
}
func (p *Proxy) MovePRClaim(key, to string) (hubstore.PRClaim, error) {
	return p.forgeService().MovePRClaim(key, to)
}
func (p *Proxy) MovePRClaimWith(ctx context.Context, key, to, walker string) (PRClaimMove, error) {
	return p.forgeService().MovePRClaimWith(ctx, key, to, walker)
}

const CIListenerKind = forgepkg.CIListenerKind
