package pulls

import (
	"strings"
	"testing"
)

func TestPanelEmptyReasonNamesMissingAgentFiles(t *testing.T) {
	t.Parallel()
	got := panelEmptyReason([]string{"review-panel", "go-reviewer"}, "/home/x/.claude/agents")
	for _, want := range []string{"review-panel.md", "go-reviewer.md", "/home/x/.claude/agents", "room-check -Fix"} {
		if !strings.Contains(got, want) {
			t.Errorf("reason %q lacks %q", got, want)
		}
	}
	if none := panelEmptyReason(nil, "/d"); strings.Contains(none, "not installed") {
		t.Errorf("no missing agent must not blame the install: %q", none)
	}
}
