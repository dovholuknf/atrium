// Package daemon wires the three layers together: the store owns state, the
// hub serves agents, and the api serves humans.
//
// The two listeners are separate so one can be closed without the other. When
// the store halts the agent-facing listener closes and does not reopen, so
// every runner sees connection-refused and parks on the backoff it already has,
// while the human-facing listener stays up to say what broke.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/hub"
	"github.com/dovholuknf/atrium/internal/store"
)

// Options configures a daemon.
type Options struct {
	AgentAddr string        // agent-facing listener, e.g. ":7777"
	HumanAddr string        // human-facing listener, e.g. ":7778"
	DBPath    string        // sqlite file
	LongPoll  time.Duration // agent long-poll ceiling
	// ShutdownToken guards POST /v1/shutdown. Empty means loopback only.
	// Setting one says the endpoint is meant to be reachable remotely.
	ShutdownToken string
}

// Daemon owns the store, the hub, and both listeners.
type Daemon struct {
	opts Options
	st   *store.Store
	hb   *hub.Hub
	ap   *api.Server

	// sup holds the runners atrium owns, when a harness launches in pty mode.
	sup *supervisor

	// act holds what each runner is doing right now, in memory only. See
	// docs/activity-design.md.
	act *activityTracker

	// nats holds any overlay listener the board is being served on, so it can
	// be reached from somewhere else. See overlay_native.go.
	nats   map[overlayKind]*native
	natsMu sync.Mutex

	// stop is how a shutdown request reaches the wind-down Run is waiting on.
	stop *stopper

	mu          sync.Mutex
	agentServer *http.Server
}

// DefaultDBPath puts the database next to the rest of atrium's state.
func DefaultDBPath() string {
	dir := hub.HubDir()
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".atrium")
		} else {
			dir = "."
		}
	}
	return filepath.ToSlash(filepath.Join(dir, "atrium.db"))
}

// New opens the store and builds the daemon. A storage failure here is fatal
// by design: the daemon refuses to start rather than run without durable state.
func New(opts Options) (*Daemon, error) {
	if opts.LongPoll == 0 {
		opts.LongPoll = time.Minute
	}
	if opts.DBPath == "" {
		opts.DBPath = DefaultDBPath()
	}
	if dir := filepath.Dir(opts.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	st, err := store.Open(opts.DBPath)
	if err != nil {
		return nil, err
	}
	d := &Daemon{
		opts: opts, st: st, hb: hub.New(opts.LongPoll), ap: api.New(st),
		sup: newSupervisor(), act: newActivityTracker(), stop: newStopper(),
		nats: map[overlayKind]*native{},
	}
	st.OnHalt = d.onHalt
	d.hb.Record = d.hooks()
	d.ap.Prompt = d.prompt
	d.ap.Decide = d.decide
	d.ap.Launch = d.launchFromJSON
	d.ap.Kill = d.Kill
	d.ap.RunSource = d.RunSourceNow
	d.ap.CancelPending = d.CancelPending
	d.ap.DrainAuto = d.drainForAuto
	d.ap.Attach = d.handleAttach
	d.ap.Message = d.handleMessage
	d.ap.Shutdown = d.handleShutdown
	d.ap.Shelve = d.Shelve
	d.ap.StopRunner = d.StopRunner
	d.ap.Unshelve = d.Unshelve
	d.ap.Overlays = d.overlayViews
	d.ap.SaveOverlay = d.saveOverlay
	d.ap.StartOverlay = d.startOverlay
	d.ap.StopOverlay = d.stopOverlay
	d.ap.SetupOverlay = func(kind string, body []byte) (string, error) {
		var req SetupRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return "", err
		}
		return d.setupOverlay(kind, req)
	}
	d.ap.TeardownOverlay = d.teardownOverlay
	d.ap.InspectToken = d.InspectToken
	d.ap.ReserveName = d.ReserveZrokName
	d.ap.Capabilities = func() any { return d.ZitiCapabilities() }
	d.ap.SetApiEndpoint = d.SetZrokApiEndpoint
	// The board only offers attach for a runner atrium owns, because a window
	// mode launch has no terminal here to show.
	api.IsSupervised = func(taskID string) bool { return d.sup.get(taskID) != nil }
	// What a runner is doing right now. Held in the daemon, never written down.
	api.ActivityOf = d.activityFor
	// Starting a fixture is spawning a process, which the daemon owns.
	api.StartFixture = d.StartFixtureNow
	return d, nil
}

