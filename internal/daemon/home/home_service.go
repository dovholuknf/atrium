package home

import (
	"context"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"io"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// Service is the card homes, the hub remote and the hub forge.
type Service struct {
	St *store.Store
	Ap *api.Server
	Calls

	// homeSize is the size walk over the card homes. See homesize.go.
	HomeSize HomeSizer
	// hubGit is the room's stable hub remote. See hubremote.go.
	HubGit HubGit
	// hubForge is the hub's forge, for a room with a hub. See hubforge.go.
	ForgeState HubForge
	// forgeOpen is the forge access alerts open now, by tool@host. See forgeaccess.go.
	ForgeMu   sync.Mutex
	ForgeOpen map[string]ForgeAlert
}

// Calls is every call out of the card homes into another feature.
type Calls struct {
	Publish          func(id string)
	OptAgentAddr     func() string
	Room             func() string
	HasSession       func(id string) bool
	NotifyLauncher   func(worker *store.Task, source, key, body string) bool
	WaitRunnerGone   func(taskID string, within time.Duration) bool
	CloseShell       func(taskID string)
	LeaveForCull     func(t *store.Task, res *shared.CullResult) error
	Culled           func(t *store.Task, res *shared.CullResult) *shared.CullResult
	EndThrowaway     func(taskID string)
	VisibleLauncher  func(ctx context.Context, worker *store.Task, byID, byName, byDir, dir string) *store.Task
	RecordHubBranch  func(card, branch, hubRepo string)
	LandedInto       func(dir, branch, into string) (bool, error)
	PreflightLook    func(cmd string) (string, error)
	PreflightRun     func(ctx context.Context, exe string, args []string, out io.Writer) error
	PreflightEachFor func() time.Duration
}

// Config is what the root hands the service.
type Config struct {
	St *store.Store
	Ap *api.Server
	Calls
}

func New(c Config) *Service {
	return &Service{St: c.St, Ap: c.Ap, Calls: c.Calls}
}
