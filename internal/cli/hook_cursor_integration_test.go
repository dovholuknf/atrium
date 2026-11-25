//go:build integration

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/store"
)

const cursorVersion = "2026.11.22-e9042b2"

// A cursor session driven through the lines atrium's cursor plugin writes, with payloads shaped as cursor-agent
// 2026.11.22 sends them, moves its card as a claude session's does and reports runner cursor.
func TestFakeCursorDrivesTheCardThroughEveryHook(t *testing.T) {
	agentAddr, humanAddr := startTestDaemon(t)
	for _, k := range []string{"ATRIUM_AGENT_NAME", "ATRIUM_TASK_ID", "ATRIUM_RUNNER", "ATRIUM_PERM_GATE"} {
		t.Setenv(k, "")
	}
	t.Setenv("ATRIUM_HUB_URL", "http://"+agentAddr)
	cwd := filepath.Join(t.TempDir(), "fake-cursor")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(cwd, "chat.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := func(event string, extra map[string]any) map[string]any {
		m := map[string]any{"session_id": "chat-1", "conversation_id": "chat-1", "cwd": cwd,
			"hook_event_name": event, "cursor_version": cursorVersion, "workspace_roots": []string{cwd}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	steps := []struct {
		line string
		step
	}{
		{"session --event start", step{hook: "sessionStart", payload: base("sessionStart", map[string]any{"transcript_path": nil}),
			status: store.StatusNeedsInput, activity: "-"}},
		{"hook --event prompt", step{hook: "beforeSubmitPrompt", payload: base("beforeSubmitPrompt", map[string]any{"prompt": "list the files"}),
			status: store.StatusRunning, activity: daemon.ActivityThinking}},
		{"hook --event tool-start", step{hook: "preToolUse", payload: base("preToolUse", map[string]any{
			"tool_name": "Shell", "tool_input": map[string]any{"command": "ls"}, "tool_use_id": "call-1"}),
			status: store.StatusRunning, activity: daemon.ActivityTool, tool: "Shell"}},
		{"hook --event tool-end", step{hook: "postToolUse", payload: base("postToolUse", map[string]any{
			"tool_name": "Shell", "tool_use_id": "call-1"}),
			status: store.StatusRunning, activity: daemon.ActivityThinking}},
		{"turn --event end", step{hook: "stop", payload: base("stop", map[string]any{"status": "completed", "loop_count": 0,
			"transcript_path": transcript}),
			status: store.StatusNeedsInput, activity: daemon.ActivityIdle}},
		{"session --event end", step{hook: "sessionEnd", payload: base("sessionEnd", map[string]any{"reason": "completed"}),
			status: store.StatusDone, activity: "-"}},
	}
	for _, s := range steps {
		raw, _ := json.Marshal(s.payload)
		if code := runHookLine(t, fakeExe+" "+s.line, string(raw)); code != 0 {
			t.Fatalf("%s: borrowed claude line exited %d", s.hook, code)
		}
		if code := runHookLine(t, fakeExe+" "+s.line+" --runner cursor", string(raw)); code != 0 {
			t.Fatalf("%s exited %d", s.hook, code)
		}
		assertCard(t, humanAddr, filepath.Base(cwd), "cursor", s.step)
	}
}

// Cursor also runs claude's own hooks from ~/.claude/settings.json. Those say no runner, and stay silent for a
// cursor payload, so a cursor session is reported and gated once. A claude payload is never taken for one.
func TestBorrowedClaudeHooksAreSilentForCursor(t *testing.T) {
	cases := []struct {
		payload, runner string
		borrowed        bool
	}{
		{`{"cursor_version":"` + cursorVersion + `","hook_event_name":"stop"}`, "", true},
		{`{"cursor_version":"` + cursorVersion + `","hook_event_name":"stop"}`, "claude", true},
		{`{"cursor_version":"` + cursorVersion + `","hook_event_name":"stop"}`, "cursor", false},
		{`{"hook_event_name":"Stop","stop_hook_active":false}`, "", false},
		{`not json`, "", false},
	}
	for _, c := range cases {
		restore := withStdin(t, c.payload)
		got := borrowedByCursor(c.runner)
		restore()
		if got != c.borrowed {
			t.Errorf("%s with --runner %q: borrowed %v, want %v", c.payload, c.runner, got, c.borrowed)
		}
	}
}

// The gate answers cursor in cursor's own fields, which is the only shape cursor reads without a feature flag.
func TestCursorGateAnswersInCursorsOwnWords(t *testing.T) {
	cursorIn := `{"cursor_version":"` + cursorVersion + `","hook_event_name":"preToolUse","tool_name":"Shell"}`
	cases := []struct {
		name, payload, out, want string
	}{
		{"approved", cursorIn,
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"via atrium hub"}}`,
			`{"agent_message":"via atrium hub","permission":"allow","user_message":"via atrium hub"}`},
		{"blocked", cursorIn,
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"no"}}`,
			`{"agent_message":"no","permission":"deny","user_message":"no"}`},
		{"approved as edited", cursorIn,
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"ok","updatedInput":{"command":"ls -a"}}}`,
			`{"agent_message":"ok","permission":"allow","updated_input":{"command":"ls -a"},"user_message":"ok"}`},
		{"atrium not reached", cursorIn,
			`{"systemMessage":"down","hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"down"}}`,
			`{"additional_context":"down"}`},
		{"no decision", cursorIn, ``, ``},
		{"a claude payload is left alone", `{"hook_event_name":"PreToolUse","tool_name":"Bash"}`,
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`,
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer withStdin(t, c.payload)()
			var out []byte
			if c.out != "" {
				out = []byte(c.out)
			}
			if got := string(cursorPreToolUse(out)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

// Cursor sends no stop_hook_active. loop_count above zero says a hook already continued this turn, so a block from
// the room is thrown away there too, and the stop is still posted.
func TestCursorStopInsideAContinuedTurnNeverBlocks(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&posted)
		_, _ = w.Write([]byte(`{"decision":"block","reason":"run the tests first"}`))
	}))
	defer srv.Close()
	restore := withStdin(t, `{"cwd":"/w","hook_event_name":"stop","loop_count":1,"cursor_version":"`+cursorVersion+`"}`)
	defer restore()

	if got := turnEnded(srv.URL, "end", "probe", "cursor"); got != keepGoing {
		t.Fatalf("got %q, wanted a plain continue inside a continued turn", got)
	}
	if posted["stop_hook_active"] != true {
		t.Fatalf("posted %v, want stop_hook_active true", posted)
	}
}
