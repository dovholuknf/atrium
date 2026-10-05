package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// New context: one action that cycles a card's context. The same sequence runs
// when someone presses New context and when the card passes its limit
// (autocontext.go). See docs/context-cycle-design.md.
//
// THE DAEMON RUNS THE SEQUENCE, NOT THE AGENT. A message the agent queues for
// itself can land before the clear and be wiped with it, so the room holds each
// step until the one before it is over, and types nothing further when a step fails.
//
//  1. limit: type the limit prompt, which names the handoff file and `atrium ready`.
//     Typed as soon as the input line is free, mid-turn included, and again at most
//     once per turn until the ack comes. Never cleared without it.
//  2. clear: after `atrium ready` (ready.go), type `/clear` between turns and wait
//     for the new session's SessionStart.
//  3. wake: type `read <path> and continue.`
//
// THE HANDOFF FILE IS OUTSIDE THE CARD'S DIRECTORY, in `<dir>/<card-id>.md` (dir is
// the room's context_handoff_dir, else the system temp dir). It is fixed when the
// cycle is claimed. `atrium ready` copies it onto the card.
//
// The idle parking and the room move keep a capture of their own (ncCapture), which
// writes `HandoffName` in the cwd and clears nothing. It is not a context cycle.
//
// EVERY WRITE GOES THROUGH THE SAME GATE as a message, a note and an action
// (`typeLabelledGuarded`): an empty line, a quiet keyboard, no dialog on screen.
//
// ONLY WHERE ATRIUM OWNS THE TERMINAL. There is nothing to type into otherwise.
//
// THE STEP IS IN MEMORY, THE FACT OF A RUN IS NOT. A restart ends the terminal the
// run was typing into, so the runs in flight are journalled (newcontext_journal.go)
// and the next start ends each one past the ack with a failed chip saying where it
// was cut off.
//
// A FAILED STEP STAYS ON THE CARD with its reason until the clear is PROVEN (a
// SessionStart naming a conversation other than the run's), the action is run
// again, or it is dismissed. Never on a turn start.

// The steps a chip can be on.
const (
	// NewContextLimit is the cycle's first step: the limit prompt typed, the ack awaited.
	NewContextLimit = "limit"
	// NewContextCapture is the idle parking's and the move's capture, which clears nothing.
	NewContextCapture = "capture"
	NewContextClear   = "clear"
	NewContextWake    = "wake"
	NewContextFailed  = "failed"
)

// newContextBy is the `from` on the prompted events the sequence writes.
const newContextBy = "new-context"

// newContextLabel goes ahead of the limit, capture and wake prompts, so nobody reads
// them as the operator's own words. `/clear` goes with none, because a label in
// front of it is no longer a slash command. See atriumLabel.
var newContextLabel = atriumLabel("new context:")

// newContextCapture is what the session is asked to do before its context goes.
//
// The token is matched as a SUBSTRING of the file's first 4 KB, not as a whole line:
// a model writing markdown puts it in backticks, a bullet or bold, and refusing a
// fresh capture for that is the failure this exists to fix. The token is random per
// run, so a substring is as unforgeable as an exact line.
//
// ONE LINE. It is typed and submitted like any prompt, and the model gets the
// whole of it. It says what happens next, so a session that would otherwise
// carry on working after writing the file knows to stop.
func newContextCapture(file, token string) string {
	return "Your context is about to be cleared. First commit or stash any work in " +
		"progress. Then write everything a fresh session needs to carry on to " + file + " in the current " +
		"directory: what you are doing and why, what is done, what is left, decisions made and the reasons, " +
		"branches, commits and files involved, how to check the work, and anything you were waiting on. " +
		"Put the line " + captureLine(token) + " near the top of the file, also when it is already written. " +
		"When it is written, reply with one line saying so and stop. Do not start anything else. You will " +
		"be told to read " + file + " back once the context is clear."
}

// captureLine is the line a card puts in its handoff file to say it is this run's.
func captureLine(token string) string { return captureMarker + token }

