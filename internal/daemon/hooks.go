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
	runnerHooks
	idleHooks
	windDownHooks
	opencodeHooks
	procHooks
	finishLimitHooks
	repliesHooks
	updateRegistry string
}

type runnerHooks struct {
	helpRunner  func(exe string) (string, error)
	runnerFound func(h *store.Harness) string
	agentSpawn  func(exe string, args []string) (string, []string)
	agentFork   func(exe string, args []string) (string, []string)
}

type idleHooks struct {
	idleLeave    func(r *runner, id string, keys [][]byte, keyGap time.Duration)
	idleGoneWait time.Duration
}

type windDownHooks struct {
	windDownKeyGap  time.Duration
	shelveGrace     time.Duration
	restartAskGrace time.Duration
	taskExitWait    time.Duration
}

type opencodeHooks struct {
	opencodeTTL       time.Duration
	opencodeWaitDelay time.Duration
	opencodeExec      func(ctx context.Context, bin string, args []string, max int64, waitDelay time.Duration) ([]byte, error)
	opencodeLookPath  func(string) (string, error)
}

type procHooks struct {
	// setPriorityClass raises a process. Windows only, see priority_windows.go.
	setPriorityClass func(h uintptr, class uint32) error
	pidAlive         func(pid int) bool
	procAncestors    func(pid, max int) []cardproc.Proc
}

type finishLimitHooks struct {
	backoffSteps []time.Duration
	stuckAfter   time.Duration
	silentNudges int
	OrphanGrace  time.Duration
	RelayKeep    time.Duration
	listMax      int
}

type repliesHooks struct {
	repliesWindow int64
	repliesReach  int64
	repliesCap    int64
	// repliesAllBudget is the bytes `replies?all=1` reads across a card's transcripts.
	repliesAllBudget int64
}

// defaultHooks is what production runs with. The package level values it copies are never assigned.
func defaultHooks() hooks {
	hk := hooks{
		exitHooks:       defaultExitHooks(),
		newContextHooks: defaultNewContextHooks(),
		keptHooks:       defaultKeptHooks(),
		modelHooks:      defaultModelHooks(),
		leanHooks:       defaultLeanHooks(),
		preflightHooks:  defaultPreflightHooks(),
		hostHooks:       defaultHostHooks(),
		finishHooks:     defaultFinishHooks(),
		healthHooks:     defaultHealthHooks(),
		runnerHooks: runnerHooks{
			helpRunner:  helpRunner,
			runnerFound: runnerFound,
			agentSpawn:  agentSpawn,
			agentFork:   agentFork,
		},
		idleHooks: idleHooks{idleLeave: idleLeave, idleGoneWait: idleGoneWait},
		windDownHooks: windDownHooks{
			windDownKeyGap:  windDownKeyGap,
			shelveGrace:     shelveGrace,
			restartAskGrace: restartAskGrace,
			taskExitWait:    taskExitWait,
		},
		opencodeHooks: opencodeHooks{
			opencodeTTL:       opencodeTTL,
			opencodeWaitDelay: opencodeWaitDelay,
			opencodeExec:      opencodeExec,
			opencodeLookPath:  opencodeLookPath,
		},
		procHooks: procHooks{pidAlive: pidAlive, procAncestors: cardproc.Ancestors},
		finishLimitHooks: finishLimitHooks{
			backoffSteps: append([]time.Duration(nil), backoffSteps...),
			stuckAfter:   heldStuckAfter,
			silentNudges: silentNudges,
			OrphanGrace:  OrphanGrace,
			RelayKeep:    RelayKeep,
			listMax:      pulls.ListMax,
		},
		repliesHooks: repliesHooks{
			repliesWindow:    repliesWindow,
			repliesReach:     repliesReach,
			repliesCap:       repliesCap,
			repliesAllBudget: repliesAllBudget,
		},
		updateRegistry: updateRegistry,
	}
	platformHooks(&hk)
	return hk
}
