package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
// A HOOK MUST NEVER FAIL A SESSION. Every failure below exits 0 with no
// decision, and Claude Code then runs its own permission flow. A room that
// cannot be reached within hookBudget is also said in a systemMessage. That is
// the whole posture: a daemon that is down, frozen or confused lets work
// through rather than bricking every session.

// permissionEvent is the --event value that selects this hook.
const permissionEvent = "permission"

// permSkipTools are never gated: pure reads, and ToolSearch, whose eventual
// tool call is what gets gated. Claude Code renamed Task to Agent; both stay.
var permSkipTools = map[string]bool{
	"Read": true, "Grep": true, "Glob": true, "WebFetch": true, "WebSearch": true,
	"TodoWrite": true, "Task": true, "Agent": true, "ToolSearch": true,
}

// permMaxDetails caps details so a huge write does not become a wall of text
// on the board.
const permMaxDetails = 6000

// defaultProbeTimeout is how long /gate may take. The POST below has no
// deadline once the room has taken it, so this probe and permHeaderTimeout
// are what stop one frozen room holding every gated call on the machine.
const defaultProbeTimeout = 2 * time.Second

type permissionPayload struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	CWD           string          `json:"cwd"`
	SessionID     string          `json:"session_id"`
	ToolUseID     string          `json:"tool_use_id"`
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
	return permissionHookForRunner(hubURL, stdin, pidOf, "")
}

func permissionHookForRunner(hubURL string, stdin []byte, pidOf func() int, runner string) []byte {
	start := time.Now()
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
	native := in.HookEventName == "PermissionRequest"
	if !native && permSkipTools[in.ToolName] {
		return nil
	}

	agent, nameSource := agentNameSource("")
	base := hubAddress(hubURL)

	hookEvent := "PreToolUse"
	if native {
		hookEvent = in.HookEventName
	}
	joined, err := probeGate(base, agent, hookLeft(start))
	if errors.Is(err, errAnswered) {
		return nil
	}
	if err != nil {
		return hookWarning(hookEvent, runner, unreachable("hook --event permission", base, err))
	}
	if !force && !joined && !mcpWired() {
		return nil
	}

	var input map[string]any
	if len(in.ToolInput) > 0 {
		_ = json.Unmarshal(in.ToolInput, &input)
	}
	summary := permSummary(input, in.ToolInput)
	// Native requests without a tool id must stay distinct even when their commands match.
	key := in.ToolUseID
	if key == "" && !native {
		sum := sha256.Sum256([]byte(in.SessionID + "|" + agent + "|" + in.ToolName + "|" + summary))
		key = hex.EncodeToString(sum[:12])
	}

	if native && runner == "" {
		runner = "codex"
	}
	body, err := json.Marshal(map[string]any{
		"runner":      whichRunner(runner),
		"agent":       agent,
		"task_id":     os.Getenv("ATRIUM_TASK_ID"),
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

	var ans permissionAnswer
	if native {
		ans, err = postPermissionOnce(permClient(hookLeft(start)), base, body)
	} else {
		ans, err = postPermission(base, body, start.Add(hookBudget))
	}
	if errors.Is(err, errAnswered) {
		return nil
	}
	if err != nil {
		return hookWarning(hookEvent, runner, unreachable("hook --event permission", base, err))
	}

	if native {
		return nativePermissionAnswer(ans, summary)
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
	if ans.Decision == "approve" && ans.Command != "" && ans.Command != summary {
		if upd := editedInput(input, ans.Command); upd != nil {
			out["updatedInput"] = upd
		} else {
			// Without updatedInput the original command would run instead of the approved edit.
			out["permissionDecision"] = "deny"
			out["permissionDecisionReason"] = "the edit did not parse, nothing was run"
		}
	}
	b, err := json.Marshal(map[string]any{"hookSpecificOutput": out})
	if err != nil {
		return nil
	}
	return b
}

// permRetryFor and permRetryEvery bound the retry of a permission request that got no decision. A room restart
// takes a few seconds and the daemon upserts the request row by its dedup key before it waits, so asking again with
// the same key is answered once by the new daemon. ATRIUM_PERM_RETRY_FOR and ATRIUM_PERM_RETRY_EVERY (durations)
// shorten them, which is how a test does it without assigning a package variable.
func permRetryFor() time.Duration { return envDuration("ATRIUM_PERM_RETRY_FOR", hookBudget) }

func permRetryEvery() time.Duration { return envDuration("ATRIUM_PERM_RETRY_EVERY", time.Second) }

// permHeaderTimeout bounds how long the room may take to send the headers of /permission. The room sends them as soon
// as it parks a request for a human, so only a room that does not answer runs into it.
func permHeaderTimeout() time.Duration { return envDuration("ATRIUM_PERM_HEADER_TIMEOUT", hookBudget) }

// permClient waits on a parked request as long as ATRIUM_PERM_TIMEOUT allows, and on the headers no longer than left.
func permClient(left time.Duration) *http.Client {
	c := hookClient(permPostTimeout())
	// Zero would mean no bound at all, so a spent budget still gets a tenth of a second.
	left = max(left, 100*time.Millisecond)
	t := c.Transport.(*http.Transport)
	t.DialContext = (&net.Dialer{Timeout: min(time.Second, left)}).DialContext
	t.ResponseHeaderTimeout = min(permHeaderTimeout(), left)
	return c
}

func envDuration(name string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name))); err == nil && d > 0 {
		return d
	}
	return def
}

