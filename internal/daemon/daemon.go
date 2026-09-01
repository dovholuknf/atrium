// Package daemon wires the three layers together: the store owns state, the
// hub serves agents, and the api serves humans.
//
// The two listeners are separate on purpose. When the store wedges, the
// agent-facing listener closes and does not reopen, so every runner sees
// connection-refused and parks on the backoff it already has. The human-facing
// listener stays up so the board can say what broke.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	// Ledger adopts sessions from the gwt session ledger, so claude sessions
	// that were started outside atrium still show up on the board.
	Ledger     bool
	LedgerPoll time.Duration
	// LedgerMaxAge bounds how far back a finished session is worth adopting.
	// The ledger keeps everything ever recorded, and without this the board
	// fills with months of dead work on first run.
	LedgerMaxAge time.Duration
}

// Daemon owns the store, the hub, and both listeners.
type Daemon struct {
	opts Options
	st   *store.Store
	hb   *hub.Hub
	ap   *api.Server

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
	d := &Daemon{opts: opts, st: st, hb: hub.New(opts.LongPoll), ap: api.New(st)}
	st.OnWedge = d.onWedge
	d.hb.Record = d.hooks()
	d.ap.Prompt = d.prompt
	d.ap.Decide = d.decide
	return d, nil
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

// onWedge closes the agent-facing listener and leaves it closed.
func (d *Daemon) onWedge(cause error) {
	log.Printf("[atrium] WEDGED: %v", cause)
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
	d.ap.Broadcast("wedge", map[string]string{"cause": fmt.Sprint(cause)})
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

func (d *Daemon) onPermRequest(agent, tool, command string) (string, *hub.AutoDecision, error) {
	task, _, err := d.st.Register(observedFor(agent))
	if err != nil {
		return "", nil, err
	}
	// The hook does not send a dedup key yet, so requests are un-keyed for now
	// and the store treats each as distinct.
	p, decided, err := d.st.RecordPermission(task.ID, tool, command, "")
	if err != nil {
		return "", nil, err
	}
	if decided {
		return p.ID, &hub.AutoDecision{Decision: p.Decision, Reason: p.Reason}, nil
	}

	// A standing rule short-circuits the human entirely. The request is still
	// recorded and resolved, so the history shows what ran and which rule let
	// it through, but nothing is ever shown to be clicked.
	rule, err := d.st.MatchRule(tool, command, task.Worktree)
	if err != nil {
		return "", nil, err
	}
	if rule != nil {
		if _, err := d.st.DecidePermissionBy(p.ID, rule.Decision, rule.Reason, rule.Prefix); err != nil {
			return "", nil, err
		}
		if err := d.st.AppendEvent(task.ID, store.EventPermDecided, map[string]any{
			"id": p.ID, "decision": rule.Decision, "rule": rule.Prefix, "auto": true,
		}); err != nil {
			return "", nil, err
		}
		return p.ID, &hub.AutoDecision{Decision: rule.Decision, Reason: rule.Reason}, nil
	}

	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		return "", nil, err
	}
	d.publishTask(task.ID)
	d.ap.Broadcast("permission", p)
	return p.ID, nil, nil
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
	if d.opts.Ledger {
		go d.watchLedger(ctx, d.opts.LedgerPoll)
	}
	log.Printf("[atrium] ready. ctrl-c to stop.")

	errCh := make(chan error, 2)
	go func() {
		err := agentSrv.Serve(agentLn)
		// A wedge closes this listener on purpose, so its shutdown is not an
		// error the daemon should exit on.
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
