// Package api is atrium's human-facing surface: the JSON endpoints and SSE
// stream that the TUI, the SPA, and any future app all consume.
//
// This listener is deliberately separate from the agent-facing one. When the
// store wedges, the agent listener closes so runners park on connection-refused
// and stop burning tokens, while this one stays up to explain what broke.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/claudeconf"
	"github.com/dovholuknf/atrium/internal/store"
)

// Server serves the human-facing API.
type Server struct {
	st  *store.Store
	bus *bus
	// Prompt hands a prompt to a waiting agent. Supplied by the daemon, which
	// owns the agent-facing side.
	Prompt func(taskID, text string) error
	// Decide resolves a permission. This must go through the daemon rather
	// than straight to the store, because the agent is blocked on an in-memory
	// reply channel that only the hub can signal. Writing the decision without
	// signalling would leave the runner hanging forever.
	Decide func(permID, decision, reason, command string) (*store.Permission, error)
	// Launch starts a runner. The daemon owns process spawning, so the API
	// hands the request body straight through rather than reaching for it.
	Launch func(body []byte) (*store.Task, error)
	// Kill stops the runner behind a card.
	Kill func(taskID string) error
	// CancelPending answers every outstanding request on a task with a block.
	// Moving a card out of a waiting state has to answer the question rather
	// than hide it, or the agent stays frozen with nobody coming.
	CancelPending func(taskID, reason string) (int, error)
	// Attach upgrades to a WebSocket carrying a supervised runner's terminal.
	// Supplied by the daemon, which owns the processes. Registered only when
	// set, so a build without supervision has no dead route.
	Attach http.HandlerFunc
	// Message says something to a running session: typed into its terminal
	// when atrium owns one, queued for the next hook otherwise.
	Message http.HandlerFunc
}

// forever turns a one-off decision into a standing rule, so the same command
// never asks again.
func (s *Server) forever(permID, decision, reason, prefix string) error {
	p, err := s.st.GetPermission(permID)
	if err != nil {
		return err
	}
	if prefix == "" {
		prefix = store.DefaultPrefix(p.Tool, p.Command)
	}
	if _, err := s.st.AddRule(p.Tool, prefix, decision, reason, ""); err != nil {
		return err
	}
	// Recorded against the request so the audit log shows the rule at the
	// moment it was agreed to, not just its existence afterwards.
	return s.st.NoteRuleCreated(permID, prefix)
}

// New builds a server over a store.
func New(st *store.Store) *Server {
	return &Server{st: st, bus: newBus()}
}

// Broadcast publishes a change to every connected client.
func (s *Server) Broadcast(kind string, payload any) { s.bus.publish(kind, payload) }

// Handler returns the mux for the human-facing listener.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/tasks", s.listTasks)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("PATCH /v1/tasks/{id}", s.patchTask)
	mux.HandleFunc("DELETE /v1/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /v1/tasks/prune", s.pruneTasks)
	mux.HandleFunc("GET /v1/tasks/{id}/events", s.taskEvents)
	mux.HandleFunc("POST /v1/tasks/{id}/prompt", s.promptTask)
	mux.HandleFunc("GET /v1/waiting", s.waiting)
	mux.HandleFunc("GET /v1/permissions", s.listPermissions)
	mux.HandleFunc("POST /v1/permissions/{id}/decide", s.decidePermission)
	mux.HandleFunc("GET /v1/permissions/history", s.permissionHistory)
	mux.HandleFunc("GET /v1/rules", s.listRules)
	mux.HandleFunc("DELETE /v1/rules/{id}", s.deleteRule)
	mux.HandleFunc("GET /v1/rules/export", s.exportRules)
	mux.HandleFunc("POST /v1/rules/import", s.importRules)
	mux.HandleFunc("GET /v1/rules/preview-claude", s.previewClaudeRules)
	mux.HandleFunc("GET /v1/harnesses", s.listHarnesses)
	mux.HandleFunc("PUT /v1/harnesses/{id}", s.saveHarness)
	mux.HandleFunc("DELETE /v1/harnesses/{id}", s.deleteHarness)
	mux.HandleFunc("POST /v1/launch", s.launch)
	mux.HandleFunc("POST /v1/tasks/{id}/kill", s.kill)
	if s.Attach != nil {
		mux.HandleFunc("GET /v1/tasks/{id}/attach", s.Attach)
	}
	if s.Message != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/message", s.Message)
	}
	mux.HandleFunc("GET /v1/events", s.events)
	mux.Handle("/", webHandler())
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[atrium api] encode: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// fail maps a store error to a response. A wedged store is reported as 503
// with its cause, because this listener exists to explain the wedge.
func (s *Server) fail(w http.ResponseWriter, err error) {
	if wedged, cause := s.st.Wedged(); wedged {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":  "atrium is wedged and will not recover without a restart",
			"cause":  fmt.Sprint(cause),
			"wedged": true,
		})
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	wedged, cause := s.st.Wedged()
	body := map[string]any{"ok": !wedged, "wedged": wedged}
	if wedged {
		body["cause"] = fmt.Sprint(cause)
	}
	writeJSON(w, http.StatusOK, body)
}

