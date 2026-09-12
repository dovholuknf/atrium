package api

import (
	"strings"
	"testing"
)

// The boiling, against the shapes a real transcript mixes together: a typed
// prompt, a tool result wearing the same `user` type, an assistant record
// carrying text beside a tool call and thinking, a system reminder inside a
// turn somebody did type, and a subagent's own exchange inline.
func TestBoilDownKeepsOnlyTheConversation(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"content":"add the export\n<system-reminder>\nnot typed by anybody\n</system-reminder>\nsmall as you can"}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"pondering"},{"type":"text","text":"on it"},{"type":"tool_use","name":"Bash","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"1234 files"}]}}`,
		`{"type":"user","isSidechain":true,"message":{"content":"somebody else entirely"}}`,
		`{"type":"user","message":{"content":"/exit"}}`,
	}
	var out strings.Builder
	boilDown(&out, strings.NewReader(strings.Join(lines, "\n")), "an export", "abc-123")
	got := out.String()

	for _, want := range []string{"# an export", "abc-123", "## you", "add the export",
		"small as you can", "## claude", "on it", "1 subagent records left out"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, gone := range []string{"not typed by anybody", "pondering", "tool_use",
		"1234 files", "somebody else entirely", "/exit"} {
		if strings.Contains(got, gone) {
			t.Errorf("should have been boiled out: %q in:\n%s", gone, got)
		}
	}
}
