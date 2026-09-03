package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex keeps its hooks in a separate file in the same shape.
//
// The shape was verified rather than assumed: a hooks.json like the one these
// tests write was put in a scratch CODEX_HOME, and codex printed
// `hook: SessionStart` when it ran, so it read the file and matched the event.

func withCodexHome(t *testing.T, contents string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "hooks.json")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// A fresh machine has no file, and writing one creates it.
func TestCodexInstallCreatesHooksJson(t *testing.T) {
	path := withCodexHome(t, "")

	rep, res, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("installing into an empty codex home changed nothing")
	}
	if rep.Runner != "codex" {
		t.Fatalf("report is for %q, wanted codex", rep.Runner)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no hooks.json was written: %v", err)
	}
	// The shape codex actually reads.
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("what was written is not the shape codex reads: %v", err)
	}
	entry, ok := doc.Hooks["SessionStart"]
	if !ok || len(entry) == 0 || len(entry[0].Hooks) == 0 {
		t.Fatalf("no SessionStart command was written: %s", raw)
	}
	cmd := entry[0].Hooks[0]
	if cmd.Type != "command" {
		t.Fatalf("hook type is %q, wanted command", cmd.Type)
	}
	if !strings.Contains(cmd.Command, "session --event start") {
		t.Fatalf("SessionStart runs %q", cmd.Command)
	}
}

// The Stop hook is optional for codex too, and for the same reason: it is the
// only one whose answer changes what the session does.
func TestCodexInstallAllSkipsStop(t *testing.T) {
	withCodexHome(t, "")

	rep, _, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rep.Hooks {
		if h.Hook == "Stop" && h.Installed {
			t.Fatal("install all wrote a codex Stop hook")
		}
		if !h.Optional && !h.Installed {
			t.Fatalf("install all skipped %s", h.Hook)
		}
	}
}

// Writing codex's file must not touch claude's, and the two must not read each
// other's. They share the format and nothing else.
func TestCodexAndClaudeDoNotShareAFile(t *testing.T) {
	claudePath := withHome(t, "")
	codexPath := withCodexHome(t, "")

	if _, _, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatal("installing codex hooks wrote claude's settings.json")
	}

	// And claude sees nothing installed, because it is a different file.
	rep, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rep.Hooks {
		if h.Installed {
			t.Fatalf("claude reported %s installed from codex's file", h.Hook)
		}
	}
	if _, err := os.Stat(codexPath); err != nil {
		t.Fatalf("codex's own file went missing: %v", err)
	}
}

// The same rules that protect claude's file protect this one: whatever the
// operator put there survives, and atrium's own entry is corrected in place.
func TestCodexInstallKeepsWhatIsAlreadyThere(t *testing.T) {
	withCodexHome(t, `{
      "hooks": {
        "SessionStart": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "echo theirs"},
            {"type": "command", "command": "C:/old/atrium.exe session --event start"}
          ]}
        ]
      },
      "somethingElse": {"kept": true}
    }`)

	if _, _, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe",
		[]string{"session-start"}); err != nil {
		t.Fatal(err)
	}

	path, err := CodexHooksPath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "echo theirs") {
		t.Fatal("a command the operator put there was removed")
	}
	if !strings.Contains(body, "somethingElse") {
		t.Fatal("a key atrium knows nothing about was dropped")
	}
	if !strings.Contains(body, "C:/tools/atrium.exe session --event start") {
		t.Fatal("atrium's own entry was not corrected")
	}
	if strings.Contains(body, "C:/old/atrium.exe") {
		t.Fatal("the stale entry was left behind, so the event reports twice")
	}
}

// CODEX_HOME is how a second codex install is kept apart, and codex resolves
// it the same way.
func TestCodexHomeIsHonoured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	got, err := CodexHooksPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "hooks.json"); got != want {
		t.Fatalf("path is %q, wanted %q", got, want)
	}
}

// Codex will not run a hook it has not been shown, and atrium does not reach
// into that. The report says so instead, because a switch that reports success
// and changes nothing is worse than one that explains the next step.
func TestCodexReportCarriesTheTrustStep(t *testing.T) {
	withCodexHome(t, "")

	rep, err := InspectTarget(Codex, "C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Trust == "" {
		t.Fatal("the codex report does not mention that hooks need to be trusted")
	}
	claude, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if claude.Trust != "" {
		t.Fatal("claude's report claims a trust step it does not have")
	}
}