// view is a task shaped for a client: observed values already resolved against
// overrides, plus the derived ages the board renders.
type view struct {
	*store.Task
	DisplayTitle string `json:"display_title"`
	IdleSeconds  int64  `json:"idle_seconds"`
	WaitSeconds  int64  `json:"wait_seconds"`
	// Observed marks a session atrium is only watching. The board uses it to
	// offer resume rather than a prompt box, and to stay quiet about it.
	Observed bool `json:"observed"`
	// Supervised marks a runner atrium owns and can therefore attach to. A
	// window mode launch is not supervised, so offering attach on it would be
	// a button that cannot work.
	Supervised bool `json:"supervised"`
}

// IsSupervised reports whether atrium owns this task's runner. Supplied by the
// daemon, since the supervisor lives there.
var IsSupervised func(taskID string) bool

func toView(t *store.Task) view {
	v := view{
		Task:         t,
		DisplayTitle: t.DisplayTitle(),
		IdleSeconds:  int64(time.Since(t.LastActivityAt).Seconds()),
		Observed:     t.Observed(),
	}
	if IsSupervised != nil {
		v.Supervised = IsSupervised(t.ID)
	}
	if t.WaitingSince != nil {
		v.WaitSeconds = int64(time.Since(*t.WaitingSince).Seconds())
	}
	return v
}

func toViews(ts []*store.Task) []view {
	out := make([]view, 0, len(ts))
	for _, t := range ts {
		out = append(out, toView(t))
	}
	return out
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	var statuses []string
	if raw := r.URL.Query().Get("status"); raw != "" {
		statuses = strings.Split(raw, ",")
	}
	tasks, err := s.st.List(statuses...)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": toViews(tasks)})
}

func (s *Server) waiting(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.st.Waiting()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": toViews(tasks)})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toView(t))
}

