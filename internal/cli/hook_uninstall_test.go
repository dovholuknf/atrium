package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

func firstRunHome(t *testing.T, settings string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	bash := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(bash, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", bash)
	t.Setenv(claudeconf.HookExeEnv, "/opt/atrium/atrium.exe")
	path := filepath.Join(home, ".claude", "settings.json")
	if settings != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestFirstRunWiresHooksIntoSettingsThatHaveNone(t *testing.T) {
	path := firstRunHome(t, `{"model":"opus"}`)
	var out bytes.Buffer
	wireHooksOnFirstRun(&out, false)
	if strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), "wired atrium's hooks") ||
		!strings.Contains(out.String(), ".bak") {
		t.Fatalf("want one line naming the backup, got %q", out.String())
	}
	if !claudeconf.HasAtriumHooks(claudeconf.Claude) {
		t.Fatal("the hooks were not written")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"model": "opus"`) {
		t.Fatalf("the user's own setting is gone:\n%s", raw)
	}
}

func TestFirstRunLeavesSettingsWithAtriumHooksAlone(t *testing.T) {
	const had = `{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"/old/atrium hook --event tool-start"}]}]}}`
	path := firstRunHome(t, had)
	var out bytes.Buffer
	wireHooksOnFirstRun(&out, false)
	if raw, _ := os.ReadFile(path); string(raw) != had || out.Len() != 0 {
		t.Fatalf("settings with atrium hooks were touched: %q\n%s", out.String(), raw)
	}
}

func TestFirstRunWithNoHooksWritesNothing(t *testing.T) {
	path := firstRunHome(t, "")
	var out bytes.Buffer
	wireHooksOnFirstRun(&out, true)
	if _, err := os.Stat(path); !os.IsNotExist(err) || out.Len() != 0 {
		t.Fatalf("--no-hooks wrote settings or said something: %q", out.String())
	}
	if newRun().Flags().Lookup("no-hooks") == nil {
		t.Fatal("atrium run has no --no-hooks")
	}
}
