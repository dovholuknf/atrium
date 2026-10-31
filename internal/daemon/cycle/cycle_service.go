package cycle

import (
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// Service is the context cycle, the keepalive, the restart wrap and wake, and the room hold.
type Service struct {
	St  *store.Store
	Ap  *api.Server
	Hk  *NewContextHooks
	Act Activity
	Sup shared.Registry
	Calls

	// wake holds the after-restart wakes, mirrored from the store, and when each
	// card's session last started. See restartwake.go.
	Wake *Wakes
	// holds is the room's deploy hold, copied from the store for the permission
	// path, and build is the binary it was set against. See roomhold.go.
	Holds *HoldState
	Build string
	// nctx holds the new-context sequences under way and the failed ones nobody
	// has dismissed. In memory. See newcontext.go.
	Nctx *NewContexts
	// wrap is the restart wrap-up under way. See restartwrap.go.
	Wrap *RestartWrap
	// ka keeps idle Claude cards' prompt caches warm. See keepalive.go.
	Ka *Keepalive
}

// Activity is what the context cycle asks of the activity tracker.
type Activity interface {
	DialogOpen(id string) bool
	TurnsBegun(id string) int
	MidTurn(id string) bool
	SinceBusy(id string) time.Duration
	OnSubagents(id string) bool
	QuietFor(id string) time.Duration
	LooksIdle(id string) bool
	What(id string) string
	Describe(id string, now time.Time) (what, age string)
}

// Calls is every call out of the context cycle into another feature.
type Calls struct {
	Publish                 func(id string)
	LabelFor                func(w *store.RestartWake) string
	DBPath                  func() string
	Room                    func() string
	HomeOf                  func(cardID string) (store.CardHome, bool)
	HasGuest                func(id string) bool
	ResetPending            func(taskID string)
	ContextTokens           func(t *store.Task) (int64, bool)
	CycleLimit              func(t *store.Task) int64
	CycleSubject            func(t *store.Task) bool
	HarnessOf               func(taskID string) (*store.Harness, *store.Task)
	MidTurnInputFor         func(taskID string) bool
	TypeLabelledGuarded     func(run shared.Runner, taskID, banner, text string, ok func() bool) (bool, error)
	TypeLabelledThroughGate func(run shared.Runner, taskID, banner, text string) (bool, error)
	TakeInContext           func(taskID string) []shared.DeliveredSay
	RequeueCarried          func(taskID string, carried []shared.DeliveredSay)
	ItemMissed              func(taskID string, carried []shared.DeliveredSay) []shared.DeliveredSay
	ShutdownAllowed         func(w http.ResponseWriter, r *http.Request) bool
}

// Config is what the root hands the service.
type Config struct {
	St *store.Store
	Ap *api.Server
	Hk *NewContextHooks
	Calls
}

func New(c Config) *Service {
	return &Service{St: c.St, Ap: c.Ap, Hk: c.Hk, Calls: c.Calls}
}
