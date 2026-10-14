package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/guard"
)

const replyDeploy = "Run this:\n\n```\npwsh scripts/live/atrium-deploy.ps1\n```\n"

func replyEnv(t *testing.T, guardOn string) {
	t.Helper()
	t.Setenv("ATRIUM_GUARD", guardOn)
	t.Setenv(guardRulesEnv, "")
}

func TestReplyGuardBlocksADeployCommand(t *testing.T) {
	replyEnv(t, "on")
	out := replyGuard(turnInput{LastAssistantMessage: replyDeploy}, guard.OSEnv{})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "deploy command") {
		t.Fatalf("got %q", out)
	}
}

func TestReplyGuardLetsProseThrough(t *testing.T) {
	replyEnv(t, "on")
	in := turnInput{LastAssistantMessage: "I ran scripts/live/atrium-deploy.ps1 myself."}
	if out := replyGuard(in, guard.OSEnv{}); out != keepGoing {
		t.Fatalf("got %q", out)
	}
}

func TestReplyGuardBlocksOncePerTurn(t *testing.T) {
	replyEnv(t, "on")
	in := turnInput{LastAssistantMessage: replyDeploy, StopHookActive: true}
	if out := replyGuard(in, guard.OSEnv{}); out != keepGoing {
		t.Fatalf("blocked inside a blocked turn: %q", out)
	}
}

func TestReplyGuardNoMessageOrGuardOffSaysNothing(t *testing.T) {
	replyEnv(t, "on")
	if out := replyGuard(turnInput{}, guard.OSEnv{}); out != keepGoing {
		t.Fatalf("no message: %q", out)
	}
	replyEnv(t, "off")
	if out := replyGuard(turnInput{LastAssistantMessage: replyDeploy}, guard.OSEnv{}); out != keepGoing {
		t.Fatalf("guard off: %q", out)
	}
}

func TestReplyGuardFailsOpenOnABrokenRuleSet(t *testing.T) {
	replyEnv(t, "on")
	bad := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(bad, []byte(`{"rules": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(guardRulesEnv, bad)
	if out := replyGuard(turnInput{LastAssistantMessage: replyDeploy}, guard.OSEnv{}); out != keepGoing {
		t.Fatalf("a broken rule set blocked a reply: %q", out)
	}
}

// A panic anywhere in the check answers nothing.
type panicEnv struct{ guard.OSEnv }

func (panicEnv) Getenv(string) string { panic("boom") }

func TestReplyGuardFailsOpenOnAPanic(t *testing.T) {
	if out := replyGuard(turnInput{LastAssistantMessage: replyDeploy}, panicEnv{}); out != keepGoing {
		t.Fatalf("got %q", out)
	}
}
