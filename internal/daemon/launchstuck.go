package daemon

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A claude card stuck on something only a person can answer, found from its
// terminal. Three escalation sources, all on the one Escalation the board draws:
//
//   - launch-idle: no hook has fired since the runner started, past a grace.
//   - launch-prompt: no hook yet, and the screen shows a prompt claude draws
//     before its first turn (folder trust, login, update), or a menu nobody
//     expected.
//   - terminal-menu: at any point in a card's life, a selection menu on the
//     screen with the pty gone quiet. The trigger was a director that sat about
//     3.8 hours on "Model switch" while nobody told the operator.
//
// ALL OF IT IS DERIVED EACH TICK, never stored: the first hook after the
// launch, or the menu being answered, ends it with nothing to clear.
//
// THE SCAN COSTS NOTHING ON A BUSY CARD. A pty that spoke recently is not
// looked at (one atomic read), and a quiet one is rendered once per distinct
// last-output time, then answered from the memo until it speaks again.
var (
	// LaunchIdleAfter is the grace before a card nobody has heard from is called
	// idle since launch.
	LaunchIdleAfter = envDuration("ATRIUM_A2A_LAUNCH_IDLE", 60*time.Second)
	// LaunchPromptAfter is how long a pty must be quiet before a launch prompt
	// is believed. A trust dialog appears at once and does not move.
	LaunchPromptAfter = envDuration("ATRIUM_A2A_LAUNCH_PROMPT", 5*time.Second)
	// TerminalMenuAfter is how long a menu must sit, with the pty quiet, before
	// it is a blocker. Also the quiet launch-idle asks for.
	TerminalMenuAfter = envDuration("ATRIUM_A2A_TERMINAL_MENU", 30*time.Second)
)

// Escalation sources for a card stuck at its terminal.
const (
	NoticeLaunchIdle   = "launch-idle"
	NoticeLaunchPrompt = "launch-prompt"
	NoticeTerminalMenu = "terminal-menu"
)

// Escalation.Prompt for a launch-prompt: which prompt it is. `other` is a menu
// nobody knows, and its title is the escalation's Text. A terminal-menu says
// model-switch or other.
const (
	PromptFolderTrust = "folder-trust"
	PromptLogin       = "login"
	PromptUpdate      = "update"
	PromptModelSwitch = "model-switch"
	PromptOther       = "other"
)

// menuFooter is the marker every Claude Code selection menu ends on, lower case.
const menuFooter = "enter to select · ↑/↓ to navigate"

// menuTailBytes is how much of the ring is rendered to read a menu: a screen,
// not the scrollback.
const menuTailBytes = 32 << 10

// menuScanLines bounds how far up from the footer a menu's title is looked for.
const menuScanLines = 24

// termHit is what a rendered frame showed.
type termHit struct {
	prompt string // one of the Prompt constants, "" for none
	menu   bool   // a selection menu footer is on screen
	title  string // the menu's title line, or the matched launch text
}

type scanMemo struct {
	outAt int64
	hit   termHit
}

// termScans memoizes the last frame read per card.
type termScans struct {
	mu sync.Mutex
	by map[string]scanMemo
}

func (m *termScans) get(id string, outAt int64) (termHit, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.by[id]
	return e.hit, ok && e.outAt == outAt
}

func (m *termScans) put(id string, outAt int64, h termHit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.by == nil {
		m.by = map[string]scanMemo{}
	}
	m.by[id] = scanMemo{outAt, h}
}

func (m *termScans) forgetExcept(live map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.by {
		if !live[id] {
			delete(m.by, id)
		}
	}
}

// heardSince reports whether any hook has spoken for this card since its runner
// started: a tool or stop hook recorded after it, or any activity in memory.
func (d *Daemon) heardSince(t *store.Task, started time.Time) bool {
	if t.ToolHookSeenAt != nil && t.ToolHookSeenAt.After(started) {
		return true
	}
	if t.StopHookSeenAt != nil && t.StopHookSeenAt.After(started) {
		return true
	}
	return d.act.heard(t.ID)
}

// neverHeard is a claude card with a live runner no hook has spoken for since
// it started. Such a card has not "stopped without reporting": it never began.
// False when there is no runner, or one with no recorded start.
func (d *Daemon) neverHeard(t *store.Task) bool {
	if !strings.EqualFold(t.Runner, "claude") {
		return false
	}
	run := d.sup.get(t.ID)
	if run == nil || run.started.IsZero() {
		return false
	}
	return !d.heardSince(t, run.started)
}

// heard is whether any hook has set an activity for the card, or its runner's
// session hook has spoken.
func (a *activityTracker) heard(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	return a.sessions[taskID] || (cur != nil && cur.What != "")
}

// sessionSpoke records that the card's runner's own session hook has spoken.
func (a *activityTracker) sessionSpoke(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions == nil {
		a.sessions = map[string]bool{}
	}
	a.sessions[taskID] = true
}

