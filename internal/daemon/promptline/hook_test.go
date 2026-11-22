package promptline

import "testing"

func TestHookPromptOfReadsBothDeliveryPaths(t *testing.T) {
	stop := `{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-11-22T09:40:00.000Z",` +
		`"hookErrors":["Message from the human, sent through atrium:\n\nUpdate the CI loop"]}`
	perm := `{"type":"user","timestamp":"2026-11-22T09:41:00.000Z","message":{"content":[{"type":"tool_result",` +
		`"is_error":true,"content":"PreToolUse:Bash hook error: Message from the human, sent through atrium. ` +
		`This tool call was not refused on its merits: it was interrupted to reach you. Read this, act on it, ` +
		`and retry the call if it still makes sense:\n\nExplain the ci loop"}]}}`
	for line, want := range map[string]string{stop: "Update the CI loop", perm: "Explain the ci loop"} {
		p, ok := HookPromptOf([]byte(line))
		if !ok || p.Text != want || p.Kind != PromptOperator || p.At.IsZero() {
			t.Errorf("HookPromptOf(%.40s) = %+v, %v; want operator prompt %q", line, p, ok, want)
		}
	}
}

func TestHookPromptOfLeavesPeersAndPlainResultsOut(t *testing.T) {
	for _, line := range []string{
		`{"type":"system","subtype":"stop_hook_summary","timestamp":"2026-11-22T09:40:00.000Z",` +
			`"hookErrors":["@orch says (a session, not the human). Answer:\n\nhi"]}`,
		`{"type":"user","timestamp":"2026-11-22T09:41:00.000Z","message":{"content":[{"type":"tool_result",` +
			`"content":"Message from the human, sent through atrium:\n\nnot an error"}]}}`,
	} {
		if p, ok := HookPromptOf([]byte(line)); ok {
			t.Errorf("HookPromptOf(%.40s) = %+v, want none", line, p)
		}
	}
}
