package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// New context: one action that cycles a card's context. Capture what the
// session knows, clear it, and wake it to read that back.
//
// THE DAEMON RUNS THE SEQUENCE, NOT THE AGENT. A message the agent queues for
// itself can land before the clear and be wiped with it, and a `/clear` typed
// mid-turn can cut the capture short. So the room holds each step until the one
// before it is over, and types nothing further when a step fails.
//
//  1. Type the capture prompt: commit or stash, write everything relevant to
//     HANDOFF.md in the cwd, and stop.
//  2. Wait for that turn to end.
//  3. Type `/clear` and wait for the new session's SessionStart.
//  4. Type the wake prompt, which reads HANDOFF.md back.
//
// EVERY WRITE GOES THROUGH THE SAME GATE as a message, a note and an action
// (`typeLabelledThroughGate`): an empty line, a quiet keyboard, no dialog on
// screen. A closed gate is waited out, not skipped.
//
// ONLY WHERE ATRIUM OWNS THE TERMINAL. There is nothing to type into otherwise.
//
// IN MEMORY. Like the activity it watches, a step describes a process that is
// running now, and a restart ends the terminal it was typing into. A failed
// chip does not survive one either: it described a sequence nobody is running.
//
// A FAILED STEP STAYS ON THE CARD with its reason until it is dismissed or the
// action is run again, so a sequence that stopped is never mistaken for one that
// finished.

// The steps a chip can be on.
const (
	NewContextCapture = "capture"
	NewContextClear   = "clear"
	NewContextWake    = "wake"
	NewContextFailed  = "failed"
)

// newContextBy is the `from` on the prompted events the sequence writes.
const newContextBy = "new-context"

// newContextLabel goes ahead of the capture and wake prompts, so nobody reads
// them as the operator's own words. `/clear` goes with none, because a label in
// front of it is no longer a slash command. See atriumLabel.
var newContextLabel = atriumLabel("new context:")

// newContextCapture is what the session is asked to do before its context goes.
//
// ONE LINE. It is typed and submitted like any prompt, and the model gets the
// whole of it. It says what happens next, so a session that would otherwise
// carry on working after writing the file knows to stop.
const newContextCapture = "Your context is about to be cleared. First commit or stash any work in " +
	"progress. Then write everything a fresh session needs to carry on to HANDOFF.md in the current " +
	"directory: what you are doing and why, what is done, what is left, decisions made and the reasons, " +
	"branches, commits and files involved, how to check the work, and anything you were waiting on. " +
	"When it is written, reply with one line saying so and stop. Do not start anything else. You will " +
	"be told to read HANDOFF.md back once the context is clear."

// newContextClear clears the session.
const newContextClear = "/clear"

// newContextWake is what the new session is told, and reads the capture back.
const newContextWake = "Read HANDOFF.md and continue from it."

// ncTiming is how long each step waits, and how often it looks. Variables so a
// test can run the whole sequence in milliseconds rather than name the thing that
// must not happen by waiting for it.
var ncTiming = struct {
	// poll is how often a wait looks at the card.
	poll time.Duration
	// typeWait is how long a step waits for the gate to open and the runner to be
	// between turns before it gives up typing. The capture prompt waits for
	// captureEnd instead: the operator may press the button in the middle of a
	// long turn, and the capture then belongs after it.
	typeWait time.Duration
	// captureBegin is how long the capture prompt has to start a turn.
	captureBegin time.Duration
	// captureEnd is how long the capture turn has to finish.
	captureEnd time.Duration
	// turnSettle is how long the runner has to stay out of a turn for it to count
	// as over. A gap between two tool calls is shorter.
	turnSettle time.Duration
	// clearWait is how long `/clear` has to produce a new session.
	clearWait time.Duration
	// sessionSettle is how long after the new session's SessionStart the wake
	// waits. The hook fires a moment before the input box is drawn, and bytes
	// typed before then are lost. See wakeSettle.
	sessionSettle time.Duration
}{
	poll:          250 * time.Millisecond,
	typeWait:      2 * time.Minute,
	captureBegin:  time.Minute,
	captureEnd:    15 * time.Minute,
	turnSettle:    2 * time.Second,
	clearWait:     time.Minute,
	sessionSettle: wakeSettle,
}