func (d *Daemon) launchFromJSON(body []byte) (*store.Task, error) {
	var req LaunchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return d.Launch(req)
}

// decide resolves a permission from a human client. It goes through the hub so
// the blocked runner is actually released: the hub owns the reply channel the
// permission hook is waiting on, and recording the decision without signalling
// it would leave that runner hanging.
func (d *Daemon) decide(permID, decision, reason, command string) (*store.Permission, error) {
	if command != "" {
		// Record the rewrite before releasing the agent, so the audit log shows
		// what actually ran rather than what was asked for.
		if err := d.st.RewriteCommand(permID, command); err != nil {
			return nil, err
		}
	}
	if d.hb.DecideByStoreID(permID, decision, reason, command) {
		// The hub called back into onPermDecided, which recorded it.
		return d.st.GetPermission(permID)
	}
	// Nothing is blocked on it: the agent gave up, or the daemon restarted
	// while the request was pending. Record the decision anyway so the queue
	// does not keep showing it.
	p, err := d.st.DecidePermission(permID, decision, reason)
	if err != nil {
		return nil, err
	}
	d.publishTask(p.TaskID)
	d.ap.Broadcast("permission", p)
	return p, nil
}

// Hub exposes the hub so an in-process TUI can still attach during the
// migration. New clients should use the HTTP API instead.
func (d *Daemon) Hub() *hub.Hub { return d.hb }

// Store exposes the store.
func (d *Daemon) Store() *store.Store { return d.st }

// Close releases the database.
func (d *Daemon) Close() error { return d.st.Close() }

// reportRunners says which configured runners this machine actually has.
//
// Printed at startup because PATH is read when the process starts. Installing
// something afterwards is invisible until the daemon is restarted, and a
// runner that fails to launch for that reason gives no hint that a restart is
// the answer.
func (d *Daemon) reportRunners() {
	hs, err := d.st.Harnesses()
	if err != nil || len(hs) == 0 {
		return
	}
	width := 0
	for _, h := range hs {
		if len(h.ID) > width {
			width = len(h.ID)
		}
	}
	log.Printf("[atrium] runners, resolved against this process's PATH:")
	missing := 0
	for _, h := range hs {
		state := "off"
		if h.Enabled {
			state = "on "
		}
		if p := api.LookPath(h.Cmd); p != "" {
			log.Printf("[atrium]   %-*s  %s  %s", width, h.ID, state, p)
			continue
		}
		log.Printf("[atrium]   %-*s  %s  NOT ON PATH (%s)", width, h.ID, state, h.Cmd)
		if h.Enabled {
			missing++
		}
	}
	if missing > 0 {
		log.Printf("[atrium] %d enabled runner(s) cannot start. install them, then restart "+
			"the daemon so it picks up the new PATH.", missing)
	}
}

// onHalt closes the agent-facing listener and leaves it closed.
func (d *Daemon) onHalt(cause error) {
	log.Printf("[atrium] HALTED: %v", cause)
	log.Printf("[atrium] agent listener closing. runners will park on connection-refused and burn nothing.")
	log.Printf("[atrium] fix the cause and restart. atrium will not recover on its own.")
	d.mu.Lock()
	srv := d.agentServer
	d.agentServer = nil
	d.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	d.ap.Broadcast("halted", map[string]string{"cause": fmt.Sprint(cause)})
}

// prompt routes a prompt from a human client to the agent holding that task.
func (d *Daemon) prompt(taskID, text string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if t.WireName == "" {
		return fmt.Errorf("task %s has no connected agent", taskID)
	}
	d.hb.SendPrompt(t.WireName, text)
	return nil
}

