package cycle

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// New context: one action that cycles a card's context. The same sequence runs
// when someone presses New context and when the card passes its limit
// (autocontext.go). See docs/runtime/context-cycle-design.md.
//
// THE DAEMON RUNS THE SEQUENCE, NOT THE AGENT. A message the agent queues for
// itself can land before the clear and be wiped with it, so the room holds each
// step until the one before it is over, and types nothing further when a step fails.
//
//  1. limit: type the limit prompt, which names the handoff file and `atrium ready`.
//     Typed as soon as the input line is free, mid-turn included, and again only at
//     an idle prompt promptLost later. Never cleared without the ack.
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
const NewContextBy = "new-context"

// newContextLabel goes ahead of the limit, capture and wake prompts, so nobody reads
// them as the operator's own words. `/clear` goes with none, because a label in
// front of it is no longer a slash command. See atriumLabel.
var NewContextLabel = shared.AtriumLabel("new context:")

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
func NewContextCaptureText(file, token string) string {
	return "Your context is about to be cleared. First commit or stash any work in " +
		"progress. Then write everything a fresh session needs to carry on to " + file + " in the current " +
		"directory: what you are doing and why, what is done, what is left, decisions made and the reasons, " +
		"branches, commits and files involved, how to check the work, and anything you were waiting on. " +
		"Put the line " + CaptureLine(token) + " near the top of the file, also when it is already written. " +
		"When it is written, reply with one line saying so and stop. Do not start anything else. You will " +
		"be told to read " + file + " back once the context is clear."
}

// captureLine is the line a card puts in its handoff file to say it is this run's.
func CaptureLine(token string) string { return CaptureMarker + token }

// captureMarker leads the line. handoffWritten looks for the whole line, token and all.
const CaptureMarker = "atrium-capture: "

// handoffFloor is the fewest bytes a handoff may be. A token alone is not one.
const HandoffFloor = 200

// handoffHead is how much of the file is read looking for the token. Never all of it.
const HandoffHead = 4096

// captureToken is unique to one capture run: the run's generation and a random id.
func CaptureToken(gen uint64) string {
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
const NewContextClearCommand = "/clear"

// newContextLimitPrompt is the limit prompt: why the card is cycling, where the handoff goes and how to say it is
// written. The binary is named in full, since the CLI is not on PATH in every room.
//
// THE LIMIT IS ATRIUM'S, NOT THE MODEL'S, and an agent told only "you are at context limit" reads it as the window
// being full and rushes. So an automatic cycle names both numbers. A cycle somebody started by hand has none.
//
// PAUSE, NOT FINISH. "Wrap what is in flight" let the agent decide its step was nearly done and keep going, further
// over the limit. The handoff says what is in flight instead, and the next context picks it up.
//
// ONE WAY TO SAY IT IS WRITTEN. A card with a shell has `atrium ready`; atrium_ready is not on every card's tool
// list, so it is named only to a card with no shell, which is the card it exists for.
func NewContextLimitPrompt(path, bin string, noShell bool, tokens, limit int64) string {
	why := "Atrium is starting a new context on this card."
	if limit > 0 {
		why = fmt.Sprintf("This card is over its configured atrium context limit (%dk of %dk).", tokens/1000, limit/1000)
	}
	write, then := "Write a handoff to "+path, "Then run "+bin+" ready."
	if noShell {
		write, then = "Write a handoff", "Then call the atrium_ready tool with your handoff as its text."
	}
	return why + " Pause your work now. " + write + " that says what is complete, what is in flight, and what's " +
		"still to be done. " + then + " Atrium clears your context when this turn ends and starts a new one from " +
		"your handoff."
}

// runnerHasNoShell is whether the card's launch withholds both shells, so the prompt names the tool alone.
func (d *Service) RunnerHasNoShell(taskID string) bool {
	t, err := d.St.Get(taskID)
	if err != nil || t == nil {
		return false
	}
	args := t.LaunchArgs
	if h, _ := d.HarnessOf(taskID); h != nil {
		args = append(append([]string{}, h.Args...), args...)
	}
	denied := map[string]bool{}
	for i, a := range args {
		v := ""
		switch {
		case (a == "--disallowedTools" || a == "--disallowed-tools") && i+1 < len(args):
			v = args[i+1]
		case strings.HasPrefix(a, "--disallowedTools=") || strings.HasPrefix(a, "--disallowed-tools="):
			v = a[strings.Index(a, "=")+1:]
		}
		for _, n := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
			denied[n] = true
		}
	}
	return denied["Bash"] && denied["PowerShell"]
}

// newContextWake is what the new session is told, and reads the handoff back.
func NewContextWakeText(path string) string { return "read " + path + " and continue." }

// sentLines is how many already delivered says the wake lists, newest kept, and sentLen how much of each.
const (
	SentLines = 8
	SentLen   = 160
)

// newContextWakeFor is the wake with the says the card sent since its launcher last prompted it, marked as already
// delivered. The fresh context knows only the handoff, and one that re-ran its last errand sent the same reply twice.
func (d *Service) NewContextWakeFor(taskID, path string) string {
	text := NewContextWakeText(path)
	t, err := d.St.Get(taskID)
	if err != nil {
		return text
	}
	f := store.SayFilter{From: taskID}
	if t.OwedAt != nil {
		f.Since = t.OwedAt.UTC().Format(store.TimeFormat)
	}
	rows, err := d.St.SaysWhere(f, "", SentLines)
	if err != nil || len(rows) == 0 {
		return text
	}
	var parts []string
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.State == store.SayRefused || r.State == store.SayUnresolved {
			continue
		}
		to := r.ToWire
		if to == "" {
			to = r.ToInput
		}
		body := strings.Join(strings.Fields(r.Text), " ")
		if r.Text == "" {
			body = r.Preview
		}
		if rs := []rune(body); len(rs) > SentLen {
			body = string(rs[:SentLen]) + "..."
		}
		parts = append(parts, fmt.Sprintf("to %s: %q", to, body))
	}
	if len(parts) == 0 {
		return text
	}
	return text + " these says were already sent and delivered before this context started, so do NOT resend them: " +
		strings.Join(parts, "; ")
}

// atriumBinary is the full path of this atrium, for the limit prompt. A variable for a test.
var AtriumBinary = func() string {
	if p, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	return "atrium"
}