// newContext is one card's sequence as the board draws it.
type newContext struct {
	step   string
	since  time.Time
	reason string
	// gen tells a run whether it is still the card's. A dismiss or a fresh run
	// bumps it, and the goroutine it replaced stops without saying anything.
	gen uint64
}

type newContexts struct {
	mu   sync.Mutex
	by   map[string]*newContext
	gens uint64
	// stop ends every run in flight, at shutdown.
	stop     chan struct{}
	stopOnce sync.Once
}

func newNewContexts() *newContexts {
	return &newContexts{by: map[string]*newContext{}, stop: make(chan struct{})}
}

func (n *newContexts) stopAll() { n.stopOnce.Do(func() { close(n.stop) }) }

// begin claims a card for a run and returns its generation, or false when one is
// already going. A failed chip is not going: running the action again replaces it.
func (n *newContexts) begin(taskID string) (uint64, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if cur := n.by[taskID]; cur != nil && cur.step != NewContextFailed {
		return 0, false
	}
	n.gens++
	n.by[taskID] = &newContext{step: NewContextCapture, since: time.Now(), gen: n.gens}
	return n.gens, true
}

// mine reports whether gen is still the card's run.
func (n *newContexts) mine(taskID string, gen uint64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	return cur != nil && cur.gen == gen
}

// advance moves a run to its next step.
func (n *newContexts) advance(taskID string, gen uint64, step string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	if cur == nil || cur.gen != gen {
		return false
	}
	cur.step, cur.since = step, time.Now()
	return true
}

// fail leaves the chip on the step that stopped, saying why.
func (n *newContexts) fail(taskID string, gen uint64, reason string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	if cur == nil || cur.gen != gen {
		return false
	}
	cur.step, cur.reason, cur.since = NewContextFailed, reason, time.Now()
	return true
}

// finish takes the chip off: the wake prompt landed.
func (n *newContexts) finish(taskID string, gen uint64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if cur := n.by[taskID]; cur == nil || cur.gen != gen {
		return false
	}
	delete(n.by, taskID)
	return true
}

// clear removes a card's chip whatever it says and stops its run.
func (n *newContexts) clear(taskID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, had := n.by[taskID]
	delete(n.by, taskID)
	return had
}

// holding reports whether a card is inside a new-context cycle that has not
// ended: from `begin` until the wake prompt has been typed. A failed chip is not
// holding, since nothing is running any more and a held message would be
// stranded.
func (n *newContexts) holding(taskID string) bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	return cur != nil && cur.step != NewContextFailed
}

func (n *newContexts) get(taskID string) *newContext {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	if cur == nil {
		return nil
	}
	cp := *cur
	return &cp
}

// newContextFor is the card's sequence as the board draws it, or nil.
func (d *Daemon) newContextFor(taskID string) any {
	cur := d.nctx.get(taskID)
	if cur == nil {
		return nil
	}
	return newContextView(cur)
}

// newContextView carries the wording, so the board says what the daemon means
// and there is one place to change it.
func newContextView(c *newContext) map[string]any {
	n, label := 0, ""
	switch c.step {
	case NewContextCapture:
		n, label = 1, "capturing state to HANDOFF.md"
	case NewContextClear:
		n, label = 2, "clearing the context"
	case NewContextWake:
		n, label = 3, "waking it to read HANDOFF.md"
	case NewContextFailed:
		label = "new context failed"
	}
	out := map[string]any{"step": c.step, "n": n, "of": 3, "label": label, "since": c.since}
	if c.reason != "" {
		out["reason"] = c.reason
	}
	return out
}

var errNewContextGone = errors.New("superseded")

// holdingMessages is the question every delivery path asks for a card: is it in
// a new-context cycle. While it is, no message is typed and no hook carries one,
// because capture would put it in the context about to be cleared and clear
// would lose it. The cycle's own typing (`ncType`) does not ask.
func (d *Daemon) holdingMessages(taskID string) bool { return d.nctx.holding(taskID) }

// newContextHoldNote is what a sender is told while a card is held.
const newContextHoldNote = "queued: this card is starting a new context, and everything for it is held " +
	"until its wake prompt has been typed."