// hooks is the durable side of the hub.
func (d *Daemon) hooks() *hub.Hooks {
	return &hub.Hooks{
		Submit:      d.onSubmit,
		Prompt:      d.onPrompt,
		PermRequest: d.onPermRequest,
		PermDecided: d.onPermDecided,
	}
}

// observedFor builds the observed bucket from what the wire name tells us.
// v1 agents send only a name, so that is all there is until the agent learns
// to send a registration payload.
func observedFor(agent string) store.Observed {
	host, _ := os.Hostname()
	return store.Observed{WireName: agent, Runner: "claude", Hostname: host}
}

func (d *Daemon) onSubmit(agent, kind, content string) (string, error) {
	task, created, err := d.st.Register(observedFor(agent))
	if err != nil {
		return "", err
	}
	switch kind {
	case "keepalive":
		// Liveness only. Never UI-visible, never a status change: a parked
		// agent is not doing anything worth showing.
		return task.ID, nil
	case "task-complete":
		if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
			return "", err
		}
	default:
		// A greeting or a response means the agent has handed control back.
		if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
			return "", err
		}
	}
	if err := d.st.AppendEvent(task.ID, store.EventSubmitted, map[string]any{
		"kind": kind, "content": content,
	}); err != nil {
		return "", err
	}
	d.publishTask(task.ID)
	_ = created
	return task.ID, nil
}

func (d *Daemon) onPrompt(agent, text string) {
	task, _, err := d.st.Register(observedFor(agent))
	if err != nil {
		log.Printf("[atrium] record prompt for %s: %v", agent, err)
		return
	}
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		log.Printf("[atrium] status for %s: %v", agent, err)
		return
	}
	if err := d.st.AppendEvent(task.ID, store.EventPrompted, map[string]any{"text": text}); err != nil {
		log.Printf("[atrium] event for %s: %v", agent, err)
		return
	}
	d.publishTask(task.ID)
}

