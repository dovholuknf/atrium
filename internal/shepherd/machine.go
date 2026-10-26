// Package shepherd is `atrium shepherd run <item>`: a program, not a model session, that takes one backlog item from
// launch to a held packet. See docs/rnd/script-minion.md.
//
//   - A PHASE IS A STEP BEHIND AN INTERFACE. The machine knows the order and nothing about what a phase does, so a test
//     drives it with a fake Runner, Hub, Store and Files and no repository.
//   - THE ROW IS WRITTEN BEFORE A PHASE RUNS. A killed shepherd loads it and resumes at the phase it was in, so every
//     phase finds its own work already done and does not do it twice. The one window is a launch that started a card and
//     died before saving it, which the launch step says at length.
//   - A PHASE THAT FAILS LEAVES THE ROW WHERE IT WAS, with the error on it. Running the shepherd again is the retry.
//   - A SHEPHERD STOPS ON A PHASE THAT NEEDS A PERSON: held, held-blocked, exception, failed. None of them lands
//     anything. Accept, land and close belong to rnd-shepherd-accept-land and are not here.
package shepherd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Phase is where a shepherd is. Text on the hub row, so a new one is not a migration.
type Phase string

const (
	PhaseCheck    Phase = "check"
	PhaseWorktree Phase = "worktree"
	PhaseLaunch   Phase = "launch"
	PhaseWait     Phase = "wait"
	PhaseCollect  Phase = "collect"
	PhaseStage    Phase = "stage"
	PhaseHold     Phase = "hold"

	// The phases a shepherd stops in. Held is the goal of this build.
	PhaseHeld        Phase = "held"
	PhaseHeldBlocked Phase = "held-blocked"
	// PhaseException is a conflict, a red test that is not known, or an empty range: the cases a one-shot judge will
	// read (recommendation 12). Until it exists they are a person's.
	PhaseException Phase = "exception"
	PhaseFailed    Phase = "failed"
	// PhaseRefused is an item the shepherd would not start on: unknown, or already done.
	PhaseRefused Phase = "refused"
)

// State is the row. It mirrors the hub's, field for field.
type State struct {
	ItemID    string `json:"item_id"`
	Phase     Phase  `json:"phase"`
	Room      string `json:"room"`
	BaseSHA   string `json:"base_sha"`
	Worktree  string `json:"worktree"`
	Branch    string `json:"branch"`
	Card      string `json:"card,omitempty"`
	WorkItem  string `json:"work_item,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Stage     string `json:"stage,omitempty"`
	Packet    string `json:"packet,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// Store holds the row. The real one is the hub.
type Store interface {
	// Load answers false when the shepherd has no row yet.
	Load(ctx context.Context, item string) (State, bool, error)
	Save(ctx context.Context, st State) error
}

// Result is one finished command.
type Result struct {
	Stdout, Stderr string
	Code           int
}

// Out is both streams, stdout first.
func (r Result) Out() string { return strings.TrimSpace(r.Stdout + r.Stderr) }

// Runner runs a command in a directory. A non-zero exit is a Result, not an error: the error is for a command that
// could not be started at all.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (Result, error)
}

// BacklogItem is what the check phase and the packet need of the hub's backlog.
type BacklogItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

// WorkItem is the hub's item for the worker's assignment (`/_hub/items`).
type WorkItem struct {
	ID    string `json:"item_id"`
	Card  string `json:"card"`
	State string `json:"state"`
	Note  string `json:"note"`
}

// Hub is what the shepherd reads of the hub. Never the MCP, which a lean launch does not have.
type Hub interface {
	Backlog(ctx context.Context, id string) (BacklogItem, error)
	// ItemForCard is the newest item the card ran, false until the room has pushed one.
	ItemForCard(ctx context.Context, card string) (WorkItem, bool, error)
	// CardStatus is the card's own status on this machine's room: running, dead and the rest.
	CardStatus(ctx context.Context, card string) (string, error)
	// Profile is the operator a worker commits as.
	Profile(ctx context.Context) (Profile, error)
}

// Profile is the part of the hub's operator profile a worker's git config and brief need.
type Profile struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Display string `json:"display"`
}

