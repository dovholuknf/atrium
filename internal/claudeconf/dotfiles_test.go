package claudeconf

import (
	"strings"
	"testing"
)

// The setup this was reported against: an operator whose SessionStart and
// SessionEnd each run three commands, one of which is atrium's old script and
// two of which are theirs. Wiring the two stale rows must correct exactly one
// command in each and leave the other four alone.
//
// Written as the real shape rather than a minimal one, because the failure
// being guarded against is a writer that replaces a whole event array, and a
// single-command fixture cannot tell the difference.
func TestWiringStaleRowsLeavesTheDotfilesHooksAlone(t *testing.T) {
	withHome(t, `{
      "hooks": {
        "SessionStart": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "pwsh -NoProfile -File C:/dot/session-bootstrap.ps1 -Phase start"},
            {"type": "command", "command": "pwsh -NoProfile -File C:/Users/x/.claude/hooks/atrium-session-hook.ps1 -Event start"},
            {"type": "command", "command": "pwsh -NoProfile -File C:/dot/set-session-state.ps1 -FromPayloadSource"}
          ]}
        ],
        "SessionEnd": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "pwsh -NoProfile -File C:/dot/set-session-state.ps1 -State ended"},
            {"type": "command", "command": "pwsh -NoProfile -File C:/Users/x/.claude/hooks/atrium-session-hook.ps1 -Event end"},
            {"type": "command", "command": "pwsh -NoProfile -File C:/dot/session-bootstrap.ps1 -Phase end"}
          ]}
        ]
      }
    }`)

	if _, _, err := InstallOnly("C:/tools/atrium.exe",
		[]string{"session-start", "session-end"}); err != nil {
		t.Fatal(err)
	}

	start := commandsFor(t, "SessionStart")
	wantStart := []string{
		"session-bootstrap.ps1 -Phase start",
		"atrium.exe session --event start",
		"set-session-state.ps1 -FromPayloadSource",
	}
	assertCommands(t, "SessionStart", start, wantStart)

	end := commandsFor(t, "SessionEnd")
	wantEnd := []string{
		"set-session-state.ps1 -State ended",
		"atrium.exe session --event end",
		"session-bootstrap.ps1 -Phase end",
	}
	assertCommands(t, "SessionEnd", end, wantEnd)
}

// Adding the Stop hook to that same file must not touch any of it, and must
// not mistake `set-session-state.ps1 -State ended` for something of atrium's.
func TestWiringStopDoesNotDisturbTheSessionHooks(t *testing.T) {
	withHome(t, `{
      "hooks": {
        "SessionEnd": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "pwsh -NoProfile -File C:/dot/set-session-state.ps1 -State ended"},
            {"type": "command", "command": "C:/tools/atrium.exe session --event end"}
          ]}
        ]
      }
    }`)

	if _, _, err := InstallOnly("C:/tools/atrium.exe", []string{"turn-end"}); err != nil {
		t.Fatal(err)
	}

	assertCommands(t, "SessionEnd", commandsFor(t, "SessionEnd"), []string{
		"set-session-state.ps1 -State ended",
		"atrium.exe session --event end",
	})
	assertCommands(t, "Stop", commandsFor(t, "Stop"), []string{
		"atrium.exe turn --event end",
	})
}

func assertCommands(t *testing.T, hook string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s runs %d commands, wanted %d: %v", hook, len(got), len(want), got)
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Fatalf("%s command %d is %q, wanted it to contain %q", hook, i, got[i], want[i])
		}
	}
}
