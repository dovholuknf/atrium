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
//     the card's own handoff file in the cwd (`HandoffName`), and stop.
//  2. Wait for that turn to end.
//  3. Type `/clear` and wait for the new session's SessionStart.
//  4. Type the wake prompt, which reads that file back.
//
// THE FILE NAME IS PER CARD, chosen once when the cycle begins. Two cards can share a
// directory (the main checkout has two), and one fixed name let one card's capture
// overwrite the other's, and one card's write satisfy the other's check. Item 91.
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
// A FAILED STEP STAYS ON THE CARD with its reason until the clear is PROVEN (a
// SessionStart naming a conversation other than the run's), the action is run
// again, or it is dismissed. Never on a turn start: a turn says nothing about
// whether the context went. So a sequence that stopped is never mistaken for one
// that finished. The reason stays in the card's history as an event.

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
func newContextCapture(file string) string {
	return "Your context is about to be cleared. First commit or stash any work in " +
		"progress. Then write everything a fresh session needs to carry on to " + file + " in the current " +
		"directory: what you are doing and why, what is done, what is left, decisions made and the reasons, " +
		"branches, commits and files involved, how to check the work, and anything you were waiting on. " +
		"When it is written, reply with one line saying so and stop. Do not start anything else. You will " +
		"be told to read " + file + " back once the context is clear."
}

// HandoffName is the file a card's new-context cycle writes and reads: its alias
// when it has one, else the first 13 characters of its id. Eight is not enough,
// two cards here share `01a0ede4`. Exported for the idle parking, which wants the
// same file.
func HandoffName(t *store.Task) string {
	name := store.NormalizeAlias(t.Alias)
	if name == "" {
		name = t.ID
		if len(name) > 13 {
			name = name[:13]
		}
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		}
		return '-'
	}, name)
	return "HANDOFF." + name + ".md"
}

// newContextClear clears the session.
const newContextClear = "/clear"

// newContextWake is what the new session is told, and reads the capture back.
func newContextWake(file string) string { return "Read " + file + " and continue from it." }

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
	file   string
	since  time.Time
	reason string
	// conv is the conversation the run started in. A failed chip clears when a
	// SessionStart names a different one, which is the proof the clear happened.
	conv string
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
func (n *newContexts) begin(taskID, file, conv string) (uint64, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if cur := n.by[taskID]; cur != nil && cur.step != NewContextFailed {
		return 0, false
	}
	n.gens++
	n.by[taskID] = &newContext{step: NewContextCapture, file: file, conv: conv, since: time.Now(), gen: n.gens}
	return n.gens, true
}

// conversationOf is the conversation a card's session is in now: the id its last
// SessionStart reported, else the resume id stored on the card.
func (d *Daemon) conversationOf(t *store.Task) string {
	if d.wake != nil {
		if c := d.wake.conversation(t.ID); c != "" {
			return c
		}
	}
	return t.ResumeID
}

// sessionStarted clears a FAILED chip when a SessionStart names a conversation
// other than the one the run began in: a new conversation really began, so the
// clear did happen. It reports whether it cleared one.
//
// NEVER ON A TURN START. A peer message can start a turn in a card whose clear
// failed, and in one whose context had cleared but whose wake was not typed. Both
// turns say nothing about whether the context went, so the chip stays until this,
// a rerun, or a dismissal. A run with no starting conversation on record cannot
// be proven and stays as well.
func (n *newContexts) sessionStarted(taskID, conv string) bool {
	if n == nil || conv == "" {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	if cur == nil || cur.step != NewContextFailed || cur.conv == "" || cur.conv == conv {
		return false
	}
	delete(n.by, taskID)
	return true
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
		n, label = 1, "capturing state to "+c.file
	case NewContextClear:
		n, label = 2, "clearing the context"
	case NewContextWake:
		n, label = 3, "waking it to read "+c.file
	case NewContextFailed:
		label = "new context failed"
	}
	out := map[string]any{"step": c.step, "n": n, "of": 3, "label": label, "file": c.file, "since": c.since}
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
	// Two cards in one directory are fine, each with its own file. Two cycles at once
	// in one directory are refused: the typing of both would interleave.
	if other := d.nctx.sameDirBusy(d, task); other != "" {
		return &newContextSharedError{other: other}
	}
	gen, ok := d.nctx.begin(taskID, HandoffName(task), d.conversationOf(task))
	if !ok {
		return errNewContextBusy
	}
	log.Printf("[atrium] new context started on %s", task.DisplayTitle())
	d.publishTask(taskID)
	go d.runNewContext(taskID, gen)
	return nil
}

