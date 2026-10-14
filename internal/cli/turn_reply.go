package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dovholuknf/atrium/internal/guard"
)

// replyGuard is the guard's reply rules, run on a Stop payload. It answers a
// block for turnEnded to print, or keepGoing.
//
// IT FAILS OPEN, unlike the PreToolUse guard. A Stop that blocks keeps a
// session working, so a guard that failed closed here is a session that never
// stops. A rule set that does not load, a panic and a payload with no message
// all answer keepGoing. The first is logged to stderr, once per run, and the
// session ends as it would have.
//
// IT BLOCKS AT MOST ONCE A TURN. stop_hook_active means the model is already
// answering a block, and blocking that answer never terminates.
//
// It corrects and cannot prevent: the reply is written before a Stop fires.
func replyGuard(in turnInput, env guard.Env) (out string) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(os.Stderr, "atrium: the reply guard failed (%v), so the turn ends\n", p)
			out = keepGoing
		}
	}()
	if in.StopHookActive || strings.TrimSpace(in.LastAssistantMessage) == "" || !guardOn(env.Getenv("ATRIUM_GUARD")) {
		return keepGoing
	}
	rules, err := guardRules(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atrium: the guard rules do not load (%v), so the reply is not checked\n", err)
		return keepGoing
	}
	d := rules.Evaluate(guard.Input{ToolName: guard.ReplyTool, CWD: in.CWD,
		LastAssistantMessage: in.LastAssistantMessage}, env)
	if d.Action != "deny" || strings.TrimSpace(d.Reason) == "" {
		return keepGoing
	}
	b, err := json.Marshal(map[string]string{"decision": "block", "reason": d.Reason})
	if err != nil {
		return keepGoing
	}
	return string(b)
}
