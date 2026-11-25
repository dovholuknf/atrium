package cli

import (
	"encoding/json"
	"strings"
)

// cursorPayload reports whether a hook payload came from cursor-agent, which marks every one with cursor_version.
func cursorPayload(raw []byte) bool {
	var in struct {
		CursorVersion string `json:"cursor_version"`
	}
	return json.Unmarshal(raw, &in) == nil && in.CursorVersion != ""
}

// borrowedByCursor says this hook is one of claude's that cursor ran from ~/.claude/settings.json. Cursor runs those
// beside atrium's own plugin hooks, which say --runner cursor, so the borrowed copy stays silent and nothing is
// reported or gated twice.
func borrowedByCursor(runnerFlag string) bool {
	return !strings.EqualFold(strings.TrimSpace(runnerFlag), "cursor") && cursorPayload(readStdin())
}

// cursorPreToolUse is the gate's claude-shaped answer in cursor's own words, for a cursor payload. Cursor reads
// claude's hookSpecificOutput only behind a feature flag, and its `allow` skips nothing unless the card runs with
// --force, which withCursorLaunch adds for a gated launch.
func cursorPreToolUse(out []byte) []byte {
	if out == nil || !cursorPayload(readStdin()) {
		return out
	}
	var ans struct {
		Hook struct {
			Decision string          `json:"permissionDecision"`
			Reason   string          `json:"permissionDecisionReason"`
			Input    json.RawMessage `json:"updatedInput"`
			Context  string          `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
		System string `json:"systemMessage"`
	}
	if json.Unmarshal(out, &ans) != nil {
		return nil
	}
	native := map[string]any{}
	switch ans.Hook.Decision {
	case "allow", "deny":
		native["permission"] = ans.Hook.Decision
		native["user_message"] = ans.Hook.Reason
		native["agent_message"] = ans.Hook.Reason
		if len(ans.Hook.Input) > 0 {
			native["updated_input"] = ans.Hook.Input
		}
	default:
		ctx := ans.Hook.Context
		if ctx == "" {
			ctx = ans.System
		}
		if ctx == "" {
			return nil
		}
		native["additional_context"] = ctx
	}
	b, _ := json.Marshal(native)
	return b
}
