// Package hub is the human-side terminal + HTTP server that brokers prompts
// to claude agents over HTTP long-poll. Single-agent MVP (no auth, no routing).
package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Message is one piece of content sent by an agent to the hub.
type Message struct {
	Agent   string    `json:"agent"`
	Kind    string    `json:"kind"`    // "greeting" or "response"
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// SubmitRequest is the wire shape POST'd by atrium-agent on every call.
type SubmitRequest struct {
	Agent   string `json:"agent"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// SubmitResponse is what the hub returns once a human prompt is available.
type SubmitResponse struct {
	Prompt string `json:"prompt"`
}

// ansiSentinels translates {red}/{reset}/etc into raw ANSI escape sequences.
// Lets the agent emit colors without trying to smuggle a 0x1b control byte
// through the model output (which Claude is unreliable about). Sentinels are
// case-insensitive and idempotent: unknown sentinels pass through unchanged.
var ansiSentinels = map[string]string{
	// foreground
	"{reset}":     "\x1b[0m",
	"{bold}":      "\x1b[1m",
	"{dim}":       "\x1b[2m",
	"{underline}": "\x1b[4m",
	"{black}":     "\x1b[30m",
	"{red}":       "\x1b[31m",
	"{green}":     "\x1b[32m",
	"{yellow}":    "\x1b[33m",
	"{blue}":      "\x1b[34m",
	"{magenta}":   "\x1b[35m",
	"{cyan}":      "\x1b[36m",
	"{white}":     "\x1b[37m",
	"{gray}":      "\x1b[90m",
	// backgrounds
	"{bgblack}":   "\x1b[40m",
	"{bgred}":     "\x1b[41m",
	"{bggreen}":   "\x1b[42m",
	"{bgyellow}":  "\x1b[43m",
	"{bgblue}":    "\x1b[44m",
	"{bgmagenta}": "\x1b[45m",
	"{bgcyan}":    "\x1b[46m",
	"{bgwhite}":   "\x1b[47m",
}

// applySentinels runs the agent's content through the sentinel table, then
// auto-appends a reset if the message contained any sentinels but the last
// one wasn't a reset. Keeps subsequent hub lines from inheriting bold/color.
func applySentinels(s string) string {
	if !strings.Contains(s, "{") {
		return s
	}
	saw := false
	for token, ansi := range ansiSentinels {
		if strings.Contains(strings.ToLower(s), token) {
			// case-insensitive replace
			s = caseInsensitiveReplace(s, token, ansi)
			saw = true
		}
	}
	if saw && !strings.HasSuffix(s, "\x1b[0m") {
		s += "\x1b[0m"
	}
	return s
}

// caseInsensitiveReplace replaces every case-insensitive occurrence of old in s.
// old MUST be lowercase; we only normalize s for matching.
func caseInsensitiveReplace(s, old, repl string) string {
	var b strings.Builder
	low := strings.ToLower(s)
	i := 0
	for {
		idx := strings.Index(low[i:], old)
		if idx < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+idx])
		b.WriteString(repl)
		i += idx + len(old)
	}
}

// PendingPermission is a permission request waiting on a human decision.
type PendingPermission struct {
	ID      int       `json:"id"`
	Agent   string    `json:"agent"`
	Command string    `json:"command"`
	Tool    string    `json:"tool"`
	At      time.Time `json:"at"`
	reply   chan permissionDecision

	// storeID is the durable permission id, empty when no store is wired. It
	// lets a decision made in the TUI be recorded against the same row the
	// request created.
	storeID string
}

type permissionDecision struct {
	Decision string // "approve" or "block"
	Reason   string
	// Command, when set, is a rewritten command the human wants run instead of
	// the one the agent asked for. The hook decides whether to honor it.
	Command string
}

// Hooks is the durable side of the hub. Every field is optional: a zero Hooks
// leaves v1's in-memory-only behavior exactly as it was, which is what keeps
// `atrium hub` working while the daemon is built out around it.
//
// The hub does not know about wedging. When the store wedges the daemon closes
// the agent-facing listener outright, so agents see connection-refused and park
// on their existing backoff instead of being told about a failure they cannot
// act on.
type Hooks struct {
	// Submit records an agent message and returns the task it belongs to.
	Submit func(agent, kind, content string) (taskID string, err error)
	// Prompt records the human's reply to an agent.
	Prompt func(agent, text string)
	// PermRequest records a pending permission and returns its durable id. A
	// non-empty auto means a standing rule already answered it, in which case
	// the hub replies immediately and never shows the request.
	PermRequest func(agent, tool, command string) (permID string, auto *AutoDecision, err error)
	// PermDecided records the resolution of a permission.
	PermDecided func(permID, decision, reason string)
}

func (h *Hooks) submit(agent, kind, content string) {
	if h == nil || h.Submit == nil {
		return
	}
	if _, err := h.Submit(agent, kind, content); err != nil {
		log.Printf("[atrium hub] record submit from %s: %v", agent, err)
	}
}

func (h *Hooks) prompt(agent, text string) {
	if h == nil || h.Prompt == nil {
		return
	}
	h.Prompt(agent, text)
}

// AutoDecision is a standing rule's answer to a permission request.
type AutoDecision struct {
	Decision string
	Reason   string
}

func (h *Hooks) permRequest(agent, tool, command string) (string, *AutoDecision) {
	if h == nil || h.PermRequest == nil {
		return "", nil
	}
	id, auto, err := h.PermRequest(agent, tool, command)
	if err != nil {
		log.Printf("[atrium hub] record permission from %s: %v", agent, err)
		return "", nil
	}
	return id, auto
}

func (h *Hooks) permDecided(permID, decision, reason string) {
	if h == nil || h.PermDecided == nil || permID == "" {
		return
	}
	h.PermDecided(permID, decision, reason)
}

// Hub holds the in-memory bus.
type Hub struct {
	// Record is the optional durable side. Set it before serving.
	Record *Hooks

	mu          sync.Mutex
	inbox       chan Message           // agent -> hub
	prompts     map[string]chan string // hub -> agent (per-agent fanout)
	known       map[string]time.Time   // every agent we've ever seen submit
	waiting     map[string]bool        // agent submitted and is waiting for the human
	lastSeen    string                 // most recent submitter; default prompt target
	timeout     time.Duration          // max long-poll duration
	permSeq     int                    // monotonically increasing perm id
	pendingPerm map[int]*PendingPermission
}

// New builds an empty hub.
func New(longPoll time.Duration) *Hub {
	return &Hub{
		inbox:       make(chan Message, 64),
		prompts:     map[string]chan string{},
		known:       map[string]time.Time{},
		waiting:     map[string]bool{},
		pendingPerm: map[int]*PendingPermission{},
		timeout:     longPoll,
	}
}

// Waiting returns a snapshot of every agent currently waiting for a human prompt.
func (h *Hub) Waiting() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]bool, len(h.waiting))
	for k, v := range h.waiting {
		out[k] = v
	}
	return out
}

// IsWaiting reports whether the given agent is currently waiting on input.
func (h *Hub) IsWaiting(agent string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waiting[agent]
}

// Forget removes an agent from every in-memory map. Used by the TUI's /forget
// command to drop stale entries (agents whose claude process died but whose
// wire name still appears in /agents). If the agent later POSTs again it'll
// be re-registered fresh.
func (h *Hub) Forget(agent string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.known, agent)
	delete(h.waiting, agent)
	delete(h.prompts, agent)
	if h.lastSeen == agent {
		h.lastSeen = ""
	}
}

// KnownAgents returns a snapshot of every agent name that has submitted at
// least once, with the timestamp of its last contact.
func (h *Hub) KnownAgents() map[string]time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]time.Time, len(h.known))
	for k, v := range h.known {
		out[k] = v
	}
	return out
}

// Inbox is the read-end of messages arriving from agents. Drain it in a goroutine.
func (h *Hub) Inbox() <-chan Message { return h.inbox }

// SendPrompt queues a prompt for the named agent. Buffers up to promptQueueDepth
// unread per agent. Also clears the agent's "waiting" flag (the human just
// answered).
func (h *Hub) SendPrompt(agent, prompt string) {
	ch := h.promptChan(agent)
	select {
	case ch <- prompt:
	default:
		// drop oldest, push newest -- never block the typing loop
		<-ch
		ch <- prompt
	}
	h.mu.Lock()
	delete(h.waiting, agent)
	h.mu.Unlock()
	h.Record.prompt(agent, prompt)
}

// LastAgent returns the most recent agent name that submitted. Empty if none yet.
func (h *Hub) LastAgent() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSeen
}

// promptQueueDepth is how many unread prompts we buffer per agent. When the
// buffer is full, SendPrompt evicts the oldest (FIFO drop). 16 is comfortably
// past any realistic "agent is busy and I want to stack five follow-ups"
// scenario without being so big you forget which prompts are pending.
const promptQueueDepth = 16

func (h *Hub) promptChan(agent string) chan string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.prompts[agent]
	if !ok {
		ch = make(chan string, promptQueueDepth)
		h.prompts[agent] = ch
	}
	return ch
}

// HandleSubmit is the HTTP handler for POST /submit. It records the message,
// then long-polls (up to h.timeout) for a queued prompt to send back.
func (h *Hub) HandleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var in SubmitRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Agent) == "" {
		http.Error(w, "agent required", http.StatusBadRequest)
		return
	}

	msg := Message{Agent: in.Agent, Kind: in.Kind, Content: in.Content, At: time.Now()}
	// Real submits (not silent keepalives) mark the agent as waiting for input
	// and get pushed to the inbox. Keepalives do nothing UI-visible.
	isReal := in.Kind != "keepalive" && strings.TrimSpace(in.Content) != ""
	h.mu.Lock()
	h.lastSeen = in.Agent
	h.known[in.Agent] = msg.At
	if isReal {
		h.waiting[in.Agent] = true
	}
	h.mu.Unlock()
	if isReal {
		select {
		case h.inbox <- msg:
		default:
		}
	}
	// Keepalives are recorded too, as a liveness touch, but the recorder is the
	// one that decides they are not UI-visible.
	h.Record.submit(in.Agent, in.Kind, in.Content)

	// Long-poll.
	ch := h.promptChan(in.Agent)
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	var resp SubmitResponse
	select {
	case p := <-ch:
		resp.Prompt = p
	case <-ctx.Done():
		// timeout / client gone: return empty prompt; agent will re-poll.
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PermissionRequest is the JSON shape POST'd by the claude PreToolUse hook
// when ATRIUM_PERM_GATE is on. The handler creates a PendingPermission, prints
// a loud notice in the TUI, and long-polls the reply channel until the user
// runs /approve <id> or /deny <id>.
type PermissionRequest struct {
	Agent   string `json:"agent"`
	Command string `json:"command"`
	Tool    string `json:"tool,omitempty"`
}

// PermissionResponse is what the hub returns to the hook.
type PermissionResponse struct {
	Decision string `json:"decision"`        // "approve" or "block"
	Reason   string `json:"reason,omitempty"`
	// Command is present only when the human edited the command before
	// approving it. A hook that does not understand this field ignores it and
	// runs the original, so sending it is safe either way.
	Command string `json:"command,omitempty"`
}

// HandlePermission is the HTTP handler for POST /permission.
func (h *Hub) HandlePermission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var in PermissionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Agent) == "" {
		http.Error(w, "agent required", http.StatusBadRequest)
		return
	}
	if in.Tool == "" {
		in.Tool = "Bash"
	}

	pp := &PendingPermission{
		Agent:   in.Agent,
		Command: in.Command,
		Tool:    in.Tool,
		At:      time.Now(),
		reply:   make(chan permissionDecision, 1),
	}
	// A standing rule answers without ever reaching the human. This is the
	// whole point of deciding something "forever": the request is recorded for
	// the history, then answered, and no card or banner appears.
	storeID, auto := h.Record.permRequest(in.Agent, in.Tool, in.Command)
	if auto != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PermissionResponse{Decision: auto.Decision, Reason: auto.Reason})
		return
	}
	pp.storeID = storeID
	h.mu.Lock()
	h.permSeq++
	pp.ID = h.permSeq
	h.pendingPerm[pp.ID] = pp
	h.mu.Unlock()

	// Loud announce via inbox so the TUI prints it next to normal traffic.
	announce := Message{
		Agent:   in.Agent,
		Kind:    "perm-request",
		Content: fmt.Sprintf("{bold}{yellow}[PERM #%d]{reset} %s wants to run ({cyan}%s{reset}): %s\n  {green}/approve %d{reset}  ·  {red}/deny %d{reset}  ·  '{green}y{reset}'/'{red}n{reset}' (oldest)  ·  in the perms tab, {bold}type guidance + enter{reset} to deny with instructions",
			pp.ID, in.Agent, in.Tool, in.Command, pp.ID, pp.ID),
		At: pp.At,
	}
	select {
	case h.inbox <- announce:
	default:
	}

	// Block until human decides or client gives up. No internal timeout: the
	// hook side has its own deadline if it cares.
	w.Header().Set("Content-Type", "application/json")
	select {
	case dec := <-pp.reply:
		_ = json.NewEncoder(w).Encode(PermissionResponse{
			Decision: dec.Decision, Reason: dec.Reason, Command: dec.Command,
		})
	case <-r.Context().Done():
		// caller disconnected; clean up so the entry doesn't linger as a phantom
		h.mu.Lock()
		delete(h.pendingPerm, pp.ID)
		h.mu.Unlock()
		return
	}
}

// resolvePerm finds a pending permission by ID (or, when id == 0, picks the
// oldest pending one). Returns nil if nothing matches.
func (h *Hub) resolvePerm(id int) *PendingPermission {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id != 0 {
		return h.pendingPerm[id]
	}
	var oldest *PendingPermission
	for _, p := range h.pendingPerm {
		if oldest == nil || p.At.Before(oldest.At) {
			oldest = p
		}
	}
	return oldest
}

// decide answers a pending permission. Returns true if a pending entry was found.
func (h *Hub) decide(id int, decision, reason string) (*PendingPermission, bool) {
	pp := h.resolvePerm(id)
	if pp == nil {
		return nil, false
	}
	h.mu.Lock()
	delete(h.pendingPerm, pp.ID)
	h.mu.Unlock()
	h.Record.permDecided(pp.storeID, decision, reason)
	pp.reply <- permissionDecision{Decision: decision, Reason: reason}
	return pp, true
}

// DecideByStoreID resolves a pending permission by its durable id. This is how
// a decision made in the web UI reaches the agent that is blocked on it. A
// non-empty command is a rewrite the human typed in place of what the agent
// asked for.
func (h *Hub) DecideByStoreID(storeID, decision, reason, command string) bool {
	h.mu.Lock()
	var match *PendingPermission
	for _, p := range h.pendingPerm {
		if p.storeID == storeID {
			match = p
			break
		}
	}
	h.mu.Unlock()
	if match == nil {
		return false
	}
	if command != "" && command != match.Command {
		match.Command = command
	} else {
		command = ""
	}
	pp := h.resolvePerm(match.ID)
	if pp == nil {
		return false
	}
	h.mu.Lock()
	delete(h.pendingPerm, pp.ID)
	h.mu.Unlock()
	h.Record.permDecided(pp.storeID, decision, reason)
	pp.reply <- permissionDecision{Decision: decision, Reason: reason, Command: command}
	return true
}

// DecideOldest resolves the oldest pending permission. Returns id, agent, ok.
// Convenience wrapper for the TUI 'y'/'n' shortcut and the 'a'/'d' perms-view keys.
func (h *Hub) DecideOldest(decision, reason string) (int, string, bool) {
	pp, ok := h.decide(0, decision, reason)
	if !ok {
		return 0, "", false
	}
	return pp.ID, pp.Agent, true
}

// DecideByIDString resolves a permission by its string-form id (empty = oldest).
// Returns id, agent, ok. Returns ok=false if the id is malformed or no match.
func (h *Hub) DecideByIDString(idStr, decision, reason string) (int, string, bool) {
	id := 0
	if s := strings.TrimSpace(idStr); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, "", false
		}
		id = v
	}
	pp, ok := h.decide(id, decision, reason)
	if !ok {
		return 0, "", false
	}
	return pp.ID, pp.Agent, true
}

// PendingPermissions returns a snapshot of unanswered requests, oldest first.
func (h *Hub) PendingPermissions() []PendingPermission {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]PendingPermission, 0, len(h.pendingPerm))
	for _, p := range h.pendingPerm {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Serve runs the HTTP server until ctx is cancelled.
func (h *Hub) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", h.HandleSubmit)
	mux.HandleFunc("/permission", h.HandlePermission)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// RunTUI prints incoming agent messages to stdout and reads typed lines from
// stdin as prompts to the most-recently-active agent. Optional `@name ` prefix
// retargets that line at a specific agent. Quit with Ctrl-C or EOF.
//
// This is purposely dumb: stdin reads and stdout writes interleave; if you're
// mid-type when a message arrives, it'll print over your line. Good enough for
// "shovel one prompt at a time" dev work.
func (h *Hub) RunTUI(ctx context.Context, out io.Writer, in io.Reader) error {
	fmt.Fprintln(out, "[atrium hub] type prompts and press Enter. '@<agent> <text>' targets a specific agent. '/agents' lists known agents.")

	// goroutine: drain inbox -> stdout
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-h.Inbox():
				rendered := applySentinels(strings.TrimRight(m.Content, "\r\n"))
				fmt.Fprintf(out, "\n[%s] <%s/%s>\n%s\n\n> ",
					m.At.Format("15:04:05"), m.Agent, m.Kind, rendered)
			}
		}
	}()

	// foreground: read stdin lines
	r := bufio.NewScanner(in)
	r.Buffer(make([]byte, 0, 64*1024), 1<<20)
	fmt.Fprint(out, "> ")
	for r.Scan() {
		line := r.Text()
		if line == "" {
			fmt.Fprint(out, "> ")
			continue
		}
		// y/n shortcuts ALWAYS mean a permission decision. If nothing's pending
		// we say so instead of forwarding the literal "n" to the agent (a
		// surprise prompt the human definitely didn't mean to send).
		if line == "y" || line == "yes" || line == "n" || line == "no" {
			decision := "approve"
			if line == "n" || line == "no" {
				decision = "block"
			}
			if pp, ok := h.decide(0, decision, "via "+line); ok {
				fmt.Fprintf(out, "[atrium] %s perm #%d (%s)\n> ", decision, pp.ID, pp.Agent)
			} else {
				fmt.Fprintln(out, "[atrium] no pending permissions to "+decision)
				fmt.Fprint(out, "> ")
			}
			continue
		}
		// Slash-commands: meta operations that don't go to an agent.
		if strings.HasPrefix(line, "/") {
			h.handleSlash(out, line)
			fmt.Fprint(out, "> ")
			continue
		}
		agent, text := parseTarget(line, h.LastAgent())
		if agent == "" {
			fmt.Fprintln(out, "[atrium] no agent has greeted yet -- can't target. waiting...")
			fmt.Fprint(out, "> ")
			continue
		}
		// Warn on unknown @<name> instead of silently dropping the prompt on a
		// channel nobody is polling.
		known := h.KnownAgents()
		if _, ok := known[agent]; !ok {
			fmt.Fprintf(out, "[atrium] warn: no agent named '%s' has submitted yet. known: %s\n",
				agent, joinAgentNames(known))
			fmt.Fprint(out, "> ")
			continue
		}
		h.SendPrompt(agent, text)
		fmt.Fprintf(out, "[atrium] -> %s\n> ", agent)
	}
	return r.Err()
}

func (h *Hub) handleSlash(out io.Writer, line string) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "/"))
	cmd, rest := body, ""
	if sp := strings.IndexAny(body, " \t"); sp >= 0 {
		cmd, rest = body[:sp], strings.TrimSpace(body[sp+1:])
	}
	switch cmd {
	case "agents", "ls", "list":
		known := h.KnownAgents()
		if len(known) == 0 {
			fmt.Fprintln(out, "[atrium] no agents have submitted yet")
			return
		}
		last := h.LastAgent()
		names := make([]string, 0, len(known))
		for n := range known {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintln(out, "[atrium] known agents:")
		for _, n := range names {
			marker := "  "
			if n == last {
				marker = "* "
			}
			fmt.Fprintf(out, "  %s%s  (last %s)\n", marker, n, known[n].Format("15:04:05"))
		}
	case "perms", "pending":
		pp := h.PendingPermissions()
		if len(pp) == 0 {
			fmt.Fprintln(out, "[atrium] no pending permissions")
			return
		}
		fmt.Fprintln(out, "[atrium] pending permissions (oldest first):")
		for _, p := range pp {
			fmt.Fprintf(out, "  #%d  %s  (%s)  %s\n", p.ID, p.Agent, p.At.Format("15:04:05"), p.Command)
		}
	case "approve":
		h.applyPermDecision(out, rest, "approve")
	case "deny":
		h.applyPermDecision(out, rest, "block")
	case "help", "?":
		fmt.Fprintln(out, "[atrium] commands:")
		fmt.Fprintln(out, "  /agents              list known agents")
		fmt.Fprintln(out, "  /perms               list pending permission requests")
		fmt.Fprintln(out, "  /approve [id] [why]  approve a pending permission (omit id for oldest)")
		fmt.Fprintln(out, "  /deny  [id] [why]    deny a pending permission; 'why' is handed to the agent as guidance")
		fmt.Fprintln(out, "  y / n                shortcut: approve / deny the oldest pending")
		fmt.Fprintln(out, "  @<agent> <text>      target a specific agent")
		fmt.Fprintln(out, "  <text>               send to most recent agent")
	default:
		fmt.Fprintf(out, "[atrium] unknown command '/%s' -- try /help\n", cmd)
	}
}

func (h *Hub) applyPermDecision(out io.Writer, rest, decision string) {
	// rest may be "[id] [reason...]". A leading integer is the perm id; the
	// remainder (or the whole string, if it doesn't start with an int) is a
	// free-form reason handed back to the agent. Empty id means oldest pending.
	id := 0
	reason := "via /" + decision
	rest = strings.TrimSpace(rest)
	if rest != "" {
		fields := strings.SplitN(rest, " ", 2)
		if v, err := strconv.Atoi(fields[0]); err == nil {
			id = v
			if len(fields) == 2 {
				reason = strings.TrimSpace(fields[1])
			}
		} else {
			reason = rest
		}
	}
	pp, ok := h.decide(id, decision, reason)
	if !ok {
		if id == 0 {
			fmt.Fprintln(out, "[atrium] no pending permissions")
		} else {
			fmt.Fprintf(out, "[atrium] no pending permission with id %d\n", id)
		}
		return
	}
	fmt.Fprintf(out, "[atrium] %s perm #%d (%s)\n", decision, pp.ID, pp.Agent)
}

func joinAgentNames(known map[string]time.Time) string {
	if len(known) == 0 {
		return "(none yet)"
	}
	names := make([]string, 0, len(known))
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// parseTarget splits an "@name rest..." line. With no prefix, defaults to `def`.
func parseTarget(line, def string) (string, string) {
	if !strings.HasPrefix(line, "@") {
		return def, line
	}
	rest := strings.TrimPrefix(line, "@")
	sp := strings.IndexAny(rest, " \t")
	if sp < 0 {
		return strings.TrimSpace(rest), ""
	}
	return strings.TrimSpace(rest[:sp]), strings.TrimSpace(rest[sp+1:])
}

// HubDir returns the hub data dir under WORKTREE_ROOT (placeholder for future use).
func HubDir() string {
	root := os.Getenv("WORKTREE_ROOT")
	if root == "" {
		root = `D:\worktrees`
	}
	return strings.TrimRight(root, `\/`) + `\hub`
}
