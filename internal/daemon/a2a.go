package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/claudeconf"
	"github.com/dovholuknf/atrium/internal/store"
)

// Work handed from one session to another, and making sure nobody is left
// waiting on it without being told.
//
// See docs/a2a-reliability-design.md for the whole argument. The short form:
//
//   - A card launched by another session (the `origin:agent` tag, lineage in
//     `spawned_by`) owes that session a report every turn. A structured one
//     through `atrium_report` or `atrium finish --status`, or any peer message
//     to the launcher.
//   - A turn that ends without one is a SILENT STOP. The launcher is told,
//     once per prompt, and the board is told on a widening backoff until the
//     card moves.
//   - ATRIUM NEVER FORCES A TURN. The Stop hook only reports that a turn
//     ended, and this file never answers it with a block. A forced turn costs
//     tokens every time it fires, and the operator wants atrium conservative
//     with them. So a silent stop is reported, not corrected.
//   - Automatic notices only travel upward: worker to launcher, or to the
//     board. Nothing here ever writes to a worker, so no loop of automatic
//     messages can exist.

// OriginAgentTag marks a card that `atrium_launch` created. The hub adds it
// (see link.OriginTag, the same string) and the room stores it as an ordinary
// tag. Duplicated rather than imported because neither package imports the
// other, and a test pins the two together.
const OriginAgentTag = "origin:agent"

// Notice sources. Each is one reason a launcher is told something, and each
// pairs with a key naming the one event that caused it. See RecordNotice.
const (
	NoticeReport     = "report"
	NoticeSilentStop = "silent-stop"
	NoticeLongTool   = "long-tool"
)

// The thresholds. Each has an environment override that takes a Go duration,
// and a value that does not parse is ignored, the same rule `launchCap` uses.
var (
	// SilentStopNotifyAfter is how long a stop may go unreported before the
	// watchdog tells the launcher, for a session whose Stop hook did not
	// already. With the hook the notice is sent as the turn ends.
	SilentStopNotifyAfter = envDuration("ATRIUM_A2A_SILENT_STOP", 2*time.Minute)
	// LongToolAfter is how long one tool call may run before it counts as
	// stuck. It cannot tell a hung process from a long build, so it reports and
	// never kills anything.
	LongToolAfter = envDuration("ATRIUM_A2A_LONG_TOOL", 20*time.Minute)
)

// EscalationBackoff is when the board is told again about a worker that is
// still stuck, measured from the moment it became stuck. The operator's own
// schedule. Past the last step it repeats every 24 hours. It resets when the
// card moves, because a card that moved is a new situation.
//
// The board's permission nag in notify.js follows the same schedule, so a
// permission, a silent stop and a stuck tool all ring on one rhythm.
var EscalationBackoff = []time.Duration{
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 24 * time.Hour,
}

func envDuration(name string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

// agentLaunched reports whether a card was started by `atrium_launch`.
func agentLaunched(t *store.Task) bool { return t != nil && hasTag(t.Tags, OriginAgentTag) }

// launcherFor is who to record as a launch's parent. A launch through the
// board's endpoint with no launcher named and no agent marker is the board's
// own dialog, so the human is the parent. An agent launch carries the marker,
// and one that lost its sender stays unnamed rather than being filed as the
// human's.
func launcherFor(req LaunchRequest) string {
	if by := strings.TrimSpace(req.SpawnedBy); by != "" {
		return by
	}
	if hasTag(req.Tags, OriginAgentTag) {
		return ""
	}
	return store.HumanLauncher
}

// ── the Stop hook for launched sessions ─────────────────────────────────────────

// isClaude reports whether a runner row runs Claude Code, which is the one
// runner `--settings` means anything to.
func isClaude(h *store.Harness) bool {
	if h == nil {
		return false
	}
	if strings.EqualFold(h.ID, "claude") {
		return true
	}
	leaf := strings.ToLower(filepath.Base(filepath.FromSlash(strings.TrimSpace(h.Cmd))))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		leaf = strings.TrimSuffix(leaf, ext)
	}
	return leaf == "claude"
}