// captureMarker leads the line. handoffWritten looks for the whole line, token and all.
const captureMarker = "atrium-capture: "

// handoffFloor is the fewest bytes a handoff may be. A token alone is not one.
const handoffFloor = 200

// handoffHead is how much of the file is read looking for the token. Never all of it.
const handoffHead = 4096

// captureToken is unique to one capture run: the run's generation and a random id.
func captureToken(gen uint64) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%d-%x", gen, b)
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

// newContextLimitPrompt is the limit prompt: where the handoff goes and how to say
// it is written. The binary is named in full, since the CLI is not on PATH in every room.
func newContextLimitPrompt(path, bin string) string {
	return "you are at context limit. wrap what is in flight, write your handoff to " + path +
		", then run " + bin + " ready."
}

// newContextWake is what the new session is told, and reads the handoff back.
func newContextWake(path string) string { return "read " + path + " and continue." }

// atriumBinary is the full path of this atrium, for the limit prompt. A variable for a test.
var atriumBinary = func() string {
	if p, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return "atrium"
}

// handoffPath is where a card's cycle writes its handoff: the room's
// context_handoff_dir, else <temp>/atrium/handoffs, then <card-id>.md.
func (d *Daemon) handoffPath(t *store.Task) string {
	dir := d.st.ContextHandoffDir()
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "atrium", "handoffs")
	}
	return filepath.Join(dir, t.ID+".md")
}