// handoffPath is where a card's cycle writes its handoff: the room's context_handoff_dir, else the card's home
// (<home>/.atrium/handoff.md), else the handoffs folder beside the room's database, then <card-id>.md.
//
// NOT THE SYSTEM TEMP FOLDER. A handoff is the only record of the work in flight, and temp is swept by the OS and
// sits on a drive the operator may keep clean of working files on purpose.
func (d *Service) HandoffPath(t *store.Task) string {
	dir := d.St.ContextHandoffDir()
	if dir == "" {
		if h, ok := d.HomeOf(t.ID); ok {
			return filepath.Join(home.DirsOf(h.Path).Meta, "handoff.md")
		}
	}
	if dir == "" {
		dir = filepath.Join(filepath.Dir(d.DBPath()), HandoffDirName)
	}
	return filepath.Join(dir, t.ID+".md")
}

// handoffDirName is the handoffs folder beside the database, for a card with no home. See handoffPath.
const HandoffDirName = "handoffs"

// prepareHandoff is handoffPath stamped with the time, with its directory made, so the agent can write there.
// A new name each cycle, since an agent that overwrites must read the old file first.
func (d *Service) PrepareHandoff(t *store.Task) (string, error) {
	path := StampHandoff(d.HandoffPath(t), time.Now())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("could not make the handoff directory %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

// stampHandoff puts the time before the extension: <card-id>-20261022233412.md.
func StampHandoff(path string, now time.Time) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + "-" + now.Format("20060102150405") + ext
}

// ncTimings is how long each step waits, and how often it looks. Fields of the daemon's hooks so a
// test can run the whole sequence in milliseconds rather than name the thing that
// must not happen by waiting for it.
type NcTimings struct {
	// poll is how often a wait looks at the card.
	Poll time.Duration
	// typeWait is how long a step waits for the gate to open and the runner to be
	// between turns before it gives up typing. `/clear`, the wake and the capture
	// wait for captureEnd instead: the turn they follow may be long.
	TypeWait time.Duration
	// ackWait is how long the limit step waits for `atrium ready` in all. Past it
	// the cycle fails and the card's held input is released, since an agent that
	// never acks (died, wedged, ignored the prompt) must not hold its card for ever.
	AckWait time.Duration
	// captureBegin is how long the capture prompt has to start a turn.
	CaptureBegin time.Duration
	// captureEnd is how long the capture turn has to finish.
	CaptureEnd time.Duration
	// turnSettle is how long the runner has to stay out of a turn for it to count
	// as over. A gap between two tool calls is shorter.
	TurnSettle time.Duration
	// clearWait is how long `/clear` has to produce a new session.
	ClearWait time.Duration
	// clearTypeWait is how long `/clear` has to find a turn end and an open gate once
	// the ack is in. Past it the cycle fails naming what blocked it, rather than
	// sitting on step 2 for captureEnd.
	ClearTypeWait time.Duration
	// ackQuiet is how long after `atrium ready` the card may post no activity at all before the turn counts as over
	// for `/clear`, whatever the activity says. The agent was told to end its turn, so a card this quiet lost its
	// Stop hook, and the screen cannot say so while a background shell keeps redrawing it.
	AckQuiet time.Duration
	// noteAfter is how long step 2 waits before it puts what it waits on on the card.
	NoteAfter time.Duration
	// logEvery is how often a step that waits says what it waits on.
	LogEvery time.Duration
	// sessionSettle is how long after the new session's SessionStart the wake
	// waits. The hook fires a moment before the input box is drawn, and bytes
	// typed before then are lost. See wakeSettle.
	SessionSettle time.Duration
	// promptGap is the least time between two limit prompts, for a runner whose
	// turns are not counted.
	PromptGap time.Duration
	// promptLost is how long a limit prompt may go unanswered before it is typed
	// again, at an idle prompt only.
	PromptLost time.Duration
}

// defaultNCTiming is what a daemon starts with. A test changes its own daemon's copy, never a shared one.
func DefaultNCTiming() NcTimings {
	return NcTimings{
		Poll:          250 * time.Millisecond,
		AckWait:       30 * time.Minute,
		TypeWait:      2 * time.Minute,
		CaptureBegin:  time.Minute,
		CaptureEnd:    15 * time.Minute,
		TurnSettle:    2 * time.Second,
		ClearWait:     time.Minute,
		ClearTypeWait: 5 * time.Minute,
		AckQuiet:      20 * time.Second,
		NoteAfter:     5 * time.Second,
		LogEvery:      30 * time.Second,
		SessionSettle: WakeSettle,
		PromptGap:     30 * time.Second,
		PromptLost:    10 * time.Minute,
	}
}

// maxLimitAsks is how many times the limit prompt is typed before the cycle stops re-asking and waits.
const MaxLimitAsks = 3

// newContext is one card's sequence as the board draws it.
type NewContext struct {
	Step   string
	File   string
	Since  time.Time
	Reason string
	// conv is the conversation the run started in. A failed chip clears when a
	// SessionStart names a different one, which is the proof the clear happened.
	Conv string
	// gen tells a run whether it is still the card's. A dismiss or a fresh run
	// bumps it, and the goroutine it replaced stops without saying anything.
	Gen uint64
	// auto marks a run the daemon started at the card's limit, and tokens and limit
	// are what it started on. See autocontext.go.
	Auto          bool
	Tokens, Limit int64
	// acked is set when `atrium ready` came, and cleared when `/clear` was typed. A
	// rerun of a failed chip resumes after what they say happened.
	Acked, Cleared bool
	// prompted is how many times the limit prompt was typed, the last at lastPrompt,
	// in the turn promptTurn (see cyclePromptDue).
	Prompted   int
	PromptTurn int
	LastPrompt time.Time
	// kick wakes the run's wait at once: an ack, a statusline update, a tick.
	Kick chan struct{}
	// capOnly marks the idle parking's capture, which clears nothing and is not
	// journalled: a restart that cuts it off has nothing to report.
	CapOnly bool
	// abandoned marks a failed chip seeded at startup from the journal. A
	// SessionStart cannot clear it: see sessionStarted.
	Abandoned bool
	// typing is set while atrium's own write to the terminal is under way, and says
	// so on the card. Board typing is refused for the whole cycle. See typingHeld.
	Typing bool
	// armed is set once the operator's line gate has been open since the claim. Until then the cycle holds
	// nothing, so keys already typed are never shut behind it. See ncArm.
	Armed bool
}

type NewContexts struct {
	Mu   sync.Mutex
	By   map[string]*NewContext
	Gens uint64
	// stop ends every run in flight, at shutdown.
	Stop     chan struct{}
	StopOnce sync.Once
	// persist writes the runs in flight somewhere a restart can find them. See
	// newcontext_journal.go. Nil in a test that does not care.
	Persist func(map[string]NcJournalRow)
	// saveMu keeps two snapshots from reaching persist out of order.
	SaveMu sync.Mutex
}

func NewNewContexts() *NewContexts {
	return &NewContexts{By: map[string]*NewContext{}, Stop: make(chan struct{})}
}

func (n *NewContexts) StopAll() { n.StopOnce.Do(func() { close(n.Stop) }) }

func (n *NewContexts) Claim(taskID string, c *NewContext) (uint64, bool) {
	n.Mu.Lock()
	if cur := n.By[taskID]; cur != nil && cur.Step != NewContextFailed {
		n.Mu.Unlock()
		return 0, false
	}
	n.Gens++
	c.Since, c.Gen, c.Kick = time.Now(), n.Gens, make(chan struct{}, 1)
	n.By[taskID] = c
	gen := n.Gens
	n.Mu.Unlock()
	n.Save()
	return gen, true
}

// conversationOf is the conversation a card's session is in now: the id its last
// SessionStart reported, else the resume id stored on the card.
func (d *Service) ConversationOf(t *store.Task) string {
	if d.Wake != nil {
		if c := d.Wake.Conversation(t.ID); c != "" {
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
func (n *NewContexts) SessionStarted(taskID, conv string) bool {
	if n == nil || conv == "" {
		return false
	}
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil || cur.Step != NewContextFailed || cur.Abandoned || cur.Conv == "" || cur.Conv == conv {
		return false
	}
	delete(n.By, taskID)
	return true
}

// mine reports whether gen is still the card's run.
func (n *NewContexts) Mine(taskID string, gen uint64) bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	return cur != nil && cur.Gen == gen
}

// advance moves a run to its next step.
func (n *NewContexts) Advance(taskID string, gen uint64, step string) bool {
	n.Mu.Lock()
	cur := n.By[taskID]
	if cur == nil || cur.Gen != gen {
		n.Mu.Unlock()
		return false
	}
	cur.Step, cur.Since = step, time.Now()
	n.Mu.Unlock()
	n.Save()
	return true
}

// kick wakes the card's run, if it is waiting, without blocking.
func (n *NewContexts) Kick(taskID string) {
	if n == nil {
		return
	}
	n.Mu.Lock()
	cur := n.By[taskID]
	n.Mu.Unlock()
	if cur == nil || cur.Kick == nil {
		return
	}
	select {
	case cur.Kick <- struct{}{}:
	default:
	}
}

// ack marks the card's run acked, when it is waiting on the limit step. It reports
// whether there was one.
func (n *NewContexts) Ack(taskID string) bool {
	n.Mu.Lock()
	cur := n.By[taskID]
	ok := cur != nil && cur.Step == NewContextLimit && !cur.Acked
	if ok {
		cur.Acked = true
	}
	n.Mu.Unlock()
	if ok {
		n.Kick(taskID)
	}
	return ok
}

// waitingForAck is whether the card's run is on the limit step with no ack yet.
func (n *NewContexts) WaitingForAck(taskID string) bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	return cur != nil && cur.Step == NewContextLimit && !cur.Acked
}

// notePrompt records a limit prompt typed in turn.
func (n *NewContexts) NotePrompt(taskID string, gen uint64, turn int, at time.Time) bool {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil || cur.Gen != gen {
		return false
	}
	cur.Prompted++
	cur.PromptTurn, cur.LastPrompt = turn, at
	return true
}

// noteCleared records that `/clear` was typed.
func (n *NewContexts) NoteCleared(taskID string, gen uint64) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	if cur := n.By[taskID]; cur != nil && cur.Gen == gen {
		cur.Cleared = true
	}
}

// fail leaves the chip on the step that stopped, saying why.
func (n *NewContexts) Fail(taskID string, gen uint64, reason string) bool {
	n.Mu.Lock()
	cur := n.By[taskID]
	if cur == nil || cur.Gen != gen {
		n.Mu.Unlock()
		return false
	}
	cur.Step, cur.Reason, cur.Since = NewContextFailed, reason, time.Now()
	n.Mu.Unlock()
	n.Save()
	return true
}

// finish takes the chip off: the wake prompt landed.
func (n *NewContexts) Finish(taskID string, gen uint64) bool {
	return n.FinishRecorded(taskID, gen, nil)
}

// finishRecorded is finish that runs record first, while the chip is still on and
// only if it is still gen's, so nobody who sees the chip gone reads a history that
// does not yet say why.
func (n *NewContexts) FinishRecorded(taskID string, gen uint64, record func()) bool {
	n.Mu.Lock()
	if cur := n.By[taskID]; cur == nil || cur.Gen != gen {
		n.Mu.Unlock()
		return false
	}
	if record != nil {
		record()
	}
	delete(n.By, taskID)
	n.Mu.Unlock()
	n.Save()
	return true
}

// clear removes a card's chip whatever it says and stops its run.
func (n *NewContexts) Clear(taskID string) bool {
	n.Mu.Lock()
	_, had := n.By[taskID]
	delete(n.By, taskID)
	n.Mu.Unlock()
	n.Save()
	return had
}

// holding reports whether a card is inside a new-context cycle that has not
// ended: from `begin` until the wake prompt has been typed. A failed chip is not
// holding, since nothing is running any more and a held message would be
// stranded.
func (n *NewContexts) Holding(taskID string) bool {
	if n == nil {
		return false
	}
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	return cur != nil && cur.Step != NewContextFailed
}

// holdsInput is holding once the limit prompt has gone in. Before it, nothing is under way that a message could
// land in the wrong side of, so a cycle whose prompt was never typed holds nothing.
func (n *NewContexts) HoldsInput(taskID string) bool {
	if n == nil {
		return false
	}
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil || cur.Step == NewContextFailed || !cur.Armed {
		return false
	}
	return cur.Step != NewContextLimit || cur.Prompted > 0 || cur.Acked || cur.Typing
}

func (n *NewContexts) Get(taskID string) *NewContext {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	cur := n.By[taskID]
	if cur == nil {
		return nil
	}
	cp := *cur
	return &cp
}

// newContextFor is the card's sequence as the board draws it, or nil.
func (d *Service) NewContextFor(taskID string) any {
	cur := d.Nctx.Get(taskID)
	if cur == nil {
		return nil
	}
	v := NewContextView(cur)
	if cur.Step != NewContextFailed {
		v["typing"] = cur.Typing && !cur.CapOnly
		v["held"] = cur.Armed && !cur.CapOnly
		if !cur.Armed {
			v["label"] = "waiting for your line to clear"
		} else if cur.Typing {
			v["label"] = "atrium is typing into the terminal"
		}
		// What the operator composed while a write was under way waits as an ordinary
		// held message, and the card says how many.
		if msgs, err := d.St.PendingMessages(taskID); err == nil {
			q := 0
			for _, m := range msgs {
				if m.FromPeer == "" {
					q++
				}
			}
			if q > 0 {
				v["queued"] = q
			}
		}
	}
	return v
}

// newContextView carries the wording, so the board says what the daemon means
// and there is one place to change it.
func NewContextView(c *NewContext) map[string]any {
	n, label := 0, ""
	switch c.Step {
	case NewContextLimit:
		n, label = 1, "waiting for atrium ready"
		if c.Acked {
			label = "atrium ready came, clearing when the turn ends"
		} else if c.Prompted >= MaxLimitAsks {
			label = fmt.Sprintf("new context waiting: the session did not call ready after %d asks", MaxLimitAsks)
		} else if c.Prompted > 1 {
			label += fmt.Sprintf(" (asked %d times)", c.Prompted)
		}
	case NewContextCapture:
		n, label = 1, "capturing state to "+c.File
	case NewContextClear:
		n, label = 2, "clearing the context"
	case NewContextWake:
		n, label = 3, "waking it to read "+c.File
	case NewContextFailed:
		label = "new context failed"
	}
	out := map[string]any{"step": c.Step, "n": n, "of": 3, "label": label, "file": c.File, "since": c.Since}
	if c.Auto {
		out["auto"], out["tokens"], out["limit"] = true, c.Tokens, c.Limit
	}
	if c.Prompted > 0 {
		out["prompted"] = c.Prompted
	}
	if c.Reason != "" {
		out["reason"] = c.Reason
	}
	return out
}

var ErrNewContextGone = errors.New("superseded")

// holdingMessages is the question every delivery path asks for a card: is it in
// a new-context cycle. While it is, no message is typed and no hook carries one,
// because it would land in the context about to be cleared, or be lost with it. The cycle's own typing (`ncType`) does not ask.
func (d *Service) HoldingMessages(taskID string) bool {
	return d.Nctx.HoldsInput(taskID) || d.FrozenForMove(taskID)
}

// frozenForMove reports whether the card is frozen for a move between rooms. It
// takes no turn and types nothing until the move ends or is undone.
func (d *Service) FrozenForMove(taskID string) bool {
	f, err := d.St.Frozen(taskID)
	return err == nil && f != nil
}

// newContextHoldNote is what a sender is told while a card is held.
const NewContextHoldNote = "queued: this card is starting a new context, and everything for it is held " +
	"until its wake prompt has been typed."

// releaseHeld is called when a cycle ends any way at all: the wake typed, a
// failed step, a dismissal. What was held is then delivered by the ordinary
// paths, so the typist is kicked to try at once rather than at the end of a
// backoff. It never types ahead of the wake prompt, which has been typed by now.
func (d *Service) ReleaseHeld(taskID string) {
	d.ResetPending(taskID)
	d.Publish(taskID)
}

// StartNewContext begins the sequence on a card and returns at once. The steps
// run in the background and the card's chip says where they are. A failed chip
// past the ack resumes where it stopped, so a session that already wrote its
// handoff, or was already cleared, is not asked to write another.
func (d *Service) StartNewContext(taskID string) error {
	task, err := d.St.Get(taskID)
	if err != nil {
		return err
	}
	if d.Sup.Get(taskID) == nil {
		return ErrNoTerminal
	}
	// Two cycles at once in one directory are refused: the typing of both would interleave.
	if other := d.Nctx.SameDirBusy(d.St, task); other != "" {
		return &NewContextSharedError{Other: other}
	}
	c := &NewContext{Step: NewContextLimit, Conv: d.ConversationOf(task)}
	if cur := d.Nctx.Get(taskID); cur != nil && cur.Step == NewContextFailed && cur.Acked && cur.File != "" {
		c.File, c.Acked, c.Cleared, c.Conv = cur.File, true, cur.Cleared, cur.Conv
		c.Step = NewContextClear
		if cur.Cleared {
			c.Step = NewContextWake
		}
	} else if c.File, err = d.PrepareHandoff(task); err != nil {
		return err
	}
	gen, ok := d.Nctx.Claim(taskID, c)
	if !ok {
		if cur := d.Nctx.Get(taskID); cur != nil {
			v := NewContextView(cur)
			return fmt.Errorf("%w, on step %v of 3 (%s) for %s", ErrNewContextBusy, v["n"], cur.Step,
				time.Since(cur.Since).Round(time.Second))
		}
		return ErrNewContextBusy
	}
	log.Printf("[atrium] new context started on %s", task.DisplayTitle())
	d.Publish(taskID)
	go d.RunNewContext(taskID, gen)
	return nil
}

// newContextSharedError refuses a cycle while another card in the same directory
// is itself mid-cycle.
type NewContextSharedError struct{ Other string }

func (e *NewContextSharedError) Error() string {
	return "another card in this directory, " + e.Other + ", is mid new-context, so wait for it to finish"
}

func IsSharedErr(err error) bool {
	var e *NewContextSharedError
	return errors.As(err, &e)
}

// sameDirBusy names a card, other than task, that shares its directory and has a
// cycle under way. A failed chip is not under way.
func (n *NewContexts) SameDirBusy(st *store.Store, task *store.Task) string {
	dir := filepath.ToSlash(strings.TrimSpace(task.Worktree))
	if dir == "" {
		return ""
	}
	all, err := st.List()
	if err != nil {
		return ""
	}
	for _, o := range all {
		if o.ID == task.ID || filepath.ToSlash(strings.TrimSpace(o.Worktree)) != dir {
			continue
		}
		if cur := n.Get(o.ID); cur != nil && cur.Step != NewContextFailed {
			return o.DisplayTitle()
		}
	}
	return ""
}

var (
	ErrNoTerminal     = errors.New("atrium does not own this session's terminal, so it cannot type into it")
	ErrNewContextBusy = errors.New("a new context is already under way on this card")
)

// errCycleUnder ends an automatic cycle whose card fell back under its limit
// before the ack: the runner compacted, and there is nothing left to cycle.
var ErrCycleUnder = errors.New("the card fell back under its limit")

// runNewContext is the sequence. Each step ends only when the one before it has
// really finished, and a step that cannot leaves the chip failed with a reason
// and types nothing further.
func (d *Service) RunNewContext(taskID string, gen uint64) {
	cur := d.Nctx.Get(taskID)
	if cur == nil || cur.Gen != gen {
		return
	}
	// Recorded at the claim, so a setting changed mid-cycle cannot split the three uses.
	file, start, by := cur.File, cur.Step, NewContextBy
	// What reached the card in its latest turn is in the context about to be cleared, and the model is told to write
	// a handoff, not to act on it. Handed back once the wake is typed. See contextlog.go.
	carried := d.ItemMissed(taskID, d.TakeInContext(taskID))
	if cur.Auto {
		by = shared.CycleBy
	}
	record := func(ev map[string]any) {
		ev["by"] = by
		if err := d.St.AppendEvent(taskID, store.EventNotified, ev); err != nil {
			log.Printf("[atrium] could not record the new context on %s: %v", taskID, err)
		}
	}
	after := func() {
		d.Publish(taskID)
		d.ReleaseHeld(taskID)
	}
	end := func(ev map[string]any) {
		record(ev)
		after()
	}
	// finish takes the chip off with ev already in the card's history.
	finish := func(ev map[string]any) bool {
		if !d.Nctx.FinishRecorded(taskID, gen, func() { record(ev) }) {
			return false
		}
		after()
		return true
	}
	fail := func(step string, err error) {
		if errors.Is(err, ErrNewContextGone) {
			return
		}
		reason := step + ": " + err.Error()
		if d.Nctx.Fail(taskID, gen, reason) {
			log.Printf("[atrium] new context on %s stopped, %s", taskID, reason)
			// The chip goes when the clear is proven, and the reason stays in the card's
			// history after it.
			end(map[string]any{"failed": reason})
		}
	}

	// 1. The limit prompt, again at most once per turn, until `atrium ready`.
	if start == NewContextLimit {
		if err := d.CycleAwaitAck(taskID, gen); err != nil {
			if errors.Is(err, ErrCycleUnder) {
				if finish(map[string]any{"dropped": err.Error()}) {
					log.Printf("[atrium] context cycle on %s dropped: %v", taskID, err)
				}
				return
			}
			fail("waiting for atrium ready", err)
			return
		}
		if !d.Nctx.Advance(taskID, gen, NewContextClear) {
			return
		}
		log.Printf("[atrium] new context on %s: step 2/3, waiting for the turn to end to type /clear", taskID)
		d.Publish(taskID)
	}

	if start != NewContextLimit {
		if err := d.NcAwaitArm(taskID, gen, d.Hk.NcTiming.AckWait); err != nil {
			fail("waiting for the input line to clear", err)
			return
		}
	}

	// 2. `/clear`, between turns, then the new session's SessionStart. The agent ran
	// `ready` inside a turn, and a `/clear` typed mid-turn is not a slash command.
	if start != NewContextWake {
		before, _ := d.Wake.SessionStarted(taskID)
		typedAt := time.Now()
		if err := d.NcTypeBusy(taskID, gen, "", NewContextClearCommand, d.Hk.NcTiming.ClearTypeWait, d.NcBusyAfterAck); err != nil {
			fail("could not type /clear", err)
			return
		}
		log.Printf("[atrium] new context on %s: typed /clear, waiting up to %s for the new session",
			taskID, d.Hk.NcTiming.ClearWait.Round(time.Second))
		d.Nctx.NoteCleared(taskID, gen)
		err := d.NcWait(taskID, gen, d.Hk.NcTiming.ClearWait, "a new session to start after /clear (is the session hook installed?)",
			func() (bool, error) {
				at, ok := d.Wake.SessionStarted(taskID)
				return ok && at.After(before) && !at.Before(typedAt), nil
			})
		if err != nil {
			fail("the context did not clear", err)
			return
		}
		if !d.Nctx.Advance(taskID, gen, NewContextWake) {
			return
		}
		log.Printf("[atrium] new context on %s: step 3/3, the new session started, typing the wake", taskID)
		d.Publish(taskID)
	}

	// 3. The wake, once the new session has drawn its input box.
	err := d.NcWait(taskID, gen, d.Hk.NcTiming.SessionSettle+d.Hk.NcTiming.ClearWait, "the new session to settle", func() (bool, error) {
		at, _ := d.Wake.SessionStarted(taskID)
		return time.Since(at) >= d.Hk.NcTiming.SessionSettle, nil
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
	if err := d.NcType(taskID, gen, NewContextLabel, d.NewContextWakeFor(taskID, file), d.Hk.NcTiming.CaptureEnd); err != nil {
		fail("could not type the wake prompt", err)
		return
	}
	if finish(map[string]any{"done": true, "path": file}) {
		log.Printf("[atrium] new context on %s done", taskID)
		d.RequeueCarried(taskID, carried)
	}
}

// cycleAwaitAck types the limit prompt and waits for `atrium ready`, for as long as
// it takes. Woken by a poll, an ack, a statusline update or the tick.
func (d *Service) CycleAwaitAck(taskID string, gen uint64) error {
	t := time.NewTicker(d.Hk.NcTiming.Poll)
	defer t.Stop()
	for {
		cur := d.Nctx.Get(taskID)
		if cur == nil || cur.Gen != gen {
			return ErrNewContextGone
		}
		if d.Sup.Get(taskID) == nil {
			return errors.New("the terminal closed")
		}
		if cur.Acked {
			return nil
		}
		// A turn in progress is never typed into because the bound passed: the
		// cycle just ends and the held input goes at the next clear line.
		if since := time.Since(cur.Since); since > d.Hk.NcTiming.AckWait {
			if !cur.Armed {
				return fmt.Errorf("the input line did not clear for %s, nothing was held", since.Round(time.Minute))
			}
			return fmt.Errorf("input was held for %s waiting for atrium ready, released",
				since.Round(time.Minute))
		}
		if cur.Auto {
			if task, err := d.St.Get(taskID); err == nil && task != nil {
				if tokens, _ := d.ContextTokens(task); tokens > 0 && tokens < d.CycleLimit(task) {
					return ErrCycleUnder
				}
			}
		}
		if !cur.Armed {
			d.NcArm(taskID, gen)
		} else if d.CyclePromptDue(taskID, cur) {
			if err := d.CyclePrompt(taskID, gen, cur); err != nil {
				return err
			}
		}
		select {
		case <-d.Nctx.Stop:
			return ErrNewContextGone
		case <-cur.Kick:
		case <-t.C:
		}
	}
}

// cyclePromptDue is whether the limit prompt goes now. ONCE PER CYCLE: the first goes
// as soon as the line is free, mid-turn included, unless the runner loses input typed
// mid-turn, when it waits for the turn to end. Another goes only at an idle prompt
// promptLost after the last. Never mid-turn: a prompt queued mid-turn starts a turn of
// its own when it is read, and the agent may be inside `atrium ready` (f-new-context-nudge-repeats).
//
// Never closer than promptGap. Never with a dialog on screen, whose Enter it would answer.
func (d *Service) CyclePromptDue(taskID string, c *NewContext) bool {
	if d.Act.DialogOpen(taskID) || c.Prompted >= MaxLimitAsks {
		return false
	}
	busy := d.NcBusy(taskID)
	if c.Prompted == 0 {
		return !busy || d.MidTurnInputFor(taskID)
	}
	since := cycleTimeNow().Sub(c.LastPrompt)
	if since < d.Hk.NcTiming.PromptGap {
		return false
	}
	return !busy && since >= d.Hk.NcTiming.PromptLost
}

// cyclePrompt types the limit prompt through the gate, with no turn check for a
// runner that takes input mid-turn. A closed gate writes nothing, and the next
// poll tries again.
func (d *Service) CyclePrompt(taskID string, gen uint64, c *NewContext) error {
	run := d.Sup.Get(taskID)
	if run == nil {
		return nil
	}
	mid := d.MidTurnInputFor(taskID)
	gone, turn := false, 0
	ok := func() bool {
		if !d.Nctx.Mine(taskID, gen) {
			gone = true
			return false
		}
		if d.Act.DialogOpen(taskID) {
			return false
		}
		// Read before the write: the prompt can start its turn before the Enter
		// that follows it returns. Mid-turn, the prompt belongs to the turn going;
		// between turns, to the one it starts.
		busy := d.NcBusy(taskID)
		turn = d.Act.TurnsBegun(taskID)
		if !busy {
			turn++
		}
		return mid || !busy
	}
	text := NewContextLimitPrompt(c.File, d.Hk.AtriumBinary(), d.RunnerHasNoShell(taskID), c.Tokens, c.Limit)
	d.Nctx.SetTyping(taskID, gen, true)
	wrote, err := d.TypeLabelledGuarded(run, taskID, NewContextLabel, text, ok)
	if gone {
		d.Nctx.SetTyping(taskID, gen, false)
		return ErrNewContextGone
	}
	if err != nil || !wrote {
		d.Nctx.SetTyping(taskID, gen, false)
		return err
	}
	d.Nctx.NotePrompt(taskID, gen, turn, cycleTimeNow())
	d.Nctx.SetTyping(taskID, gen, false)
	if err := d.St.AppendEvent(taskID, store.EventPrompted, map[string]any{
		"text": text, "via": "terminal", "from": NewContextBy, "kind": store.PromptFYI,
	}); err != nil {
		log.Printf("[atrium] could not record the limit prompt on %s: %v", taskID, err)
	}
	d.Publish(taskID)
	return nil
}

// ncCapture is the capture half of the sequence: the prompt typed between turns,
// the turn it starts waited out, and the card's own file checked. It returns the
// step that stopped it and why. The idle parking runs this alone, with no clear
// after it, so a card is asked to write its handoff without losing its context.
func (d *Service) NcCapture(taskID string, gen uint64, file string) (string, error) {
	if err := d.NcAwaitArm(taskID, gen, d.Hk.NcTiming.CaptureEnd); err != nil {
		return "waiting for the input line to clear", err
	}
	started := time.Now()
	token := CaptureToken(gen)
	// NOTHING IS TYPED WHILE THE CARD IS RUNNING (r-022). `ncType` waits for the
	// card's status to leave running as well as for its activity to go idle. A
	// cycle armed while the card was running used to type the capture into a turn
	// that had ended on background work, then take that turn's Stop as the
	// capture's and fail in seconds with "not updated by the capture turn" (@ui,
	// twice). With the card between turns when the prompt is typed, the count
	// taken here is before the capture's own turn and after every other.
	turns := d.Act.TurnsBegun(taskID)
	if err := d.NcType(taskID, gen, NewContextLabel, NewContextCaptureText(file, token), d.Hk.NcTiming.CaptureEnd); err != nil {
		return "could not type the capture prompt", err
	}
	err := d.NcWait(taskID, gen, d.Hk.NcTiming.CaptureBegin, "the capture prompt to start a turn",
		func() (bool, error) { return d.Act.TurnsBegun(taskID) > turns, nil })
	if err != nil {
		return "the capture prompt did nothing", err
	}
	quiet := time.Time{}
	err = d.NcWait(taskID, gen, d.Hk.NcTiming.CaptureEnd, "the capture turn to end", func() (bool, error) {
		if d.Act.MidTurn(taskID) || d.Act.OnSubagents(taskID) || d.CardRunning(taskID) {
			quiet = time.Time{}
			return false, nil
		}
		if quiet.IsZero() {
			quiet = time.Now()
		}
		return time.Since(quiet) >= d.Hk.NcTiming.TurnSettle, nil
	})
	if err != nil {
		return "the capture did not finish", err
	}
	if err := d.HandoffWritten(taskID, file, started, token); err != nil {
		return "nothing cleared", err
	}
	return "", nil
}

// cardRunning is whether the card's own status says a turn is going (r-022). The
// activity alone is not enough: a turn that ended on background work reads idle
// while the card stays running, and the next turn starts when that work
// reports. A capture typed into that gap is merged into the turn that follows,
// whose Stop then reads as the capture's.
func (d *Service) CardRunning(taskID string) bool {
	t, err := d.St.Get(taskID)
	return err == nil && t != nil && t.Status == store.StatusRunning
}

// ncBusy is whether the card is inside a turn as far as typing is concerned: the
// activity says so or the status says running, and the screen does not show a
// settled prompt. A card whose Stop never landed, or whose daemon restarted
// under a status of running, reads busy to both and will never end a turn it is
// not taking, so the step waited for a turn end that could not come. The screen
// is the tiebreak, the same signature `watchLooksIdle` flags on.
func (d *Service) NcBusy(taskID string) bool {
	if !d.Act.MidTurn(taskID) && !d.CardRunning(taskID) {
		return false
	}
	return !d.AtIdlePrompt(taskID)
}

// atIdlePrompt is whether a claude card is flagged looks-idle or shows a settled
// prompt on a pty silent for shared.LooksIdleAfter, with no dialog and no subagents.
func (d *Service) AtIdlePrompt(taskID string) bool {
	run := d.Sup.Get(taskID)
	if run == nil || d.Act.DialogOpen(taskID) || d.Act.OnSubagents(taskID) {
		return false
	}
	if d.Act.LooksIdle(taskID) {
		return true
	}
	if t, err := d.St.Get(taskID); err != nil || t == nil || !strings.EqualFold(t.Runner, "claude") {
		return false
	}
	if time.Since(run.LastOutputAt()) < shared.LooksIdleAfter {
		return false
	}
	return run.LooksIdleFrame()
}

// ncArm arms gen's run if the operator's line gate is open now, and reports whether it is armed. Judged under the
// input lock, so an operator key already written has closed the gate first.
func (d *Service) NcArm(taskID string, gen uint64) bool {
	run := d.Sup.Get(taskID)
	if run == nil {
		return false
	}
	armed, changed := false, false
	run.WhileGateOpen(func() { armed, changed = d.Nctx.Arm(taskID, gen) })
	if changed {
		log.Printf("[atrium] new context on %s: the input line is clear, holding input", taskID)
		d.Publish(taskID)
	}
	return armed
}

// ncAwaitArm waits for ncArm, up to limit.
func (d *Service) NcAwaitArm(taskID string, gen uint64, limit time.Duration) error {
	return d.NcWait(taskID, gen, limit, "the input line to clear", func() (bool, error) {
		return d.NcArm(taskID, gen), nil
	})
}

// maxClearLine bounds the backspaces one clear-line frame can type.
const MaxClearLine = 64 << 10

// ncClearLine takes n characters, which the board moved into its compose box when the hold began, off the terminal's
// input line, so they are neither left behind nor sent twice. Only while the hold is on, and never while atrium is
// typing: judged under the input lock, so its own write cannot be under way.
func (d *Service) NcClearLine(taskID string, n int) bool {
	run := d.Sup.Get(taskID)
	if run == nil || n <= 0 || !d.Nctx.TypingHeld(taskID) {
		return false
	}
	bs := bytes.Repeat([]byte{0x7f}, min(n, MaxClearLine))
	return run.TypeOperatorBytes(bs, func() bool { return !d.Nctx.TypingNow(taskID) })
}

// ncWait polls cond until it is true, the run is replaced, the terminal goes, or
// the time is up.
func (d *Service) NcWait(taskID string, gen uint64, limit time.Duration, what string, cond func() (bool, error)) error {
	deadline := time.Now().Add(limit)
	t := time.NewTicker(d.Hk.NcTiming.Poll)
	defer t.Stop()
	for {
		if !d.Nctx.Mine(taskID, gen) {
			return ErrNewContextGone
		}
		if d.Sup.Get(taskID) == nil {
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
		case <-d.Nctx.Stop:
			return ErrNewContextGone
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
func (d *Service) NcType(taskID string, gen uint64, label, text string, limit time.Duration) error {
	return d.NcTypeBusy(taskID, gen, label, text, limit, d.NcBusy)
}

// ncBusyAfterAck is ncBusy for `/clear`, typed once the agent has run `atrium ready`
// and been told to end its turn. Only the activity counts, with the settled screen as
// the tiebreak for a lost Stop: the Stop hook has landed when it reads idle. The card's status and the subagent tally do not, because a
// turn that ended with subagents or background work out leaves the status on running
// and the screen unsettled for as long as they last, or for ever when a SubagentStop
// hook was lost. That held `/clear` back with no end (r-new-context-stuck-at-clearing),
// and the context it clears is the one that work was reporting into anyway.
func (d *Service) NcBusyAfterAck(taskID string) bool {
	if !d.Act.MidTurn(taskID) {
		return false
	}
	// A long silence of hook events is a turn over whose Stop was lost. A background shell of the session keeps the
	// screen redrawing, so the settled screen never says it (r-new-context-never-clears).
	if q := d.Hk.NcTiming.AckQuiet; q > 0 && d.Act.QuietFor(taskID) >= q {
		return false
	}
	return !d.AtIdlePrompt(taskID)
}

// ncBlocker says what keeps a step from typing now, for the log and the card's reason.
func (d *Service) NcBlocker(taskID string, busy func(string) bool) string {
	switch {
	case d.Sup.Get(taskID) == nil:
		return "no terminal"
	case d.Act.DialogOpen(taskID):
		return "a dialog is open on the screen"
	case busy(taskID):
		what, _ := d.Act.Describe(taskID, time.Now())
		return "a turn is in progress (activity " + what + ")"
	case d.Act.SinceBusy(taskID) < d.Hk.NcTiming.TurnSettle:
		return "a prompt arrived moments ago"
	}
	return "the input line is not clear or the keyboard is not quiet"
}

// ncTypeBusy is ncType with the question of what counts as a turn in progress given.
// It logs what it waits on every ncTiming.logEvery, and names it in the reason it
// gives up with.
func (d *Service) NcTypeBusy(taskID string, gen uint64, label, text string, limit time.Duration, busy func(string) bool) error {
	began, logged := time.Now(), time.Now()
	err := d.NcWait(taskID, gen, limit, "an empty line and no turn in progress", func() (bool, error) {
		// The card says what `/clear` waits on, so a stall is read there and not only in the log.
		if label == "" && text == NewContextClearCommand && time.Since(began) >= d.Hk.NcTiming.NoteAfter {
			if d.Nctx.SetWaiting(taskID, gen, "waiting to type /clear: "+d.NcBlocker(taskID, busy)) {
				d.Publish(taskID)
			}
		}
		if time.Since(logged) >= d.Hk.NcTiming.LogEvery {
			logged = time.Now()
			log.Printf("[atrium] new context on %s still waiting to type %q after %s: %s", taskID, text,
				time.Since(began).Round(time.Second), d.NcBlocker(taskID, busy))
		}
		run := d.Sup.Get(taskID)
		if run == nil || d.Act.DialogOpen(taskID) || busy(taskID) {
			return false, nil
		}
		gone := false
		quiet := func() bool {
			// Checked at the last moment: a dismiss between the poll and the
			// write must not be typed after.
			if !d.Nctx.Mine(taskID, gen) {
				gone = true
				return false
			}
			return !d.Act.DialogOpen(taskID) && !busy(taskID) &&
				d.Act.SinceBusy(taskID) >= d.Hk.NcTiming.TurnSettle
		}
		d.Nctx.SetWaiting(taskID, gen, "")
		d.Nctx.SetTyping(taskID, gen, true)
		wrote, err := d.TypeLabelledGuarded(run, taskID, label, text, quiet)
		d.Nctx.SetTyping(taskID, gen, false)
		if gone {
			return false, ErrNewContextGone
		}
		if err != nil || !wrote {
			return false, err
		}
		if err := d.St.AppendEvent(taskID, store.EventPrompted, map[string]any{
			"text": text, "via": "terminal", "from": NewContextBy, "kind": store.PromptFYI,
		}); err != nil {
			log.Printf("[atrium] could not record the new context prompt on %s: %v", taskID, err)
		}
		return true, nil
	})
	if err != nil && !errors.Is(err, ErrNewContextGone) {
		return fmt.Errorf("%w; blocked by: %s", err, d.NcBlocker(taskID, busy))
	}
	return err
}

// handoffDir is the card's directory, or "" when it cannot be read from here,
// which is a card whose files this daemon has no way to look at. The capture
// then trusts the turn ending as its only signal.
func (d *Service) HandoffDir(taskID string) (string, error) {
	task, err := d.St.Get(taskID)
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
func (d *Service) HandoffWritten(taskID, file string, since time.Time, token string) error {
	dir, err := d.HandoffDir(taskID)
	if err != nil || dir == "" {
		return err
	}
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		return fmt.Errorf("the capture turn ended without %s in %s, so the card should have written it and added the line %s",
			file, dir, CaptureLine(token))
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("could not read %s in %s: %v", file, dir, err)
	}
	if info.Size() < HandoffFloor {
		return fmt.Errorf("%s in %s is only %d bytes, so the card should have written its whole handoff, at least %d bytes",
			file, dir, info.Size(), HandoffFloor)
	}
	// A little slack for file systems that round a modified time down.
	if !info.ModTime().Before(since.Add(-2 * time.Second)) {
		return nil
	}
	head := make([]byte, HandoffHead)
	n, _ := io.ReadFull(f, head)
	if bytes.Contains(head[:n], []byte(CaptureLine(token))) {
		return nil
	}
	return fmt.Errorf("%s in %s is older than the capture and lacks the line %s in its first 4 KB, so the card should have added it",
		file, dir, CaptureLine(token))
}

// handleNewContext is `/v1/tasks/{id}/new-context`: POST starts the sequence,
// DELETE takes the chip off and stops a run, and GET reads it.
func (d *Service) HandleNewContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := d.St.Get(id); err != nil {
		small.WriteJSONErr(w, http.StatusNotFound, small.ErrString("no card "+id))
		return
	}
	switch r.Method {
	case http.MethodGet:
		NcJSON(w, http.StatusOK, map[string]any{"card": id, "new_context": d.NewContextFor(id)})
	case http.MethodDelete:
		gone := d.Nctx.Clear(id)
		d.Publish(id)
		d.ReleaseHeld(id)
		NcJSON(w, http.StatusOK, map[string]any{"card": id, "cleared": gone})
	case http.MethodPost:
		switch err := d.StartNewContext(id); {
		case errors.Is(err, ErrNoTerminal), errors.Is(err, ErrNewContextBusy), IsSharedErr(err):
			small.WriteJSONErr(w, http.StatusConflict, err)
		case err != nil:
			small.WriteJSONErr(w, http.StatusInternalServerError, err)
		default:
			NcJSON(w, http.StatusAccepted, map[string]any{
				"card": id, "started": true, "new_context": d.NewContextFor(id),
			})
		}
	default:
		small.WriteJSONErr(w, http.StatusMethodNotAllowed, small.ErrString("GET, POST or DELETE"))
	}
}

func NcJSON(w http.ResponseWriter, code int, v any) { small.WriteJSON(w, code, v) }

// newContextHooks are the seams of the new-context and restart-wrap sequences.
type NewContextHooks struct {
	NcTiming       NcTimings
	WrapTiming     WrapTimings
	WrapJoinMargin time.Duration
	AtriumBinary   func() string
}

func DefaultNewContextHooks() NewContextHooks {
	return NewContextHooks{
		NcTiming:       DefaultNCTiming(),
		WrapTiming:     WrapTimings{Poll: 250 * time.Millisecond},
		WrapJoinMargin: WrapJoinMargin,
		AtriumBinary:   AtriumBinary,
	}
}

// BusyCard is a card inside a new-context cycle that has not ended, or "".
func (n *NewContexts) BusyCard() string {
	if n == nil {
		return ""
	}
	n.Mu.Lock()
	defer n.Mu.Unlock()
	for id, cur := range n.By {
		if cur != nil && cur.Step != NewContextFailed {
			return id
		}
	}
	return ""
}
