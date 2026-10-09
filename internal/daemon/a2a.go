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
	"strconv"
)

// Work handed from one session to another, and making sure nobody is left
// waiting on it without being told.
//
// See docs/runtime/a2a-reliability-design.md for the whole argument. The short form:
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
	// NoticeLongTurn is one turn running past the long-turn setting. See
	// longTurn.
	NoticeLongTurn = "long-turn"
	// NoticeStuckWake is the one notice typed to a launcher that holds its notices, when a
	// card it launched has been stuck past the second step of the backoff. See wakeLauncher.
	NoticeStuckWake = "stuck-wake"
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
	// BackgroundHoldMax is how long background work named by a Stop holds the
	// silent-stop alert. A build finishes and wakes the session well inside it.
	// A dev server left running never does, and must not hide a real stop forever.
	BackgroundHoldMax = envDuration("ATRIUM_A2A_BACKGROUND_HOLD", 2*time.Hour)
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

// DirectorTag marks a resident session that launches workers and waits on them.
const DirectorTag = "atrium:director"

// hasOutstandingWorker reports whether any card launched by this one is still
// around: a live runner in any status, `done` at its prompt included. A culled
// or dead worker is not outstanding.
func (d *Daemon) hasOutstandingWorker(launcherID string) bool {
	ids, err := d.st.WorkerIDs(launcherID)
	if err != nil {
		return false
	}
	for _, id := range ids {
		if d.workerOutstanding(id) {
			return true
		}
	}
	return false
}

