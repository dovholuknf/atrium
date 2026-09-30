package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// `atrium hook --event permission`: atrium's own permission gate, the Go
// replacement for the dotfiles script atrium-perm-hook.ps1.
//
// The wire shape is the contract in the header of internal/daemon/permwait.go.
//
// A HOOK MUST NEVER FAIL A SESSION. Every failure below returns nil, which
// prints nothing and exits 0, and Claude Code then runs its own permission
// flow. That is the whole posture: a daemon that is down, frozen or confused
// lets work through rather than bricking every session.

// permissionEvent is the --event value that selects this hook.
const permissionEvent = "permission"

// permSkipTools are never gated: pure reads, and ToolSearch, whose eventual
// tool call is what gets gated.
var permSkipTools = map[string]bool{
	"Read": true, "Grep": true, "Glob": true, "WebFetch": true, "WebSearch": true,
	"TodoWrite": true, "Task": true, "ToolSearch": true,
}

// permMaxDetails caps details so a huge write does not become a wall of text
// on the board.
const permMaxDetails = 6000

// defaultProbeTimeout is how long /gate may take. The POST below has no
// deadline, so this probe is what stops one frozen room holding every gated
// call on the machine.
const defaultProbeTimeout = 3 * time.Second

type permissionPayload struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	CWD       string          `json:"cwd"`
	SessionID string          `json:"session_id"`
	ToolUseID string          `json:"tool_use_id"`
}

// runPermissionHook does the whole job and returns the bytes to print, or nil
// to print nothing. It never returns an error because nothing could act on one.
func runPermissionHook(hubURL string, stdin []byte, pid int) []byte {
	return permissionHook(hubURL, stdin, func() int { return pid })
}

// permissionHook is runPermissionHook with the runner's pid asked for only when
// there is something to post. Finding it walks the process table, and this is the
// hot path: every tool call in every session runs it, and most of them have
// nothing to do (gate off, no payload, a read-only tool, no gate on the room).
func permissionHook(hubURL string, stdin []byte, pidOf func() int) []byte {
	gate := strings.ToLower(strings.TrimSpace(os.Getenv("ATRIUM_PERM_GATE")))
	if gate == "off" {
		return nil
	}
	force := false
	switch gate {
	case "on", "force", "1", "true", "yes":
		force = true
	}

	var in permissionPayload
	if len(stdin) == 0 || json.Unmarshal(stdin, &in) != nil {
		return nil
	}
	if permSkipTools[in.ToolName] {
		return nil
	}

	agent, nameSource := agentNameSource("")
	base := hubAddress(hubURL)

	joined, ok := probeGate(base, agent)
	if !ok {
		return nil
	}
	if !force && !joined && !mcpWired() {
		return nil
	}

	var input map[string]any
	if len(in.ToolInput) > 0 {
		_ = json.Unmarshal(in.ToolInput, &input)
	}
	summary := permSummary(input, in.ToolInput)
	// The tool_use_id is the dedup key when there is one: the same across a
	// retry of this attempt and different for an identical command run later.
	// Without it, fall back to a hash, which the daemon replays for a couple
	// of minutes only.
	key := in.ToolUseID
	if key == "" {
		sum := sha256.Sum256([]byte(in.SessionID + "|" + agent + "|" + in.ToolName + "|" + summary))
		key = hex.EncodeToString(sum[:12])
	}

	body, err := json.Marshal(map[string]any{
		"agent":       agent,
		"name_source": nameSource,
		"tool":        in.ToolName,
		"command":     summary,
		"pid":         pidOf(),
		"cwd":         in.CWD,
		"details":     permDetails(input),
		"dedup_key":   key,
		"tool_use_id": in.ToolUseID,
	})
	if err != nil {
		return nil
	}

	client := &http.Client{Timeout: permPostTimeout()}
	resp, err := client.Post(base+"/permission", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var ans struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		Command  string `json:"command"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ans) != nil {
		return nil
	}

	out := map[string]any{"hookEventName": "PreToolUse"}
	switch ans.Decision {
	case "approve":
		out["permissionDecision"] = "allow"
	case "block":
		out["permissionDecision"] = "deny"
	default:
		return nil
	}
	reason := ans.Reason
	if reason == "" {
		reason = "via atrium hub"
	}
	out["permissionDecisionReason"] = reason
	// A rewrite only applies to an approval: a block already carries its
	// guidance in the reason.
	if ans.Decision == "approve" && ans.Command != "" && ans.Command != summary {
		if upd := editedInput(input, ans.Command); upd != nil {
			out["updatedInput"] = upd
		}
	}
	b, err := json.Marshal(map[string]any{"hookSpecificOutput": out})
	if err != nil {
		return nil
	}
	return b
}

// probeGate asks /gate whether this session is joined. ok is false when atrium
// does not answer within the deadline, which means fail open.
func probeGate(base, agent string) (joined, ok bool) {
	timeout := defaultProbeTimeout
	if v := strings.TrimSpace(os.Getenv("ATRIUM_PERM_PROBE_TIMEOUT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			timeout = time.Duration(max(1, n)) * time.Second
		}
	}
	// The client timeout covers the body too, so a daemon that sends headers
	// and stalls is still cut off.
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(base + "/gate?agent=" + url.QueryEscape(agent))
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var g struct {
		Gate bool `json:"gate"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&g) != nil {
		return false, false
	}
	return g.Gate, true
}