// prepareHandoff is handoffPath with its directory made, so the agent can write there.
func (d *Daemon) prepareHandoff(t *store.Task) (string, error) {
	path := d.handoffPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("could not make the handoff directory %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

// ncTiming is how long each step waits, and how often it looks. Variables so a
// test can run the whole sequence in milliseconds rather than name the thing that
// must not happen by waiting for it.
var ncTiming = struct {
	// poll is how often a wait looks at the card.
	poll time.Duration
	// typeWait is how long a step waits for the gate to open and the runner to be
	// between turns before it gives up typing. `/clear`, the wake and the capture
	// wait for captureEnd instead: the turn they follow may be long.
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
	// promptGap is the least time between two limit prompts, for a runner whose
	// turns are not counted.
	promptGap time.Duration
	// promptLost is how long a limit prompt typed between turns may go without
	// starting the turn it was typed for before it is taken as lost and typed again.
	promptLost time.Duration
}{
	poll:          250 * time.Millisecond,
	typeWait:      2 * time.Minute,
	captureBegin:  time.Minute,
	captureEnd:    15 * time.Minute,
	turnSettle:    2 * time.Second,
	clearWait:     time.Minute,
	sessionSettle: wakeSettle,
	promptGap:     30 * time.Second,
	promptLost:    2 * time.Minute,
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
	// auto marks a run the daemon started at the card's limit, and tokens and limit
	// are what it started on. See autocontext.go.
	auto          bool
	tokens, limit int64
	// acked is set when `atrium ready` came, and cleared when `/clear` was typed. A
	// rerun of a failed chip resumes after what they say happened.
	acked, cleared bool
	// prompted is how many times the limit prompt was typed, the last at lastPrompt,
	// in the turn promptTurn (see cyclePromptDue).
	prompted   int
	promptTurn int
	lastPrompt time.Time
	// kick wakes the run's wait at once: an ack, a statusline update, a tick.
	kick chan struct{}
	// capOnly marks the idle parking's capture, which clears nothing and is not
	// journalled: a restart that cuts it off has nothing to report.
	capOnly bool
	// abandoned marks a failed chip seeded at startup from the journal. A
	// SessionStart cannot clear it: see sessionStarted.
	abandoned bool
}

type newContexts struct {
	mu   sync.Mutex
	by   map[string]*newContext
	gens uint64
	// stop ends every run in flight, at shutdown.
	stop     chan struct{}
	stopOnce sync.Once
	// persist writes the runs in flight somewhere a restart can find them. See
	// newcontext_journal.go. Nil in a test that does not care.
	persist func(map[string]ncJournalRow)
	// saveMu keeps two snapshots from reaching persist out of order.
	saveMu sync.Mutex
}

func newNewContexts() *newContexts {
	return &newContexts{by: map[string]*newContext{}, stop: make(chan struct{})}
}

func (n *newContexts) stopAll() { n.stopOnce.Do(func() { close(n.stop) }) }

func (n *newContexts) claim(taskID string, c *newContext) (uint64, bool) {
	n.mu.Lock()
	if cur := n.by[taskID]; cur != nil && cur.step != NewContextFailed {
		n.mu.Unlock()
		return 0, false
	}
	n.gens++
	c.since, c.gen, c.kick = time.Now(), n.gens, make(chan struct{}, 1)
	n.by[taskID] = c
	gen := n.gens
	n.mu.Unlock()
	n.save()
	return gen, true
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
	if cur == nil || cur.step != NewContextFailed || cur.abandoned || cur.conv == "" || cur.conv == conv {
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
	cur := n.by[taskID]
	if cur == nil || cur.gen != gen {
		n.mu.Unlock()
		return false
	}
	cur.step, cur.since = step, time.Now()
	n.mu.Unlock()
	n.save()
	return true
}

// kick wakes the card's run, if it is waiting, without blocking.
func (n *newContexts) kick(taskID string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	cur := n.by[taskID]
	n.mu.Unlock()
	if cur == nil || cur.kick == nil {
		return
	}
	select {
	case cur.kick <- struct{}{}:
	default:
	}
}

// ack marks the card's run acked, when it is waiting on the limit step. It reports
// whether there was one.
func (n *newContexts) ack(taskID string) bool {
	n.mu.Lock()
	cur := n.by[taskID]
	ok := cur != nil && cur.step == NewContextLimit && !cur.acked
	if ok {
		cur.acked = true
	}
	n.mu.Unlock()
	if ok {
		n.kick(taskID)
	}
	return ok
}

// waitingForAck is whether the card's run is on the limit step with no ack yet.
func (n *newContexts) waitingForAck(taskID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	return cur != nil && cur.step == NewContextLimit && !cur.acked
}

// notePrompt records a limit prompt typed in turn.
func (n *newContexts) notePrompt(taskID string, gen uint64, turn int, at time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.by[taskID]
	if cur == nil || cur.gen != gen {
		return false
	}
	cur.prompted++
	cur.promptTurn, cur.lastPrompt = turn, at
	return true
}

// noteCleared records that `/clear` was typed.
func (n *newContexts) noteCleared(taskID string, gen uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if cur := n.by[taskID]; cur != nil && cur.gen == gen {
		cur.cleared = true
	}
}

// fail leaves the chip on the step that stopped, saying why.
func (n *newContexts) fail(taskID string, gen uint64, reason string) bool {
	n.mu.Lock()
	cur := n.by[taskID]
	if cur == nil || cur.gen != gen {
		n.mu.Unlock()
		return false
	}
	cur.step, cur.reason, cur.since = NewContextFailed, reason, time.Now()
	n.mu.Unlock()
	n.save()
	return true
}

// finish takes the chip off: the wake prompt landed.
func (n *newContexts) finish(taskID string, gen uint64) bool {
	n.mu.Lock()
	if cur := n.by[taskID]; cur == nil || cur.gen != gen {
		n.mu.Unlock()
		return false
	}
	delete(n.by, taskID)
	n.mu.Unlock()
	n.save()
	return true
}

// clear removes a card's chip whatever it says and stops its run.
func (n *newContexts) clear(taskID string) bool {
	n.mu.Lock()
	_, had := n.by[taskID]
	delete(n.by, taskID)
	n.mu.Unlock()
	n.save()
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
	case NewContextLimit:
		n, label = 1, "waiting for atrium ready"
		if c.acked {
			label = "atrium ready came, clearing when the turn ends"
		} else if c.prompted > 1 {
			label += fmt.Sprintf(" (asked %d times)", c.prompted)
		}
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
	if c.auto {
		out["auto"], out["tokens"], out["limit"] = true, c.tokens, c.limit
	}
	if c.prompted > 0 {
		out["prompted"] = c.prompted
	}
	if c.reason != "" {
		out["reason"] = c.reason
	}
	return out
}

var errNewContextGone = errors.New("superseded")

// holdingMessages is the question every delivery path asks for a card: is it in
// a new-context cycle. While it is, no message is typed and no hook carries one,
// because it would land in the context about to be cleared, or be lost with it. The cycle's own typing (`ncType`) does not ask.
func (d *Daemon) holdingMessages(taskID string) bool {
	return d.nctx.holding(taskID) || d.frozenForMove(taskID)
}

// frozenForMove reports whether the card is frozen for a move between rooms. It
// takes no turn and types nothing until the move ends or is undone.
func (d *Daemon) frozenForMove(taskID string) bool {
	f, err := d.st.Frozen(taskID)
	return err == nil && f != nil
}

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
// run in the background and the card's chip says where they are. A failed chip
// past the ack resumes where it stopped, so a session that already wrote its
// handoff, or was already cleared, is not asked to write another.
func (d *Daemon) StartNewContext(taskID string) error {
	task, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if d.sup.get(taskID) == nil {
		return errNoTerminal
	}
	// Two cycles at once in one directory are refused: the typing of both would interleave.
	if other := d.nctx.sameDirBusy(d, task); other != "" {
		return &newContextSharedError{other: other}
	}
	c := &newContext{step: NewContextLimit, conv: d.conversationOf(task)}
	if cur := d.nctx.get(taskID); cur != nil && cur.step == NewContextFailed && cur.acked && cur.file != "" {
		c.file, c.acked, c.cleared, c.conv = cur.file, true, cur.cleared, cur.conv
		c.step = NewContextClear
		if cur.cleared {
			c.step = NewContextWake
		}
	} else if c.file, err = d.prepareHandoff(task); err != nil {
		return err
	}
	gen, ok := d.nctx.claim(taskID, c)
	if !ok {
		if cur := d.nctx.get(taskID); cur != nil {
			v := newContextView(cur)
			return fmt.Errorf("%w, on step %v of 3 (%s) for %s", errNewContextBusy, v["n"], cur.step,
				time.Since(cur.since).Round(time.Second))
		}
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
	errNoTerminal     = errors.New("atrium does not own this session's terminal, so it cannot type into it")
	errNewContextBusy = errors.New("a new context is already under way on this card")
)

// errCycleUnder ends an automatic cycle whose card fell back under its limit
// before the ack: the runner compacted, and there is nothing left to cycle.
var errCycleUnder = errors.New("the card fell back under its limit")

// runNewContext is the sequence. Each step ends only when the one before it has
// really finished, and a step that cannot leaves the chip failed with a reason
// and types nothing further.
func (d *Daemon) runNewContext(taskID string, gen uint64) {
	cur := d.nctx.get(taskID)
	if cur == nil || cur.gen != gen {
		return
	}
	// Recorded at the claim, so a setting changed mid-cycle cannot split the three uses.
	file, start, by := cur.file, cur.step, newContextBy
	if cur.auto {
		by = cycleBy
	}
	end := func(ev map[string]any) {
		ev["by"] = by
		if err := d.st.AppendEvent(taskID, store.EventNotified, ev); err != nil {
			log.Printf("[atrium] could not record the new context on %s: %v", taskID, err)
		}
		d.publishTask(taskID)
		d.releaseHeld(taskID)
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
			end(map[string]any{"failed": reason})
		}
	}

	// 1. The limit prompt, again at most once per turn, until `atrium ready`.
	if start == NewContextLimit {
		if err := d.cycleAwaitAck(taskID, gen); err != nil {
			if errors.Is(err, errCycleUnder) {
				if d.nctx.finish(taskID, gen) {
					log.Printf("[atrium] context cycle on %s dropped: %v", taskID, err)
					end(map[string]any{"dropped": err.Error()})
				}
				return
			}
			fail("waiting for atrium ready", err)
			return
		}
		if !d.nctx.advance(taskID, gen, NewContextClear) {
			return
		}
		d.publishTask(taskID)
	}

	// 2. `/clear`, between turns, then the new session's SessionStart. The agent ran
	// `ready` inside a turn, and a `/clear` typed mid-turn is not a slash command.
	if start != NewContextWake {
		before, _ := d.wake.sessionStarted(taskID)
		typedAt := time.Now()
		if err := d.ncType(taskID, gen, "", newContextClear, ncTiming.captureEnd); err != nil {
			fail("could not type /clear", err)
			return
		}
		d.nctx.noteCleared(taskID, gen)
		err := d.ncWait(taskID, gen, ncTiming.clearWait, "a new session to start after /clear (is the session hook installed?)",
			func() (bool, error) {
				at, ok := d.wake.sessionStarted(taskID)
				return ok && at.After(before) && !at.Before(typedAt), nil
			})
		if err != nil {
			fail("the context did not clear", err)
			return
		}
		if !d.nctx.advance(taskID, gen, NewContextWake) {
			return
		}
		d.publishTask(taskID)
	}

	// 3. The wake, once the new session has drawn its input box.
	err := d.ncWait(taskID, gen, ncTiming.sessionSettle+ncTiming.clearWait, "the new session to settle", func() (bool, error) {
		at, _ := d.wake.sessionStarted(taskID)
		return time.Since(at) >= ncTiming.sessionSettle, nil
	})
	if err != nil {
		fail("the new session did not settle", err)
		return
	}
	if _, err := os.Stat(file); err != nil {
		fail("the wake was not typed", fmt.Errorf("the handoff %s is gone", file))
		return
	}
	// A turn in progress is waited out: the new session may already be taking a turn
	// on something else (r-016, @ui).
	if err := d.ncType(taskID, gen, newContextLabel, newContextWake(file), ncTiming.captureEnd); err != nil {
		fail("could not type the wake prompt", err)
		return
	}
	if d.nctx.finish(taskID, gen) {
		log.Printf("[atrium] new context on %s done", taskID)
		end(map[string]any{"done": true, "path": file})
	}
}

// cycleAwaitAck types the limit prompt and waits for `atrium ready`, for as long as
// it takes. Woken by a poll, an ack, a statusline update or the tick.
func (d *Daemon) cycleAwaitAck(taskID string, gen uint64) error {
	t := time.NewTicker(ncTiming.poll)
	defer t.Stop()
	for {
		cur := d.nctx.get(taskID)
		if cur == nil || cur.gen != gen {
			return errNewContextGone
		}
		if d.sup.get(taskID) == nil {
			return errors.New("the terminal closed")
		}
		if cur.acked {
			return nil
		}
		if cur.auto {
			if task, err := d.st.Get(taskID); err == nil && task != nil {
				if tokens, _ := d.ctx.read(task); tokens > 0 && tokens < d.cycleLimit(task) {
					return errCycleUnder
				}
			}
		}
		if d.cyclePromptDue(taskID, cur) {
			if err := d.cyclePrompt(taskID, gen, cur.file); err != nil {
				return err
			}
		}
		select {
		case <-d.nctx.stop:
			return errNewContextGone
		case <-cur.kick:
		case <-t.C:
		}
	}
}

// cyclePromptDue is whether the limit prompt goes now. AT MOST ONCE PER TURN:
//
//   - the first goes as soon as the line is free, mid-turn included, unless the
//     runner loses input typed mid-turn, when it waits for the turn to end,
//   - one typed mid-turn in turn N is due again once turn N is over, or once a
//     later turn has begun,
//   - one typed between turns starts turn N+1, and is due again when that turn is
//     over, or a later one begins. Should it never start a turn, it is taken as lost
//     after promptLost.
//
// Never closer than promptGap, for a runner whose turns are not counted. Never with
// a dialog on screen, whose Enter it would answer.
func (d *Daemon) cyclePromptDue(taskID string, c *newContext) bool {
	if d.act.dialogOpen(taskID) {
		return false
	}
	busy := d.ncBusy(taskID)
	if c.prompted == 0 {
		return !busy || d.midTurnInputFor(taskID)
	}
	since := cycleTimeNow().Sub(c.lastPrompt)
	if since < ncTiming.promptGap {
		return false
	}
	switch turns := d.act.turnsBegun(taskID); {
	case turns > c.promptTurn:
		return !busy || d.midTurnInputFor(taskID)
	case turns == c.promptTurn:
		return !busy
	default:
		return !busy && since >= ncTiming.promptLost
	}
}

// cyclePrompt types the limit prompt through the gate, with no turn check for a
// runner that takes input mid-turn. A closed gate writes nothing, and the next
// poll tries again.
func (d *Daemon) cyclePrompt(taskID string, gen uint64, path string) error {
	run := d.sup.get(taskID)
	if run == nil {
		return nil
	}
	mid := d.midTurnInputFor(taskID)
	gone, turn := false, 0
	ok := func() bool {
		if !d.nctx.mine(taskID, gen) {
			gone = true
			return false
		}
		if d.act.dialogOpen(taskID) {
			return false
		}
		// Read before the write: the prompt can start its turn before the Enter
		// that follows it returns. Mid-turn, the prompt belongs to the turn going;
		// between turns, to the one it starts.
		busy := d.ncBusy(taskID)
		turn = d.act.turnsBegun(taskID)
		if !busy {
			turn++
		}
		return mid || !busy
	}
	text := newContextLimitPrompt(path, atriumBinary())
	wrote, err := d.typeLabelledGuarded(run, taskID, newContextLabel, text, ok)
	if gone {
		return errNewContextGone
	}
	if err != nil || !wrote {
		return err
	}
	d.nctx.notePrompt(taskID, gen, turn, cycleTimeNow())
	if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
		"text": text, "via": "terminal", "from": newContextBy,
	}); err != nil {
		log.Printf("[atrium] could not record the limit prompt on %s: %v", taskID, err)
	}
	d.publishTask(taskID)
	return nil
}

// ncCapture is the capture half of the sequence: the prompt typed between turns,
// the turn it starts waited out, and the card's own file checked. It returns the
// step that stopped it and why. The idle parking runs this alone, with no clear
// after it, so a card is asked to write its handoff without losing its context.
func (d *Daemon) ncCapture(taskID string, gen uint64, file string) (string, error) {
	started := time.Now()
	token := captureToken(gen)
	// NOTHING IS TYPED WHILE THE CARD IS RUNNING (r-022). `ncType` waits for the
	// card's status to leave running as well as for its activity to go idle. A
	// cycle armed while the card was running used to type the capture into a turn
	// that had ended on background work, then take that turn's Stop as the
	// capture's and fail in seconds with "not updated by the capture turn" (@ui,
	// twice). With the card between turns when the prompt is typed, the count
	// taken here is before the capture's own turn and after every other.
	turns := d.act.turnsBegun(taskID)
	if err := d.ncType(taskID, gen, newContextLabel, newContextCapture(file, token), ncTiming.captureEnd); err != nil {
		return "could not type the capture prompt", err
	}
	err := d.ncWait(taskID, gen, ncTiming.captureBegin, "the capture prompt to start a turn",
		func() (bool, error) { return d.act.turnsBegun(taskID) > turns, nil })
	if err != nil {
		return "the capture prompt did nothing", err
	}
	quiet := time.Time{}
	err = d.ncWait(taskID, gen, ncTiming.captureEnd, "the capture turn to end", func() (bool, error) {
		if d.act.midTurn(taskID) || d.act.onSubagents(taskID) || d.cardRunning(taskID) {
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
	if err := d.handoffWritten(taskID, file, started, token); err != nil {
		return "nothing cleared", err
	}
	return "", nil
}

// cardRunning is whether the card's own status says a turn is going (r-022). The
// activity alone is not enough: a turn that ended on background work reads idle
// while the card stays running, and the next turn starts when that work
// reports. A capture typed into that gap is merged into the turn that follows,
// whose Stop then reads as the capture's.
func (d *Daemon) cardRunning(taskID string) bool {
	t, err := d.st.Get(taskID)
	return err == nil && t != nil && t.Status == store.StatusRunning
}

// ncBusy is whether the card is inside a turn as far as typing is concerned: the
// activity says so or the status says running, and the screen does not show a
// settled prompt. A card whose Stop never landed, or whose daemon restarted
// under a status of running, reads busy to both and will never end a turn it is
// not taking, so the step waited for a turn end that could not come. The screen
// is the tiebreak, the same signature `watchLooksIdle` flags on.
func (d *Daemon) ncBusy(taskID string) bool {
	if !d.act.midTurn(taskID) && !d.cardRunning(taskID) {
		return false
	}
	return !d.atIdlePrompt(taskID)
}

// atIdlePrompt is whether a claude card is flagged looks-idle or shows a settled
// prompt on a pty silent for LooksIdleAfter, with no dialog and no subagents.
func (d *Daemon) atIdlePrompt(taskID string) bool {
	run := d.sup.get(taskID)
	if run == nil || d.act.dialogOpen(taskID) || d.act.onSubagents(taskID) {
		return false
	}
	if _, on := d.act.looksIdleMark(taskID); on {
		return true
	}
	if t, err := d.st.Get(taskID); err != nil || t == nil || !strings.EqualFold(t.Runner, "claude") {
		return false
	}
	if time.Since(run.lastOutputAt()) < LooksIdleAfter || run.buf == nil {
		return false
	}
	cols, rows := run.buf.CurrentSize()
	idle, _ := classifyFrame(run.buf.Tail(frameTailBytes), cols, rows)
	return idle
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
// injector and every delivery path (`holdingMessages`) from the claim to the
// wake, so nothing else types into the card during a run. What that could not
// give was atomicity: a message already past its own check when the run began.
// The lock gives it.
func (d *Daemon) ncType(taskID string, gen uint64, label, text string, limit time.Duration) error {
	return d.ncWait(taskID, gen, limit, "an empty line and no turn in progress", func() (bool, error) {
		run := d.sup.get(taskID)
		if run == nil || d.act.dialogOpen(taskID) || d.ncBusy(taskID) {
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
			return !d.act.dialogOpen(taskID) && !d.ncBusy(taskID) &&
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
// which is a card whose files this daemon has no way to look at. The capture
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

// handoffWritten checks the capture left this card's file. Two ways through: the
// file carries this run's `atrium-capture: <token>` line in its first 4 KB, which
// a card that wrote its notes a minute ago can add, or it was written during the
// capture turn. Under 200 bytes is refused either way. Each refusal says what the
// card should have done, in one sentence, because the chip shows it.
func (d *Daemon) handoffWritten(taskID, file string, since time.Time, token string) error {
	dir, err := d.handoffDir(taskID)
	if err != nil || dir == "" {
		return err
	}
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		return fmt.Errorf("the capture turn ended without %s in %s, so the card should have written it and added the line %s",
			file, dir, captureLine(token))
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("could not read %s in %s: %v", file, dir, err)
	}
	if info.Size() < handoffFloor {
		return fmt.Errorf("%s in %s is only %d bytes, so the card should have written its whole handoff, at least %d bytes",
			file, dir, info.Size(), handoffFloor)
	}
	// A little slack for file systems that round a modified time down.
	if !info.ModTime().Before(since.Add(-2 * time.Second)) {
		return nil
	}
	head := make([]byte, handoffHead)
	n, _ := io.ReadFull(f, head)
	if bytes.Contains(head[:n], []byte(captureLine(token))) {
		return nil
	}
	return fmt.Errorf("%s in %s is older than the capture and lacks the line %s in its first 4 KB, so the card should have added it",
		file, dir, captureLine(token))
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