// releaseHeld is called when a cycle ends any way at all: the wake typed, a
// failed step, a dismissal. What was held is then delivered by the ordinary
// paths, so the typist is kicked to try at once rather than at the end of a
// backoff. It never types ahead of the wake prompt, which has been typed by now.
func (d *Daemon) releaseHeld(taskID string) {
	if d.pending != nil {
		d.pending.reset(taskID)
	}
	d.publishTask(taskID)
}

// StartNewContext begins the sequence on a card and returns at once. The steps
// run in the background and the card's chip says where they are.
func (d *Daemon) StartNewContext(taskID string) error {
	task, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if d.sup.get(taskID) == nil {
		return errNoTerminal
	}
	gen, ok := d.nctx.begin(taskID)
	if !ok {
		return errNewContextBusy
	}
	log.Printf("[atrium] new context started on %s", task.DisplayTitle())
	d.publishTask(taskID)
	go d.runNewContext(taskID, gen)
	return nil
}

var (
	errNoTerminal     = errors.New("atrium does not own this session's terminal, so it cannot type into it")
	errNewContextBusy = errors.New("a new context is already under way on this card")
)

// runNewContext is the sequence. Each step ends only when the one before it has
// really finished, and a step that cannot leaves the chip failed with a reason
// and types nothing further.
func (d *Daemon) runNewContext(taskID string, gen uint64) {
	fail := func(step string, err error) {
		if errors.Is(err, errNewContextGone) {
			return
		}
		reason := step + ": " + err.Error()
		if d.nctx.fail(taskID, gen, reason) {
			log.Printf("[atrium] new context on %s stopped, %s", taskID, reason)
			d.publishTask(taskID)
			d.releaseHeld(taskID)
		}
	}
	started := time.Now()

	// 1. The capture prompt, typed once the runner is between turns.
	turns := d.act.turnsBegun(taskID)
	if err := d.ncType(taskID, gen, newContextLabel, newContextCapture, ncTiming.captureEnd); err != nil {
		fail("could not type the capture prompt", err)
		return
	}
	// 2. Wait for the turn it starts to end.
	err := d.ncWait(taskID, gen, ncTiming.captureBegin, "the capture prompt to start a turn",
		func() (bool, error) { return d.act.turnsBegun(taskID) > turns, nil })
	if err != nil {
		fail("the capture prompt did nothing", err)
		return
	}
	quiet := time.Time{}
	err = d.ncWait(taskID, gen, ncTiming.captureEnd, "the capture turn to end", func() (bool, error) {
		if d.act.midTurn(taskID) || d.act.onSubagents(taskID) {
			quiet = time.Time{}
			return false, nil
		}
		if quiet.IsZero() {
			quiet = time.Now()
		}
		return time.Since(quiet) >= ncTiming.turnSettle, nil
	})
	if err != nil {
		fail("the capture did not finish", err)
		return
	}
	// Not cleared over a handoff that was never written. The clear cannot be
	// taken back, and the capture is the only thing that makes it safe.
	if err := d.handoffWritten(taskID, started); err != nil {
		fail("nothing cleared", err)
		return
	}

	// 3. `/clear`, then the new session's SessionStart.
	if !d.nctx.advance(taskID, gen, NewContextClear) {
		return
	}
	d.publishTask(taskID)
	before, _ := d.wake.sessionStarted(taskID)
	typedAt := time.Now()
	if err := d.ncType(taskID, gen, "", newContextClear, ncTiming.typeWait); err != nil {
		fail("could not type /clear", err)
		return
	}
	err = d.ncWait(taskID, gen, ncTiming.clearWait, "a new session to start after /clear (is the session hook installed?)",
		func() (bool, error) {
			at, ok := d.wake.sessionStarted(taskID)
			return ok && at.After(before) && !at.Before(typedAt), nil
		})
	if err != nil {
		fail("the context did not clear", err)
		return
	}

	// 4. The wake, once the new session has drawn its input box.
	if !d.nctx.advance(taskID, gen, NewContextWake) {
		return
	}
	d.publishTask(taskID)
	err = d.ncWait(taskID, gen, ncTiming.sessionSettle+ncTiming.clearWait, "the new session to settle", func() (bool, error) {
		at, _ := d.wake.sessionStarted(taskID)
		return time.Since(at) >= ncTiming.sessionSettle, nil
	})
	if err != nil {
		fail("the new session did not settle", err)
		return
	}
	if err := d.ncType(taskID, gen, newContextLabel, newContextWake, ncTiming.typeWait); err != nil {
		fail("could not type the wake prompt", err)
		return
	}
	if d.nctx.finish(taskID, gen) {
		log.Printf("[atrium] new context on %s done", taskID)
		d.publishTask(taskID)
		d.releaseHeld(taskID)
	}
}