// launchStuck works out one claude card's terminal escalation, or nil.
func (d *Daemon) launchStuck(t *store.Task, now time.Time) *Escalation {
	if !strings.EqualFold(t.Runner, "claude") {
		return nil
	}
	run := d.sup.get(t.ID)
	if run == nil || run.buf == nil || run.started.IsZero() {
		return nil
	}
	who := t.DisplayTitle()
	outAt := run.lastOutputAt()
	silent := now.Sub(outAt)
	mkEsc := func(source, prompt string, since time.Time, text string) *Escalation {
		return &Escalation{
			Source: source, Prompt: prompt, Since: since, Count: escalationStep(now.Sub(since)),
			Minutes: int(now.Sub(since) / time.Minute), Text: text,
		}
	}
	if d.heardSince(t, run.started) {
		// A permission atrium holds, and a dialog a Notification raised, are
		// already on the card in their own words.
		if t.Status == store.StatusNeedsPermission || d.act.dialogOpen(t.ID) || d.hasPendingPermission(t.ID) {
			return nil
		}
		if silent < TerminalMenuAfter {
			return nil
		}
		hit := d.scanTerminal(t.ID, run, outAt)
		if !hit.menu {
			return nil
		}
		// A MENU WAITING FOR AN ANSWER IS NOT A FINISHED TURN: when the turn has
		// ended and a report is owed, that is a silent stop, and it says so.
		if _, silent := d.stoppedSilently(t); silent {
			return nil
		}
		kind := PromptOther
		if strings.Contains(strings.ToLower(hit.title), "model switch") {
			kind = PromptModelSwitch
		}
		since := outAt
		if since.Before(run.started) {
			since = run.started
		}
		return mkEsc(NoticeTerminalMenu, kind, since,
			fmt.Sprintf("%s is STUCK at a menu: %s", who, hit.title))
	}
	if silent >= LaunchPromptAfter {
		if hit := d.scanTerminal(t.ID, run, outAt); hit.prompt != "" {
			text := fmt.Sprintf("%s is STUCK at the %s prompt", who, hit.prompt)
			if hit.prompt == PromptOther {
				text = fmt.Sprintf("%s is STUCK at a prompt: %s", who, hit.title)
			}
			since := outAt
			if since.Before(run.started) {
				since = run.started
			}
			return mkEsc(NoticeLaunchPrompt, hit.prompt, since, text)
		}
	}
	// Hooks are only expected of a card an agent launched: a hand-started session
	// with no atrium hooks is healthy and silent.
	if d.reportsToLauncher(t) && now.Sub(run.started) >= LaunchIdleAfter && silent >= TerminalMenuAfter {
		since := run.started.Add(LaunchIdleAfter)
		mins := int(now.Sub(run.started) / time.Minute)
		e := mkEsc(NoticeLaunchIdle, "", since, fmt.Sprintf("%s: no activity since launch, %d min", who, mins))
		e.Minutes = mins
		return e
	}
	return nil
}

// scanTerminal renders the card's last screen once per distinct last-output time.
func (d *Daemon) scanTerminal(id string, run *runner, outAt time.Time) termHit {
	key := outAt.UnixNano()
	if h, ok := d.scans.get(id, key); ok {
		return h
	}
	cols, rows := run.buf.CurrentSize()
	sc := newScreenSized(cols, rows)
	sc.apply(run.buf.Tail(menuTailBytes))
	h := readFrame(frameText(sc))
	d.scans.put(id, key, h)
	return h
}

// readFrame looks at rendered screen text for a launch prompt and a menu.
func readFrame(text string) termHit {
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimRight(ln, " \r\t"); ln != "" {
			lines = append(lines, ln)
		}
	}
	h := termHit{}
	// THE FOOTER IS ANCHORED to the last two lines of the screen, and is the whole
	// marker in one piece. A real menu draws it there, with no input box below it,
	// so the same words in a grep or a doc higher up the screen are only text.
	foot := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-2; i-- {
		if strings.Contains(strings.ToLower(lines[i]), menuFooter) {
			foot = i
			break
		}
	}
	if foot >= 0 {
		h.menu = true
		h.title = menuTitle(lines[:foot])
	}
	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "trust this folder") || strings.Contains(low, "do you trust the files"):
		h.prompt, h.title = PromptFolderTrust, "folder trust"
	case strings.Contains(low, "select login method") || strings.Contains(low, "paste code here") ||
		strings.Contains(low, "browser didn't open"):
		h.prompt, h.title = PromptLogin, "login"
	// An update BANNER is not a prompt: only a dialog waiting for an answer is.
	case h.menu && strings.Contains(low, "update available"):
		h.prompt, h.title = PromptUpdate, "update"
	case h.menu:
		h.prompt = PromptOther
	}
	return h
}

// menuTitle is the first line of the block above a menu's footer: the lines back
// to the nearest rule, or at most menuScanLines, with box-drawing stripped.
func menuTitle(above []string) string {
	start := len(above) - 1
	for ; start >= 0 && len(above)-start <= menuScanLines; start-- {
		if isRule(above[start]) {
			break
		}
	}
	for i := start + 1; i < len(above); i++ {
		s := strings.Trim(above[i], " │┃╭╮╰╯─━|\t")
		if s != "" {
			return s
		}
	}
	return "a selection menu"
}