// stopHookCommand is the Stop hook an agent-launched claude session should
// run, or empty when it needs nothing added.
//
// Empty when the operator's own settings already register it: Claude Code
// runs that one for every session, and adding a second copy under another
// spelling would post each turn end twice.
//
// Otherwise the program is taken from a hook the operator already has, since
// that is a binary known to run from a claude session on this machine. The
// room's own binary is the wrong answer: `atrium2` has no `turn` subcommand.
// A variable so a test can say what it wants without a settings file.
var stopHookCommand = func() string {
	rep, err := claudeconf.Inspect(daemonBinary())
	if err != nil {
		return ""
	}
	program := ""
	for _, h := range rep.Hooks {
		if h.Hook == "Stop" && h.Installed {
			return ""
		}
		if program != "" || !h.Installed {
			continue
		}
		for _, sub := range []string{" session --event ", " hook --event "} {
			if i := strings.Index(h.Found, sub); i > 0 {
				program = h.Found[:i]
				break
			}
		}
	}
	if program == "" {
		return ""
	}
	return program + " turn --event end"
}

// stopHookSettings is the `--settings` value that registers one Stop hook.
func stopHookSettings(command string) string {
	raw, _ := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"Stop": []any{map[string]any{
				"matcher": "",
				"hooks":   []any{map[string]any{"type": "command", "command": command}},
			}},
		},
	})
	return string(raw)
}

// withStopHook adds the Stop hook to an agent-launched claude session's
// arguments. Approved for these sessions only: a human's session keeps the
// opt-in `CLAUDE.md` describes, because the Stop hook is the one hook whose
// answer can change what a session does. Here it only REPORTS that the turn
// ended. The guard behind it never blocks.
//
// In front of the rest, so it can never land after the positional prompt.
func withStopHook(h *store.Harness, args []string, agent bool) []string {
	if !agent || !isClaude(h) {
		return args
	}
	cmd := stopHookCommand()
	if cmd == "" {
		return args
	}
	return append([]string{"--settings", stopHookSettings(cmd)}, args...)
}

// ── telling the launcher ────────────────────────────────────────────────────────

// launcherOf is the card that launched this one, or nil.
func (d *Daemon) launcherOf(worker *store.Task) *store.Task {
	if worker == nil || !worker.Launched() {
		return nil
	}
	if worker.SpawnedByID != "" {
		if t, err := d.st.Get(worker.SpawnedByID); err == nil {
			return t
		}
	}
	if t, err := d.st.GetByWireName(d.st.Qualify(worker.SpawnedBy)); err == nil && t.ID != worker.ID {
		return t
	}
	return nil
}

// notifyLauncher queues one automatic notice to a worker's launcher, and
// reports whether it sent it.
//
// ONCE PER (worker, source, key). The key names the event that caused it, so
// the same silent stop seen at turn end and again by every watchdog tick is
// one notice.
//
// NOT RATE LIMITED. The per-sender limit exists to stop a looping model, and
// this is not a model. Dropping a notice to the limit is the failure the
// design exists to prevent.
//
// NEVER FAILS ITS CALLER. It runs beside a report, a Stop hook and a tick, and
// none of those may fail over a side effect. Everything is logged.
func (d *Daemon) notifyLauncher(worker *store.Task, source, key, body string) bool {
	launcher := d.launcherOf(worker)
	if launcher == nil {
		return false
	}
	fresh, err := d.st.RecordNotice(worker.ID, source, key)
	if err != nil {
		log.Printf("[atrium] could not record a %s notice for %s: %v", source, worker.DisplayTitle(), err)
		return false
	}
	if !fresh {
		return false
	}
	text := body
	if len(text) > maxPeerMessage {
		text = text[:maxPeerMessage]
	}
	typed, err := d.deliverPeer(launcher, worker.WireName, text)
	if err != nil {
		log.Printf("[atrium] could not tell %s about %s: %v", launcher.DisplayTitle(), worker.DisplayTitle(), err)
		return false
	}
	log.Printf("[atrium] told %s that %s: %s (typed %v)", launcher.DisplayTitle(), worker.DisplayTitle(), source, typed)
	return true
}

