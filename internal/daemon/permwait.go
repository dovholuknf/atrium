package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The permission long-poll: a gated tool call parked on POST /permission until
// somebody answers it.
//
// THE WIRE SHAPE IS A CONTRACT with the dotfiles PreToolUse hook,
// `atrium-perm-hook.ps1`, which POSTs every gated call here and blocks on the
// answer. The URL, the request fields and the response fields below do not
// change without that script changing first. Neither does the posture: the
// hook fails open when nothing answers, so a daemon that is down lets work
// through rather than bricking every session.

// PermissionRequest is the JSON shape POST'd by the claude PreToolUse hook.
type PermissionRequest struct {
	Agent   string `json:"agent"`
	Command string `json:"command"`
	Tool    string `json:"tool,omitempty"`
	// PID is the runner's own process, not the hook's. It is what lets atrium
	// tell a live session from a dead one by asking the operating system,
	// which costs nothing, instead of asking the runner, which costs a turn.
	PID int `json:"pid,omitempty"`
	// Cwd is where the runner is working, used to fill in the card.
	Cwd string `json:"cwd,omitempty"`
	// Details is what the tool would actually do: the diff for an edit, the
	// content for a write. The command line alone names the target without
	// saying what happens to it, which is not enough to decide on.
	Details string `json:"details,omitempty"`
	// DedupKey identifies one attempt, so a retried request is recognized as
	// the same question rather than asked again.
	//
	// The hook fails open when atrium is unreachable, and it also retries. A
	// daemon that crashed between recording a decision and answering would
	// otherwise ask the operator the same thing twice, and the second answer
	// would be given against a situation that had already moved on. Empty is
	// allowed and means "treat this as new".
	//
	// A key built by hashing the session, the tool and the command is stable
	// across a retry AND across running the same command tomorrow, which is
	// why a decided key is only replayable for a couple of minutes. Prefer
	// ToolUseID: it does not have that problem.
	DedupKey string `json:"dedup_key,omitempty"`
	// ToolUseID is the runner's own id for this tool-use ATTEMPT.
	//
	// Claude Code's PreToolUse payload carries `tool_use_id`, and Codex has
	// the same field. It is exactly what a dedup key wants and what a hash of
	// the command can never be: the same across a retry of this attempt, and
	// different for an identical command run later.
	//
	// When present it becomes the key and the replay window does not apply,
	// because there is nothing to guard against. A hook that sends only a hash
	// keeps the old behavior.
	ToolUseID string `json:"tool_use_id,omitempty"`
}

// PermissionResponse is what the daemon returns to the hook.
type PermissionResponse struct {
	Decision string `json:"decision"` // "approve" or "block"
	Reason   string `json:"reason,omitempty"`
	// Command is present only when the human edited the command before
	// approving it. A hook that does not understand this field ignores it and
	// runs the original, so sending it is safe either way.
	Command string `json:"command,omitempty"`
}

// AutoDecision is the permission chain's answer to a request that never
// reaches a human: a replay, a message, a shelved card, a rule, auto mode.
type AutoDecision struct {
	Decision string
	Reason   string
}

// pendingPermission is one hook connection parked on an answer.
type pendingPermission struct {
	id      int
	command string
	at      time.Time
	reply   chan permissionDecision

	// storeID is the durable permission id. Empty when recording the request
	// failed, in which case the hook still waits and a restart answers it.
	storeID string
}

type permissionDecision struct {
	Decision string // "approve" or "block"
	Reason   string
	// Command, when set, is a rewritten command the human wants run instead of
	// the one the agent asked for. The hook decides whether to honor it.
	Command string
}

// permWait holds the reply channels. In memory on purpose: each one belongs to
// a live connection and dies with it.
type permWait struct {
	mu      sync.Mutex
	seq     int
	pending map[int]*pendingPermission
}

func newPermWait() *permWait {
	return &permWait{pending: map[int]*pendingPermission{}}
}

// handlePermission is the HTTP handler for POST /permission.
func (d *Daemon) handlePermission(w http.ResponseWriter, r *http.Request) {
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

	pp := &pendingPermission{
		command: in.Command,
		at:      time.Now(),
		reply:   make(chan permissionDecision, 1),
	}
	// The chain answers without ever reaching the human when it can. This is
	// the whole point of deciding something "forever": the request is recorded
	// for the history, then answered, and no card or banner appears.
	storeID, auto, err := d.onPermRequest(in)
	if err != nil {
		log.Printf("[atrium] record permission from %s: %v", in.Agent, err)
		storeID, auto = "", nil
	}
	if auto != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PermissionResponse{Decision: auto.Decision, Reason: auto.Reason})
		return
	}
	pp.storeID = storeID
	pw := d.perms
	pw.mu.Lock()
	pw.seq++
	pp.id = pw.seq
	pw.pending[pp.id] = pp
	pw.mu.Unlock()

	// Block until a human decides or the client gives up. No internal timeout:
	// the hook side has its own deadline if it cares.
	w.Header().Set("Content-Type", "application/json")
	select {
	case dec := <-pp.reply:
		_ = json.NewEncoder(w).Encode(PermissionResponse{
			Decision: dec.Decision, Reason: dec.Reason, Command: dec.Command,
		})
	case <-r.Context().Done():
		// Caller disconnected. Clean up so the entry does not linger as a
		// phantom, which is what lets the orphan reaper see it has gone.
		pw.mu.Lock()
		delete(pw.pending, pp.id)
		pw.mu.Unlock()
		return
	}
}

// decideByStoreID resolves a parked request by its durable id. This is how a
// decision made on the board reaches the agent that is blocked on it. A
// non-empty command is a rewrite the human typed in place of what the agent
// asked for. False means nobody is parked on that id.
func (d *Daemon) decideByStoreID(storeID, decision, reason, command string) bool {
	pw := d.perms
	pw.mu.Lock()
	var match *pendingPermission
	for _, p := range pw.pending {
		if p.storeID == storeID {
			match = p
			break
		}
	}
	if match != nil {
		delete(pw.pending, match.id)
	}
	pw.mu.Unlock()
	if match == nil {
		return false
	}
	if command == match.command {
		command = ""
	}
	if match.storeID != "" {
		d.onPermDecided(match.storeID, decision, reason)
	}
	match.reply <- permissionDecision{Decision: decision, Reason: reason, Command: command}
	return true
}

// liveStoreIDs is the set of durable permission ids an agent is actually
// parked on right now.
//
// The store knows which requests are unanswered. Only this process knows which
// of those still has somebody listening for the answer: the reply channel lives
// here and dies with the connection that created it.
//
// The difference between the two sets is the orphans. A request whose agent
// has gone can never be answered, because there is nothing on the other end
// to hand the answer to, and a card sitting in needs-permission behind one is
// asking a question for a session that no longer exists.
func (d *Daemon) liveStoreIDs() map[string]bool {
	pw := d.perms
	pw.mu.Lock()
	defer pw.mu.Unlock()
	out := make(map[string]bool, len(pw.pending))
	for _, p := range pw.pending {
		if p.storeID != "" {
			out[p.storeID] = true
		}
	}
	return out
}
