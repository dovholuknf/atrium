package cli

import (
	"strings"
	"testing"
)

// atrium_ci on the stdio server: the question goes to this room's daemon at /v1/hub/ci, which asks the hub.

func TestStdioCIAsksTheHubThroughTheRoomAndSplitsTheRepo(t *testing.T) {
	b := &gitBoard{}
	cs := gitSession(t, b)
	out, msg := callTool(t, cs, "atrium_ci", map[string]any{"action": "runs", "repo": "dovholuknf/atrium",
		"branch": "claude/x", "limit": 3})
	if msg != "" {
		t.Fatal(msg)
	}
	if out["kind"] != "github" || len(b.ci) != 1 {
		t.Fatalf("out = %v, asked = %v", out, b.ci)
	}
	a := b.ci[0]
	if a["host"] != "github.com" || a["org"] != "dovholuknf" || a["repo"] != "atrium" || a["action"] != "runs" ||
		a["branch"] != "claude/x" || a["limit"] != float64(3) {
		t.Errorf("asked = %v", a)
	}
	b.ci = nil
	callTool(t, cs, "atrium_ci", map[string]any{"action": "log", "repo": "git.corp.example/o/r", "run_id": 5})
	if a := b.ci[0]; a["host"] != "git.corp.example" || a["run_id"] != float64(5) {
		t.Errorf("asked = %v", a)
	}
}

func TestStdioCIAnswersTheHubsSentenceAndABadRepo(t *testing.T) {
	b := &gitBoard{ciErr: "gh is not logged in on the hub for github.com: run `gh auth login --hostname github.com` on the hub"}
	cs := gitSession(t, b)
	_, msg := callTool(t, cs, "atrium_ci", map[string]any{"action": "runs", "repo": "o/r"})
	if !strings.Contains(msg, "gh auth login --hostname github.com` on the hub") {
		t.Errorf("msg = %q", msg)
	}
	b.ci = nil
	if _, msg = callTool(t, cs, "atrium_ci", map[string]any{"action": "runs", "repo": "atrium"}); !strings.Contains(msg, "<owner>/<repo>") || len(b.ci) != 0 {
		t.Errorf("msg = %q, asked = %v", msg, b.ci)
	}
	b.old = true
	if _, msg = callTool(t, cs, "atrium_ci", map[string]any{"action": "runs", "repo": "o/r"}); !strings.Contains(msg, "predates atrium_ci") {
		t.Errorf("msg = %q", msg)
	}
}
