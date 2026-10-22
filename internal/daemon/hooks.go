package daemon

import (
	"context"
	"io"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// hooks are the seams, delays, limits and locations a test changes on ONE daemon. Each daemon owns its copy,
// made by defaultHooks, so a timer or a goroutine an earlier test left running reads that test's values and
// never this test's. Nothing here is a package variable a test assigns: scripts/check-test-globals.go fails
// the build when one does.
type hooks struct {
	// Exit on report.
	exitOnReportDelay time.Duration
	stopAfterReport   func(d *Daemon, taskID string) error

	// The new-context and restart-wrap sequences.
	ncTiming       ncTimings
	wrapTiming     wrapTimings
	wrapJoinMargin time.Duration
	atriumBinary   func() string

	// Waking a kept card.
	keptTick, keptSettle, keptGiveUp time.Duration
	settleWindow                     time.Duration

	// Model switches.
	modelRetryEvery, modelWaitMax time.Duration

	// Lean cards.
	readUserSettings func() []byte
	userAgentsDir    func() string
	userSkillsDir    func() string
	// leanPluginRoot is where the plugin directories are written: in atrium's own state, set from the database's
	// directory when a daemon starts. Never a worktree, which has to stay clean for the cull.
	leanPluginRoot        string
	leanAgentsHookCommand func(agents []string) string
	noPollHookCommand     func() string
	nowHookCommand        func() string
	stopHookCommand       func() string

	// Preflight.
	preflightLook     func(string) (string, error)
	preflightRun      func(ctx context.Context, exe string, args []string, out io.Writer) error
	preflightEachFor  time.Duration
	preflightTotalFor time.Duration

	// The pty host at startup. See waitForHost.
	hostStartWait, hostStartPoll time.Duration

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

	// Finish, health, relay, orphans and the rest.
	commitExists  func(dir, sha string) bool
	commitInDir   func(dir, sha string) bool
	commitOnHub   func(d *Daemon, task *store.Task, sha string) bool
	healthHalted  func(d *Daemon) (bool, error)
	backoffSteps  []time.Duration
	stuckAfter    time.Duration
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
		exitOnReportDelay:     exitOnReportDelay,
		stopAfterReport:       stopAfterReport,
		ncTiming:              defaultNCTiming(),
		wrapTiming:            wrapTimings{poll: 250 * time.Millisecond},
		wrapJoinMargin:        wrapJoinMargin,
		atriumBinary:          atriumBinary,
		keptTick:              keptTick,
		keptSettle:            keptSettle,
		keptGiveUp:            keptGiveUp,
		settleWindow:          settleWindow,
		modelRetryEvery:       modelRetryEvery,
		modelWaitMax:          modelWaitMax,
		readUserSettings:      readUserSettings,
		userAgentsDir:         userAgentsDir,
		userSkillsDir:         userSkillsDir,
		leanAgentsHookCommand: leanAgentsHookCommand,
		noPollHookCommand:     noPollHookCommand,
		nowHookCommand:        nowHookCommand,
		stopHookCommand:       stopHookCommand,
		preflightLook:         preflightLook,
		preflightRun:          preflightRun,
		preflightEachFor:      preflightEachFor,
		preflightTotalFor:     preflightTotalFor,
		hostStartWait:         hostStartWait,
		hostStartPoll:         hostStartPoll,
		helpRunner:            helpRunner,
		runnerFound:           runnerFound,
		agentSpawn:            agentSpawn,
		agentFork:             agentFork,
		windDownKeyGap:        windDownKeyGap,
		idleLeave:             idleLeave,
		idleGoneWait:          idleGoneWait,
		shelveGrace:           shelveGrace,
		restartAskGrace:       restartAskGrace,
		opencodeTTL:           opencodeTTL,
		opencodeWaitDelay:     opencodeWaitDelay,
		opencodeExec:          opencodeExec,
		opencodeLookPath:      opencodeLookPath,
		updateRegistry:        updateRegistry,
		pidAlive:              pidAlive,
		commitExists:          commitExists,
		commitInDir:           commitInDir,
		commitOnHub:           commitOnHub,
		healthHalted:          healthHalted,
		backoffSteps:          append([]time.Duration(nil), backoffSteps...),
		stuckAfter:            heldStuckAfter,
		OrphanGrace:           OrphanGrace,
		RelayKeep:             RelayKeep,
		listMax:               listMax,
		repliesWindow:         repliesWindow,
		repliesReach:          repliesReach,
		repliesCap:            repliesCap,
		repliesAllBudget:      repliesAllBudget,
	}
	platformHooks(&hk)
	return hk
}
