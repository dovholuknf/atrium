package cli

import (
	"encoding/json"
	"regexp"
	"strings"
)

// `atrium hook --event no-poll`: refuses the commands a worker uses to wait on
// something it started, because every poll is a paid turn.
//
// It rides PreToolUse on Bash and PowerShell, in the `--settings` atrium writes
// for a lean worker (internal/daemon/lean.go). A worker that wants a test result
// runs the test in ONE foreground call with a long timeout and gets the answer
// when it ends.
//
// A HOOK MUST NEVER FAIL A SESSION. Anything unreadable, unrecognised or
// surprising prints nothing and exits 0, and the tool call runs.

// noPollEvent is the --event value that selects this hook.
const noPollEvent = "no-poll"

const noPollReason = "Do not sleep or poll. Run the command in ONE foreground call with a long " +
	"timeout (up to 600000 ms) and read its result when it returns. Never run a test in the " +
	"background, never wait on it with sleep, and never read a background task's .output file."

var (
	// sleep N, Start-Sleep N, Start-Sleep -Seconds N, in any position of a command line.
	noPollSleep = regexp.MustCompile(`(?i)(^|[\s;&|(])(start-)?sleep\s+(-\w+\s+)?[\d$]`)
	// A read of a background task's output file, however it is read.
	noPollOutput = regexp.MustCompile(`(?i)\b(tail|cat|head|type|get-content|gc|grep|rg|select-string)\b[^\n]*\.output\b`)
	// A command that runs the test suite.
	noPollTest = regexp.MustCompile(`(?i)\bgo\s+test\b|test-board-headless\.js`)
)

type noPollPayload struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	} `json:"tool_input"`
}

// noPollVerdict is the reason to refuse a tool call, or empty to let it run.
func noPollVerdict(stdin []byte) (reason string) {
	defer func() {
		if recover() != nil {
			reason = ""
		}
	}()
	var in noPollPayload
	if len(stdin) == 0 || json.Unmarshal(stdin, &in) != nil {
		return ""
	}
	switch in.ToolName {
	case "Bash", "PowerShell":
	default:
		return ""
	}
	cmd := in.ToolInput.Command
	if noPollSleep.MatchString(cmd) || noPollOutput.MatchString(cmd) {
		return noPollReason
	}
	if in.ToolInput.RunInBackground && noPollTest.MatchString(cmd) {
		return noPollReason
	}
	return ""
}

// noPollHook is the bytes to print for a PreToolUse payload, or nil to print nothing.
func noPollHook(stdin []byte) []byte {
	reason := noPollVerdict(stdin)
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	b, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
	if err != nil {
		return nil
	}
	return b
}