// workerOutstanding is the one place that says what still counts as a worker
// being around. A parked worker counts: it is idle, not finished, and its
// launcher is still waiting on it.
func (d *Daemon) workerOutstanding(id string) bool {
	if d.sup.get(id) != nil {
		return true
	}
	t, err := d.st.Get(id)
	return err == nil && isParked(t)
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
// room's own binary was the wrong answer while rooms ran `atrium2`, which had
// no `turn` subcommand, and a working hook's program is still the surer one.
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
	// THE ONE PLACE A DELIVERY FINDS ITS LAUNCHER, so the stored `report_to` is
	// resolved again here: a report, a stop notice and a context notice all come
	// through. The stored id below is the fallback. See reportto.go.
	if t := d.currentLauncher(worker); t != nil {
		return t
	}
	if worker.SpawnedByID != "" {
		if t, err := d.st.Get(worker.SpawnedByID); err == nil {
			return d.launcherAfterMove(worker, t)
		}
	}
	if t, err := d.st.GetByWireName(d.st.Qualify(worker.SpawnedBy)); err == nil && t.ID != worker.ID {
		return d.launcherAfterMove(worker, t)
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
		return d.notifyRemoteLauncher(worker, source, key, body)
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
	if holdsNotices(launcher) {
		d.holdNotice(launcher, worker, source, text)
		return true
	}
	typed, err := d.deliverPeerAs(store.DeliveryReport, launcher, worker.WireName, text)
	if err != nil {
		log.Printf("[atrium] could not tell %s about %s: %v", launcher.DisplayTitle(), worker.DisplayTitle(), err)
		return false
	}
	log.Printf("[atrium] told %s that %s: %s (typed %v)", launcher.DisplayTitle(), worker.DisplayTitle(), source, typed)
	return true
}

// HoldNoticesTag puts a launcher's automatic notices on its card instead of in its
// terminal. The orchestrator's rule, for any card that wants it without the rest
// of what OrchestratorTag means.
const HoldNoticesTag = "atrium:hold-notices"

// holdsNotices reports whether a launcher reads its automatic notices when it
// asks, rather than having them typed.
//
// THE ORCHESTRATOR'S TERMINAL IS WHERE THE OPERATOR READS. A silent stop, a
// context size or a session that ended, typed there, interrupts him, and on a
// board of resident directors that report elsewhere every turn end is one. So
// they go on the launcher's card, on the worker's work item and on the board's
// bell, and `atrium_task` with `notices` reads them back in one call.
func holdsNotices(launcher *store.Task) bool {
	return launcher != nil && (hasTag(launcher.Tags, OrchestratorTag) || hasTag(launcher.Tags, HoldNoticesTag))
}

// holdsReports reports whether a launcher's workers' reports are held the same
// way. Only HoldNoticesTag does it: a card that asked for its terminal to be left
// alone meant reports too, but OrchestratorTag alone keeps today's delivery.
func holdsReports(launcher *store.Task) bool {
	return launcher != nil && hasTag(launcher.Tags, HoldNoticesTag)
}

// holdNotice records one automatic notice where a launcher that holds them reads
// it. Never fails its caller, the posture of notifyLauncher.
func (d *Daemon) holdNotice(launcher, worker *store.Task, source, text string) {
	ev := store.HeldNoticePayload(source, worker.WireName, worker.ID, text)
	if err := d.st.AppendEvent(launcher.ID, store.EventNotified, ev); err != nil {
		log.Printf("[atrium] could not hold a %s notice for %s: %v", source, launcher.DisplayTitle(), err)
		return
	}
	if err := d.st.LogWorkAtrium(worker.ID, source+" notice held for "+launcher.WireName+": "+text); err != nil {
		log.Printf("[atrium] could not put the %s notice on %s's work item: %v", source, worker.DisplayTitle(), err)
	}
	d.ringHeld(launcher.ID, worker.ID, source, text)
	log.Printf("[atrium] held for %s that %s: %s", launcher.DisplayTitle(), worker.DisplayTitle(), source)
}

// ringHeld is the board's bell for a held notice: one `notice` event, with the
// words the board shows. See docs/changes/r-hold-notices.md for what the board
// does with it.
func (d *Daemon) ringHeld(launcherID, workerID, source, text string) {
	d.publishTask(launcherID)
	d.ap.Broadcast("notice", map[string]any{
		"task_id": launcherID, "about_card": workerID, "source": source, "toast": text,
	})
}

// notifyRemoteLauncher is notifyLauncher for a launcher on another room. The
// same once-per-event dedupe, then HELD in the relay outbox and sent from
// there, so a launcher whose room is offline hears it when it is back. See
// relay.go.
func (d *Daemon) notifyRemoteLauncher(worker *store.Task, source, key, body string) bool {
	spec := d.launcherRelay(worker, body)
	if spec == nil {
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
	if _, err := d.holdRelay(worker, spec.FromWire, spec.ToName, spec.ToRoom, spec.ToCard, spec.Text, "", "",
		store.RelaySourceNotice); err != nil {
		log.Printf("[atrium] could not hold a notice to %s@%s about %s: %v", spec.ToName, spec.ToRoom,
			worker.DisplayTitle(), err)
		return false
	}
	log.Printf("[atrium] told %s@%s that %s: %s (by way of the hub)", spec.ToName, spec.ToRoom,
		worker.DisplayTitle(), source)
	return true
}

// peerSaid records that a session said something to another, which counts
// as a report when the other is its launcher. Called by the doors a model
// sends through, never by notifyLauncher: a notice atrium wrote about a
// worker is not the worker reporting.
//
// It also puts the words on the work they are about: a worker's message to its
// launcher on the worker's item, and the launcher's to the worker likewise, so
// what used to scroll away in a terminal is on record. Never moves the work: free
// text cannot be checked. A failure is logged, the posture of a hook.
func (d *Daemon) peerSaid(from string, target *store.Task, text, kind string) {
	from = strings.TrimSpace(from)
	if from == "" || target == nil {
		return
	}
	sender, err := d.st.GetByWireName(d.st.Qualify(from))
	if err != nil {
		return
	}
	if _, err := d.st.LogWorkMessage(sender.ID, target.ID, sender.WireName, text); err != nil {
		log.Printf("[atrium] could not put %s's message to %s on the work ledger: %v",
			sender.DisplayTitle(), target.DisplayTitle(), err)
	}
	d.owedSaid(sender, target, kind, text)
	if !sender.Launched() {
		return
	}
	d.currentLauncher(sender)
	if sender.SpawnedByID != target.ID && d.st.Qualify(sender.SpawnedBy) != target.WireName {
		return
	}
	if err := d.st.MarkReported(sender.ID); err != nil {
		log.Printf("[atrium] could not record that %s reported: %v", sender.DisplayTitle(), err)
	}
	d.doneBySay(sender, text)
}

// ── silent stops ────────────────────────────────────────────────────────────────

// silentNudgeText is what atrium says to a worker whose first turn ended silently. The same one
// instruction the launch prompt ends with (link.reportLine): atrium_done or atrium_blocked, and wait for every
// command. Never atrium_report, which a launcher's brief may forbid because a done report closes the card.
const silentNudgeText = "Your turn ended without telling your launcher anything. End with atrium_done if the work is " +
	"finished, or atrium_blocked if something stops you. Wait for every command in the same turn: never end a turn on a " +
	"background command. Otherwise carry on with your brief."

// NoticeSilentNudge is the claim that the worker was nudged for one launcher prompt.
const NoticeSilentNudge = "silent-nudge"

// silentStop deals with a worker that ended its turn without saying anything.
// Called as the turn ends, and by the watchdog for a session that has no Stop
// hook. Returns whether the launcher was told.
//
// THE FIRST SILENT STOP GOES TO THE WORKER, not the launcher: atrium says a
// fixed line to it, immediately, so the launcher spends no turn on it. Only a
// turn that ends silently AFTER that nudge reaches the launcher, with the nudge
// named. One nudge per launcher prompt, never a loop.
func (d *Daemon) silentStop(taskID string) bool {
	t, err := d.st.Get(taskID)
	if err != nil || !d.reportsToLauncher(t) {
		return false
	}
	ended, ok := d.stoppedSilently(t)
	if !ok {
		return false
	}
	// A CARD WAITING ON ITS HUMAN IS NOT STALLED. An unanswered question to the operator is the turn's point,
	// and a card that already said done or blocked since its launcher's prompt has reported.
	if sn, err := d.st.GetSeen(t.ID); err == nil && sn.View().OpenCount() != 0 {
		return false
	}
	if t.OwedAt != nil && d.st.ReportedSince(t.ID, *t.OwedAt) {
		return false
	}
	key := t.PromptKey()
	nudgedAt, nudged := d.st.NoticeAt(t.ID, NoticeSilentNudge, key)
	if !nudged {
		fresh, err := d.st.RecordNotice(t.ID, NoticeSilentNudge, key)
		if err != nil {
			log.Printf("[atrium] could not record a nudge for %s: %v", t.DisplayTitle(), err)
		} else if fresh {
			if _, err := d.deliverPeerAs(store.DeliveryNudge, t, "atrium", silentNudgeText); err != nil {
				log.Printf("[atrium] could not nudge %s: %v", t.DisplayTitle(), err)
			} else {
				log.Printf("[atrium] nudged %s: its turn ended without a report", t.DisplayTitle())
			}
		}
		return false
	}
	// The stop seen is the one the nudge answers, or older: wait for a new turn.
	if !ended.After(nudgedAt) {
		return false
	}
	body := fmt.Sprintf("%s ended its turn without reporting, twice, nudged once at %s. It has been "+
		"waiting since %s with nothing to say about the work. card %s",
		t.WireName, nudgedAt.Local().Format("15:04"), ended.Local().Format("15:04:05"), t.ID)
	return d.notifyLauncher(t, NoticeSilentStop, key, body)
}

// stoppedSilently reports whether a card is waiting after a turn that ran
// since its launcher's last prompt and said nothing to its launcher, and when
// that turn ended.
//
// THE PROMPT HAS TO BE THE LAUNCHER'S. A message from another session, or a
// turn the session's own monitor woke, creates no debt, so a resident session
// whose workers report to it is not stuck for hearing from them. The board's
// STUCK mark reads this too, on purpose: one definition of owing. See
// docs/rnd/owed-report-design.md.
//
// A TURN HAS TO HAVE RUN. Owing a report is not enough: a built-in slash
// command such as `/model` is a prompt that starts no turn, and a card a
// restart resumed onto an idle prompt has a fresh `waiting_since` and no turn
// behind it. Both read stuck when this asked only for a prompt newer than the
// last report, and the restart made it ring again from one minute. The turn's
// end is also the clock, so a restart does not restart the backoff.
//
// A RESIDENT DIRECTOR IS NOT SILENT WHILE ITS WORKERS ARE OUTSTANDING. Its
// next report is due when its batch is done, not after every worker message
// that wakes it, so ringing its launcher each time it ends a turn waiting is
// noise. Once every worker has ended it works as for any card. See
// docs/rnd/keepalive-policy-design.md section 7.
func (d *Daemon) stoppedSilently(t *store.Task) (time.Time, bool) {
	if t.Status != store.StatusNeedsInput || !t.OwesReport() {
		return time.Time{}, false
	}
	// A PARKED CARD IS NEVER SILENT: it was put down on purpose, with no process
	// to have stopped.
	if isParked(t) {
		return time.Time{}, false
	}
	// A DIRECTOR IS NOT A WORKER OWING A REPORT. It is resident: it ends a turn
	// waiting for work or for its workers, and its launcher prompting it created
	// the debt this checks. The tag is what atrium_launch sets for it. Its real
	// blockers (a menu, a stuck tool) have their own escalations.
	if hasTag(t.Tags, DirectorTag) {
		return time.Time{}, false
	}
	// A CARD NO HOOK HAS SPOKEN FOR SINCE LAUNCH never began a turn to stop. Its
	// escalation is launch-idle. See launchStuck.
	if d.neverHeard(t) {
		return time.Time{}, false
	}
	ended, err := d.st.TurnEndedAt(t.ID)
	if err != nil || ended == nil || ended.Before(*t.OwedAt) {
		return time.Time{}, false
	}
	// A turn that ended on background work is waiting, not stopped. Each of
	// those tasks wakes the session when it finishes, and the Stop after that is
	// a new turn end that carries the clock forward. See BackgroundHoldMax.
	if n, _ := d.act.backgroundWork(t.ID); n > 0 {
		return time.Time{}, false
	}
	return *ended, true
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
	// Source is `silent-stop`, `long-tool`, `long-turn`, `launch-idle`,
	// `launch-prompt` or `terminal-menu`.
	Source string `json:"source"`
	// Prompt names which prompt a launch-prompt or terminal-menu is stuck at:
	// folder-trust, login, update, model-switch or other. Empty otherwise.
	Prompt string `json:"prompt,omitempty"`
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
		var x *Escalation
		if d.reportsToLauncher(t) {
			x = d.stuckNow(t, now)
			d.wakeLauncher(t, x)
		} else {
			// A LONG TURN IS SHOWN ON EVERY CARD, clint's own included. Only a
			// card with a launcher has somebody to tell.
			if x = d.launchStuck(t, now); x == nil {
				x = d.longTurn(t, now)
			}
		}
		live[t.ID] = true
		if d.esc.put(t.ID, x) {
			d.publishTask(t.ID)
		}
	}
	d.scans.forgetExcept(live)
	for _, id := range d.esc.forgetExcept(live) {
		d.publishTask(id)
	}
	return nil
}

// The escalation step after which a stuck card wakes a launcher that holds its notices. A
// long tool call is already LongToolAfter old when it escalates, so its second step is
// the wake. A silent stop is told to the launcher at SilentStopNotifyAfter, the second
// step, so waking there would wake on every one: it waits for the fourth, ten minutes.
const (
	wakeStep       = 2
	wakeStepSilent = 4
)

// wakeLauncher types one notice to a launcher that holds them, once a card it launched has
// been stuck past its wake step. Every earlier stuck notice stays held.
// Only a silent stop or a long tool call is stuck: a long turn is a report, not an alarm.
// Once per stuck episode, keyed on when it began.
func (d *Daemon) wakeLauncher(t *store.Task, x *Escalation) {
	if x == nil {
		return
	}
	step := wakeStep
	switch x.Source {
	case NoticeSilentStop:
		step = wakeStepSilent
	case NoticeLongTool:
	default:
		return
	}
	if x.Count < step {
		return
	}
	launcher := d.launcherOf(t)
	if !holdsNotices(launcher) {
		return
	}
	fresh, err := d.st.RecordNotice(t.ID, NoticeStuckWake, x.Source+x.Since.UTC().Format(time.RFC3339Nano))
	if err != nil || !fresh {
		return
	}
	text := truncatePeer(fmt.Sprintf("%s has been stuck for %d minutes (%s). card %s", t.WireName, x.Minutes,
		x.Source, t.ID))
	if _, err := d.deliverPeerAs(store.DeliveryNotice, launcher, t.WireName, text); err != nil {
		log.Printf("[atrium] could not wake %s for stuck %s: %v", launcher.DisplayTitle(), t.DisplayTitle(), err)
	}
}

// stuckNow works out one card's escalation, telling its launcher on the way
// when the launcher has not been told yet. Nil when the card is fine.
func (d *Daemon) stuckNow(t *store.Task, now time.Time) *Escalation {
	who := t.DisplayTitle()
	if x := d.launchStuck(t, now); x != nil {
		d.notifyLauncher(t, x.Source, x.Since.UTC().Format(time.RFC3339Nano), fmt.Sprintf(
			"%s. atrium cannot answer it for the session, so this is a report. card %s", x.Text, t.ID))
		return x
	}
	if since, ok := d.stoppedSilently(t); ok {
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
	return d.longTurn(t, now)
}

// longTurnDefault is how long one turn may run before it is a long turn.
const longTurnDefault = 45 * time.Minute

// turnAfter is the long-turn setting, in minutes, empty for the default.
func (d *Daemon) turnAfter() time.Duration {
	v, err := d.st.Setting(store.SettingEscalateTurnAfter)
	if err != nil {
		return longTurnDefault
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return longTurnDefault
	}
	return time.Duration(n) * time.Minute
}

// longTurn is a running card whose turn has run past the setting: flagged on the
// card, and its launcher told once per turn with how many tool calls the last ten
// minutes held. A REPORT, NEVER AN ACTION: a long test run and a runaway look the
// same from here, and the rate is what lets whoever reads it tell them apart.
// Last in the priority, after a silent stop and one long tool call, which are the
// more specific facts.
func (d *Daemon) longTurn(t *store.Task, now time.Time) *Escalation {
	if t.Status != store.StatusRunning {
		return nil
	}
	began, ok := d.act.turnSince(t.ID)
	after := d.turnAfter()
	if !ok || now.Sub(began) < after {
		return nil
	}
	mins := int(now.Sub(began) / time.Minute)
	calls := d.act.callsLately(t.ID)
	doing := ""
	if tool, since, ok := d.act.toolSince(t.ID); ok {
		doing = fmt.Sprintf(", now in %s for %d minutes", tool, int(now.Sub(since)/time.Minute))
	}
	if d.reportsToLauncher(t) {
		// Keyed on the turn's start, so a second pass of the same turn says
		// nothing and the next turn says it again.
		d.notifyLauncher(t, NoticeLongTurn, began.UTC().Format(time.RFC3339Nano), fmt.Sprintf(
			"%s has been in one turn for %d minutes. %d tool calls in the last 10 minutes%s. a report, "+
				"not an action. card %s", t.WireName, mins, calls, doing, t.ID))
	}
	since := began.Add(after)
	return &Escalation{
		Source: NoticeLongTurn, Since: since, Count: escalationStep(now.Sub(since)), Minutes: mins,
		Text: fmt.Sprintf("%s has been in one turn for %d minutes, %d tool calls in the last 10", t.DisplayTitle(),
			mins, calls),
	}
}
