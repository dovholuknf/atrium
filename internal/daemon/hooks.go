package daemon

import (
	"context"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
	"github.com/dovholuknf/atrium/internal/daemon/pulls"

	"github.com/dovholuknf/atrium/internal/store"
)

// hooks are the seams, delays, limits and locations a test changes on ONE daemon. Each daemon owns its copy,
// made by defaultHooks, so a timer or a goroutine an earlier test left running reads that test's values and
// never this test's. Nothing here is a package variable a test assigns: scripts/check-test-globals.go fails
// the build when one does.
type hooks struct {
	exitHooks
	newContextHooks
	keptHooks
	modelHooks
	leanHooks
	preflightHooks
	hostHooks
	finishHooks
	healthHooks

	// Runners.
	helpRunner        func(exe string) (string, error)
	runnerFound       func(h *store.Harness) string
	agentSpawn        func(exe string, args []string) (string, []string)
	agentFork         func(exe string, args []string) (string, []string)
	windDownKeyGap    time.Duration
	idleLeave         func(d *Daemon, r *runner, id string)
	idleGoneWait      time.Duration
	shelveGrace       time.Duration
	restartAskGrace   time.Duration
	opencodeTTL       time.Duration
	opencodeWaitDelay time.Duration
	opencodeExec      func(ctx context.Context, bin string, args []string, max int64, waitDelay time.Duration) ([]byte, error)
	opencodeLookPath  func(string) (string, error)
	updateRegistry    string
	// setPriorityClass raises a process. Windows only, see priority_windows.go.
	setPriorityClass func(h uintptr, class uint32) error
	pidAlive         func(pid int) bool
	procAncestors    func(pid, max int) []cardproc.Proc
	taskExitWait     time.Duration

	// Finish, health, relay, orphans and the rest.
	backoffSteps  []time.Duration
	stuckAfter    time.Duration
	silentNudges  int
	OrphanGrace   time.Duration
	RelayKeep     time.Duration
	listMax       int
	repliesWindow int64
	repliesReach  int64
	repliesCap    int64
	// repliesAllBudget is the bytes `replies?all=1` reads across a card's transcripts.
	repliesAllBudget int64
}

// defaultHooks is what production runs with. The package level values it copies are never assigned.
func defaultHooks() hooks {
	hk := hooks{
		exitHooks:         defaultExitHooks(),
		newContextHooks:   defaultNewContextHooks(),
		keptHooks:         defaultKeptHooks(),
		modelHooks:        defaultModelHooks(),
		leanHooks:         defaultLeanHooks(),
		preflightHooks:    defaultPreflightHooks(),
		hostHooks:         defaultHostHooks(),
		finishHooks:       defaultFinishHooks(),
		healthHooks:       defaultHealthHooks(),
		helpRunner:        helpRunner,
		runnerFound:       runnerFound,
		agentSpawn:        agentSpawn,
		agentFork:         agentFork,
		windDownKeyGap:    windDownKeyGap,
		idleLeave:         idleLeave,
		idleGoneWait:      idleGoneWait,
		shelveGrace:       shelveGrace,
		restartAskGrace:   restartAskGrace,
		opencodeTTL:       opencodeTTL,
		opencodeWaitDelay: opencodeWaitDelay,
		opencodeExec:      opencodeExec,
		opencodeLookPath:  opencodeLookPath,
		updateRegistry:    updateRegistry,
		pidAlive:          pidAlive,
		procAncestors:     cardproc.Ancestors,
		taskExitWait:      taskExitWait,
		backoffSteps:      append([]time.Duration(nil), backoffSteps...),
		stuckAfter:        heldStuckAfter,
		silentNudges:      silentNudges,
		OrphanGrace:       OrphanGrace,
		RelayKeep:         RelayKeep,
		listMax:           pulls.ListMax,
		repliesWindow:     repliesWindow,
		repliesReach:      repliesReach,
		repliesCap:        repliesCap,
		repliesAllBudget:  repliesAllBudget,
	}
	platformHooks(&hk)
	return hk
}