// errAnswered marks a room that answered without a decision. Only a room that could not be reached is warned about.
var errAnswered = errors.New("atrium answered")

type permissionAnswer struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Command  string `json:"command"`
}

// postPermission asks the room for a decision. ANY answer that is not one is retried with the same body, so the
// same dedup key: a refused or reset connection, a timeout, a non-2xx status, or a 2xx with no decision in it, which
// are what a room on its way down or not yet migrated gives. After permRetryFor it gives up and the caller fails
// open, recording an allow nowhere. No retry starts after budget, the end of the hook's hookBudget.
func postPermission(base string, body []byte, budget time.Time) (permissionAnswer, error) {
	every := permRetryEvery()
	deadline := time.Now().Add(permRetryFor())
	if budget.Before(deadline) {
		deadline = budget
	}
	for {
		ans, err := postPermissionOnce(permClient(time.Until(deadline)), base, body)
		if err == nil {
			return ans, nil
		}
		if !time.Now().Add(every).Before(deadline) {
			return permissionAnswer{}, err
		}
		time.Sleep(every)
	}
}

func postPermissionOnce(client *http.Client, base string, body []byte) (permissionAnswer, error) {
	var ans permissionAnswer
	resp, err := client.Post(base+"/permission", "application/json", bytes.NewReader(body))
	if err != nil {
		return ans, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ans, fmt.Errorf("%w %s", errAnswered, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ans); err != nil {
		return ans, fmt.Errorf("%w something that did not parse: %v", errAnswered, err)
	}
	if ans.Decision != "approve" && ans.Decision != "block" {
		return ans, fmt.Errorf("%w with no decision", errAnswered)
	}
	return ans, nil
}

// probeGate asks /gate whether this session is joined, within left. Any error
// means fail open, and one that is not errAnswered means atrium was not reached.
func probeGate(base, agent string, left time.Duration) (joined bool, err error) {
	timeout := defaultProbeTimeout
	if v := strings.TrimSpace(os.Getenv("ATRIUM_PERM_PROBE_TIMEOUT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			timeout = time.Duration(max(1, n)) * time.Second
		}
	}
	// The client timeout covers the body too, so a daemon that sends headers
	// and stalls is still cut off.
	client := hookClient(min(timeout, left))
	resp, err := client.Get(base + "/gate?agent=" + url.QueryEscape(agent))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%w %s", errAnswered, resp.Status)
	}
	var g struct {
		Gate bool `json:"gate"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&g); err != nil {
		return false, fmt.Errorf("%w something that did not parse: %v", errAnswered, err)
	}
	return g.Gate, nil
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

func nativePermissionAnswer(ans permissionAnswer, summary string) []byte {
	behavior := ""
	switch ans.Decision {
	case "approve":
		behavior = "allow"
	case "block":
		behavior = "deny"
	default:
		return nil
	}
	reason := ans.Reason
	if behavior == "allow" && ans.Command != "" && ans.Command != summary {
		// PermissionRequest rejects updatedInput, so an edit must never approve the original call.
		behavior = "deny"
		reason = "atrium edited this request. retry with the edited input: " + ans.Command
	}
	decision := map[string]any{"behavior": behavior}
	if behavior == "deny" {
		if reason == "" {
			reason = "blocked via atrium"
		}
		decision["message"] = reason
	}
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionRequest", "decision": decision,
	}})
	return b
}