// permPostTimeout is zero (none) unless ATRIUM_PERM_TIMEOUT says otherwise,
// since a human may take minutes. Accepts seconds, a Go duration, or the
// hh:mm:ss the script took.
func permPostTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("ATRIUM_PERM_TIMEOUT"))
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if parts := strings.Split(v, ":"); len(parts) == 3 {
		h, e1 := strconv.Atoi(parts[0])
		m, e2 := strconv.Atoi(parts[1])
		s, e3 := strconv.Atoi(parts[2])
		if e1 == nil && e2 == nil && e3 == nil && h*3600+m*60+s > 0 {
			return time.Duration(h*3600+m*60+s) * time.Second
		}
	}
	return 0
}

// mcpWired reports whether an .mcp.json naming atrium-agent sits in the cwd or
// any parent: the signal that this is an atrium-connected agent.
func mcpWired() bool {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	for {
		if raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json")); err == nil &&
			bytes.Contains(raw, []byte("atrium-agent")) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func strField(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// permSummary picks the most useful field of the tool input for a human to read.
func permSummary(input map[string]any, raw json.RawMessage) string {
	if input == nil {
		return ""
	}
	if s := strField(input, "command"); s != "" {
		return s
	}
	if s := strField(input, "file_path"); s != "" {
		if strField(input, "new_string") != "" {
			s += " <- (replace edit)"
		}
		if c := strField(input, "content"); c != "" {
			s += " <- (write " + strconv.Itoa(len([]rune(c))) + " chars)"
		}
		return s
	}
	if s := strField(input, "url"); s != "" {
		return s
	}
	if s := strField(input, "pattern"); s != "" {
		return s
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return ""
	}
	return buf.String()
}

// permDetails is what the tool would actually change: a path says which file
// and not what happens to it.
func permDetails(input map[string]any) string {
	var d string
	_, hasNew := input["new_string"]
	_, hasOld := input["old_string"]
	switch {
	case hasNew || hasOld:
		d = "--- removing\n" + strField(input, "old_string") + "\n\n+++ adding\n" + strField(input, "new_string")
	case input["content"] != nil:
		d = "+++ writing\n" + strField(input, "content")
	default:
		if edits, ok := input["edits"].([]any); ok {
			var parts []string
			for _, e := range edits {
				em, _ := e.(map[string]any)
				parts = append(parts, "--- removing\n"+strField(em, "old_string")+"\n\n+++ adding\n"+strField(em, "new_string"))
			}
			d = strings.Join(parts, "\n\n=== next edit ===\n\n")
		}
	}
	if r := []rune(d); len(r) > permMaxDetails {
		d = string(r[:permMaxDetails]) + "\n\n... truncated, " + strconv.Itoa(len(r)-permMaxDetails) + " more characters"
	}
	return d
}

// editedInput maps the human's edited display string back onto the tool's own
// input, mirroring permSummary. Every original field is kept: a partial
// tool_input would drop the other arguments. Nil means there is nothing safe to
// send.
func editedInput(input map[string]any, edited string) map[string]any {
	upd := make(map[string]any, len(input))
	for k, v := range input {
		upd[k] = v
	}
	switch {
	case strField(input, "command") != "":
		upd["command"] = edited
	case strField(input, "file_path") != "":
		// File tools show as `<path> <- (what is happening)`. Only the path
		// is real input.
		if i := strings.Index(edited, " <- "); i >= 0 {
			edited = edited[:i]
		}
		upd["file_path"] = strings.TrimSpace(edited)
	case strField(input, "url") != "":
		upd["url"] = edited
	case strField(input, "pattern") != "":
		upd["pattern"] = edited
	default:
		// The fallback showed raw JSON, so an edit has to parse back as JSON.
		var parsed map[string]any
		if json.Unmarshal([]byte(edited), &parsed) != nil {
			return nil
		}
		return parsed
	}
	return upd
}