// Files is the few file reads and writes a phase makes, so a test needs no disk.
type Files interface {
	Read(path string) ([]byte, error)
	Write(path string, b []byte) error
}

// Config is where things are. Nothing here is a secret.
type Config struct {
	// Repo is the main checkout. Nothing is written to it: worktrees are added to it and read from it.
	Repo string
	// Worktrees is the directory the worker's worktree and the staging worktree go in.
	Worktrees string
	// Atrium is the binary `launch` and the rest are run through.
	Atrium string
	// Main is the integration branch.
	Main string
	// Poll is the first wait between two reads of the hub, and the longest it backs off to.
	PollStart, PollMax time.Duration
	// Rules is the block appended to the worker's brief.
	Rules string
}

// Env is everything a step may touch.
type Env struct {
	Store  Store
	Runner Runner
	Hub    Hub
	Files  Files
	Cfg    Config
	Out    io.Writer
	// Sleep waits, and gives the context's error back when it ends first. A function so a test never waits.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Outcome is what a step answers: go to Next, or stop where the row says.
type Outcome struct {
	Next Phase
	// Stop is true when the shepherd is done for now. The step has set the phase it stopped in.
	Stop bool
}

// Step is one phase.
type Step interface {
	Phase() Phase
	Run(ctx context.Context, env *Env, st *State) (Outcome, error)
}

// Steps is the fixed sequence.
func Steps() []Step {
	return []Step{checkStep{}, worktreeStep{}, launchStep{}, waitStep{}, collectStep{}, stageStep{}, holdStep{}}
}

// Machine runs the steps in order.
type Machine struct {
	Env   *Env
	steps map[Phase]Step
}

// New is a machine over Steps().
func New(env *Env) *Machine { return NewWith(env, Steps()) }

// NewWith is a machine over the steps it is given, for a test that replaces one.
func NewWith(env *Env, steps []Step) *Machine {
	m := &Machine{Env: env, steps: map[Phase]Step{}}
	for _, s := range steps {
		m.steps[s.Phase()] = s
	}
	return m
}

// resumeAt is the phase a row in `p` goes on from. A stopped shepherd is not rerun, except held-blocked, which is
// waiting on a worker a person may have unblocked since.
func resumeAt(p Phase) Phase {
	if p == PhaseHeldBlocked {
		return PhaseWait
	}
	return p
}

// Stopped says whether a phase is one a shepherd stops in.
func Stopped(p Phase) bool {
	switch p {
	case PhaseHeld, PhaseException, PhaseFailed, PhaseRefused:
		return true
	}
	return false
}

// Run takes the item as far as it goes and returns the row it stopped on. An error leaves the row at the phase that
// failed, with the error on it, and running again resumes there.
func (m *Machine) Run(ctx context.Context, item string) (State, error) {
	e := m.Env
	st, ok, err := e.Store.Load(ctx, item)
	if err != nil {
		return st, fmt.Errorf("read the shepherd row: %w", err)
	}
	if !ok {
		st = State{ItemID: item, Phase: PhaseCheck}
	}
	phase := resumeAt(st.Phase)
	if Stopped(phase) {
		return st, nil
	}
	for {
		step := m.steps[phase]
		if step == nil {
			return st, fmt.Errorf("the row names a phase this build does not know: %q", phase)
		}
		st.Phase, st.LastError = phase, ""
		if err := e.Store.Save(ctx, st); err != nil {
			return st, fmt.Errorf("write the shepherd row before %s: %w", phase, err)
		}
		out, err := step.Run(ctx, e, &st)
		if err != nil {
			st.Phase, st.LastError = phase, err.Error()
			// A cancelled context cannot write, and the row is already at this phase.
			if ctx.Err() == nil {
				_ = e.Store.Save(ctx, st)
			}
			return st, fmt.Errorf("%s: %w", phase, err)
		}
		if err := e.Store.Save(ctx, st); err != nil {
			return st, fmt.Errorf("write the shepherd row after %s: %w", phase, err)
		}
		if out.Stop {
			return st, nil
		}
		phase = out.Next
	}
}

func (e *Env) say(format string, a ...any) {
	if e.Out != nil {
		fmt.Fprintf(e.Out, format+"\n", a...)
	}
}
