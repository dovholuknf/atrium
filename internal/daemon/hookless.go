package daemon

import (
	"context"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/runnerprofile"
	"github.com/dovholuknf/atrium/internal/store"
)

// A runner that posts no prompt, tool or stop events (pi, copilot, qwen, aider, crush) gets its turns from its screen:
// a terminal that keeps drawing is working, and one that has gone quiet is waiting for its next instruction.

var (
	hooklessEvery = envDuration("ATRIUM_HOOKLESS_EVERY", time.Second)
	hooklessQuiet = envDuration("ATRIUM_HOOKLESS_QUIET", 8*time.Second)
	hooklessReady = envDuration("ATRIUM_HOOKLESS_READY", 2*time.Second)
)

// hooklessKeyEcho is how soon after a keystroke output is that key's echo and not work.
const hooklessKeyEcho = time.Second

// hooklessBusyTicks is how many passes in a row must see new output before a waiting card is working again, so one
// redraw (a resize, an attach) does not move it.
const hooklessBusyTicks = 2

// hooklessOpeningTries is how often an opening prompt the screen never answered is typed before atrium gives up.
const hooklessOpeningTries = 3

// hooklessSeen is one card's screen as the last pass saw it.
type hooklessSeen struct {
	out     int64
	busy    int
	settled bool
}

// typesOpeningPrompt says a launch prompt is typed into the terminal, for a pty row with no prompt_args.
func typesOpeningPrompt(h *store.Harness) bool {
	return h != nil && len(h.PromptArgs) == 0 && h.LaunchMode == store.LaunchPTY
}

func (d *Daemon) hooklessLoop(ctx context.Context) {
	tick := time.NewTicker(hooklessEvery)
	defer tick.Stop()
	seen := map[string]*hooklessSeen{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		d.hooklessPass(seen, time.Now())
	}
}

func (d *Daemon) hooklessPass(seen map[string]*hooklessSeen, now time.Time) {
	live := map[string]bool{}
	for _, run := range d.sup.all() {
		live[run.taskID] = true
		d.typeOpeningPrompt(run, now)
		t, err := d.st.Get(run.taskID)
		if err != nil || !d.hookless(t) {
			delete(seen, run.taskID)
			continue
		}
		s := seen[t.ID]
		if s == nil {
			s = &hooklessSeen{}
			seen[t.ID] = s
		}
		d.hooklessTurn(t, run, s, now)
	}
	for id := range seen {
		if !live[id] {
			delete(seen, id)
		}
	}
}

// hookless says the card's runner has no hooks atrium writes and none has been heard from it.
func (d *Daemon) hookless(t *store.Task) bool {
	if t.ToolHookSeenAt != nil || t.StopHookSeenAt != nil || d.act.sessionHeard(t.ID) {
		return false
	}
	h, err := d.st.Harness(t.Runner)
	return err == nil && h != nil && runnerprofile.For(h).Hooks == "" && !isOpenCode(h)
}

func (a *activityTracker) sessionHeard(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[taskID]
}

// typeOpeningPrompt types a launch prompt the runner could not take on its command line, once its screen has drawn
// and gone quiet.
func (d *Daemon) typeOpeningPrompt(run *runner, now time.Time) {
	p := run.openingPrompt.Load()
	if p == nil {
		return
	}
	// A runner still loading (crush) drops keys without drawing them, so a prompt nothing answered is typed again.
	if typed := run.openingTyped.Load(); typed != 0 {
		if run.lastOut.Load() > typed {
			run.openingPrompt.CompareAndSwap(p, nil)
			return
		}
		if now.Sub(time.Unix(0, typed)) < hooklessQuiet {
			return
		}
		if run.openingTries.Load() >= hooklessOpeningTries {
			run.openingPrompt.CompareAndSwap(p, nil)
			log.Printf("[atrium] %s: typed the opening prompt %d times and its screen never answered", run.taskID, hooklessOpeningTries)
			return
		}
	}
	if run.lastOut.Load() == 0 || now.Sub(run.lastOutputAt()) < hooklessReady {
		return
	}
	wrote, err := d.typeLabelledGuardedAs(run, run.taskID, "", *p, nil, "")
	if err != nil {
		log.Printf("[atrium] could not type the opening prompt of %s: %v", run.taskID, err)
		return
	}
	if wrote {
		run.openingTyped.Store(now.UnixNano())
		run.openingTries.Add(1)
		run.owedPrompt.Store(nil)
		log.Printf("[atrium] %s: typed the opening prompt, its runner takes none on its command line", run.taskID)
	}
}

func (d *Daemon) hooklessTurn(t *store.Task, run *runner, s *hooklessSeen, now time.Time) {
	out := run.lastOut.Load()
	run.typeMu.Lock()
	typed := run.lastTyped
	run.typeMu.Unlock()
	fresh := out != 0 && out != s.out && (typed.IsZero() || time.Unix(0, out).Sub(typed) > hooklessKeyEcho)
	s.out = out
	if fresh {
		s.busy++
	} else {
		s.busy = 0
	}
	quiet := now.Sub(run.lastOutputAt()) >= hooklessQuiet && (typed.IsZero() || now.Sub(typed) >= hooklessQuiet)
	switch t.Status {
	case store.StatusRunning:
		s.settled = false
		if quiet && run.openingPrompt.Load() == nil {
			log.Printf("[atrium] %s (%s) has no hooks and its screen is quiet, so its turn is taken as ended", t.WireName, t.ID)
			d.turnEnded(t.ID)
			s.settled = true
		}
	case store.StatusNeedsInput:
		if quiet {
			s.settled = true
			return
		}
		// Settled first, or drawing for longer than a report's trailing output, so that output is not read as new work.
		if s.busy >= hooklessBusyTicks && (s.settled || time.Duration(s.busy)*hooklessEvery >= hooklessQuiet) {
			s.settled = false
			log.Printf("[atrium] %s (%s) has no hooks and its screen is drawing again, so it is working", t.WireName, t.ID)
			d.turnResumed(t.ID)
		}
	default:
		s.settled = false
	}
}