// ncWait polls cond until it is true, the run is replaced, the terminal goes, or
// the time is up.
func (d *Daemon) ncWait(taskID string, gen uint64, limit time.Duration, what string, cond func() (bool, error)) error {
	deadline := time.Now().Add(limit)
	t := time.NewTicker(ncTiming.poll)
	defer t.Stop()
	for {
		if !d.nctx.mine(taskID, gen) {
			return errNewContextGone
		}
		if d.sup.get(taskID) == nil {
			return errors.New("the terminal closed")
		}
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gave up after %s waiting for %s", limit.Round(time.Second), what)
		}
		select {
		case <-d.nctx.stop:
			return errNewContextGone
		case <-t.C:
		}
	}
}

// ncType types one step into the card's terminal, through the gate every
// automated write goes through, and waits for the gate rather than skipping.
//
// Only between turns: a prompt typed mid-turn is held by the runner and merged
// with whatever else lands, which is the race this sequence exists to avoid. A
// dialog on screen stops it as well, since the Enter would answer it.
func (d *Daemon) ncType(taskID string, gen uint64, label, text string, limit time.Duration) error {
	return d.ncWait(taskID, gen, limit, "an empty line and no turn in progress", func() (bool, error) {
		run := d.sup.get(taskID)
		if run == nil || d.act.dialogOpen(taskID) || d.act.midTurn(taskID) {
			return false, nil
		}
		// Checked again at the last moment: a dismiss between the poll and the
		// write must not be typed after.
		if !d.nctx.mine(taskID, gen) {
			return false, errNewContextGone
		}
		wrote, err := d.typeLabelledThroughGate(run, taskID, label, text)
		if err != nil || !wrote {
			return false, err
		}
		if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
			"text": text, "via": "terminal", "from": newContextBy,
		}); err != nil {
			log.Printf("[atrium] could not record the new context prompt on %s: %v", taskID, err)
		}
		return true, nil
	})
}

// handoffWritten checks the capture left a HANDOFF.md, written since it began.
//
// Skipped when the card's directory cannot be read from here, which is a card
// whose files this daemon has no way to look at, and the sequence then trusts
// the turn ending as its only signal.
func (d *Daemon) handoffWritten(taskID string, since time.Time) error {
	task, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(task.Worktree)
	if dir == "" {
		return nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	path := filepath.Join(dir, "HANDOFF.md")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the capture turn ended without a HANDOFF.md in %s", dir)
	}
	// A little slack for file systems that round a modified time down.
	if info.ModTime().Before(since.Add(-2 * time.Second)) {
		return fmt.Errorf("HANDOFF.md in %s was not updated by the capture turn, so it is from an "+
			"earlier session", dir)
	}
	return nil
}

// handleNewContext is `/v1/tasks/{id}/new-context`: POST starts the sequence,
// DELETE takes the chip off and stops a run, and GET reads it.
func (d *Daemon) handleNewContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := d.st.Get(id); err != nil {
		writeJSONErr(w, http.StatusNotFound, errString("no card "+id))
		return
	}
	switch r.Method {
	case http.MethodGet:
		ncJSON(w, http.StatusOK, map[string]any{"card": id, "new_context": d.newContextFor(id)})
	case http.MethodDelete:
		gone := d.nctx.clear(id)
		d.publishTask(id)
		d.releaseHeld(id)
		ncJSON(w, http.StatusOK, map[string]any{"card": id, "cleared": gone})
	case http.MethodPost:
		switch err := d.StartNewContext(id); {
		case errors.Is(err, errNoTerminal), errors.Is(err, errNewContextBusy):
			writeJSONErr(w, http.StatusConflict, err)
		case err != nil:
			writeJSONErr(w, http.StatusInternalServerError, err)
		default:
			ncJSON(w, http.StatusAccepted, map[string]any{
				"card": id, "started": true, "new_context": d.newContextFor(id),
			})
		}
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, errString("GET, POST or DELETE"))
	}
}

func ncJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