func (d *Daemon) onPermRequest(req hub.PermissionRequest) (string, *hub.AutoDecision, error) {
	obs := observedFor(req.Agent)
	// The hook reports the runner's own pid and working directory. The pid is
	// what makes free liveness checks possible.
	obs.PID = req.PID
	if req.Cwd != "" {
		obs.Worktree = strings.ReplaceAll(req.Cwd, `\`, "/")
	}
	task, _, err := d.st.Register(obs)
	if err != nil {
		return "", nil, err
	}
	tool, command := req.Tool, req.Command
	// A permission request is a tool starting, so it doubles as an activity
	// report. A gated session therefore shows live activity with no activity
	// hook wired up, at no cost: this call is already being made.
	d.act.set(task.ID, ActivityTool, tool)
	if strings.EqualFold(tool, "Task") {
		d.act.addSubagents(task.ID, 1)
	}
	// The key makes a retry the same question rather than a new one. A daemon
	// that crashed between recording a decision and answering would otherwise
	// ask twice, and the second answer would be given against a situation that
	// had moved on. Empty means the store treats this as distinct.
	// The runner's own id for this attempt beats a hash of the command, which
	// cannot tell a retry from the same command run tomorrow. When one is sent
	// the request is deduplicated exactly and the replay window does not
	// apply. A hook that sends only a hash keeps the window.
	key := req.DedupKey
	if id := strings.TrimSpace(req.ToolUseID); id != "" {
		key = store.ExactKey(id)
	}
	p, decided, err := d.st.RecordPermission(task.ID, tool, command, key, req.Details)
	if err != nil {
		return "", nil, err
	}
	if decided {
		// Recorded, because a replay reaches an agent as a real answer and
		// the log is what the gate is justified by. Without this a refusal
		// arrives with nothing anywhere saying it happened, which is the one
		// thing that log exists to prevent.
		if err := d.st.AppendEvent(task.ID, store.EventPermDecided, map[string]any{
			"id": p.ID, "decision": p.Decision, "reason": p.Reason,
			"by": "replay", "tool": tool, "command": command,
		}); err != nil {
			log.Printf("[atrium] could not record a replayed decision: %v", err)
		}
		return p.ID, &hub.AutoDecision{Decision: p.Decision, Reason: p.Reason}, nil
	}

	// Anything queued for this session rides the next tool call, which is how a
	// message reaches a session that is working. The call is refused in order
	// to carry the text, and the banner says so, or the model reads a delivery
	// as a judgement on its command.
	if msgs, err := d.takeMessages(task.ID, "permission"); err == nil && len(msgs) > 0 {
		reason := messageBanner(msgs, true)
		if _, err := d.st.DecidePermissionBy(p.ID, "block", reason, store.DecidedByMessage); err != nil {
			return "", nil, err
		}
		d.publishTask(task.ID)
		return p.ID, &hub.AutoDecision{Decision: "block", Reason: reason}, nil
	}

	// A shelved card is a standing no. Putting work down has to answer for that
	// work, or the agent asks, gets nothing, and freezes behind a card nobody
	// is looking at any more.
	if task.Status == store.StatusShelved {
		if _, err := d.st.DecidePermissionBy(p.ID, "block", shelvedReason, "shelved"); err != nil {
			return "", nil, err
		}
		return p.ID, &hub.AutoDecision{Decision: "block", Reason: shelvedReason}, nil
	}

	// A standing rule short-circuits the human entirely. The request is still
	// recorded and resolved, so the history shows what ran and which rule let
	// it through, but nothing is ever shown to be clicked.
	rule, err := d.st.MatchRule(tool, command, task.Worktree)
	if err != nil {
		return "", nil, err
	}
	if rule != nil {
		// DecidePermissionBy writes the audit event, carrying what answered
		// this in `by`. A second one here would show every rule decision
		// twice.
		if _, err := d.st.DecidePermissionBy(p.ID, rule.Decision, rule.Reason, rule.Prefix); err != nil {
			return "", nil, err
		}
		return p.ID, &hub.AutoDecision{Decision: rule.Decision, Reason: rule.Reason}, nil
	}

	// Auto mode: stop asking, keep recording.
	//
	// Last, after messages, shelving and standing rules. Auto mode stops new
	// questions; it does not discard answers already given, so a never rule and
	// a shelved card both still block. What it lets through goes to the same
	// audit log as every other decision, marked auto, and the review reads it.
	//
	// Global auto is the same thing for every session at once, including ones
	// that do not exist yet. It is recorded under its own name rather than as
	// `auto`, because "I turned this session loose" and "I turned the whole
	// board loose" are different answers to give six hours later.
	if global := d.st.GlobalAuto(); task.AutoApprove || global {
		by, reason := "auto", autoReason
		if global && !task.AutoApprove {
			by, reason = "global-auto", globalAutoReason
		}
		if _, err := d.st.DecidePermissionBy(p.ID, "approve", reason, by); err != nil {
			return "", nil, err
		}
		d.ap.Broadcast("permission", p)
		return p.ID, &hub.AutoDecision{Decision: "approve", Reason: reason}, nil
	}

	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		return "", nil, err
	}
	d.publishTask(task.ID)
	d.ap.Broadcast("permission", p)
	return p.ID, nil, nil
}

// autoReason is recorded against every request auto mode lets through, so the
// audit log separates "a human said yes" from "nobody was asked".
const autoReason = "auto mode: approved without asking, and recorded"

// globalAutoReason names the board-wide switch rather than the per-session one,
// so an agent told why it was let through says which of the two answered.
const globalAutoReason = "global auto mode: every session is approved without asking, and recorded"

// shelvedReason is handed back to an agent whose request was refused because
// its card is shelved, so the agent is told why rather than left waiting.
const shelvedReason = "this task is shelved in atrium. unshelve it to answer requests from it."

// CancelPending answers every outstanding request on a task with a block.
//
// Moving a card out of a waiting state has to answer the question, not just
// hide it. Otherwise the agent stays frozen with nobody coming, and the request
// sits in the queue to be approved later against a situation that has moved on.
func (d *Daemon) CancelPending(taskID, reason string) (int, error) {
	pending, err := d.st.PendingForTask(taskID)
	if err != nil {
		return 0, err
	}
	for _, p := range pending {
		// Through decide, so the blocked agent is actually released rather
		// than having its answer written to a row it never reads.
		if _, err := d.decide(p.ID, "block", reason, ""); err != nil {
			return 0, err
		}
	}
	if len(pending) > 0 {
		log.Printf("[atrium] blocked %d pending request(s) on %s: %s", len(pending), taskID, reason)
	}
	return len(pending), nil
}

func (d *Daemon) onPermDecided(permID, decision, reason string) {
	p, err := d.st.DecidePermission(permID, decision, reason)
	if err != nil {
		log.Printf("[atrium] decide %s: %v", permID, err)
		return
	}
	// Permission resolved means the runner goes back to work.
	if err := d.st.SetStatus(p.TaskID, store.StatusRunning); err != nil {
		log.Printf("[atrium] status after decision: %v", err)
	}
	d.publishTask(p.TaskID)
	d.ap.Broadcast("permission", p)
}

func (d *Daemon) publishTask(id string) {
	t, err := d.st.Get(id)
	if err != nil {
		return
	}
	d.ap.Broadcast("task", t)
}

// Run serves both listeners until ctx is cancelled or a listener fails.
func (d *Daemon) Run(ctx context.Context) error {
	agentMux := http.NewServeMux()
	agentMux.HandleFunc("/submit", d.hb.HandleSubmit)
	agentMux.HandleFunc("/permission", d.hb.HandlePermission)
	agentMux.HandleFunc("/session", d.handleSession)
	agentMux.HandleFunc("/gate", d.handleGate)
	agentMux.HandleFunc("/stop", d.handleStop)
	agentMux.HandleFunc("/activity", d.handleActivity)
	agentMux.HandleFunc("/hooks-changed", d.handleHooksChanged)

	agentSrv := &http.Server{Addr: d.opts.AgentAddr, Handler: agentMux}
	humanSrv := &http.Server{Addr: d.opts.HumanAddr, Handler: d.ap.Handler()}

	d.mu.Lock()
	d.agentServer = agentSrv
	d.mu.Unlock()

	agentLn, err := net.Listen("tcp", d.opts.AgentAddr)
	if err != nil {
		return fmt.Errorf("agent listener: %w", err)
	}
	humanLn, err := net.Listen("tcp", d.opts.HumanAddr)
	if err != nil {
		agentLn.Close()
		return fmt.Errorf("human listener: %w", err)
	}

	log.Printf("[atrium] agents  -> http://localhost%s", d.opts.AgentAddr)
	log.Printf("[atrium] board   -> http://localhost%s", d.opts.HumanAddr)
	log.Printf("[atrium] state   -> %s", d.opts.DBPath)

	// Written once both listeners are bound, so the file never advertises an
	// address that failed to open.
	d.writeLocation()
	defer d.clearLocation()

	// A new database is indistinguishable from every card and every rule having
	// vanished. WORKTREE_ROOT unset once sent the path to the home directory,
	// and a hundred and twenty five rules appeared to be gone.
	d.reportRunners()

	if d.st.Fresh() {
		log.Printf("[atrium] ---------------------------------------------------------------")
		log.Printf("[atrium] THIS IS A NEW DATABASE. There was no file at that path, so one")
		log.Printf("[atrium] was created. The board will be empty and you will have no rules.")
		log.Printf("[atrium] If you expected your existing state, you are pointed at the")
		log.Printf("[atrium] wrong path. Check WORKTREE_ROOT, or pass --db explicitly.")
		log.Printf("[atrium] ---------------------------------------------------------------")
	}
	// Free liveness: ask the operating system whether each runner still
	// exists, rather than asking the runner.
	go d.reap(ctx, ReapEvery)
	// The commands that find work. Its own loop rather than the reap ticker,
	// because a source runs on the interval its own row names and the reaper
	// asks one question at one rate. Nothing here can halt anything: intake is
	// a suggestion, and a source that fails says so on its row.
	go d.sourceLoop(ctx)
	// Terminals that come up with the daemon. In the background, so a runner
	// that is slow to start cannot delay the board answering: a board that is
	// not up yet looks like a hang, a terminal that is not open yet does not.
	go d.startFixtures()
	// Cards named before atrium asked git. Once, at startup, rather than on
	// registration: registration runs on every hook of every session, and a
	// directory in no repository would re-answer that question forever.
	if n, err := d.st.BackfillGitInfo(); err != nil {
		log.Printf("[atrium] could not name cards from their repositories: %v", err)
	} else if n > 0 {
		log.Printf("[atrium] named %d card(s) from their repository and branch", n)
	}
	log.Printf("[atrium] ready. ctrl-c to stop.")

	errCh := make(chan error, 2)
	go func() {
		err := agentSrv.Serve(agentLn)
		// A halt closes this listener, so its shutdown is not an error to exit
		// on.
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		errCh <- fmt.Errorf("agent listener: %w", err)
	}()
	go func() {
		err := humanSrv.Serve(humanLn)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		errCh <- fmt.Errorf("human listener: %w", err)
	}()

	select {
	case <-ctx.Done():
		log.Printf("[atrium] interrupt received, shutting down")
	case <-d.stop.ch:
		// The same wind-down as ctrl-c, reachable from anywhere the board is.
		// Killing the process takes every supervised runner with it.
		log.Printf("[atrium] stopping: %s", d.stop.why())
	case err := <-errCh:
		log.Printf("[atrium] listener failed, shutting down: %v", err)
		d.shutdown(agentSrv, humanSrv)
		return err
	}
	d.shutdown(agentSrv, humanSrv)
	return nil
}

// shutdown closes both listeners and says what it is doing at each step.
// Agents parked in a long poll keep the connection open until the poll expires,
// so this can take several seconds. Silence for that long looks like a hang.
func (d *Daemon) shutdown(servers ...*http.Server) {
	start := time.Now()

	// Release the event streams first. Each open browser tab holds one, and
	// Shutdown waits for in-flight requests, so leaving them open is what made
	// the board listener sit out the whole grace period.
	d.ap.Close()

	// Any share goes with the board it publishes. An address that outlives
	// what it points at answers with a connection refused, which reads as the
	// overlay being broken.
	d.closeOverlays()

	// Runners atrium owns get a real chance to wind up before their terminal
	// closes underneath them. Ten seconds because an agent mid-turn may be
	// writing a file, and losing that costs far more than a slow shutdown.
	d.stopSupervised(10 * time.Second)

	if n := d.blockedAgents(); n > 0 {
		log.Printf("[atrium] %d agent connection(s) still parked. they will retry against the "+
			"next daemon, and will not wake their model while waiting.", n)
	}

	// Long polls hold their connection until the client goes away, so give
	// them a bounded window rather than waiting for the full poll timeout.
	grace := 5 * time.Second
	if d.opts.LongPoll > 0 && d.opts.LongPoll < grace {
		grace = d.opts.LongPoll
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	names := []string{"agent listener", "board listener"}
	done := make(chan string, len(servers))
	for i, s := range servers {
		if s == nil {
			continue
		}
		name := "listener"
		if i < len(names) {
			name = names[i]
		}
		log.Printf("[atrium] closing %s...", name)
		go func(s *http.Server, name string) {
			if err := s.Shutdown(ctx); err != nil {
				log.Printf("[atrium] %s did not close cleanly: %v", name, err)
			}
			done <- name
		}(s, name)
	}

	for range servers {
		select {
		case name := <-done:
			log.Printf("[atrium] %s closed", name)
		case <-ctx.Done():
			log.Printf("[atrium] gave up waiting after %s, forcing the rest closed", grace)
			for _, s := range servers {
				if s != nil {
					_ = s.Close()
				}
			}
			log.Printf("[atrium] stopped in %s", time.Since(start).Round(time.Millisecond))
			return
		}
	}

	log.Printf("[atrium] state is on disk at %s", d.opts.DBPath)
	log.Printf("[atrium] stopped in %s", time.Since(start).Round(time.Millisecond))
}

// blockedAgents counts permission requests currently holding an agent still.
func (d *Daemon) blockedAgents() int {
	pending, err := d.st.PendingPermissions()
	if err != nil {
		return 0
	}
	return len(pending)
}