type patchBody struct {
	Status    *string           `json:"status"`
	Why       *string           `json:"why"`
	Rank      *float64          `json:"rank"`
	Overrides map[string]string `json:"overrides"`
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body patchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cancelled := 0
	if body.Status != nil {
		// Answer anything outstanding before the card moves. A request left
		// pending on a card that is no longer waiting keeps its agent frozen
		// and stays in the queue to be approved later against a situation the
		// operator has already walked away from.
		if *body.Status != store.StatusNeedsPermission && s.CancelPending != nil {
			reason := fmt.Sprintf("the operator moved this task to %s in atrium", *body.Status)
			if *body.Status == store.StatusShelved {
				reason = "this task was shelved in atrium. unshelve it to answer requests from it."
			}
			n, err := s.CancelPending(id, reason)
			if err != nil {
				s.fail(w, err)
				return
			}
			cancelled = n
		}
		if err := s.st.SetStatus(id, *body.Status); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Why != nil {
		if err := s.st.SetWhy(id, *body.Why); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Rank != nil {
		if err := s.st.SetRank(id, *body.Rank); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Overrides != nil {
		if err := s.st.SetOverrides(id, body.Overrides); err != nil {
			s.fail(w, err)
			return
		}
	}
	t, err := s.st.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("task", toView(t))
	out := toView(t)
	if cancelled > 0 {
		s.Broadcast("permission", map[string]any{"cancelled": cancelled, "task": id})
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": out, "cancelled": cancelled})
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.Forget(id); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("task-removed", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

// pruneTasks clears out finished cards in one go.
//
// Done and dead only. A shelved card is something to come back to, so a sweep
// never touches one no matter how long it has sat there.
func (s *Server) pruneTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		// OlderThanHours defaults to now, meaning every finished card goes.
		OlderThanHours *float64 `json:"older_than_hours"`
		// Statuses narrows the sweep to one column. Empty means all prunable.
		Statuses []string `json:"statuses"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	var age time.Duration
	if body.OlderThanHours != nil && *body.OlderThanHours > 0 {
		age = time.Duration(*body.OlderThanHours * float64(time.Hour))
	}
	n, err := s.st.Prune(age, body.Statuses...)
	if err != nil {
		s.fail(w, err)
		return
	}
	if n > 0 {
		s.Broadcast("tasks-pruned", map[string]int{"removed": n})
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

func (s *Server) taskEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.st.Events(r.PathValue("id"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) promptTask(w http.ResponseWriter, r *http.Request) {
	if s.Prompt == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no agent transport wired"))
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Prompt(r.PathValue("id"), body.Text); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := s.st.PendingPermissions()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

func (s *Server) decidePermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		Forever  bool   `json:"forever"`
		Prefix   string `json:"prefix"`
		Command  string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Decision != "approve" && body.Decision != "block" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decision must be approve or block"))
		return
	}
	// The rule is written before the decision so a crash in between leaves the
	// request pending rather than leaving a rule nobody agreed to.
	if body.Forever {
		if err := s.forever(r.PathValue("id"), body.Decision, body.Reason, body.Prefix); err != nil {
			// A rejected pattern is the caller's problem, not a server fault,
			// so it must not be reported as one. Only a wedged store gets to
			// take the 5xx path here.
			if wedged, _ := s.st.Wedged(); wedged {
				s.fail(w, err)
				return
			}
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	if s.Decide == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no agent transport wired"))
		return
	}
	// Answering something already answered silently returns the original
	// decision, which from a notification button looks like nothing happened.
	// Say so instead, and say what the answer was.
	if existing, err := s.st.GetPermission(r.PathValue("id")); err == nil && existing.DecidedAt != nil {
		by := existing.DecidedBy
		if by == "" || by == store.DecidedBySelf {
			by = "you"
		} else {
			by = "the rule " + by
		}
		// "approve" plus "ed" is "approveed". Past tense is a lookup, not a
		// suffix.
		past := map[string]string{"approve": "approved", "block": "blocked"}[existing.Decision]
		if past == "" {
			past = existing.Decision
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("already %s by %s at %s",
				past, by, existing.DecidedAt.Format("15:04:05")),
			"already":  true,
			"decision": existing.Decision,
		})
		return
	}
	p, err := s.Decide(r.PathValue("id"), body.Decision, body.Reason, body.Command)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) permissionHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	perms, err := s.st.History(limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.Rules()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteRule(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("rules", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// exportRules writes every standing rule as JSON that importRules accepts, so
// a rule set can be moved between machines or kept in a file.
func (s *Server) exportRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.Rules()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]claudeconf.Entry, 0, len(rules))
	for _, rule := range rules {
		out = append(out, claudeconf.Entry{
			Tool: rule.Tool, Pattern: rule.Prefix, Decision: rule.Decision, Source: "atrium",
		})
	}
	w.Header().Set("Content-Disposition", `attachment; filename="atrium-rules.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

// previewClaudeRules shows what importing from Claude Code's settings would
// do, without doing it. Import is not something to run blind.
func (s *Server) previewClaudeRules(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	entries, skipped, err := claudeconf.Load(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rules":   entries,
		"skipped": skipped,
		"sources": claudeconf.SettingsPaths(dir),
	})
}

// importRules adds rules from Claude Code's settings, or from a JSON body that
// exportRules produced.
func (s *Server) importRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source     string             `json:"source"` // "claude" or "json"
		Dir        string             `json:"dir"`
		Rules      []claudeconf.Entry `json:"rules"`
		IncludeAll bool               `json:"include_broad"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	entries := body.Rules
	var skipped []claudeconf.Skipped
	if body.Source == "claude" {
		var err error
		entries, skipped, err = claudeconf.Load(body.Dir)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	added, updated := 0, 0
	var failed []map[string]string
	for _, e := range entries {
		if e.Broad && !body.IncludeAll {
			skipped = append(skipped, claudeconf.Skipped{
				Raw:    e.Tool + " " + e.Pattern,
				Source: e.Source,
				Reason: "matches every request for this tool, not imported unless you ask for it",
			})
			continue
		}
		existing, err := s.st.Rules()
		if err != nil {
			s.fail(w, err)
			return
		}
		had := false
		for _, r := range existing {
			if r.Tool == e.Tool && r.Prefix == e.Pattern {
				had = true
				break
			}
		}
		add := s.st.AddRule
		if e.Broad {
			add = s.st.AddBroadRule
		}
		if _, err := add(e.Tool, e.Pattern, e.Decision, "imported from "+e.Source, ""); err != nil {
			failed = append(failed, map[string]string{
				"tool": e.Tool, "pattern": e.Pattern, "error": err.Error(),
			})
			continue
		}
		if had {
			updated++
		} else {
			added++
		}
	}
	s.Broadcast("rules", map[string]int{"added": added})
	writeJSON(w, http.StatusOK, map[string]any{
		"added": added, "updated": updated, "skipped": skipped, "failed": failed,
	})
}

func (s *Server) listHarnesses(w http.ResponseWriter, r *http.Request) {
	hs, err := s.st.Harnesses()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"harnesses": hs})
}

func (s *Server) saveHarness(w http.ResponseWriter, r *http.Request) {
	var h store.Harness
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	h.ID = r.PathValue("id")
	saved, err := s.st.SaveHarness(h)
	if err != nil {
		if wedged, _ := s.st.Wedged(); wedged {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("harnesses", saved)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteHarness(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteHarness(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("harnesses", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	if s.Launch == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no launcher wired"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.Launch(body)
	if err != nil {
		// A bad harness, a missing directory or a mode that is not built are
		// all the caller's problem, not a server fault.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("task", toView(task))
	writeJSON(w, http.StatusOK, toView(task))
}

func (s *Server) kill(w http.ResponseWriter, r *http.Request) {
	if s.Kill == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no launcher wired"))
		return
	}
	if err := s.Kill(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// events is the SSE stream every live client subscribes to.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := s.bus.subscribe()
	defer s.bus.unsubscribe(sub)

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sub:
			// A closed channel means the daemon is shutting down. Returning
			// releases the request, which is what lets the listener close
			// instead of waiting out an open stream.
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.kind, msg.data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// Close releases every SSE subscriber. An event stream is an open request, and
// http.Server.Shutdown waits for those, so without this a single browser tab
// holds the board listener open until the shutdown grace period expires.
func (s *Server) Close() { s.bus.close() }

type message struct {
	kind string
	data []byte
}

// bus is a tiny fan-out for SSE subscribers. Slow subscribers are dropped
// rather than allowed to block a publisher.
type bus struct {
	mu     sync.Mutex
	subs   map[chan message]struct{}
	closed bool
}

func newBus() *bus { return &bus{subs: map[chan message]struct{}{}} }

func (b *bus) subscribe() chan message {
	ch := make(chan message, 32)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		// Already shutting down. Hand back a closed channel so the caller
		// returns immediately rather than parking on a stream nobody feeds.
		close(ch)
		return ch
	}
	b.subs[ch] = struct{}{}
	return ch
}

func (b *bus) unsubscribe(ch chan message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
	}
}

// close releases every subscriber exactly once.
func (b *bus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		close(ch)
		delete(b.subs, ch)
	}
}

func (b *bus) publish(kind string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[atrium api] publish %s: %v", kind, err)
		return
	}
	msg := message{kind: kind, data: data}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}