// newContextSharedError refuses a cycle while another card in the same directory
// is itself mid-cycle.
type newContextSharedError struct{ other string }

func (e *newContextSharedError) Error() string {
	return "another card in this directory, " + e.other + ", is mid new-context, so wait for it to finish"
}

func isSharedErr(err error) bool {
	var e *newContextSharedError
	return errors.As(err, &e)
}

// sameDirBusy names a card, other than task, that shares its directory and has a
// cycle under way. A failed chip is not under way.
func (n *newContexts) sameDirBusy(d *Daemon, task *store.Task) string {
	dir := filepath.ToSlash(strings.TrimSpace(task.Worktree))
	if dir == "" {
		return ""
	}
	all, err := d.st.List()
	if err != nil {
		return ""
	}
	for _, o := range all {
		if o.ID == task.ID || filepath.ToSlash(strings.TrimSpace(o.Worktree)) != dir {
			continue
		}
		if cur := n.get(o.ID); cur != nil && cur.step != NewContextFailed {
			return o.DisplayTitle()
		}
	}
	return ""
}

var (
	errNoTerminal    = errors.New("atrium does not own this session's terminal, so it cannot type into it")
	errNewContextBusy = errors.New("a new context is already under way on this card")
)

// runNewContext is the sequence. Each step ends only when the one before it has
// really finished, and a step that cannot leaves the chip failed with a reason
// and types nothing further.
func (d *Daemon) runNewContext(taskID string, gen uint64) {
	// Recorded at begin, so an alias change mid-cycle cannot split the three uses.
	file := ""
	if cur := d.nctx.get(taskID); cur != nil {
		file = cur.file
	}
	fail := func(step string, err error) {
		if errors.Is(err, errNewContextGone) {
			return
		}
		reason := step + ": " + err.Error()
		if d.nctx.fail(taskID, gen, reason) {
			log.Printf("[atrium] new context on %s stopped, %s", taskID, reason)
			// The chip goes when the clear is proven, and the reason stays in the card's
			// history after it.
			if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
				"by": newContextBy, "failed": reason,
			}); err != nil {
				log.Printf("[atrium] could not record the new context failure on %s: %v", taskID, err)
			}
			d.publishTask(taskID)
			d.releaseHeld(taskID)
		}
	}

	// 1 and 2. The capture prompt, the turn it starts, and the file it wrote. Not
	// cleared over a handoff that was never written: the clear cannot be taken
	// back, and the capture is the only thing that makes it safe.
	if step, err := d.ncCapture(taskID, gen, file); err != nil {
		fail(step, err)
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
	err := d.ncWait(taskID, gen, ncTiming.clearWait, "a new session to start after /clear (is the session hook installed?)",
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
	// Only this card's own file will do. A plain HANDOFF.md written meanwhile is
	// some other card's, which is the bug the per-card name exists to prevent.
	if err := d.handoffExists(taskID, file); err != nil {
		fail("the wake was not typed", err)
		return
	}
	// A turn in progress is waited out like the capture's, not for typeWait: the new
	// session may already be taking a turn on something else (r-016, @ui), and the
	// cycle fails only if no gap opens in captureEnd.
	if err := d.ncType(taskID, gen, newContextLabel, newContextWake(file), ncTiming.captureEnd); err != nil {
		fail("could not type the wake prompt", err)
		return
	}
	if d.nctx.finish(taskID, gen) {
		log.Printf("[atrium] new context on %s done", taskID)
		d.publishTask(taskID)
		d.releaseHeld(taskID)
	}
}

// ncCapture is the capture half of the sequence: the prompt typed between turns,
// the turn it starts waited out, and the card's own file checked. It returns the
// step that stopped it and why. The idle parking runs this alone, with no clear
// after it, so a card is asked to write its handoff without losing its context.
func (d *Daemon) ncCapture(taskID string, gen uint64, file string) (string, error) {
	started := time.Now()
	turns := d.act.turnsBegun(taskID)
	if err := d.ncType(taskID, gen, newContextLabel, newContextCapture(file), ncTiming.captureEnd); err != nil {
		return "could not type the capture prompt", err
	}
	// Wait for the turn it starts to end.
	err := d.ncWait(taskID, gen, ncTiming.captureBegin, "the capture prompt to start a turn",
		func() (bool, error) { return d.act.turnsBegun(taskID) > turns, nil })
	if err != nil {
		return "the capture prompt did nothing", err
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
		return "the capture did not finish", err
	}
	if err := d.handoffWritten(taskID, file, started); err != nil {
		return "nothing cleared", err
	}
	return "", nil
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
//
// THE QUIET CHECK AND THE WRITE ARE ONE STEP. The check (no prompt event and no
// turn begun for `turnSettle`, no turn in progress, no dialog, and the run still
// the card's) runs under the input lock `injectPeer` takes, so nothing typed by
// the injector or the operator can land between "quiet" and the write. The
// cheap look before it only spares taking the lock every poll.
//
// THE HOLD IS NOT ADDED HERE. r-007 stage 1b already holds the card's peer
// injector and every delivery path (`holdingMessages`) from the capture to the
// wake, so nothing else types into the card during a run. What that could not
// give was atomicity: a message already past its own check when the run began.
// The lock gives it.
func (d *Daemon) ncType(taskID string, gen uint64, label, text string, limit time.Duration) error {
	return d.ncWait(taskID, gen, limit, "an empty line and no turn in progress", func() (bool, error) {
		run := d.sup.get(taskID)
		if run == nil || d.act.dialogOpen(taskID) || d.act.midTurn(taskID) {
			return false, nil
		}
		gone := false
		quiet := func() bool {
			// Checked at the last moment: a dismiss between the poll and the
			// write must not be typed after.
			if !d.nctx.mine(taskID, gen) {
				gone = true
				return false
			}
			return !d.act.dialogOpen(taskID) && !d.act.midTurn(taskID) &&
				d.act.sinceBusy(taskID) >= ncTiming.turnSettle
		}
		wrote, err := d.typeLabelledGuarded(run, taskID, label, text, quiet)
		if gone {
			return false, errNewContextGone
		}
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

// handoffDir is the card's directory, or "" when it cannot be read from here,
// which is a card whose files this daemon has no way to look at. The sequence
// then trusts the turn ending as its only signal.
func (d *Daemon) handoffDir(taskID string) (string, error) {
	task, err := d.st.Get(taskID)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(task.Worktree)
	if dir == "" {
		return "", nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", nil
	}
	return dir, nil
}

// handoffExists checks the card's own file is there for the wake to read. A plain
// HANDOFF.md is never accepted in its place: it is some other card's.
func (d *Daemon) handoffExists(taskID, file string) error {
	dir, err := d.handoffDir(taskID)
	if err != nil || dir == "" {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
		return fmt.Errorf("expected %s in %s and it is not there", file, dir)
	}
	return nil
}

// handoffWritten checks the capture left this card's file, written since it began.
func (d *Daemon) handoffWritten(taskID, file string, since time.Time) error {
	dir, err := d.handoffDir(taskID)
	if err != nil || dir == "" {
		return err
	}
	info, err := os.Stat(filepath.Join(dir, file))
	if err != nil {
		return fmt.Errorf("the capture turn ended without %s in %s", file, dir)
	}
	// A little slack for file systems that round a modified time down.
	if info.ModTime().Before(since.Add(-2 * time.Second)) {
		return fmt.Errorf("%s in %s was not updated by the capture turn, so it is from an "+
			"earlier session", file, dir)
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
		case errors.Is(err, errNoTerminal), errors.Is(err, errNewContextBusy), isSharedErr(err):
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