// peerSaid records that a session said something to another, which counts
// as a report when the other is its launcher. Called by the doors a model
// sends through, never by notifyLauncher: a notice atrium wrote about a
// worker is not the worker reporting.
func (d *Daemon) peerSaid(from string, target *store.Task) {
	from = strings.TrimSpace(from)
	if from == "" || target == nil {
		return
	}
	sender, err := d.st.GetByWireName(d.st.Qualify(from))
	if err != nil || !sender.Launched() {
		return
	}
	if sender.SpawnedByID != target.ID && d.st.Qualify(sender.SpawnedBy) != target.WireName {
		return
	}
	if err := d.st.MarkReported(sender.ID); err != nil {
		log.Printf("[atrium] could not record that %s reported: %v", sender.DisplayTitle(), err)
	}
}

// ── silent stops ────────────────────────────────────────────────────────────────

// silentStop tells the launcher when a worker ended its turn without saying
// anything. Called as the turn ends, and by the watchdog for a session that
// has no Stop hook. Returns whether a notice went.
func (d *Daemon) silentStop(taskID string) bool {
	t, err := d.st.Get(taskID)
	if err != nil || !agentLaunched(t) || t.Status != store.StatusNeedsInput || !t.OwesReport() {
		return false
	}
	body := fmt.Sprintf("%s ended its turn without reporting. It has been waiting since %s with "+
		"nothing to say about the work. card %s",
		t.WireName, t.WaitingSinceOr(time.Now()).Local().Format("15:04:05"), t.ID)
	return d.notifyLauncher(t, NoticeSilentStop, t.PromptKey(), body)
}

// ── reachability (F15) ──────────────────────────────────────────────────────────

// How a message can reach a card.
const (
	ReachTyped       = "typed"
	ReachHook        = "hook"
	ReachUnconfirmed = "unconfirmed"
	ReachNo          = "no"
)

// runnerDelivers reports whether a runner kind has a hook that can carry a
// queued message into the model.
//
// A STAND-IN FOR `Adapter.Delivery`, which lives with the per-runner adapters
// on `claude/runner-setup` (internal/runnersetup) and moves there when that
// lands. Claude Code and codex both have a pre-tool and a Stop hook that can
// answer with text. Everything else is reachable only by typing.
func runnerDelivers(runner string) bool {
	switch strings.ToLower(strings.TrimSpace(runner)) {
	case "claude", "codex":
		return true
	}
	return false
}

// reachability says whether a message queued to a card will ever arrive, and
// if not why not.
func (d *Daemon) reachability(t *store.Task) (string, string) {
	if t.PeerTyping && d.sup.get(t.ID) != nil {
		return ReachTyped, ""
	}
	if t.ToolHookSeenAt != nil || t.StopHookSeenAt != nil {
		return ReachHook, ""
	}
	runner := t.Runner
	if runner == "" {
		runner = "its runner"
	}
	if runnerDelivers(t.Runner) {
		return ReachUnconfirmed, fmt.Sprintf("queued, but %s has never been heard from on a hook that "+
			"carries messages, so it may never arrive. its atrium hooks may not be installed. "+
			"relaunch it under atrium so the text can be typed, or ask the human to relay it.", t.WireName)
	}
	return ReachNo, fmt.Sprintf("queued, but %s has no way to receive it: %s has no atrium hook, and "+
		"atrium does not own its terminal. relaunch it under atrium so the text can be typed, or "+
		"ask the human to relay it.", t.WireName, runner)
}

// deliveredWord is what the message endpoint answers for a queued message,
// given how reachable its card is.
func deliveredWord(reach string) string {
	switch reach {
	case ReachUnconfirmed:
		return "queued-unconfirmed"
	case ReachNo:
		return "undeliverable"
	}
	return "queued"
}

// ── the watchdog and the board ──────────────────────────────────────────────────

// Escalation is a worker the board should hear about, served on the card. The
// board rings each time Count goes up.
type Escalation struct {
	// Source is `silent-stop` or `long-tool`.
	Source string `json:"source"`
	// Since is when it became stuck. The backoff counts from here.
	Since time.Time `json:"since"`
	// Count is how many steps of the backoff have passed.
	Count int `json:"count"`
	// Minutes is how long it has been stuck, for the notice.
	Minutes int `json:"minutes"`
	// Text is the notice, worded here so the board says what the room knows.
	Text string `json:"text"`
}

type escalations struct {
	mu sync.Mutex
	by map[string]*Escalation
}

func (e *escalations) get(taskID string) *Escalation {
	e.mu.Lock()
	defer e.mu.Unlock()
	if x := e.by[taskID]; x != nil {
		out := *x
		return &out
	}
	return nil
}

