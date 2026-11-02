package roomconf

import (
	"sync"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/publish"
	"github.com/dovholuknf/atrium/internal/store"
)

// Options is what the room configuration reads of the room's options.
type Options struct {
	AgentAddr, HumanAddr, DBPath, Room, RoomDir, LocationFile string
}

// Service is the room's own configuration: export, import, where-am-i, health, lifecycle and ready.
type Service struct {
	St   *store.Store
	Ap   *api.Server
	Opts func() Options
	Calls

	// clearOnce guards removing the address file, which the shutdown path does
	// before it closes the store and Run's deferred call would do again after.
	// See clearLocation.
	ClearOnce sync.Once
}

// Calls is every call out of the room configuration into another feature.
type Calls struct {
	PidAlive          func(pid int) bool
	HealthHalted      func(h HaltReader) (bool, error)
	Settling          func() bool
	LoginHealth       func() map[string]any
	ToolchainHealth   func() map[string]any
	ZrokConfig        func() publish.ZrokConfig
	ZitiConfig        func() publish.ZitiConfig
	SaveOverlayConfig func(key string, v any) error
	TaskFor           func(agent, taskID string) (*store.Task, error)
	// cycle is the card's context cycle: the handoff file, whether it waits at the limit step, and whether it was acked.
	Cycle    func(taskID string) (file string, atLimit, acked bool)
	CycleAck func(taskID string) bool
	WrapAck  func(taskID string) bool
	CycleBy  string
	Publish  func(id string)
}

// Config is what the root hands the service.
type Config struct {
	St   *store.Store
	Ap   *api.Server
	Opts func() Options
	Calls
}

func New(c Config) *Service {
	return &Service{St: c.St, Ap: c.Ap, Opts: c.Opts, Calls: c.Calls}
}