// put records a card's escalation and reports whether the board needs to
// hear about it again: a new one, or one whose count moved.
func (e *escalations) put(taskID string, x *Escalation) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.by == nil {
		e.by = map[string]*Escalation{}
	}
	old := e.by[taskID]
	if x == nil {
		delete(e.by, taskID)
		return old != nil
	}
	e.by[taskID] = x
	return old == nil || old.Count != x.Count || old.Source != x.Source || !old.Since.Equal(x.Since)
}

func (e *escalations) forgetExcept(live map[string]bool) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var gone []string
	for id := range e.by {
		if !live[id] {
			delete(e.by, id)
			gone = append(gone, id)
		}
	}
	return gone
}

// escalationStep is how many steps of the backoff have passed after being
// stuck for `elapsed`. Zero before the first step.
func escalationStep(elapsed time.Duration) int {
	n := 0
	for _, s := range EscalationBackoff {
		if elapsed >= s {
			n++
		}
	}
	if last := EscalationBackoff[len(EscalationBackoff)-1]; elapsed > last {
		n += int((elapsed - last) / (24 * time.Hour))
	}
	return n
}

// escalationFor adapts the table to what the api package expects, with an
// untyped nil for none, the same way activityFor does.
func (d *Daemon) escalationFor(taskID string) any {
	if x := d.esc.get(taskID); x != nil {
		return x
	}
	return nil
}

// watchWorkers is the watchdog. One pass over the live agent-launched cards,
// on the reaper's tick. It reads the store and the in-memory activity, never
// touches a runner, and only ever tells the launcher or the board.
//
// Two conditions in stage 1:
//
//   - A SILENT STOP: waiting in needs-input, owing a report. The launcher is
//     told after SilentStopNotifyAfter when the Stop hook did not already, and
//     the board from the moment it stopped.
//   - A STUCK TOOL: one tool call running past LongToolAfter. The launcher is
//     told once, and the board from that moment.
//
// A permission wait is the board's own permission nag, on the same backoff.
// A launcher never answers a worker's permission.
func (d *Daemon) watchWorkers(now time.Time) error {
	tasks, err := d.st.List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, t := range tasks {
		if !agentLaunched(t) {
			continue
		}
		live[t.ID] = true
		x := d.stuckNow(t, now)
		if d.esc.put(t.ID, x) {
			d.publishTask(t.ID)
		}
	}
	for _, id := range d.esc.forgetExcept(live) {
		d.publishTask(id)
	}
	return nil
}

// stuckNow works out one card's escalation, telling its launcher on the way
// when the launcher has not been told yet. Nil when the card is fine.
func (d *Daemon) stuckNow(t *store.Task, now time.Time) *Escalation {
	who := t.DisplayTitle()
	if t.Status == store.StatusNeedsInput && t.OwesReport() && t.WaitingSince != nil {
		since := *t.WaitingSince
		if now.Sub(since) >= SilentStopNotifyAfter {
			d.silentStop(t.ID)
		}
		mins := int(now.Sub(since) / time.Minute)
		return &Escalation{
			Source: NoticeSilentStop, Since: since, Count: escalationStep(now.Sub(since)), Minutes: mins,
			Text: fmt.Sprintf("%s is STUCK: it stopped without reporting, %d minutes", who, mins),
		}
	}
	if t.Status == store.StatusRunning {
		if tool, started, ok := d.act.toolSince(t.ID); ok && now.Sub(started) >= LongToolAfter {
			stuck := started.Add(LongToolAfter)
			mins := int(now.Sub(started) / time.Minute)
			d.notifyLauncher(t, NoticeLongTool, started.UTC().Format(time.RFC3339Nano), fmt.Sprintf(
				"%s has been in one %s call for %d minutes with no hook heard since. atrium cannot tell "+
					"a hung process from a long build, so this is a report, not an action. card %s",
				t.WireName, tool, mins, t.ID))
			return &Escalation{
				Source: NoticeLongTool, Since: stuck, Count: escalationStep(now.Sub(stuck)), Minutes: mins,
				Text: fmt.Sprintf("%s is STUCK in %s, %d minutes", who, tool, mins),
			}
		}
	}
	return nil
}
